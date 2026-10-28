import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../models/game.dart';
import '../models/tunnel.dart';
import 'tunnel_service.dart';

/// Status of the Fibr DNS resolver behind the tunnel gateway — the answer
/// of `GET http://<gateway>:8080/api/dns/status` (docs/dns-games.md §3).
class DnsStatus {
  const DnsStatus({required this.enabled, required this.defaultProfile});

  final bool enabled;
  final String defaultProfile; // off | adblock | family

  factory DnsStatus.fromJson(Map<String, dynamic> json) => DnsStatus(
        enabled: json['enabled'] == true,
        defaultProfile: json['default_profile']?.toString() ?? 'off',
      );
}

/// Fail-soft client for the Fibr server's public endpoints (the same ones
/// the panel exposes, docs/dns-games.md §3), reached through the tunnel
/// gateway. Every method returns null/empty on ANY error — the UI falls
/// back to config-derived verdicts and the offline game snapshot.
///
/// dart:io HttpClient on purpose — no `package:http` dependency, same
/// approach as join_service.dart.
class ServerApiService {
  static const _timeout = Duration(seconds: 3);
  static const _port = 8080;

  /// DNS-resolver status; null when unreachable/broken (the caller then
  /// judges by the config alone).
  static Future<DnsStatus?> fetchDnsStatus(String gateway) async {
    final json = await _getJson('${_base(gateway)}/api/dns/status');
    if (json is! Map<String, dynamic>) return null;
    return DnsStatus.fromJson(json);
  }

  /// Game catalog; empty on any failure (use the offline snapshot then).
  static Future<List<GameInfo>> fetchGames(String gateway) async {
    final json = await _getJson('${_base(gateway)}/api/games');
    if (json is! List) return const [];
    return json
        .whereType<Map>()
        .map((m) => GameInfo.fromJson(Map<String, dynamic>.from(m)))
        .where((g) => g.id.isNotEmpty)
        .toList();
  }

  /// Per-game subnets: `{game_id: [cidr...]}`. Empty map on any failure —
  /// callers merge the offline snapshot routes instead.
  static Future<Map<String, List<String>>> fetchGameRoutes(
    String gateway,
    List<String> ids,
  ) async {
    if (ids.isEmpty) return const {};
    final json = await _getJson(
        '${_base(gateway)}/api/games/routes?ids=${ids.join(',')}');
    if (json is! Map<String, dynamic>) return const {};
    final routes = <String, List<String>>{};
    for (final e in json.entries) {
      if (e.value is! List) continue;
      final cidrs = (e.value as List)
          .whereType<String>()
          .map((s) => s.trim())
          .where((s) => s.isNotEmpty)
          .toList();
      if (cidrs.isNotEmpty) routes[e.key] = cidrs;
    }
    return routes;
  }

  /// Base URL on the gateway; an IPv6 literal needs brackets in a URL.
  static String _base(String gateway) {
    final host = gateway.contains(':') ? '[$gateway]' : gateway;
    return 'http://$host:$_port';
  }

  /// GET → decoded JSON (a List or a Map), or null on any error. Three
  /// timeout stages (connect, response, body) + connectionTimeout, and the
  /// client is always force-closed — one-shot calls, no keep-alive leaks.
  static Future<Object?> _getJson(String url) async {
    final client = HttpClient()..connectionTimeout = _timeout;
    try {
      final uri = Uri.parse(url);
      final req = await client.getUrl(uri).timeout(_timeout);
      final res = await req.close().timeout(_timeout);
      final body = await res.transform(utf8.decoder).join().timeout(_timeout);
      if (res.statusCode != 200 || body.isEmpty) return null;
      return const JsonDecoder().convert(body);
    } on Exception {
      return null; // fail-soft: timeouts, sockets, bad JSON — all the same
    } finally {
      client.close(force: true);
    }
  }
}

/* ---------------------------------------------------------------------- */
/* Server login ("этап 2.5"): account models, API client, session store.  */
/* ---------------------------------------------------------------------- */

/// One device row of `GET /api/me` (the server's deviceOut shape).
class ServerDevice {
  const ServerDevice({
    required this.id,
    required this.name,
    required this.protocol,
    required this.ipv4,
    required this.enabled,
    required this.online,
  });

  final int id;
  final String name;
  final String protocol; // "wg" | "xray"
  final String ipv4;
  final bool enabled;
  final bool online;

  bool get isXray => protocol == 'xray';

  factory ServerDevice.fromJson(Map<String, dynamic> json) => ServerDevice(
        id: (json['id'] as num).toInt(),
        name: json['name']?.toString() ?? '',
        protocol: json['protocol']?.toString() == 'xray' ? 'xray' : 'wg',
        ipv4: json['ipv4']?.toString() ?? '',
        enabled: json['enabled'] != false,
        online: json['online'] == true,
      );
}

