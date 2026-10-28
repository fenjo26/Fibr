import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';

/// fibr://import/<base64 wg-quick config> deep links.
///
/// The payload is standard or URL-safe base64 of the whole wg-quick config
/// (URL-safe alphabet and stripped padding recommended); percent-encoding is
/// decoded first. Platform wiring: Android — MainActivity.kt; iOS —
/// AppDelegate.swift snippet in README (flutter create overwrites nothing,
/// so paste it manually).
class DeepLinkService {
  static const _method = MethodChannel('fibr/deeplink');
  static const _events = EventChannel('fibr/deeplink/events');

  /// Link that launched the app, or null. MissingPluginException (platform
  /// wiring not applied yet) yields null — the app keeps working.
  static Future<String?> initialLink() async {
    try {
      return await _method.invokeMethod<String>('getInitialLink');
    } on PlatformException {
      return null;
    } on MissingPluginException {
      return null;
    }
  }

  /// Links arriving while the app runs.
  static Stream<String> links() {
    return _events
        .receiveBroadcastStream()
        .map((event) => event.toString())
        .handleError((_) {});
  }

  /// fibr://import/<base64> -> wg-quick config text, or null if the url is
  /// not a Fibr import link / the payload does not decode.
  static String? decodeImportLink(String url) {
    final Uri uri;
    try {
      uri = Uri.parse(url);
    } on FormatException {
      return null;
    }
    if (uri.scheme != 'fibr' || uri.host != 'import') return null;

    var payload = uri.path.replaceFirst('/', '');
    if (payload.isEmpty) return null;
    try {
      payload = Uri.decodeComponent(payload);
    } on FormatException {
      // keep raw — the sender may not have percent-encoded
    }

    // Accept both alphabets; restore stripped padding.
    payload = payload.replaceAll('-', '+').replaceAll('_', '/');
    final pad = payload.length % 4;
    if (pad > 0) payload += '=' * (4 - pad);
    try {
      return utf8.decode(base64.decode(payload));
    } on FormatException {
      return null;
    } on ArgumentError {
      return null;
    }
  }
}
