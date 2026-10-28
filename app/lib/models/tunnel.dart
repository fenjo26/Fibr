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
    this.updatedAt,
  }) : parsed = WgConfigParser.tryParse(config);

  final String id;
  String name;
  final String config; // original wg-quick text, passed to the engine as-is
  final WgConfig? parsed;

  SplitTunnelMode splitMode;
  List<String> splitApps; // Android package names (allow/deny list)

  /// When the config text was last replaced in place (server swap re-import).
  /// Null for tunnels stored before this field existed and for tunnels whose
  /// config was never replaced; renames do not bump it.
  final DateTime? updatedAt;

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

  /// Full "host:port" endpoint — what a server swap changes, shown on list
  /// rows so the target server is visible at a glance.
  String get endpointLabel => primaryEndpoint ?? '—';

  String get addressLabel => parsed?.iface.addresses.join(', ') ?? '—';
  String get dnsLabel =>
      parsed == null || parsed!.iface.dns.isEmpty
          ? '—'
          : parsed!.iface.dns.join(', ');
  String get allowedIpsLabel => parsed?.allowedIpsJoined ?? '—';
  String get serverKeyLabel => parsed?.primaryPeer?.publicKey ?? '—';

  /// Gateway of the tunnel's IPv4 subnet (network address + 1) — where the
  /// Fibr filtering resolver listens (docs/dns-games.md §1).
  String? get subnetGateway => parsed?.subnetGatewayV4;

  /// Whether the config's DNS points at the tunnel gateway (= Fibr's
  /// filtering resolver) rather than a public one.
  bool get dnsPointsToTunnel => parsed?.dnsPointsToTunnel ?? false;

  Tunnel copyWith({String? name, SplitTunnelMode? splitMode, List<String>? splitApps}) =>
      Tunnel(
        id: id,
        name: name ?? this.name,
        config: config,
        splitMode: splitMode ?? this.splitMode,
        splitApps: splitApps ?? this.splitApps,
        updatedAt: updatedAt,
      );

  /// Copy with a new wg-quick config text (server swap re-import): the
  /// constructor re-parses the config, so the cached parse result and all
  /// derived labels (endpoint, server key, …) refresh, and [updatedAt] is
  /// stamped with now. Name and split-tunneling settings are kept.
  Tunnel withConfig(String newConfig) => Tunnel(
        id: id,
        name: name,
        config: newConfig,
        splitMode: splitMode,
        splitApps: splitApps,
        updatedAt: DateTime.now(),
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'config': config,
        'splitMode': splitModeToString(splitMode),
        'splitApps': splitApps,
        if (updatedAt != null) 'updatedAt': updatedAt!.toIso8601String(),
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
      updatedAt: DateTime.tryParse(json['updatedAt'] as String? ?? ''),
    );
  }

  /// Fresh id for a new tunnel (no uuid dependency needed).
  static String newId() =>
      DateTime.now().microsecondsSinceEpoch.toRadixString(36);
}