/// The account half of `GET /api/me` that the simple mode needs.
class MeProfile {
  const MeProfile({
    required this.username,
    required this.devices,
    required this.maxDevices,
    required this.xrayEnabled,
    required this.hasAccessKey,
    required this.activeNode,
  });

  final String username;
  final List<ServerDevice> devices;
  final int maxDevices;
  final bool xrayEnabled;
  final bool hasAccessKey;
  final String activeNode;

  factory MeProfile.fromJson(Map<String, dynamic> json) {
    final user = json['user'];
    final devices = (json['devices'] as List? ?? [])
        .whereType<Map>()
        .map((m) => ServerDevice.fromJson(Map<String, dynamic>.from(m)))
        .toList(growable: false);
    return MeProfile(
      username: user is Map<String, dynamic>
          ? user['username']?.toString() ?? ''
          : '',
      devices: devices,
      maxDevices: (json['max_devices'] as num?)?.toInt() ?? 0,
      xrayEnabled: json['xray_enabled'] == true,
      hasAccessKey: json['has_access_key'] == true,
      activeNode: json['active_node']?.toString() ?? '',
    );
  }
}

/// Why a server-login call failed — maps onto localized messages in the UI.
enum ServerLoginError {
  badDomain, // the input does not look like a host
  network, // server unreachable
  invalidKey, // 401: no such key (or rotated/revoked)
  disabled, // 403 account disabled
  expired, // 403 subscription expired
  deviceLimit, // 409 device limit reached
  server, // any other unexpected answer
}

/// Client of the authenticated Fibr server API (`/api/auth/key`, `/api/me`).
/// dart:io HttpClient on purpose — same no-extra-dependency approach as
/// join_service.dart; every call opens its own client.
class ServerAccountApi {
  const ServerAccountApi({required this.domain, required this.token});

  /// Normalized `host[:port]` (see [normalizeDomain]).
  final String domain;

  /// 30-day session token issued by POST /api/auth/key.
  final String token;

  /// [domain] is already normalized, so "https://" + it is a valid URI.
  static Uri _baseFor(String domain) => Uri.parse('https://$domain');

  Uri _uri(String path) => _baseFor(domain).replace(path: path);

  /// Exchanges an app access key for a session: `POST /api/auth/key
  /// {key}` → `{token, user}` (the same response the Telegram sign-in
  /// issues). Returns the session token; throws [ServerApiException].
  static Future<String> loginWithKey(String domain, String key,
      {Duration timeout = const Duration(seconds: 10)}) async {
    final body = await _request(
      'POST',
      _baseFor(domain).replace(path: '/api/auth/key'),
      timeout,
      body: {'key': key},
    );
    final token = body['token'];
    if (token is String && token.isNotEmpty) return token;
    throw const ServerApiException(ServerLoginError.server);
  }

  /// `GET /api/me` — profile + devices; throws on transport/auth errors.
  Future<MeProfile> fetchMe(
      {Duration timeout = const Duration(seconds: 10)}) async {
    final body = await _request('GET', _uri('/api/me'), timeout, token: token);
    return MeProfile.fromJson(body);
  }

  /// `POST /api/me/devices {name, protocol}` → the created device.
  Future<ServerDevice> createDevice(String name, String protocol,
      {Duration timeout = const Duration(seconds: 10)}) async {
    final body = await _request('POST', _uri('/api/me/devices'), timeout,
        token: token, body: {'name': name, 'protocol': protocol});
    return ServerDevice.fromJson(body);
  }

  /// `GET /api/me/devices/{id}/config` — the rendered credential: a
  /// wg-quick config for "wg" devices, a vless:// link for "xray" ones.
  /// Never shown to the user — it goes straight into the engine (or, for
  /// xray, is fetched only to stamp delivery server-side).
  Future<String> fetchDeviceConfig(int deviceId,
      {Duration timeout = const Duration(seconds: 10)}) async {
    final text = await _requestText(
        'GET', _uri('/api/me/devices/$deviceId/config'), timeout, token: token);
    if (text.isEmpty) throw const ServerApiException(ServerLoginError.server);
    return text;
  }

  /// `DELETE /api/me/devices/{id}` (204 — the body is empty).
  Future<void> deleteDevice(int deviceId,
      {Duration timeout = const Duration(seconds: 10)}) async {
    await _requestText('DELETE', _uri('/api/me/devices/$deviceId'), timeout,
        token: token);
  }

