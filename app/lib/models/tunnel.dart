import '../services/wg_config_parser.dart';

/// Split-tunneling mode. Android only; the current engine plugin cannot yet
/// apply it to the VPN (stored and edited in the UI — see TunnelService TODO).
enum SplitTunnelMode { off, allow, deny }

SplitTunnelMode splitModeFromString(String? s) {
  switch (s) {
    case 'allow':
      return SplitTunnelMode.allow;
    case 'deny':
      return SplitTunnelMode.deny;
    default:
      return SplitTunnelMode.off;
  }
}

String splitModeToString(SplitTunnelMode m) => m.name;

/// One stored tunnel: a name plus the full wg-quick config text, with the
/// parse result cached for display.
class Tunnel {
  Tunnel({
    required this.id,
    required this.name,
    required this.config,
    this.splitMode = SplitTunnelMode.off,
    this.splitApps = const [],
  }) : parsed = WgConfigParser.tryParse(config);

  final String id;
  String name;
  final String config; // original wg-quick text, passed to the engine as-is
  final WgConfig? parsed;

  SplitTunnelMode splitMode;
  List<String> splitApps; // Android package names (allow/deny list)

  /// Session start of the current connection; not persisted — cleared on
  /// disconnect and unknown after an app restart.
  DateTime? connectedSince;

  String? get primaryEndpoint => parsed?.primaryEndpoint;

  /// Short server label for list rows: "vpn.example.com" without the port.
  String get serverLabel {
    final endpoint = primaryEndpoint;
    if (endpoint == null || endpoint.isEmpty) return '—';
    final i = endpoint.lastIndexOf(':');
    return i > 0 ? endpoint.substring(0, i) : endpoint;
  }

  String get addressLabel => parsed?.iface.addresses.join(', ') ?? '—';
  String get dnsLabel =>
      parsed == null || parsed!.iface.dns.isEmpty
          ? '—'
          : parsed!.iface.dns.join(', ');
  String get allowedIpsLabel => parsed?.allowedIpsJoined ?? '—';
  String get serverKeyLabel => parsed?.primaryPeer?.publicKey ?? '—';

  Tunnel copyWith({String? name, SplitTunnelMode? splitMode, List<String>? splitApps}) =>
      Tunnel(
        id: id,
        name: name ?? this.name,
        config: config,
        splitMode: splitMode ?? this.splitMode,
        splitApps: splitApps ?? this.splitApps,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'config': config,
        'splitMode': splitModeToString(splitMode),
        'splitApps': splitApps,
      };

  static Tunnel? fromJson(Map<String, dynamic> json) {
    final id = json['id'];
    final name = json['name'];
    final config = json['config'];
    if (id is! String || name is! String || config is! String) return null;
    if (WgConfigParser.tryParse(config) == null) return null; // corrupt row
    return Tunnel(
      id: id,
      name: name,
      config: config,
      splitMode: splitModeFromString(json['splitMode'] as String?),
      splitApps: (json['splitApps'] as List?)?.cast<String>() ?? const [],
    );
  }

  /// Fresh id for a new tunnel (no uuid dependency needed).
  static String newId() =>
      DateTime.now().microsecondsSinceEpoch.toRadixString(36);
}
