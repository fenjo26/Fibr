import 'package:flutter/services.dart';

/// The "file" connection path: the system document picker, wired in
/// MainActivity.kt ("fibr/files" channel) — no pub plugin needed.
///
/// Returns the picked file's text, or null when the user cancelled or the
/// platform has no picker wiring (iOS / desktop for now).
class FileImportService {
  static const _channel = MethodChannel('fibr/files');

  static Future<String?> pickConfig() async {
    try {
      return await _channel.invokeMethod<String>('pickConfig');
    } on PlatformException {
      return null;
    } on MissingPluginException {
      return null;
    }
  }
}