  /// Shared JSON request plumbing: Bearer auth, JSON body, status →
  /// [ServerApiException] mapping, network errors folded into one kind.
  static Future<Map<String, dynamic>> _request(
    String method,
    Uri uri,
    Duration timeout, {
    String? token,
    Map<String, Object>? body,
  }) async {
    final raw = await _requestText(method, uri, timeout,
        token: token, body: body);
    try {
      final decoded = const JsonDecoder().convert(raw);
      if (decoded is Map<String, dynamic>) return decoded;
    } on FormatException {
      // fall through to the generic server error
    }
    throw const ServerApiException(ServerLoginError.server);
  }

  static Future<String> _requestText(
    String method,
    Uri uri,
    Duration timeout, {
    String? token,
    Map<String, Object>? body,
  }) async {
    final client = HttpClient()..connectionTimeout = timeout;
    try {
      final req =
          await _openRequest(client, method, uri).timeout(timeout);
      if (token != null) {
        req.headers.set(HttpHeaders.authorizationHeader, 'Bearer $token');
      }
      if (body != null) {
        req.headers.contentType = ContentType.json;
        req.write(jsonEncode(body));
      }
      final res = await req.close().timeout(timeout);
      final text =
          await res.transform(utf8.decoder).join().timeout(timeout);
      switch (res.statusCode) {
        case 200:
        case 201:
        case 204:
          return text;
        case 400:
          throw const ServerApiException(ServerLoginError.server);
        case 401:
          throw const ServerApiException(ServerLoginError.invalidKey);
        case 403:
          if (text.contains('subscription expired')) {
            throw const ServerApiException(ServerLoginError.expired);
          }
          throw const ServerApiException(ServerLoginError.disabled);
        case 409:
          throw const ServerApiException(ServerLoginError.deviceLimit);
        default:
          throw const ServerApiException(ServerLoginError.server);
      }
    } on ServerApiException {
      rethrow;
    } on TimeoutException {
      throw const ServerApiException(ServerLoginError.network);
    } on SocketException {
      throw const ServerApiException(ServerLoginError.network);
    } on HttpException {
      throw const ServerApiException(ServerLoginError.network);
    } finally {
      client.close(force: true);
    }
  }

  static Future<HttpClientRequest> _openRequest(
      HttpClient client, String method, Uri uri) {
    switch (method) {
      case 'POST':
        return client.postUrl(uri);
      case 'DELETE':
        return client.deleteUrl(uri);
      case 'PATCH':
        return client.patchUrl(uri);
      default:
        return client.getUrl(uri);
    }
  }
}

class ServerApiException implements Exception {
  const ServerApiException(this.error);
  final ServerLoginError error;
}

/// Normalizes the user-typed server address into `host[:port]`: accepts
/// "vpnoleg.de", "https://vpnoleg.de", "https://vpnoleg.de:8443/whatever".
/// Returns null when no plausible host can be recovered (the host must be
/// letters/digits/dots/dashes/underscores — good enough for a domain or an
/// IPv4; IPv6 literals are not supported for server login).
String? normalizeDomain(String input) {
  var s = input.trim();
  if (s.isEmpty) return null;
  if (!s.contains('://')) s = 'https://$s';
  final uri = Uri.tryParse(s);
  if (uri == null || uri.host.isEmpty) return null;
  final host = uri.host.toLowerCase();
  if (!RegExp(r'^[a-z0-9._-]+$').hasMatch(host)) return null;
  final port = uri.hasPort && uri.port != 443 ? ':${uri.port}' : '';
  return '$host$port';
}

/// Stable tunnel id for a server device, so a re-applied config updates the
/// same tunnel instead of piling up duplicates ("srv-<device id>").
String tunnelIdForDevice(int deviceId) => 'srv-$deviceId';

/// Fetches the device's config and lands it in the [tunnels] controller
/// WITHOUT showing it to the user. wg: creates or refreshes the hidden
/// "srv-<id>" tunnel (returns true). xray: the credential is a vless://
/// link this client cannot run — returns false, the caller shows the honest
/// "open in v2rayNG/Hiddify" plate.
Future<bool> applyDeviceToEngine({
  required ServerAccountApi api,
  required TunnelController tunnels,
  required ServerDevice device,
}) async {
  final config = await api.fetchDeviceConfig(device.id);
  if (device.isXray || !config.contains('[Interface]')) return false;
  final tunnelId = tunnelIdForDevice(device.id);
  final existing = tunnels.tunnelById(tunnelId);
  if (existing == null) {
    await tunnels.addTunnel(Tunnel(id: tunnelId, name: device.name, config: config));
  } else if (existing.config != config) {
    await tunnels.updateTunnelConfig(tunnelId, config);
  }
  return true;
}

