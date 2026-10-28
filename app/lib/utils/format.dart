/// Formatting helpers, ported 1:1 from design/app.js (fmtBytes) plus a
/// session clock for the active-tunnel screen.
library;

/// 1234 -> "1.2 KB", 1048576 -> "1.0 MB", >=100 units -> no decimals.
String formatBytes(num? b) {
  if (b == null || b.isNaN) return '—';
  if (b < 1024) return '$b B';
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  var u = -1;
  num n = b;
  do {
    n /= 1024;
    u++;
  } while (n >= 1024 && u < units.length - 1);
  final v = n >= 100 ? n.toStringAsFixed(0) : n.toStringAsFixed(1);
  return '$v ${units[u]}';
}

/// 3725s -> "1:02:05" (hours are not padded, may exceed 24).
String formatDuration(Duration d) {
  String two(int v) => v.toString().padLeft(2, '0');
  return '${d.inHours}:${two(d.inMinutes % 60)}:${two(d.inSeconds % 60)}';
}
