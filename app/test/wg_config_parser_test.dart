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
}
