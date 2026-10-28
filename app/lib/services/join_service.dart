import 'dart:async';
import 'dart:convert';
import 'dart:io';

/// Quick-connect: exchange a one-time join code for a device config via the
/// public `POST /api/join/{code}` endpoint (see docs/join.md on the server).
///
/// No `package:http` dependency on purpose — dart:io HttpClient keeps the
/// pubspec untouched and works on all client platforms.
class JoinService {
  /// Input the user may paste into the "By code" tab:
  ///  - a full join link: https://vpn.example.com/join/Ab3xK9
  ///  - a bare code (8 chars) — requires [server] to carry the host.
  ///
  /// Returns the raw credential text (a wg-quick config; a vless:// link for
  /// xray devices — the caller decides whether it can import it).
  static Future<String> exchange(
    String input, {
    String? server,
    Duration timeout = const Duration(seconds: 10),
  }) async {
    final target = _resolve(input, server);
    if (target == null) throw const JoinException(JoinError.badRequest);
    return _post(target, timeout);
  }

  /// Extracts an exchangeable join URL from a scanned QR / deep link, or
  /// null when the raw text is neither:
  ///  - fibr://join/<base64url(https://host/join/CODE)> — from the landing
  ///    page button (server-side rendering, deep_link_service decodes the
  ///    same payload);
  ///  - https://host/join/CODE — the QR shown by the Mini App next to the
  ///    code.
  static String? extractJoinUrl(String raw) {
    final s = raw.trim();
    if (s.startsWith('fibr://join/')) {
      return decodeJoinPayload(s.substring('fibr://join/'.length));
    }
    final uri = Uri.tryParse(s);
    if (uri != null && (uri.isScheme('https') || uri.isScheme('http'))) {
      final parts = uri.pathSegments;
      if (parts.length == 2 && parts[0] == 'join' && _isCode(parts[1])) {
        return s;
      }
    }
    return null;
  }

  /// fibr://join payload: URL-safe base64 of the full join URL.
  static String? decodeJoinPayload(String payload) {
    var p = payload.replaceAll('-', '+').replaceAll('_', '/');
    final pad = p.length % 4;
    if (pad > 0) p += '=' * (4 - pad);
    try {
      final decoded = utf8.decode(base64.decode(p));
      return extractJoinUrl(decoded) ?? (Uri.tryParse(decoded) != null ? decoded : null);
    } on FormatException {
      return null;
    } on ArgumentError {
      return null;
    }
  }

  /// Builds the POST endpoint from user input. Bare codes need [server]
  /// (host, with or without scheme/port); a full link ignores [server].
  static Uri? _resolve(String input, String? server) {
    final s = input.trim();
    var uri = Uri.tryParse(s);
    if (uri != null && (uri.isScheme('https') || uri.isScheme('http'))) {
      final parts = uri.pathSegments;
      if (parts.length == 2 && parts[0] == 'join' && _isCode(parts[1])) {
        return uri.replace(path: '/api/join/${parts[1]}');
      }
      return null; // some other URL — not ours to touch
    }
    if (_isCode(s)) {
      final host = (server ?? '').trim();
      if (host.isEmpty) return null;
      var base = host;
      if (!base.contains('://')) base = 'https://$base';
      final b = Uri.tryParse(base);
      if (b == null || b.host.isEmpty) return null;
      return Uri(scheme: b.scheme, host: b.host, port: b.hasPort ? b.port : 0,
          path: '/api/join/$s');
    }
    return null;
  }

  static bool _isCode(String s) =>
      s.length >= 6 && s.length <= 12 && RegExp(r'^[0-9A-Za-z]+$').hasMatch(s);

  static Future<String> _post(Uri uri, Duration timeout) async {
    final client = HttpClient()..connectionTimeout = timeout;
    try {
      final req = await client.postUrl(uri).timeout(timeout);
      req.headers.contentType = ContentType.json;
      req.write('{}');
      final res = await req.close().timeout(timeout);
      final body = await res.transform(utf8.decoder).join().timeout(timeout);
      if (res.statusCode == 200) {
        final dynamic json = const JsonDecoder().convert(body);
        final config = json is Map<String, dynamic> ? json['config'] : null;
        if (config is String && config.isNotEmpty) return config;
        throw const JoinException(JoinError.badResponse);
      }
      switch (res.statusCode) {
        case 404:
          throw const JoinException(JoinError.notFound);
        case 410:
          throw const JoinException(JoinError.gone);
        case 403:
          throw const JoinException(JoinError.forbidden);
        default:
          throw const JoinException(JoinError.badResponse);
      }
    } on JoinException {
      rethrow;
    } on TimeoutException {
      throw const JoinException(JoinError.network);
    } on SocketException {
      throw const JoinException(JoinError.network);
    } on HttpException {
      throw const JoinException(JoinError.network);
    } on FormatException {
      throw const JoinException(JoinError.badResponse);
    } finally {
      client.close();
    }
  }
}

enum JoinError {
  badRequest, // input is neither a join link nor a code+server pair
  network, // server unreachable
  notFound, // 404: no such code
  gone, // 410: already used or expired
  forbidden, // 403: expired subscription / disabled
  badResponse, // 2xx without a config, or unexpected status
}

class JoinException implements Exception {
  const JoinException(this.error);
  final JoinError error;
}
