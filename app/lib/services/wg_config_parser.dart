/// Parser for wg-quick configuration text ([Interface]/[Peer] sections).
///
/// Accepts what real configs contain: comments (`#`), blank lines, CRLF,
/// arbitrary whitespace around `=`, case-insensitive key names, several
/// [Peer] sections, and wg-quick-only keys (Table, PostUp, …), which are
/// ignored. Key names follow wg(8): matched case-insensitively.
library;

import 'dart:convert';

class WgConfigFormatException implements Exception {
  const WgConfigFormatException(this.message);
  final String message;

  @override
  String toString() => message;
}

class WgInterface {
  const WgInterface({
    required this.privateKey,
    required this.addresses,
    this.dns = const [],
    this.listenPort,
    this.mtu,
  });

  final String privateKey;
  final List<String> addresses;
  final List<String> dns;
  final int? listenPort;
  final int? mtu;
}

class WgPeer {
  const WgPeer({
    required this.publicKey,
    this.presharedKey,
    this.endpoint,
    this.allowedIps = const [],
    this.persistentKeepalive,
  });

  final String publicKey;
  final String? presharedKey;
  final String? endpoint; // host:port
  final List<String> allowedIps;
  final int? persistentKeepalive;
}

class WgConfig {
  const WgConfig({required this.iface, required this.peers});

  final WgInterface iface;
  final List<WgPeer> peers;

  /// First peer that has an Endpoint — the server this client talks to.
  WgPeer? get primaryPeer {
    for (final p in peers) {
      if (p.endpoint != null && p.endpoint!.isNotEmpty) return p;
    }
    return null;
  }

  String? get primaryEndpoint => primaryPeer?.endpoint;

  /// All AllowedIPs values joined, e.g. "0.0.0.0/0, ::/0".
  String get allowedIpsJoined =>
      peers.expand((p) => p.allowedIps).toSet().join(', ');
}

class WgConfigParser {
  /// Parses [text]; throws [WgConfigFormatException] on structural errors.
  static WgConfig parse(String text) {
    WgInterface? iface;
    final peers = <WgPeer>[];

    var section = ''; // '' | 'interface' | 'peer'
    final ifaceValues = <String, String>{};
    final peerValues = <String, String>{};

    void finishPeer() {
      if (peerValues.isEmpty) return;
      final publicKey = peerValues['publickey'];
      if (publicKey == null || publicKey.isEmpty) {
        throw const WgConfigFormatException('peer has no PublicKey');
      }
      peers.add(WgPeer(
        publicKey: publicKey,
        presharedKey: peerValues['presharedkey'],
        endpoint: peerValues['endpoint'],
        allowedIps: _splitList(peerValues['allowedips']),
        persistentKeepalive: _parseInt(peerValues['persistentkeepalive']),
      ));
      peerValues.clear();
    }

    for (final rawLine in const LineSplitter().convert(text)) {
      var line = rawLine;
      final hash = line.indexOf('#');
      if (hash >= 0) line = line.substring(0, hash);
      line = line.trim();
      if (line.isEmpty) continue;

      if (line.startsWith('[')) {
        if (!line.endsWith(']')) {
          throw WgConfigFormatException('bad section header: $line');
        }
        final name = line.substring(1, line.length - 1).trim().toLowerCase();
        if (name == 'interface') {
          if (iface != null) {
            throw const WgConfigFormatException('duplicate [Interface] section');
          }
          section = 'interface';
        } else if (name == 'peer') {
          if (iface == null) {
            throw const WgConfigFormatException('[Peer] before [Interface]');
          }
          finishPeer();
          section = 'peer';
        } else {
          throw WgConfigFormatException('unknown section: [$name]');
        }
        continue;
      }

      final eq = line.indexOf('=');
      if (eq < 0) {
        throw WgConfigFormatException('bad line (expected Key = Value): "$rawLine"');
      }
      final key = line.substring(0, eq).trim().toLowerCase();
      final value = line.substring(eq + 1).trim();
      if (key.isEmpty) {
        throw WgConfigFormatException('bad line (empty key): "$rawLine"');
      }
      if (section == 'interface') {
        ifaceValues[key] = value;
      } else if (section == 'peer') {
        peerValues[key] = value;
      } else {
        throw WgConfigFormatException('key "$key" outside of any section');
      }
    }
    finishPeer();

    if (iface == null) {
      throw const WgConfigFormatException('no [Interface] section');
    }
    final privateKey = ifaceValues['privatekey'];
    if (privateKey == null || privateKey.isEmpty) {
      throw const WgConfigFormatException('Interface has no PrivateKey');
    }
    final addresses = _splitList(ifaceValues['address']);
    if (addresses.isEmpty) {
      throw const WgConfigFormatException('Interface has no Address');
    }
    iface = WgInterface(
      privateKey: privateKey,
      addresses: addresses,
      dns: _splitList(ifaceValues['dns']),
      listenPort: _parseInt(ifaceValues['listenport']),
      mtu: _parseInt(ifaceValues['mtu']),
    );

    if (peers.isEmpty) {
      throw const WgConfigFormatException('no [Peer] section');
    }

    return WgConfig(iface: iface, peers: peers);
  }

  /// Null instead of an exception — for QR payloads and previews.
  static WgConfig? tryParse(String text) {
    try {
      return parse(text);
    } on WgConfigFormatException {
      return null;
    }
  }

  /// Human-friendly default tunnel name: the endpoint host, else "tunnel".
  static String suggestName(WgConfig cfg) {
    final endpoint = cfg.primaryEndpoint;
    if (endpoint == null) return 'tunnel';
    final host = endpoint.contains(':')
        ? endpoint.substring(0, endpoint.lastIndexOf(':'))
        : endpoint;
    return host.isEmpty ? 'tunnel' : host;
  }

  static List<String> _splitList(String? value) {
    if (value == null || value.isEmpty) return const [];
    return value
        .split(',')
        .map((s) => s.trim())
        .where((s) => s.isNotEmpty)
        .toList();
  }

  static int? _parseInt(String? value) {
    if (value == null || value.isEmpty) return null;
    return int.tryParse(value);
  }
}
