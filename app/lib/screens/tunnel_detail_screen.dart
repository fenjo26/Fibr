import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:provider/provider.dart';

import '../l10n/app_localizations.dart';
import '../models/tunnel.dart';
import '../services/server_api_service.dart';
import '../services/tunnel_service.dart';
import '../theme/app_theme.dart';
import '../utils/format.dart';
import '../widgets/fibr_widgets.dart';
import 'split_apps_screen.dart';

/// Active-tunnel screen: connect/disconnect, session duration + rx/tx,
/// config details, split tunneling (Android) and destructive actions.
class TunnelDetailScreen extends StatefulWidget {
  const TunnelDetailScreen({super.key, required this.tunnelId});

  final String tunnelId;

  @override
  State<TunnelDetailScreen> createState() => _TunnelDetailScreenState();
}

class _TunnelDetailScreenState extends State<TunnelDetailScreen> {
  Timer? _timer;
  int _ticks = 0;

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!mounted) return;
      setState(() {});
      _ticks++;
      if (_ticks % 2 == 0) {
        // Poll counters while the screen is open; readStats is a TODO stub
        // on wireguard_flutter 0.1.x and returns null.
        context.read<TunnelController>().refreshStats();
      }
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final ctrl = context.watch<TunnelController>();
    final tunnel = ctrl.tunnelById(widget.tunnelId);
    if (tunnel == null) {
      // Deleted from another path (e.g. deep-link duplicate) — leave quietly.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted && Navigator.of(context).canPop()) Navigator.of(context).pop();
      });
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    final l10n = AppLocalizations.of(context);
    final status = ctrl.activeTunnelId == tunnel.id
        ? ctrl.status
        : TunnelStatus.disconnected;

    return Scaffold(
      appBar: AppBar(title: Text(tunnel.name, maxLines: 1, overflow: TextOverflow.ellipsis)),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _StatusCard(tunnel: tunnel, status: status, onToggle: () => _toggle(ctrl, tunnel)),
          const SizedBox(height: 8),
          if (status != TunnelStatus.disconnected) ...[
            _SessionCard(ctrl: ctrl, tunnel: tunnel),
            const SizedBox(height: 8),
          ],
          _InfoCard(tunnel: tunnel),
          const SizedBox(height: 8),
          _DnsCard(tunnel: tunnel, active: status == TunnelStatus.connected),
          const SizedBox(height: 8),
          _ActionsCard(
            tunnel: tunnel,
            onShowConfig: () => _showConfigSheet(context, tunnel),
            onAskDelete: () => _confirmDelete(context, l10n, tunnel),
          ),
          const SizedBox(height: 8),
          _SplitCard(tunnel: tunnel),
        ],
      ),
    );
  }

  Future<void> _toggle(TunnelController ctrl, Tunnel tunnel) async {
    final l10n = AppLocalizations.of(context);
    if (ctrl.activeTunnelId == tunnel.id) {
      await ctrl.disconnect();
      return;
    }
    final ok = await ctrl.connect(tunnel);
    if (!ok && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('errConnect', {'msg': ctrl.lastError ?? '?'}))),
      );
    }
  }

  void _showConfigSheet(BuildContext context, Tunnel tunnel) {
    final l10n = AppLocalizations.of(context);
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (sheetContext) {
        return SafeArea(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(l10n.t('btnConfig'),
                    style: Theme.of(sheetContext).textTheme.titleMedium),
                const SizedBox(height: 8),
                Flexible(
                  child: SingleChildScrollView(
                    child: SelectableText(
                      tunnel.config,
                      style: const TextStyle(
                        fontFamily: 'monospace',
                        fontFamilyFallback: ['Courier New'],
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                        height: 1.4,
                      ),
                    ),
                  ),
                ),
                const SizedBox(height: 12),
                OutlinedButton.icon(
                  icon: const Icon(Icons.copy),
                  label: Text(l10n.t('copy')),
                  onPressed: () {
                    Clipboard.setData(ClipboardData(text: tunnel.config));
                    ScaffoldMessenger.of(sheetContext).showSnackBar(
                      SnackBar(content: Text(l10n.t('copied'))),
                    );
                  },
                ),
                const SizedBox(height: 8),
                FilledButton(
                  onPressed: () => Navigator.of(sheetContext).pop(),
                  child: Text(l10n.t('close')),
                ),
              ],
            ),
          ),
        );
      },
    );
  }

  Future<void> _confirmDelete(
    BuildContext context,
    AppLocalizations l10n,
    Tunnel tunnel,
  ) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(l10n.t('btnDelete')),
        content: Text(l10n.t('confirmDelete', {'name': tunnel.name})),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: Text(l10n.t('cancel')),
          ),
          FilledButton(
            style: FilledButton.styleFrom(
              backgroundColor: FibrColors.of(dialogContext).danger,
            ),
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: Text(l10n.t('btnDelete')),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    await context.read<TunnelController>().removeTunnel(tunnel.id);
    if (mounted && Navigator.of(context).canPop()) Navigator.of(context).pop();
  }
}

