import 'package:flutter_test/flutter_test.dart';

import 'package:fibr/services/server_api_service.dart';

void main() {
  group('normalizeDomain', () {
    test('accepts a bare host', () {
      expect(normalizeDomain('vpnoleg.de'), 'vpnoleg.de');
    });

    test('strips scheme and path', () {
      expect(normalizeDomain('https://vpnoleg.de'), 'vpnoleg.de');
      expect(normalizeDomain('https://vpnoleg.de/whatever'), 'vpnoleg.de');
      expect(normalizeDomain('http://vpnoleg.de'), 'vpnoleg.de');
    });

    test('keeps a non-default port, drops 443', () {
      expect(normalizeDomain('https://vpnoleg.de:8443/x'), 'vpnoleg.de:8443');
      expect(normalizeDomain('vpnoleg.de:8443'), 'vpnoleg.de:8443');
      expect(normalizeDomain('https://vpnoleg.de:443'), 'vpnoleg.de');
    });

    test('trims whitespace and tolerates a trailing slash', () {
      expect(normalizeDomain('  vpnoleg.de/ '), 'vpnoleg.de');
    });

    test('rejects garbage', () {
      expect(normalizeDomain(''), isNull);
      expect(normalizeDomain('   '), isNull);
      expect(normalizeDomain('https://'), isNull);
      // A host with spaces does not survive URI parsing.
      expect(normalizeDomain('not a domain'), isNull);
    });
  });

  group('MeProfile.fromJson', () {
    final json = {
      'user': {'id': 7, 'username': 'Oleg', 'role': 'user'},
      'devices': [
        {
          'id': 1,
          'name': 'phone',
          'protocol': 'wg',
          'ipv4': '10.8.0.2',
          'enabled': true,
          'online': false,
        },
        {
          'id': 2,
          'name': 'tv',
          'protocol': 'xray',
          'ipv4': '10.8.0.3',
          'enabled': true,
          'online': true,
        },
      ],
      'max_devices': 5,
      'xray_enabled': true,
      'has_access_key': true,
      'active_node': 'local',
      'endpoint_changed_at': 123,
    };

    test('parses the /api/me shape', () {
      final me = MeProfile.fromJson(json);
      expect(me.username, 'Oleg');
      expect(me.maxDevices, 5);
      expect(me.xrayEnabled, isTrue);
      expect(me.hasAccessKey, isTrue);
      expect(me.activeNode, 'local');
      expect(me.devices, hasLength(2));
    });

    test('device rows keep protocol and online state', () {
      final me = MeProfile.fromJson(json);
      final wg = me.devices.first;
      expect(wg.id, 1);
      expect(wg.isXray, isFalse);
      expect(wg.online, isFalse);
      expect(wg.enabled, isTrue);

      final xray = me.devices.last;
      expect(xray.isXray, isTrue);
      expect(xray.online, isTrue);
    });

    test('defaults survive a minimal payload', () {
      final me = MeProfile.fromJson({});
      expect(me.username, '');
      expect(me.devices, isEmpty);
      expect(me.maxDevices, 0);
      expect(me.xrayEnabled, isFalse);
      expect(me.hasAccessKey, isFalse);
    });

    test('unknown protocol falls back to wg', () {
      final d = ServerDevice.fromJson({
        'id': 3,
        'name': 'legacy',
        'protocol': '',
        'ipv4': '10.8.0.4',
        'enabled': true,
      });
      expect(d.protocol, 'wg');
      expect(d.isXray, isFalse);
    });
  });

  group('tunnelIdForDevice', () {
    test('is stable and unique per device', () {
      expect(tunnelIdForDevice(42), 'srv-42');
      expect(tunnelIdForDevice(42), tunnelIdForDevice(42));
      expect(tunnelIdForDevice(41), isNot(tunnelIdForDevice(42)));
    });
  });
}
