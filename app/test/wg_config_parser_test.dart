import 'package:flutter_test/flutter_test.dart';
import 'package:fibr/services/wg_config_parser.dart';

const _normalConfig = '''
[Interface]
PrivateKey = ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuv=
Address = 10.8.0.2/32
DNS = 1.1.1.1, 1.0.0.1
MTU = 1420

[Peer]
PublicKey = 1234567890abcdef1234567890abcdef1234567890a=
PresharedKey = fffffffffffffffffffffffffffffffffffffffffffff=
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = vpn.example.com:51820
PersistentKeepalive = 25
''';

void main() {
  group('WgConfigParser.parse — normal config', () {
    final cfg = WgConfigParser.parse(_normalConfig);

    test('interface fields', () {
      expect(cfg.iface.privateKey, 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuv=');
      expect(cfg.iface.addresses, ['10.8.0.2/32']);
      expect(cfg.iface.dns, ['1.1.1.1', '1.0.0.1']);
      expect(cfg.iface.mtu, 1420);
      expect(cfg.iface.listenPort, isNull);
    });

    test('peer fields', () {
      expect(cfg.peers, hasLength(1));
      final peer = cfg.peers.single;
      expect(peer.publicKey, '1234567890abcdef1234567890abcdef1234567890a=');
      expect(peer.presharedKey, 'fffffffffffffffffffffffffffffffffffffffffffff=');
      expect(peer.endpoint, 'vpn.example.com:51820');
      expect(peer.allowedIps, ['0.0.0.0/0', '::/0']);
      expect(peer.persistentKeepalive, 25);
    });

    test('primaryEndpoint / allowedIpsJoined / suggestName', () {
      expect(cfg.primaryEndpoint, 'vpn.example.com:51820');
      expect(cfg.allowedIpsJoined, '0.0.0.0/0, ::/0');
      expect(WgConfigParser.suggestName(cfg), 'vpn.example.com');
    });
  });

  test('messy input: comments, CRLF, extra spaces, case, blank lines', () {
    const messy = '# Fibr device config\r\n'
        '\r\n'
        '[Interface]\r\n'
        '# interfacce section\r\n'
        '   privatekey  =  sp Aces around and inside key  =\r\n'
        'ADDRESS=10.8.0.3/32, 10.8.0.4/32\r\n'
        '\r\n'
        '[Peer]   \r\n'
        'publickey=peer1key=\r\n'
        'allowedips = 10.0.0.0/8  # inline comment\r\n'
        'endpoint = vpn2.example.org : 51830\r\n'
        'persistentkeepalive = 15\r\n';
    final cfg = WgConfigParser.parse(messy);
    expect(cfg.iface.privateKey, 'sp Aces around and inside key  =');
    // "key = value = value" splits on the FIRST '=' only; values keep inner
    // spaces after trimming ends — keys with spaces are invalid anyway, but
    // the parser must not crash and must keep the payload verbatim.
    expect(cfg.iface.addresses, ['10.8.0.3/32', '10.8.0.4/32']);
    expect(cfg.peers.single.allowedIps, ['10.0.0.0/8']);
    expect(cfg.peers.single.endpoint, 'vpn2.example.org : 51830');
    expect(cfg.peers.single.persistentKeepalive, 15);
  });

  test('multiple [Peer] sections, order preserved', () {
    const config = '''
[Interface]
PrivateKey = a=
Address = 10.9.0.5/32

[Peer]
PublicKey = peerA=
Endpoint = a.example.com:51820
AllowedIPs = 192.168.1.0/24

[Peer]
PublicKey = peerB=
Endpoint = b.example.com:51820
AllowedIPs = 192.168.2.0/24

[Peer]
# roaming peer without endpoint
PublicKey = peerC=
AllowedIPs = 10.10.0.0/16
''';
    final cfg = WgConfigParser.parse(config);
    expect(cfg.peers.map((p) => p.publicKey), ['peerA=', 'peerB=', 'peerC=']);
    expect(cfg.primaryEndpoint, 'a.example.com:51820');
    expect(cfg.allowedIpsJoined.contains('192.168.1.0/24'), isTrue);
    expect(cfg.allowedIpsJoined.contains('10.10.0.0/16'), isTrue);
    expect(WgConfigParser.suggestName(cfg), 'a.example.com');
  });

  test('wg-quick-only keys are ignored (Table, PostUp, SaveConfig)', () {
    const config = '''
[Interface]
PrivateKey = a=
Address = 10.8.0.9/32
Table = off
PostUp = iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE
SaveConfig = true

[Peer]
PublicKey = p=
Endpoint = x.example.com:51820
AllowedIPs = 0.0.0.0/0
''';
    final cfg = WgConfigParser.parse(config);
    expect(cfg.iface.addresses, ['10.8.0.9/32']);
    expect(cfg.peers.single.endpoint, 'x.example.com:51820');
  });

  group('invalid configs throw WgConfigFormatException', () {
    test('empty text', () {
      expect(() => WgConfigParser.parse('  \n# only a comment\n'),
          throwsA(isA<WgConfigFormatException>()));
    });

    test('missing [Interface]', () {
      expect(() => WgConfigParser.parse('[Peer]\nPublicKey = p=\n'),
          throwsA(isA<WgConfigFormatException>()));
    });

    test('missing PrivateKey', () {
      expect(
          () => WgConfigParser.parse(
              '[Interface]\nAddress = 10.0.0.1/32\n\n[Peer]\nPublicKey = p=\n'),
          throwsA(isA<WgConfigFormatException>()));
    });

    test('missing Address', () {
      expect(
          () => WgConfigParser.parse(
              '[Interface]\nPrivateKey = a=\n\n[Peer]\nPublicKey = p=\n'),
          throwsA(isA<WgConfigFormatException>()));
    });

    test('no [Peer]', () {
      expect(
          () => WgConfigParser.parse('[Interface]\nPrivateKey = a=\nAddress = 10.0.0.1/32\n'),
          throwsA(isA<WgConfigFormatException>()));
    });

    test('peer without PublicKey', () {
      expect(
          () => WgConfigParser.parse('[Interface]\nPrivateKey = a=\nAddress = 10.0.0.1/32\n'
              '\n[Peer]\nEndpoint = x.example.com:51820\n'),
          throwsA(isA<WgConfigFormatException>()));
    });

    test('line without = inside a section', () {
      expect(
          () => WgConfigParser.parse('[Interface]\nPrivateKey = a=\nthis is garbage\n'),
          throwsA(isA<WgConfigFormatException>()));
    });

    test('duplicate [Interface]', () {
      expect(
          () => WgConfigParser.parse('[Interface]\nPrivateKey = a=\n'
              '\n[Interface]\nPrivateKey = b=\n'),
          throwsA(isA<WgConfigFormatException>()));
    });
  });

  test('tryParse returns null instead of throwing', () {
    expect(WgConfigParser.tryParse('not a config'), isNull);
    expect(WgConfigParser.tryParse('[Peer]\nPublicKey = p=\n'), isNull);
    expect(WgConfigParser.tryParse(_normalConfig), isNotNull);
  });

  group('serverPublicKey — server identity across endpoint swaps', () {
    test('public key of the primary (first peer with an Endpoint)', () {
      final cfg = WgConfigParser.parse(_normalConfig);
      expect(cfg.serverPublicKey, '1234567890abcdef1234567890abcdef1234567890a=');
    });

    test('skips peers without an Endpoint (multi-peer config)', () {
      const config = '''
[Interface]
PrivateKey = a=
Address = 10.9.0.5/32

[Peer]
# roaming peer, no endpoint — not the server
PublicKey = roamingKey=
AllowedIPs = 10.10.0.0/16

[Peer]
PublicKey = serverKey=
Endpoint = a.example.com:51820
AllowedIPs = 0.0.0.0/0
''';
      final cfg = WgConfigParser.parse(config);
      expect(cfg.serverPublicKey, 'serverKey=');
    });

    test('empty when no peer has an Endpoint', () {
      const config = '''
[Interface]
PrivateKey = a=
Address = 10.9.0.5/32

[Peer]
PublicKey = p=
AllowedIPs = 10.0.0.0/8
''';
      expect(WgConfigParser.parse(config).serverPublicKey, '');
    });

    test('same server key after an endpoint swap', () {
      final before = WgConfigParser.parse(_normalConfig);
      final after = WgConfigParser.parse(
        _normalConfig.replaceFirst('vpn.example.com:51820', 'new-node.example.org:51820'),
      );
      expect(after.primaryEndpoint, 'new-node.example.org:51820');
      expect(after.serverPublicKey, before.serverPublicKey);
    });
  });

  group('subnet gateway & DNS protection', () {
    // Minimal single-tunnel config with a variable Address/DNS pair.
    WgConfig cfg(String address, [String dns = '']) {
      final dnsLine = dns.isEmpty ? '' : 'DNS = $dns\n';
      return WgConfigParser.parse(
        '[Interface]\nPrivateKey = a=\nAddress = $address\n$dnsLine'
        '\n[Peer]\nPublicKey = p=\nEndpoint = vpn.example.com:51820\n'
        'AllowedIPs = 0.0.0.0/0\n',
      );
    }

    test('/32 interface address is a member of its /24 → x.y.z.1', () {
      expect(cfg('10.8.0.2/32').subnetGatewayV4, '10.8.0.1');
      expect(cfg('192.168.50.130/32').subnetGatewayV4, '192.168.50.1');
    });

    test('explicit prefixes → network address + 1', () {
      expect(cfg('10.66.7.9/24').subnetGatewayV4, '10.66.7.1');
      expect(cfg('172.16.5.9/16').subnetGatewayV4, '172.16.0.1');
      expect(cfg('10.0.0.4/30').subnetGatewayV4, '10.0.0.5');
      expect(cfg('10.0.15.9/21').subnetGatewayV4, '10.0.8.1');
    });

    test('bare address (no prefix) is treated like /32 → /24', () {
      expect(cfg('10.8.0.2').subnetGatewayV4, '10.8.0.1');
    });

    test('dual-stack: the IPv6 /128 member belongs to its /64', () {
      // Gateway v6 = <subnet>::1; a v6 DNS pointing there counts as
      // tunnel DNS. No IPv4 address → no v4 gateway.
      final c = cfg('fd42:f1b2:3:4::2/128', 'fd42:f1b2:3:4::1');
      expect(c.subnetGatewayV4, isNull);
      expect(c.dnsPointsToTunnel, isTrue);
      expect(cfg('fd42::2/128', '2001:4860:4860::8888').dnsPointsToTunnel, isFalse);
    });

    test('DNS = gateway → protected; public DNS → not', () {
      expect(cfg('10.8.0.2/32', '10.8.0.1').dnsPointsToTunnel, isTrue);
      expect(cfg('10.8.0.2/32', '1.1.1.1').dnsPointsToTunnel, isFalse);
      // Any entry pointing at the gateway is enough.
      expect(cfg('10.8.0.2/32', '1.1.1.1, 10.8.0.1').dnsPointsToTunnel, isTrue);
      // No DNS at all → ordinary.
      expect(cfg('10.8.0.2/32').dnsPointsToTunnel, isFalse);
    });
  });

  group('render', () {
    test('round-trips a parsed config field by field', () {
      final cfg = WgConfigParser.parse(_normalConfig);
      final back = WgConfigParser.parse(WgConfigParser.render(cfg));

      expect(back.iface.privateKey, cfg.iface.privateKey);
      expect(back.iface.addresses, cfg.iface.addresses);
      expect(back.iface.dns, cfg.iface.dns);
      expect(back.iface.mtu, cfg.iface.mtu);
      expect(back.iface.listenPort, cfg.iface.listenPort);

      expect(back.peers, hasLength(1));
      final peer = back.peers.single;
      expect(peer.publicKey, cfg.peers.single.publicKey);
      expect(peer.presharedKey, cfg.peers.single.presharedKey);
      expect(peer.endpoint, cfg.peers.single.endpoint);
      expect(peer.allowedIps, cfg.peers.single.allowedIps);
      expect(peer.persistentKeepalive, cfg.peers.single.persistentKeepalive);
    });

    test('round-trips multiple peers', () {
      const config = '''
[Interface]
PrivateKey = a=
Address = 10.9.0.5/32

[Peer]
PublicKey = peerA=
PresharedKey = pskA=
Endpoint = a.example.com:51820
AllowedIPs = 192.168.1.0/24
PersistentKeepalive = 25

[Peer]
PublicKey = peerB=
AllowedIPs = 192.168.2.0/24
''';
      final cfg = WgConfigParser.parse(config);
      final back = WgConfigParser.parse(WgConfigParser.render(cfg));
      expect(back.peers.map((p) => p.publicKey), ['peerA=', 'peerB=']);
      expect(back.peers.first.presharedKey, 'pskA=');
      expect(back.peers.first.allowedIps, ['192.168.1.0/24']);
      expect(back.peers.last.endpoint, isNull); // no endpoint stays absent
      expect(back.allowedIpsJoined, cfg.allowedIpsJoined);
    });

    test('a config without DNS renders without a DNS line', () {
      const text =
          '[Interface]\nPrivateKey = a=\nAddress = 10.8.0.2/32\n\n[Peer]\n'
          'PublicKey = p=\nEndpoint = vpn.example.com:51820\nAllowedIPs = 0.0.0.0/0\n';
      final cfg = WgConfigParser.parse(text);
      expect(cfg.iface.dns, isEmpty);
      final rendered = WgConfigParser.render(cfg);
      expect(rendered.contains('DNS'), isFalse);
      expect(WgConfigParser.parse(rendered).iface.dns, isEmpty);
    });

    test('game-tunnel shape: same keys, union AllowedIPs, no DNS', () {
      final base = WgConfigParser.parse('''
[Interface]
PrivateKey = devkey=
Address = 10.8.0.2/32
DNS = 10.8.0.1
MTU = 1420

[Peer]
PublicKey = serverkey=
PresharedKey = psk=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
''');
      final game = WgConfigParser.parse(WgConfigParser.render(WgConfig(
        iface: WgInterface(
          privateKey: base.iface.privateKey,
          addresses: base.iface.addresses,
        ),
        peers: [
          WgPeer(
            publicKey: base.primaryPeer!.publicKey,
            presharedKey: base.primaryPeer!.presharedKey,
            endpoint: base.primaryPeer!.endpoint,
            allowedIps: const ['146.66.155.0/24', '162.249.72.0/21'],
            persistentKeepalive: base.primaryPeer!.persistentKeepalive,
          ),
        ],
      )));

      expect(game.iface.privateKey, 'devkey='); // same device keys
      expect(game.iface.dns, isEmpty); // routing only, no filtering
      expect(game.iface.mtu, isNull); // not carried over by design
      final peer = game.primaryPeer!;
      expect(peer.publicKey, 'serverkey=');
      expect(peer.presharedKey, 'psk=');
      expect(peer.endpoint, 'vpn.example.com:51820'); // same server
      expect(peer.allowedIps, ['146.66.155.0/24', '162.249.72.0/21']);
      expect(peer.persistentKeepalive, 25);
      expect(game.dnsPointsToTunnel, isFalse);
    });
  });
}
