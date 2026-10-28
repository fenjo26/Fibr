import 'dart:io';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../l10n/app_localizations.dart';
import '../models/tunnel.dart';
import '../services/app_list_service.dart';
import '../services/tunnel_service.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';

/// Split-tunneling editor. Android: allowlist/denylist over installed apps.
/// iOS: informational message — per-app VPN needs an MDM profile there
/// (Apple platform limitation), so only the full tunnel exists.
///
/// The selection is stored on the Tunnel immediately. Applying it to the
/// running VPN is a TODO in WireGuardFlutterEngine (plugin limitation).
class SplitAppsScreen extends StatefulWidget {
  const SplitAppsScreen({super.key, required this.tunnelId});

  final String tunnelId;

  @override
  State<SplitAppsScreen> createState() => _SplitAppsScreenState();
}

class _SplitAppsScreenState extends State<SplitAppsScreen> {
  final AppListService _appList = AppListService();

  List<InstalledApp> _apps = [];
  bool _loading = true;
  String _query = '';
  bool _showSystem = false;

  @override
  void initState() {
    super.initState();
    if (Platform.isAndroid) {
      _appList.list().then((apps) {
        if (mounted) setState(() { _apps = apps; _loading = false; });
      });
    } else {
      _loading = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final ctrl = context.watch<TunnelController>();
    final tunnel = ctrl.tunnelById(widget.tunnelId);
    if (tunnel == null) return const Scaffold(body: SizedBox.shrink());
    final l10n = AppLocalizations.of(context);

    return Scaffold(
      appBar: AppBar(title: Text(l10n.t('splitTitle'))),
      body: Platform.isAndroid
          ? _buildAndroid(context, ctrl, tunnel, l10n)
          : _buildIosNotice(l10n),
    );
  }

  Widget _buildIosNotice(AppLocalizations l10n) {
    final c = FibrColors.of(context);
    return Center(
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: FibrCard(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.phone_iphone, size: 40, color: c.faint),
              const SizedBox(height: 12),
              Text(
                l10n.t('iosSplitNote'),
                textAlign: TextAlign.center,
                style: TextStyle(color: c.muted, fontWeight: FontWeight.w600),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildAndroid(
    BuildContext context,
    TunnelController ctrl,
    Tunnel tunnel,
    AppLocalizations l10n,
  ) {
    final selected = tunnel.splitApps.toSet();
    final visible = _apps
        .where((a) => (_showSystem || !a.isSystem))
        .where((a) => _query.isEmpty
            ? true
            : '${a.label} ${a.packageName}'
                .toLowerCase()
                .contains(_query.toLowerCase()))
        .toList();

    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            children: [
              SegmentedButton<SplitTunnelMode>(
                segments: [
                  ButtonSegment(value: SplitTunnelMode.off, label: Text(l10n.t('splitOff'))),
                  ButtonSegment(value: SplitTunnelMode.allow, label: Text(l10n.t('splitAllow'))),
                  ButtonSegment(value: SplitTunnelMode.deny, label: Text(l10n.t('splitDeny'))),
                ],
                selected: {tunnel.splitMode},
                onSelectionChanged: (s) => _applyMode(ctrl, tunnel, s.first),
              ),
            ],
          ),
        ),
        if (tunnel.splitMode != SplitTunnelMode.off) ...[
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    decoration: InputDecoration(
                      prefixIcon: const Icon(Icons.search),
                      hintText: l10n.t('searchApps'),
                      isDense: true,
                    ),
                    onChanged: (v) => setState(() => _query = v),
                  ),
                ),
                const SizedBox(width: 8),
                StatusPill(
                  label: l10n.t('selectedCount', {'n': selected.length}),
                  tone: PillTone.live,
                ),
              ],
            ),
          ),
          SwitchListTile(
            value: _showSystem,
            onChanged: (v) => setState(() => _showSystem = v),
            title: Text(l10n.t('showSystemApps')),
          ),
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : ListView.builder(
                    itemCount: visible.length,
                    itemBuilder: (context, i) {
                      final app = visible[i];
                      return CheckboxListTile(
                        value: selected.contains(app.packageName),
                        title: Text(app.label, maxLines: 1, overflow: TextOverflow.ellipsis),
                        subtitle: Text(
                          app.packageName,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                            fontFamily: 'monospace',
                            fontFamilyFallback: ['Courier New'],
                            fontSize: 11,
                          ),
                        ),
                        dense: true,
                        onChanged: (_) => _toggleApp(ctrl, tunnel, app.packageName),
                      );
                    },
                  ),
          ),
        ] else
          Expanded(
            child: _loading
                ? const Center(child: CircularProgressIndicator())
                : const SizedBox.shrink(),
          ),
      ],
    );
  }

  void _applyMode(TunnelController ctrl, Tunnel tunnel, SplitTunnelMode mode) {
    ctrl.updateTunnel(tunnel.copyWith(splitMode: mode));
  }

  void _toggleApp(TunnelController ctrl, Tunnel tunnel, String packageName) {
    final apps = tunnel.splitApps.toSet();
    if (apps.contains(packageName)) {
      apps.remove(packageName);
    } else {
      apps.add(packageName);
    }
    ctrl.updateTunnel(tunnel.copyWith(splitApps: apps.toList()));
  }
}
