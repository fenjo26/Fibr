import 'dart:async';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../l10n/app_localizations.dart';
import '../services/server_api_service.dart';
import '../services/tunnel_service.dart';
import '../theme/app_theme.dart';
import '../utils/format.dart';
import '../widgets/fibr_widgets.dart';
import 'devices_screen.dart';
import 'settings_screen.dart';
import 'tunnel_list_screen.dart';

/// Simple-mode home: one status card (connected/disconnected, server,
/// session time), ONE big Connect/Disconnect button driving the ACTIVE
/// server device, and a small "Devices" link underneath. The active
/// device's config is fetched silently and applied to the engine — keys and
/// configs are never shown here.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key});

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  Timer? _timer;
  bool _connecting = false; // config fetch + engine bring-up in flight

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
    WidgetsBinding.instance.addPostFrameCallback((_) {
      context.read<ServerAccountController>().refreshMe();
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _toggle(ServerAccountController account) async {
    final l10n = AppLocalizations.of(context);
    final tunnels = context.read<TunnelController>();
    final device = account.activeDevice;
    if (device == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('homeNoDevice'))),
      );
      return;
    }
    if (device.isXray) {
      // Honest plate: a vless:// link cannot run through this client's
      // WireGuard engine.
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('xrayPlate'))),
      );
      return;
    }
    final tunnelId = tunnelIdForDevice(device.id);
    if (tunnels.activeTunnelId == tunnelId) {
      await tunnels.disconnect();
      return;
    }
    final api = account.api;
    if (api == null) return;
    setState(() => _connecting = true);
    try {
      final ok = await applyDeviceToEngine(
          api: api, tunnels: tunnels, device: device);
      if (!mounted) return;
      if (!ok) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(l10n.t('xrayPlate'))),
        );
        return;
      }
      final tunnel = tunnels.tunnelById(tunnelId);
      if (tunnel != null) {
        final connected = await tunnels.connect(tunnel);
        if (!connected && mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(
                l10n.t('errConnect', {'msg': tunnels.lastError ?? '?'}))),
          );
        }
      }
    } on ServerApiException {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(l10n.t('errLoginNetwork'))),
        );
      }
    } finally {
      if (mounted) setState(() => _connecting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final account = context.watch<ServerAccountController>();
    final tunnels = context.watch<TunnelController>();
    final l10n = AppLocalizations.of(context);
    final me = account.me;

    final device = account.activeDevice;
    final tunnelId = device == null ? null : tunnelIdForDevice(device.id);
    final isSession = tunnelId != null && tunnels.activeTunnelId == tunnelId;
    final status =
        isSession ? tunnels.status : TunnelStatus.disconnected;
    final busy = _connecting ||
        status == TunnelStatus.connecting ||
        status == TunnelStatus.disconnecting;
    final connected = status == TunnelStatus.connected;

    return Scaffold(
      appBar: AppBar(
        title: Text(l10n.t('appName')),
        actions: [
          IconButton(
            icon: const Icon(Icons.list_alt_outlined),
            tooltip: l10n.t('tunnelsTitle'),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(builder: (_) => const TunnelListScreen()),
            ),
          ),
          IconButton(
            icon: const Icon(Icons.settings_outlined),
            tooltip: l10n.t('settings'),
            onPressed: () => Navigator.of(context).push(
              MaterialPageRoute<void>(builder: (_) => const SettingsScreen()),
            ),
          ),
        ],
      ),
      body: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              _StatusCard(
                domain: account.domain ?? '—',
                deviceName: device?.name,
                meUsername: me?.username,
                status: status,
                busy: _connecting,
                sessionSince: isSession
                    ? tunnels.tunnelById(tunnelId)?.connectedSince
                    : null,
              ),
              const SizedBox(height: 16),
              SizedBox(
                height: 56,
                child: FilledButton.icon(
                  style: connected
                      ? FilledButton.styleFrom(
                          backgroundColor: FibrColors.of(context).danger,
                          foregroundColor: Colors.white,
                        )
                      : null,
                  onPressed: busy ? null : () => _toggle(account),
                  icon: busy
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : Icon(connected
                          ? Icons.power_settings_new
                          : Icons.bolt),
                  label: Text(
                    busy
                        ? l10n.t('connecting')
                        : connected
                            ? l10n.t('disconnect')
                            : l10n.t('connect'),
                    style: const TextStyle(
                        fontSize: 17, fontWeight: FontWeight.w800),
                  ),
                ),
              ),
              if (device != null && device.isXray) ...[
                const SizedBox(height: 8),
                Text(
                  l10n.t('xrayPlate'),
                  textAlign: TextAlign.center,
                  style: TextStyle(
                    color: FibrColors.of(context).faint,
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                  ),
                ),
              ],
              const SizedBox(height: 8),
              TextButton(
                onPressed: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(builder: (_) => const DevicesScreen()),
                ),
                child: Text(l10n.t('homeDevices')),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The status card: verdict pill, server domain, device name, session time.
class _StatusCard extends StatelessWidget {
  const _StatusCard({
    required this.domain,
    required this.deviceName,
    required this.meUsername,
    required this.status,
    required this.busy,
    required this.sessionSince,
  });

  final String domain;
  final String? deviceName;
  final String? meUsername;
  final TunnelStatus status;
  final bool busy;
  final DateTime? sessionSince;

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
    final duration = sessionSince == null
        ? null
        : formatDuration(DateTime.now().difference(sessionSince!));

    return FibrCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              StatusDot(on: dotOn || busy),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  domain,
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
          const SizedBox(height: 12),
          if (deviceName != null)
            Text(
              deviceName!,
              style: Theme.of(context)
                  .textTheme
                  .headlineSmall
                  ?.copyWith(fontWeight: FontWeight.w800),
            )
          else
            Text(
              l10n.t('homeNoDevice'),
              style: Theme.of(context)
                  .textTheme
                  .titleMedium
                  ?.copyWith(color: c.muted),
            ),
          if (duration != null) ...[
            const SizedBox(height: 4),
            Text(
              '${l10n.t('session')}: $duration',
              style: TextStyle(color: c.muted, fontWeight: FontWeight.w600),
            ),
          ],
          if (meUsername != null && meUsername!.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              l10n.t('signedInAs', {'name': meUsername!}),
              style: TextStyle(color: c.faint, fontSize: 12),
            ),
          ],
        ],
      ),
    );
  }
}
