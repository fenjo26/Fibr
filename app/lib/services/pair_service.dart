import 'dart:async';
import 'dart:convert';
import 'dart:io';

/// TV pairing ("connect the TV from the phone", server internal/api/pair.go):
/// the TV asks for a short code, shows it with a QR, and polls until the
/// phone attaches a device in the Mini App. dart:io only, like JoinService.
class PairSession {
  const PairSession({
    required this.base,
    required this.code,
    required this.secret,
    required this.link,
    required this.expiresAt,
  });

  final Uri base; // https://<server>
  final String code;
  final String secret;
  final String link; // what the QR encodes (t.me/<bot>?start=tv_CODE)
  final DateTime expiresAt;

  /// The QR image rendered by the server (the client has no QR library).
  Uri get qrUrl => base.replace(path: '/api/pair/$code/qr.png');
}

/// The pairing expired or is unknown — show a new QR.
class PairExpired implements Exception {
  const PairExpired();
}

class PairService {
  static const _timeout = Duration(seconds: 10);

  /// "vpn.example.com" / "https://vpn.example.com:8443" -> https base Uri.
  static Uri? baseFor(String server) {
    var s = server.trim();
    if (s.isEmpty) return null;
    if (!s.contains('://')) s = 'https://$s';
    final u = Uri.tryParse(s);
    if (u == null || u.host.isEmpty) return null;
    return Uri(scheme: u.scheme, host: u.host, port: u.hasPort ? u.port : null);
  }

  static Future<PairSession> create(String server) async {
    final base = baseFor(server);
    if (base == null) throw const FormatException('bad server');
    final json = await _request('POST', base.replace(path: '/api/pair'));
    final code = json['code'];
    final secret = json['secret'];
    if (code is! String || secret is! String) {
      throw const FormatException('bad pairing response');
    }
    return PairSession(
      base: base,
      code: code,
      secret: secret,
      link: (json['link'] as String?) ?? '',
      expiresAt: DateTime.tryParse((json['expires_at'] as String?) ?? '') ??
          DateTime.now().add(const Duration(minutes: 10)),
    );
  }

  /// The config once the phone attached a device, null while waiting.
  /// Throws [PairExpired] when the server no longer knows the code.
  static Future<String?> poll(PairSession s) async {
    final uri = s.base.replace(
      path: '/api/pair/${s.code}',
      queryParameters: {'secret': s.secret},
    );
    final json = await _request('GET', uri);
    if (json['status'] == 'ready') {
      final config = json['config'];
      if (config is String && config.isNotEmpty) return config;
    }
    return null;
  }

  static Future<Map<String, dynamic>> _request(String method, Uri uri) async {
    final client = HttpClient()..connectionTimeout = _timeout;
    try {
      final req = await client.openUrl(method, uri).timeout(_timeout);
      if (method == 'POST') {
        req.headers.contentType = ContentType.json;
        req.write('{}');
      }
      final res = await req.close().timeout(_timeout);
      final body = await res.transform(utf8.decoder).join().timeout(_timeout);
      if (res.statusCode == 404) throw const PairExpired();
      if (res.statusCode < 200 || res.statusCode >= 300) {
        throw HttpException('HTTP ${res.statusCode}');
      }
      final dynamic json = const JsonDecoder().convert(body);
      if (json is Map<String, dynamic>) return json;
      throw const FormatException('not an object');
    } finally {
      client.close();
    }
  }
}