class _StatusCard extends StatelessWidget {
  const _StatusCard({required this.tunnel, required this.status, required this.onToggle});

  final Tunnel tunnel;
  final TunnelStatus status;
  final Future<void> Function() onToggle;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    final (dotOn, tone, label) = switch (status) {
      TunnelStatus.connected => (true, PillTone.ok, l10n.t('statusConnected')),
      TunnelStatus.connecting => (true, PillTone.live, l10n.t('statusConnecting')),
      TunnelStatus.disconnecting => (true, PillTone.live, l10n.t('statusDisconnecting')),
      TunnelStatus.failure => (false, PillTone.danger, l10n.t('statusFailure')),
      TunnelStatus.disconnected => (false, PillTone.off, l10n.t('statusDisconnected')),
    };
    final busy = status == TunnelStatus.connecting || status == TunnelStatus.disconnecting;
    final connected = status == TunnelStatus.connected;

    return FibrCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              StatusDot(on: dotOn),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  tunnel.serverLabel,
                  style: TextStyle(
                    color: c.muted,
                    fontFamily: 'monospace',
                    fontFamilyFallback: ['Courier New'],
                    fontSize: 13,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ),
              StatusPill(label: label, tone: tone),
            ],
          ),
          const SizedBox(height: 16),
          FilledButton.icon(
            // Green only for the main action (Connect); red for Disconnect.
            style: connected
                ? FilledButton.styleFrom(
                    backgroundColor: c.danger,
                    foregroundColor: Colors.white,
                  )
                : null,
            onPressed: busy ? null : () => onToggle(),
            icon: Icon(connected ? Icons.power_settings_new : Icons.bolt),
            label: Text(busy
                ? l10n.t('connecting')
                : connected
                    ? l10n.t('disconnect')
                    : l10n.t('connect')),
          ),
        ],
      ),
    );
  }
}

class _SessionCard extends StatelessWidget {
  const _SessionCard({required this.ctrl, required this.tunnel});

  final TunnelController ctrl;
  final Tunnel tunnel;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    final since = tunnel.connectedSince;
    final duration = since == null
        ? '—'
        : formatDuration(DateTime.now().difference(since));
    final stats = ctrl.stats;
    // readStats() is a TODO stub on wireguard_flutter 0.1.x → honest dashes.
    final rx = stats == null ? '—' : formatBytes(stats.rx);
    final tx = stats == null ? '—' : formatBytes(stats.tx);

    return FibrCard(
      child: Column(
        children: [
          Row(
            children: [
              Expanded(child: Text(l10n.t('session'), style: TextStyle(color: c.muted))),
              Text(duration, style: const TextStyle(fontWeight: FontWeight.w800, fontSize: 18)),
            ],
          ),
          const Divider(height: 20),
          Row(
            children: [
              Expanded(child: Text(l10n.t('rx', {'v': rx}), style: const TextStyle(fontWeight: FontWeight.w600))),
              Text(l10n.t('tx', {'v': tx}), style: const TextStyle(fontWeight: FontWeight.w600)),
            ],
          ),
          const SizedBox(height: 4),
          Text(
            l10n.t('statsUnavailable'),
            style: TextStyle(color: c.faint, fontSize: 12, fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }
}

class _InfoCard extends StatelessWidget {
  const _InfoCard({required this.tunnel});

  final Tunnel tunnel;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return FibrCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          InfoRow(label: l10n.t('lblEndpoint'), value: tunnel.primaryEndpoint ?? '—'),
          InfoRow(label: l10n.t('lblAddress'), value: tunnel.addressLabel),
          InfoRow(label: l10n.t('lblDns'), value: tunnel.dnsLabel),
          InfoRow(label: l10n.t('lblAllowedIps'), value: tunnel.allowedIpsLabel),
          InfoRow(label: l10n.t('lblServerKey'), value: tunnel.serverKeyLabel),
        ],
      ),
    );
  }
}

