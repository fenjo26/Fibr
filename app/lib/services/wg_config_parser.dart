/// Parser for wg-quick configuration text ([Interface]/[Peer] sections).
///
/// Accepts what real configs contain: comments (`#`), blank lines, CRLF,
/// arbitrary whitespace around `=`, case-insensitive key names, several
/// [Peer] sections, and wg-quick-only keys (Table, PostUp, …), which are
/// ignored. Key names follow wg(8): matched case-insensitively.
library;

import 'dart:convert';
import 'dart:io' show InternetAddress, InternetAddressType;

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

  /// Public key of the primary peer — the server's identity. It stays the
  /// same when the operator swaps the endpoint (IP/domain), which is how a
  /// re-imported config is recognised as "the same server". Empty when no
  /// peer has an Endpoint.
  String get serverPublicKey => primaryPeer?.publicKey ?? '';

  /// All AllowedIPs values joined, e.g. "0.0.0.0/0, ::/0".
  String get allowedIpsJoined =>
      peers.expand((p) => p.allowedIps).toSet().join(', ');

  /// Gateway of the tunnel's IPv4 subnet: the network address + 1, where
  /// the Fibr server sits. A /32 (or prefix-less) interface address is
  /// treated as one host inside a /24 — Fibr assigns addresses like
  /// `10.8.0.2/32` while the server answers on `10.8.0.1`
  /// (docs/dns-games.md §1). Null when the config has no parseable IPv4
  /// address.
  String? get subnetGatewayV4 {
    final bytes = _subnetGatewayBytes(InternetAddressType.IPv4);
    if (bytes == null) return null;
    return InternetAddress.fromRawBytes(bytes, type: InternetAddressType.IPv4)
        .address;
  }

  /// Whether any DNS server of the config IS the tunnel gateway — how a
  /// Fibr device is routed through the server's filtering resolver instead
  /// of a public one (docs/dns-games.md §1).
  bool get dnsPointsToTunnel {
    for (final raw in iface.dns) {
      final addr = InternetAddress.tryParse(raw);
      if (addr == null) continue;
      final gw = _subnetGatewayBytes(addr.type);
      if (gw != null && WgConfigParser._bytesEqual(addr.rawAddress, gw)) {
        return true;
      }
    }
    return false;
  }

  /// Gateway bytes for the first interface address of [type]: network
  /// address + 1. Single-host prefixes (/32, /128 — and prefix-less bare
  /// addresses) are first widened to the family's natural subnet (/24 for
  /// IPv4, /64 for IPv6), mirroring how Fibr numbers its tunnels.
  List<int>? _subnetGatewayBytes(InternetAddressType type) {
    for (final raw in iface.addresses) {
      final slash = raw.indexOf('/');
      final addr =
          InternetAddress.tryParse(slash < 0 ? raw : raw.substring(0, slash));
      if (addr == null || addr.type != type) continue;
      var prefix = addr.rawAddress.length * 8; // bare address = single host
      if (slash >= 0) {
        final p = int.tryParse(raw.substring(slash + 1));
        if (p == null || p < 0 || p > prefix) continue; // malformed → next
        prefix = p;
      }
      return WgConfigParser._gatewayBytes(addr, prefix);
    }
    return null;
  }
}

class WgConfigParser {
  /// Parses [text]; throws [WgConfigFormatException] on structural errors.
  static WgConfig parse(String text) {
    final peers = <WgPeer>[];

    var section = ''; // '' | 'interface' | 'peer'
    // Whether an [Interface] section was seen; `iface` itself is only built
    // after the loop, so it cannot double as this flag.
    var sawInterface = false;
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
          if (sawInterface) {
            throw const WgConfigFormatException('duplicate [Interface] section');
          }
          sawInterface = true;
          section = 'interface';
        } else if (name == 'peer') {
          if (!sawInterface) {
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

    if (!sawInterface) {
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
    final iface = WgInterface(
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

  /// Serialises [cfg] back to wg-quick text; the key order follows the
  /// server's own config template (internal/wg ClientConfigAt). Everything
  /// the parser keeps is written out. wg-quick-only keys (Table, PostUp, …)
  /// do NOT round-trip — render is for DERIVED configs (e.g. the game
  /// tunnel), never for rewriting a stored tunnel's original text.
  static String render(WgConfig cfg) {
    final b = StringBuffer('[Interface]\n');
    b.writeln('PrivateKey = ${cfg.iface.privateKey}');
    b.writeln('Address = ${cfg.iface.addresses.join(', ')}');
    if (cfg.iface.dns.isNotEmpty) {
      b.writeln('DNS = ${cfg.iface.dns.join(', ')}');
    }
    if (cfg.iface.listenPort != null) {
      b.writeln('ListenPort = ${cfg.iface.listenPort}');
    }
    if (cfg.iface.mtu != null) b.writeln('MTU = ${cfg.iface.mtu}');
    for (final p in cfg.peers) {
      b.writeln();
      b.writeln('[Peer]');
      b.writeln('PublicKey = ${p.publicKey}');
      final psk = p.presharedKey;
      if (psk != null && psk.isNotEmpty) b.writeln('PresharedKey = $psk');
      final endpoint = p.endpoint;
      if (endpoint != null && endpoint.isNotEmpty) {
        b.writeln('Endpoint = $endpoint');
      }
      if (p.allowedIps.isNotEmpty) {
        b.writeln('AllowedIPs = ${p.allowedIps.join(', ')}');
      }
      final keepalive = p.persistentKeepalive;
      if (keepalive != null) b.writeln('PersistentKeepalive = $keepalive');
    }
    return b.toString();
  }

  /// Bytes of the gateway of [addr]/[prefix]: the network address + 1. A
  /// single-host prefix (equal to the address width — /32, /128, or a
  /// prefix-less address) is first widened to the family's natural subnet
  /// (/24 for IPv4, /64 for IPv6) because that is how Fibr numbers its
  /// tunnels: clients get one address, the server sits on x.y.z.1 /
  /// `<subnet>::1` (docs/dns-games.md §1).
  static List<int>? _gatewayBytes(InternetAddress addr, int prefix) {
    final bytes = addr.rawAddress;
    final bits = bytes.length * 8;
    if (prefix < 0 || prefix > bits) return null;
    if (prefix == bits) prefix = bits == 32 ? 24 : 64; // host → its subnet
    final gw = List<int>.of(bytes);
    // Zero the host bits → network address.
    final fullBytes = prefix ~/ 8;
    final remBits = prefix % 8;
    for (var i = fullBytes; i < bytes.length; i++) {
      if (i == fullBytes && remBits > 0) {
        gw[i] &= (0xFF << (8 - remBits)) & 0xFF;
      } else {
        gw[i] = 0;
      }
    }
    // +1 — the gateway. Safe: masking left at least one host bit zero, so
    // the last byte never overflows past 255.
    gw[bytes.length - 1] += 1;
    return gw;
  }

  static bool _bytesEqual(List<int> a, List<int> b) {
    if (a.length != b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if (a[i] != b[i]) return false;
    }
    return true;
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
