import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:wireguard_flutter/wireguard_flutter.dart';

import '../models/tunnel.dart';
import 'wg_config_parser.dart';

enum TunnelStatus { disconnected, connecting, connected, disconnecting, failure }

class TunnelStats {
  const TunnelStats({required this.rx, required this.tx});
  final int rx; // bytes received
  final int tx; // bytes sent
}

/// Engine abstraction so the WireGuard plugin can be swapped (e.g. for the
/// AmneziaWG fork planned in stage 3, or a fake in tests) without touching
/// UI code.
abstract class TunnelEngine {
  /// Prepare the tunnel interface; safe to call more than once.
  Future<void> initialize();

  /// Bring the tunnel up from a wg-quick config string.
  Future<void> connect({required String serverAddress, required String wgQuickConfig});

  Future<void> disconnect();

  Future<TunnelStatus> currentStatus();

  /// Emits on every stage change of the underlying VPN.
  Stream<TunnelStatus> get onStatusChanged;

  /// Traffic counters of the active session, or null when the engine does
  /// not expose them.
  Future<TunnelStats?> readStats();
}

/// wireguard_flutter-backed engine (Caqil/wireguard_flutter, MIT).
///
/// The plugin takes the whole wg-quick config string but connects through
/// its first peer — multi-peer configs are parsed and stored fully, only the
/// primary endpoint is used for the tunnel (see README, "Ограничения").
class WireGuardFlutterEngine implements TunnelEngine {
  /// iOS only: bundle id of the packet tunnel provider extension created in
  /// Xcode (see README, "iOS"). Ignored on Android.
  static const providerBundleIdentifier = 'com.fibr.app.tunnel';

  static const _interfaceName = 'wg0';

  // Inferred interface type: the plugin does not export the class name,
  // WireGuardFlutter.instance's static type is its implementation interface.
  final _wg = WireGuardFlutter.instance;
  final _status = StreamController<TunnelStatus>.broadcast();

  bool _initialized = false;
  StreamSubscription<Object?>? _stageSub;

  @override
  Future<void> initialize() async {
    if (_initialized) return;
    await _wg.initialize(interfaceName: _interfaceName);
    _stageSub = _wg.vpnStageSnapshot.listen((stage) {
      _status.add(_mapStage(stage));
    });
    _initialized = true;
  }

  @override
  Future<void> connect({
    required String serverAddress,
    required String wgQuickConfig,
  }) async {
    await initialize();
    // TODO(stage-3): apply tunnel.splitMode/splitApps via
    // VpnService.Builder.addAllowedApplication/addDisallowedApplication —
    // wireguard_flutter 0.1.x has no per-app API, so the selection is stored
    // in the model and UI only.
    await _wg.startVpn(
      serverAddress: serverAddress,
      wgQuickConfig: wgQuickConfig,
      providerBundleIdentifier: providerBundleIdentifier,
    );
  }

  @override
  Future<void> disconnect() => _wg.stopVpn();

  @override
  Future<TunnelStatus> currentStatus() async => _mapStage(await _wg.stage());

  @override
  Stream<TunnelStatus> get onStatusChanged => _status.stream;

  @override
  Future<TunnelStats?> readStats() async {
    // TODO: wireguard_flutter 0.1.x exposes no rx/tx counters. Once the
    // plugin (or our own MethodChannel to WireGuardKit / com.wireguard
    // .android:tunnel) provides them, return TunnelStats here — the UI
    // already renders whatever this returns.
    return null;
  }

  static TunnelStatus _mapStage(VpnStage stage) {
    switch (stage) {
      case VpnStage.connected:
        return TunnelStatus.connected;
      case VpnStage.connecting:
      case VpnStage.authenticating:
      case VpnStage.preparing:
      case VpnStage.waitingConnection:
      case VpnStage.reconnect:
        return TunnelStatus.connecting;
      case VpnStage.disconnecting:
      case VpnStage.exiting:
        return TunnelStatus.disconnecting;
      case VpnStage.denied:
      case VpnStage.noConnection:
        return TunnelStatus.failure;
      case VpnStage.disconnected:
        return TunnelStatus.disconnected;
    }
  }

  void dispose() {
    _stageSub?.cancel();
    _status.close();
  }
}

/// Owns the tunnel list (persisted to shared_preferences as JSON under
/// `fibr.tunnels`), the active tunnel and its status. One VPN at a time.
class TunnelController extends ChangeNotifier {
  TunnelController({TunnelEngine? engine})
      : _engine = engine ?? WireGuardFlutterEngine();

  final TunnelEngine _engine;
  final List<Tunnel> _tunnels = [];

  String? _activeTunnelId;
  TunnelStatus _status = TunnelStatus.disconnected;
  TunnelStats? _stats;
  String? _lastError;