/// DNS-protection verdict (docs/dns-games.md §1). The config alone already
/// answers "is DNS the tunnel gateway"; while connected the card also
/// probes the resolver's status endpoint ONCE and refines the verdict by
/// the server's enabled/profile state.
class _DnsCard extends StatefulWidget {
  const _DnsCard({required this.tunnel, required this.active});

  final Tunnel tunnel;

  /// Whether this tunnel is currently connected — the status probe only
  /// makes sense through a live tunnel.
  final bool active;

  @override
  State<_DnsCard> createState() => _DnsCardState();
}

class _DnsCardState extends State<_DnsCard> {
  DnsStatus? _apiStatus;

  /// One probe attempt per card lifetime (docs/dns-games.md: the resolver
  /// answers through the tunnel — no retries when it does not).
  bool _probed = false;

  @override
  void initState() {
    super.initState();
    if (widget.active) _probe();
  }

  @override
  void didUpdateWidget(_DnsCard oldWidget) {
    super.didUpdateWidget(oldWidget);
    // Rising edge of active only — not on every parent rebuild tick.
    if (widget.active && !oldWidget.active) _probe();
  }

  Future<void> _probe() async {
    if (_probed) return;
    final gateway = widget.tunnel.subnetGateway;
    if (gateway == null) return;
    _probed = true;
    final status = await ServerApiService.fetchDnsStatus(gateway);
    // A failed probe is silent — the config verdict below still applies.
    if (status == null || !mounted) return;
    setState(() => _apiStatus = status);
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    final api = _apiStatus;
    // Protection is on when DNS is the gateway AND the server (when it
    // answered) runs with a profile other than "off".
    final protected = widget.tunnel.dnsPointsToTunnel &&
        (api == null || (api.enabled && api.defaultProfile != 'off'));
    final profileText = switch (api?.defaultProfile) {
      'adblock' => l10n.t('dnsProfileAdblock'),
      'family' => l10n.t('dnsProfileFamily'),
      _ => l10n.t('dnsProfileUnknown'),
    };

    return FibrCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(
                protected ? Icons.shield : Icons.shield_outlined,
                color: protected ? c.primaryStrong : c.faint,
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  l10n.t('dnsTitle'),
                  style: Theme.of(context).textTheme.titleSmall,
                ),
              ),
              StatusPill(
                label: protected ? l10n.t('dnsProtected') : l10n.t('dnsOrdinary'),
                tone: protected ? PillTone.ok : PillTone.off,
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            l10n.t('dnsProfileHint'),
            style: TextStyle(color: c.muted, fontSize: 12, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 4),
          Text(profileText, style: TextStyle(color: c.faint, fontSize: 12)),
        ],
      ),
    );
  }
}

class _ActionsCard extends StatelessWidget {
  const _ActionsCard({
    required this.tunnel,
    required this.onShowConfig,
    required this.onAskDelete,
  });

  final Tunnel tunnel;
  final VoidCallback onShowConfig;
  final VoidCallback onAskDelete;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return FibrCard(
      padding: const EdgeInsets.symmetric(vertical: 4, horizontal: 8),
      child: Column(
        children: [
          ListTile(
            leading: const Icon(Icons.description_outlined),
            title: Text(l10n.t('btnConfig')),
            onTap: onShowConfig,
          ),
          Divider(height: 1, color: c.hairline),
          ListTile(
            leading: Icon(Icons.delete_outline, color: c.danger),
            title: Text(l10n.t('btnDelete'), style: TextStyle(color: c.danger)),
            onTap: onAskDelete,
          ),
        ],
      ),
    );
  }
}

class _SplitCard extends StatelessWidget {
  const _SplitCard({required this.tunnel});

  final Tunnel tunnel;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return FibrCard(
      padding: const EdgeInsets.symmetric(vertical: 4, horizontal: 8),
      child: ListTile(
        leading: const Icon(Icons.apps),
        title: Text(l10n.t('splitTitle')),
        trailing: const Icon(Icons.chevron_right),
        onTap: () => Navigator.of(context).push(
          MaterialPageRoute<void>(
            builder: (_) => SplitAppsScreen(tunnelId: tunnel.id),
          ),
        ),
      ),
    );
  }
}
