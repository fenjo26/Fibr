import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:fibr/models/tunnel.dart';
import 'package:fibr/services/tunnel_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Same server, swapped endpoint — what a re-imported config looks like.
String _config(String host) => '''
[Interface]
PrivateKey = a=
Address = 10.8.0.2/32
DNS = 1.1.1.1

[Peer]
PublicKey = serverkey123=
AllowedIPs = 0.0.0.0/0
Endpoint = $host:51820
PersistentKeepalive = 25
''';

class _FakeEngine implements TunnelEngine {
  final _status = StreamController<TunnelStatus>.broadcast();

  @override
  Future<void> initialize() async {}

  @override
  Future<void> connect({
    required String serverAddress,
    required String wgQuickConfig,
  }) async {}

  @override
  Future<void> disconnect() async {}

  @override
  Future<TunnelStatus> currentStatus() async => TunnelStatus.disconnected;

  @override
  Stream<TunnelStatus> get onStatusChanged => _status.stream;

  @override
  Future<TunnelStats?> readStats() async => null;
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('Tunnel.withConfig', () {
    test('replaces config, re-parses and bumps updatedAt', () {
      final t = Tunnel(id: 't1', name: 'Work', config: _config('vpn.example.com'));
      expect(t.primaryEndpoint, 'vpn.example.com:51820');
      expect(t.endpointLabel, 'vpn.example.com:51820');
      expect(t.updatedAt, isNull);

      final newConfig = _config('new-node.example.org');
      final updated = t.withConfig(newConfig);

      expect(updated.id, 't1'); // identity kept — no duplicate
      expect(updated.name, 'Work');
      expect(updated.config, newConfig);
      expect(updated.primaryEndpoint, 'new-node.example.org:51820');
      expect(updated.endpointLabel, 'new-node.example.org:51820');
      expect(updated.serverKeyLabel, 'serverkey123='); // same server
      expect(updated.updatedAt, isNotNull);
    });

    test('copyWith (rename/split) keeps updatedAt and config', () {
      final t = Tunnel(id: 't1', name: 'Work', config: _config('vpn.example.com'));
      final updated = t.withConfig(_config('new-node.example.org'));
      final renamed = updated.copyWith(name: 'Other');
      expect(renamed.config, updated.config);
      expect(renamed.updatedAt, updated.updatedAt);
    });

    test('updatedAt survives a toJson/fromJson round-trip; legacy rows load null', () {
      final updated = Tunnel(id: 't1', name: 'Work', config: _config('a.example.com'))
          .withConfig(_config('b.example.com'));

      final json = updated.toJson();
      final restored = Tunnel.fromJson(jsonDecode(jsonEncode(json)) as Map<String, dynamic>);
      expect(restored!.config, updated.config);
      expect(restored.updatedAt, updated.updatedAt);

      final legacy = Tunnel.fromJson({
        'id': 't2',
        'name': 'Old',
        'config': _config('c.example.com'),
      });
      expect(legacy!.updatedAt, isNull);
    });
  });

  group('TunnelController.updateTunnelConfig', () {
    test('replaces config, bumps updatedAt, persists the round-trip', () async {
      SharedPreferences.setMockInitialValues({});
      final ctrl = TunnelController(engine: _FakeEngine());
      addTearDown(ctrl.dispose);
      await ctrl.addTunnel(Tunnel(id: 't1', name: 'Work', config: _config('vpn.example.com')));

      final ok = await ctrl.updateTunnelConfig('t1', _config('new-node.example.org'));
      expect(ok, isTrue);

      final stored = ctrl.tunnelById('t1')!;
      expect(stored.name, 'Work');
      expect(stored.config, _config('new-node.example.org'));
      expect(stored.primaryEndpoint, 'new-node.example.org:51820');
      expect(stored.updatedAt, isNotNull);
      expect(ctrl.tunnels, hasLength(1)); // updated in place, not duplicated

      // A fresh controller reads the new config back from storage.
      final reloaded = TunnelController(engine: _FakeEngine());
      addTearDown(reloaded.dispose);
      await reloaded.load();
      expect(reloaded.tunnels.single.config, _config('new-node.example.org'));
      expect(reloaded.tunnels.single.updatedAt, stored.updatedAt);
    });

    test('keeps the session timer of a running tunnel (no auto-reconnect)', () async {
      SharedPreferences.setMockInitialValues({});
      final ctrl = TunnelController(engine: _FakeEngine());
      addTearDown(ctrl.dispose);
      final t = Tunnel(id: 't1', name: 'Work', config: _config('vpn.example.com'));
      await ctrl.addTunnel(t);
      final since = DateTime.now().subtract(const Duration(minutes: 5));
      ctrl.tunnels.single.connectedSince = since;

      await ctrl.updateTunnelConfig('t1', _config('new-node.example.org'));
      expect(ctrl.tunnels.single.connectedSince, since);
      expect(ctrl.activeTunnelId, isNull); // nothing auto-connected
    });

    test('returns false for unknown id or unparseable config', () async {
      SharedPreferences.setMockInitialValues({});
      final ctrl = TunnelController(engine: _FakeEngine());
      addTearDown(ctrl.dispose);
      await ctrl.addTunnel(Tunnel(id: 't1', name: 'Work', config: _config('vpn.example.com')));

      expect(await ctrl.updateTunnelConfig('nope', _config('x.example.com')), isFalse);
      expect(await ctrl.updateTunnelConfig('t1', 'not a config'), isFalse);
      expect(ctrl.tunnels.single.config, _config('vpn.example.com')); // untouched
      expect(ctrl.tunnels.single.updatedAt, isNull);
    });
  });
}