  List<Tunnel> get tunnels => List.unmodifiable(_tunnels);
  String? get activeTunnelId => _activeTunnelId;
  TunnelStatus get status => _status;
  TunnelStats? get stats => _stats;
  String? get lastError => _lastError;

  Tunnel? tunnelById(String id) {
    for (final t in _tunnels) {
      if (t.id == id) return t;
    }
    return null;
  }

  Tunnel? get activeTunnel =>
      _activeTunnelId == null ? null : tunnelById(_activeTunnelId!);

  /// Boots the engine and starts listening for status changes. Never throws:
  /// engine failures land in [lastError] and the UI still works.
  Future<void> start() async {
    try {
      await _engine.initialize();
      _engine.onStatusChanged.listen(_onEngineStatus);
    } catch (e) {
      _lastError = '$e';
      debugPrint('TunnelController.start: $e');
    }
  }

  Future<void> load() async {
    final prefs = await SharedPreferences.getInstance();
    final raw = prefs.getString(_storageKey);
    if (raw == null || raw.isEmpty) return;
    try {
      final list = jsonDecode(raw) as List;
      for (final item in list) {
        if (item is Map<String, dynamic>) {
          final t = Tunnel.fromJson(item);
          if (t != null) _tunnels.add(t);
        }
      }
    } catch (e) {
      debugPrint('TunnelController.load: corrupt storage: $e');
    }
    notifyListeners();
  }

  Future<void> addTunnel(Tunnel tunnel) async {
    _tunnels.add(tunnel);
    await _persist();
    notifyListeners();
  }

  Future<void> updateTunnel(Tunnel tunnel) async {
    final i = _tunnels.indexWhere((t) => t.id == tunnel.id);
    if (i < 0) return;
    _tunnels[i] = tunnel;
    await _persist();
    notifyListeners();
  }

  /// Replaces the wg-quick config text of tunnel [id] (server swap
  /// re-import): re-parses the config so endpoint/server-key labels
  /// refresh, bumps updatedAt and persists. A session running on this
  /// tunnel keeps using the OLD config until the user reconnects — no
  /// auto-reconnect here; callers notify. Returns false when the id is
  /// unknown or [newConfig] does not parse.
  Future<bool> updateTunnelConfig(String id, String newConfig) async {
    if (WgConfigParser.tryParse(newConfig) == null) return false;
    final i = _tunnels.indexWhere((t) => t.id == id);
    if (i < 0) return false;
    final old = _tunnels[i];
    final updated = old.withConfig(newConfig);
    updated.connectedSince = old.connectedSince; // keep the running session
    _tunnels[i] = updated;
    await _persist();
    notifyListeners();
    return true;
  }

  Future<void> removeTunnel(String id) async {
    if (id == _activeTunnelId) await disconnect();
    _tunnels.removeWhere((t) => t.id == id);
    await _persist();
    notifyListeners();
  }

  /// Connects [tunnel]; returns false and fills [lastError] on failure.
  Future<bool> connect(Tunnel tunnel) async {
    final endpoint = tunnel.primaryEndpoint;
    if (endpoint == null || endpoint.isEmpty) {
      _lastError = 'no endpoint in config';
      notifyListeners();
      return false;
    }
    try {
      await disconnect();
      _activeTunnelId = tunnel.id;
      tunnel.connectedSince = null;
      _status = TunnelStatus.connecting;
      _stats = null;
      notifyListeners();
      await _engine.connect(serverAddress: endpoint, wgQuickConfig: tunnel.config);
      return true;
    } catch (e) {
      _lastError = '$e';
      _status = TunnelStatus.failure;
      notifyListeners();
      return false;
    }
  }

  Future<void> disconnect() async {
    try {
      await _engine.disconnect();
    } catch (e) {
      debugPrint('TunnelController.disconnect: $e');
    }
  }

  /// Polls the engine for traffic counters (null while unsupported — TODO
  /// in WireGuardFlutterEngine.readStats).
  Future<void> refreshStats() async {
    try {
      _stats = await _engine.readStats();
    } catch (_) {
      _stats = null;
    }
    notifyListeners();
  }

  void _onEngineStatus(TunnelStatus s) {
    _status = s;
    final active = activeTunnel;
    switch (s) {
      case TunnelStatus.connected:
        active?.connectedSince ??= DateTime.now();
        break;
      case TunnelStatus.disconnected:
      case TunnelStatus.failure:
        _activeTunnelId = null;
        active?.connectedSince = null;
        _stats = null;
        break;
      case TunnelStatus.connecting:
      case TunnelStatus.disconnecting:
        break;
    }
    notifyListeners();
  }

  Future<void> _persist() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(
      _storageKey,
      jsonEncode(_tunnels.map((t) => t.toJson()).toList()),
    );
  }

  static const _storageKey = 'fibr.tunnels';

  @override
  void dispose() {
    final engine = _engine;
    if (engine is WireGuardFlutterEngine) {
      engine.dispose();
    }
    super.dispose();
  }
}
