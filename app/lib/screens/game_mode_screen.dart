import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../l10n/app_localizations.dart';
import '../models/game.dart';
import '../models/tunnel.dart';
import '../services/server_api_service.dart';
import '../services/tunnel_service.dart';
import '../services/wg_config_parser.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';
import 'tunnel_detail_screen.dart';

/// Hard cap on the route union — WireGuard handles hundreds of AllowedIPs
/// fine, but a runaway catalog should not silently build a monster config.
const _kCidrLimit = 180;

/// Game mode (docs/dns-games.md §2): a SECOND WireGuard tunnel from the
/// same peer whose AllowedIPs are only the subnets of the selected games.
/// Before playing the user switches to this tunnel instead of the base
/// one — game traffic goes through the VPN, everything else stays direct.
class GameModeScreen extends StatefulWidget {
  const GameModeScreen({super.key});

  @override
  State<GameModeScreen> createState() => _GameModeScreenState();
}

class _GameModeScreenState extends State<GameModeScreen> {
  List<GameInfo> _games = kFallbackGames;
  bool _fromServer = false; // false → the snapshot note is shown
  final Set<String> _selected = {};
  bool _creating = false;

  /// Catalog is fetched once per screen lifetime, from build (the gateway
  /// only exists once a base tunnel is up); the token drops stale answers.
  bool _catalogTried = false;
  int _catalogToken = 0;

  @override
  Widget build(BuildContext context) {
    final ctrl = context.watch<TunnelController>();
    final base = ctrl.activeTunnel;
    if (base == null) return _buildNoBase(context);

    final l10n = AppLocalizations.of(context);
    final gateway = base.subnetGateway;
    if (gateway != null && !_catalogTried) {
      _catalogTried = true; // one attempt, set synchronously in build
      _loadCatalog(gateway);
    }

    return Scaffold(
      appBar: AppBar(title: Text(l10n.t('gameMode'))),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          FibrCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  l10n.t('gameIntro'),
                  style: const TextStyle(fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 8),
                Text(
                  l10n.t('gameSwitchHint'),
                  style: TextStyle(color: FibrColors.of(context).muted, fontSize: 12, fontWeight: FontWeight.w600),
                ),
              ],
            ),
          ),
          if (!_fromServer) ...[
            const SizedBox(height: 8),
            Text(
              l10n.t('gameSnapshotNote'),
              style: TextStyle(color: FibrColors.of(context).faint, fontSize: 12),
            ),
          ],
          const SizedBox(height: 8),
          FibrCard(
            padding: const EdgeInsets.symmetric(vertical: 4),
            child: Column(
              children: [
                for (final game in _games)
                  CheckboxListTile(
                    value: _selected.contains(game.id),
                    title: Text(game.displayName(l10n.langCode)),
                    subtitle: Text(l10n.t('gameCidrCount', {'n': game.cidrCount})),
                    dense: true,
                    onChanged: _creating
                        ? null
                        : (_) => setState(() {
                              if (!_selected.remove(game.id)) _selected.add(game.id);
                            }),
                  ),
              ],
            ),
          ),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: _selected.isEmpty || _creating
                ? null
                : () => _create(ctrl, base, l10n),
            icon: _creating
                ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                : const Icon(Icons.sports_esports_outlined),
            label: Text(_creating ? l10n.t('gameCreating') : l10n.t('gameCreate')),
          ),
        ],
      ),
    );
  }

  /// Without a base tunnel there is nothing to derive a game tunnel from —
  /// an explanation instead of a broken form.
  Widget _buildNoBase(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(l10n.t('gameMode'))),
      body: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: FibrCard(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(Icons.sports_esports_outlined, size: 40, color: c.faint),
                const SizedBox(height: 12),
                Text(
                  l10n.t('gameNeedBaseTunnel'),
                  textAlign: TextAlign.center,
                  style: TextStyle(color: c.muted, fontWeight: FontWeight.w600),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Future<void> _loadCatalog(String gateway) async {
    final token = ++_catalogToken;
    final games = await ServerApiService.fetchGames(gateway);
    if (!mounted || token != _catalogToken) return;
    if (games.isEmpty) return; // keep the snapshot (with its note)
    setState(() {
      _games = games;
      _fromServer = true;
    });
  }

  /// Builds and stores the game tunnel: same keys/endpoint as the base
  /// tunnel's primary peer, AllowedIPs = the deduplicated union of the
  /// selected games' subnets (server routes, snapshot as the fallback).
  /// DNS is deliberately not written — the game tunnel routes, it does
  /// not filter.
  Future<void> _create(
    TunnelController ctrl,
    Tunnel base,
    AppLocalizations l10n,
  ) async {
    final parsed = base.parsed;
    final primary = parsed?.primaryPeer;
    if (parsed == null || primary == null) return;
    setState(() => _creating = true);
    try {
      // Catalog order, not set order — stable tunnel names.
      final selectedIds =
          _games.map((g) => g.id).where(_selected.contains).toList();
      final gateway = base.subnetGateway;
      final routes = gateway == null
          ? const <String, List<String>>{}
          : await ServerApiService.fetchGameRoutes(gateway, selectedIds);
      final union = <String>{};
      for (final id in selectedIds) {
        final server = routes[id];
        union.addAll(
            server != null && server.isNotEmpty ? server : fallbackCidrs(id));
      }
      if (union.isEmpty) {
        _snack(l10n.t('gameNoRoutes'));
        return;
      }
      var cidrs = union.toList()..sort();
      if (cidrs.length > _kCidrLimit) {
        _snack(l10n.t('gameCidrLimit', {'n': cidrs.length}));
        cidrs = cidrs.sublist(0, _kCidrLimit);
      }

      final config = WgConfigParser.render(WgConfig(
        iface: WgInterface(
          privateKey: parsed.iface.privateKey,
          addresses: parsed.iface.addresses,
        ),
        peers: [
          for (final p in parsed.peers)
            identical(p, primary)
                ? WgPeer(
                    publicKey: p.publicKey,
                    presharedKey: p.presharedKey,
                    endpoint: p.endpoint,
                    allowedIps: cidrs,
                    persistentKeepalive: p.persistentKeepalive,
                  )
                : p,
        ],
      ));
      final names = _games
          .where((g) => _selected.contains(g.id))
          .map((g) => g.displayName(l10n.langCode))
          .join(', ');
      final tunnel = Tunnel(id: Tunnel.newId(), name: '🎮 $names', config: config);
      await ctrl.addTunnel(tunnel);
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text(l10n.t('gameTunnelCreated'))));
      Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) => TunnelDetailScreen(tunnelId: tunnel.id),
        ),
      );
    } finally {
      if (mounted) setState(() => _creating = false);
    }
  }

  void _snack(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context)
        .showSnackBar(SnackBar(content: Text(message)));
  }
}
