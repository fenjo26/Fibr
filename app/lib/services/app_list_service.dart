import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// One entry of the device's app list (Android package / display label).
class InstalledApp {
  const InstalledApp({
    required this.packageName,
    required this.label,
    required this.isSystem,
  });

  final String packageName;
  final String label;
  final bool isSystem;
}

/// Installed-app list for the split-tunneling screen, via the `fibr/apps`
/// MethodChannel implemented in android/.../MainActivity.kt.
///
/// Android only (list requires QUERY_ALL_PACKAGES, see README); on iOS and
/// desktop it returns an empty list and the UI shows its platform notice.
class AppListService {
  static const _channel = MethodChannel('fibr/apps');

  Future<List<InstalledApp>> list() async {
    if (!kIsWeb && defaultTargetPlatform != TargetPlatform.android) {
      return const [];
    }
    try {
      final raw = await _channel.invokeMethod('list');
      if (raw is! List) return const [];
      return raw
          .whereType<Map>()
          .map((m) => InstalledApp(
                packageName: m['package']?.toString() ?? '',
                label: m['label']?.toString() ?? '',
                isSystem: m['system'] == true,
              ))
          .where((a) => a.packageName.isNotEmpty)
          .toList()
        ..sort((a, b) => a.label.toLowerCase().compareTo(b.label.toLowerCase()));
    } on PlatformException {
      return const [];
    } on MissingPluginException {
      return const [];
    }
  }
}