/// Owns the persisted server session: `domain + token` (the 30-day token
/// from POST /api/auth/key) plus the selected "active device", all in
/// shared_preferences under `fibr.account.*`. NOT keystore-grade storage —
/// a future upgrade is flutter_secure_storage/keystore; the token is
/// revocable (the key rotates from the Mini App), which is the accepted
/// trade-off for now.
class ServerAccountController extends ChangeNotifier {
  String? _domain;
  String? _token;
  String? _activeDeviceId;
  MeProfile? _me;

  String? get domain => _domain;
  String? get activeDeviceId => _activeDeviceId;
  MeProfile? get me => _me;
  bool get isSignedIn => _domain != null && _token != null;

  /// The API client for the current session, or null when signed out.
  ServerAccountApi? get api =>
      isSignedIn ? ServerAccountApi(domain: _domain!, token: _token!) : null;

  /// The active device row from the last /api/me payload, or null.
  ServerDevice? get activeDevice {
    final id = _activeDeviceId;
    if (id == null) return null;
    for (final d in _me?.devices ?? const <ServerDevice>[]) {
      if (d.id.toString() == id) return d;
    }
    return null;
  }

  Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    _domain = prefs.getString(_domainKey);
    _token = prefs.getString(_tokenKey);
    _activeDeviceId = prefs.getString(_activeDeviceKey);
    if (_domain == null || _token == null) {
      _domain = null;
      _token = null;
    }
    notifyListeners();
  }

  /// Exchanges the key for a session and persists domain+token. Returns
  /// null on success or the failure reason for the UI to localize.
  Future<ServerLoginError?> signIn(String domainInput, String key) async {
    final domain = normalizeDomain(domainInput);
    if (domain == null) return ServerLoginError.badDomain;
    final k = key.trim();
    if (k.isEmpty) return ServerLoginError.invalidKey;
    try {
      final token = await ServerAccountApi.loginWithKey(domain, k);
      _domain = domain;
      _token = token;
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_domainKey, domain);
      await prefs.setString(_tokenKey, token);
      notifyListeners();
      await refreshMe(); // best effort; failures surface on the home screen
      return null;
    } on ServerApiException catch (e) {
      return e.error;
    }
  }

  /// Clears the session (the server session lives on — nothing to revoke
  /// client-side; rotating the key in the Mini App kills it).
  Future<void> signOut() async {
    _domain = null;
    _token = null;
    _activeDeviceId = null;
    _me = null;
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_domainKey);
    await prefs.remove(_tokenKey);
    await prefs.remove(_activeDeviceKey);
    notifyListeners();
  }

  /// Refreshes the profile; on 401 (key rotated/revoked, session aged out)
  /// signs the account out so the app returns to the login screen.
  /// Returns false on failure.
  Future<bool> refreshMe() async {
    final client = api;
    if (client == null) return false;
    try {
      _me = await client.fetchMe();
      notifyListeners();
      return true;
    } on ServerApiException catch (e) {
      if (e.error == ServerLoginError.invalidKey) {
        await signOut(); // AppRoot rebuilds into LoginScreen
      }
      return false;
    }
  }

  Future<void> setActiveDevice(String? deviceId) async {
    if (_activeDeviceId == deviceId) return;
    _activeDeviceId = deviceId;
    final prefs = await SharedPreferences.getInstance();
    if (deviceId == null) {
      await prefs.remove(_activeDeviceKey);
    } else {
      await prefs.setString(_activeDeviceKey, deviceId);
    }
    notifyListeners();
  }

  /// Brings the active wg device up ("connect on launch"): fetches its
  /// config into the tunnel list and connects. Errors are swallowed into
  /// the caller's UI (lastError surfaces on the home screen button press).
  Future<bool> connectActiveDevice(TunnelController tunnels) async {
    final client = api;
    if (client == null) return false;
    if (_me == null) {
      // Called right after boot (main.dart), before any screen loaded the
      // profile — pull it first so the active device resolves.
      final ok = await refreshMe();
      if (!ok) return false;
    }
    final device = activeDevice;
    if (device == null || device.isXray) return false;
    try {
      if (!await applyDeviceToEngine(api: client, tunnels: tunnels, device: device)) {
        return false;
      }
      final tunnel = tunnels.tunnelById(tunnelIdForDevice(device.id));
      if (tunnel == null) return false;
      return tunnels.connect(tunnel);
    } on ServerApiException {
      return false;
    }
  }

  static const _domainKey = 'fibr.account.domain';
  static const _tokenKey = 'fibr.account.token';
  static const _activeDeviceKey = 'fibr.account.activeDevice';
}
