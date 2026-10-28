import 'dart:convert';
import 'dart:io';

import '../models/game.dart';

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
