import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../l10n/app_localizations.dart';
import '../services/tunnel_service.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';
import 'add_tunnel_screen.dart';
import 'settings_screen.dart';
import 'tunnel_detail_screen.dart';

class TunnelListScreen extends StatelessWidget {
  const TunnelListScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Text(l10n.t('appName')),
        actions: [
          IconButton(
            icon: const Icon(Icons.settings_outlined),
            tooltip: l10n.t('settings'),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(builder: (_) => const SettingsScreen()),
            ),
          ),
        ],
      ),
      body: Consumer<TunnelController>(
        builder: (context, ctrl, _) {
          if (ctrl.tunnels.isEmpty) return _EmptyState(label: l10n.t('tunnelsEmpty'));
          return ListView.separated(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 96),
            itemCount: ctrl.tunnels.length,
            separatorBuilder: (_, __) => const SizedBox(height: 8),
            itemBuilder: (context, i) =>
                _TunnelCard(tunnelId: ctrl.tunnels[i].id),
          );
        },
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => Navigator.of(context).push(
          MaterialPageRoute<void>(builder: (_) => const AddTunnelScreen()),
        ),
        icon: const Icon(Icons.add),
        label: Text(l10n.t('addTunnel')),
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({required this.label});

  final String label;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: FibrCard(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.vpn_key_outlined, size: 40, color: c.faint),
              const SizedBox(height: 12),
              Text(
                label,
                textAlign: TextAlign.center,
                style: TextStyle(color: c.muted, fontWeight: FontWeight.w600),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _TunnelCard extends StatelessWidget {
  const _TunnelCard({required this.tunnelId});

  final String tunnelId;

  @override
  Widget build(BuildContext context) {
    final ctrl = context.watch<TunnelController>();
    final tunnel = ctrl.tunnelById(tunnelId);
    if (tunnel == null) return const SizedBox.shrink();
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);

    final isActive = ctrl.activeTunnelId == tunnel.id;
    final (dotOn, tone, statusLabel) = switch (isActive ? ctrl.status : TunnelStatus.disconnected) {
      TunnelStatus.connected => (true, PillTone.ok, l10n.t('statusConnected')),
      TunnelStatus.connecting => (true, PillTone.live, l10n.t('statusConnecting')),
      TunnelStatus.disconnecting => (true, PillTone.live, l10n.t('statusDisconnecting')),
      TunnelStatus.failure => (false, PillTone.danger, l10n.t('statusFailure')),
      TunnelStatus.disconnected => (false, PillTone.off, l10n.t('statusDisconnected')),
    };

    return FibrCard(
      onTap: () => Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) => TunnelDetailScreen(tunnelId: tunnelId),
        ),
      ),
      child: Row(
        children: [
          StatusDot(on: dotOn),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  tunnel.name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context)
                      .textTheme
                      .titleSmall
                      ?.copyWith(fontWeight: FontWeight.w800),
                ),
                const SizedBox(height: 2),
                Text(
                  tunnel.endpointLabel,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    color: c.muted,
                    fontFamily: 'monospace',
                    fontFamilyFallback: ['Courier New'],
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: 8),
          StatusPill(label: statusLabel, tone: tone),
          const SizedBox(width: 4),
          Icon(Icons.chevron_right, size: 20, color: c.faint),
        ],
      ),
    );
  }
}
