import 'dart:async';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../l10n/app_localizations.dart';
import '../models/tunnel.dart';
import '../services/server_api_service.dart';
import '../services/tunnel_service.dart';
import '../theme/app_theme.dart';
import '../utils/format.dart';
import '../widgets/fibr_widgets.dart';
import 'settings_screen.dart';
import 'tv_join_screen.dart';

/// TV (10-foot) home screen, feature 2 — the same "one big Connect button"
/// idea as the phone HomeScreen, but larger, and with the device picker as
/// a horizontal row of D-pad-focusable cards (FocusableTile). Two modes,
/// mirroring AppRoot's routing:
///
///  - account mode (signed in with an access key): the row is the server
///    device list (`GET /api/me`), picking a card sets the active device,
///    the button drives the same silent fetch-and-apply pipeline;
///  - tunnel mode (joined with a code / manual import): the row is the
///    local tunnel list, pressing a card connects exactly that tunnel.
///
/// Devices are created from the phone (Mini App) — there is deliberately
/// no on-TV device creation form here.
class TvHomeScreen extends StatefulWidget {
  const TvHomeScreen({super.key});

  @override
  State<TvHomeScreen> createState() => _TvHomeScreenState();
}

class _TvHomeScreenState extends State<TvHomeScreen> {
  Timer? _timer;
  bool _connecting = false; // config fetch + engine bring-up in flight

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (mounted) setState(() {});
    });
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (context.read<ServerAccountController>().isSignedIn) {
        context.read<ServerAccountController>().refreshMe();
      }
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  Future<void> _toggle() async {
    final l10n = AppLocalizations.of(context);
    final account = context.read<ServerAccountController>();
    if (account.isSignedIn) {
      await _toggleAccount(account);
      return;
    }
    // Tunnel mode: drive the active tunnel (or the first one).
    final tunnels = context.read<TunnelController>();
    final target = tunnels.activeTunnel ??
        (tunnels.tunnels.isEmpty ? null : tunnels.tunnels.first);
    if (target == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('tvNoTunnels'))),
      );
      return;
    }
    if (tunnels.activeTunnelId == target.id) {
      await tunnels.disconnect();
      return;
    }
    final ok = await tunnels.connect(target);
    if (!ok && mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(
            l10n.t('errConnect', {'msg': tunnels.lastError ?? '?'}))),
      );
    }
  }

  /// Account mode: the same pipeline as the phone HomeScreen._toggle.
  Future<void> _toggleAccount(ServerAccountController account) async {
    final l10n = AppLocalizations.of(context);
    final tunnels = context.read<TunnelController>();
    final device = account.activeDevice;
    if (device == null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('homeNoDevice'))),
      );
      return;
    }
    if (device.isForeign) {
      // Honest plate: a vless:// link cannot run through this client's
      // WireGuard engine.
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t(device.plateKey))),
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
          SnackBar(content: Text(l10n.t(device.plateKey))),
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
    final c = FibrColors.of(context);
    final pad = AppTheme.tvPad;
    final signedIn = account.isSignedIn;

    // The tunnel backing the big button + what to show about it.
    final Tunnel? sessionTunnel;
    final String title;
    final String subtitle;
    if (signedIn) {
      final device = account.activeDevice;
      final id = device == null ? null : tunnelIdForDevice(device.id);
      sessionTunnel = id == null ? null : tunnels.tunnelById(id);
      title = device?.name ?? l10n.t('homeNoDevice');
      subtitle = account.domain ?? '—';
    } else {
      sessionTunnel = tunnels.activeTunnel ??
          (tunnels.tunnels.isEmpty ? null : tunnels.tunnels.first);
      title = sessionTunnel?.name ?? l10n.t('tvNoTunnels');
      subtitle = sessionTunnel?.serverLabel ?? '—';
    }
    final isSession = sessionTunnel != null &&
        tunnels.activeTunnelId == sessionTunnel.id;
    final status =
        isSession ? tunnels.status : TunnelStatus.disconnected;
    final busy = _connecting ||
        status == TunnelStatus.connecting ||
        status == TunnelStatus.disconnecting;
    final connected = status == TunnelStatus.connected;
    final activeXray = signedIn && (account.activeDevice?.isForeign ?? false);
    final since = isSession ? sessionTunnel.connectedSince : null;
    final duration = since == null
        ? null
        : formatDuration(DateTime.now().difference(since));

    final (dotOn, tone, label) = switch (status) {
      TunnelStatus.connected => (true, PillTone.ok, l10n.t('statusConnected')),
      TunnelStatus.connecting => (true, PillTone.live, l10n.t('statusConnecting')),
      TunnelStatus.disconnecting => (true, PillTone.live, l10n.t('statusDisconnecting')),
      TunnelStatus.failure => (false, PillTone.danger, l10n.t('statusFailure')),
      TunnelStatus.disconnected => (false, PillTone.off, l10n.t('statusDisconnected')),
    };

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
      body: ListView(
        padding: EdgeInsets.all(pad(24)),
        children: [
          // Status card
          FibrCard(
            padding: EdgeInsets.all(pad(16)),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    StatusDot(on: dotOn || busy),
                    SizedBox(width: pad(12)),
                    Expanded(
                      child: Text(
                        subtitle,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                          color: c.muted,
                          fontFamily: 'monospace',
                          fontFamilyFallback: ['Courier New'],
                          fontSize: 15,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                    ),
                    StatusPill(label: label, tone: tone),
                  ],
                ),
                SizedBox(height: pad(8)),
                Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context)
                      .textTheme
                      .headlineSmall
                      ?.copyWith(fontWeight: FontWeight.w800),
                ),
                if (duration != null) ...[
                  SizedBox(height: pad(4)),
                  Text(
                    '${l10n.t('session')}: $duration',
                    style:
                        TextStyle(color: c.muted, fontWeight: FontWeight.w600),
                  ),
                ],
              ],
            ),
          ),
          SizedBox(height: pad(16)),

          // THE button — the same action as the phone home, TV-sized.
          SizedBox(
            height: 72,
            child: FilledButton.icon(
              style: connected
                  ? FilledButton.styleFrom(
                      backgroundColor: c.danger,
                      foregroundColor: Colors.white,
                    )
                  : null,
              onPressed: busy ? null : _toggle,
              icon: busy
                  ? const SizedBox(
                      width: 22,
                      height: 22,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Icon(
                      connected ? Icons.power_settings_new : Icons.bolt,
                      size: 28,
                    ),
              label: Text(
                busy
                    ? l10n.t('connecting')
                    : connected
                        ? l10n.t('disconnect')
                        : l10n.t('connect'),
                style: const TextStyle(
                    fontSize: 22, fontWeight: FontWeight.w800),
              ),
            ),
          ),
          if (activeXray) ...[
            SizedBox(height: pad(8)),
            Text(
              l10n.t(account.activeDevice?.plateKey ?? 'xrayPlate'),
              textAlign: TextAlign.center,
              style: TextStyle(
                color: c.faint,
                fontSize: 13,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
          SizedBox(height: pad(24)),

          // Device picker — a horizontal row of focusable cards.
          Text(
            l10n.t('tvPickDevice'),
            style: Theme.of(context)
                .textTheme
                .titleMedium
                ?.copyWith(color: c.muted),
          ),
          SizedBox(height: pad(12)),
          SizedBox(
            height: 128,
            child: signedIn ? _accountRow(account) : _tunnelRow(tunnels),
          ),
        ],
      ),
    );
  }

  Widget _accountRow(ServerAccountController account) {
    final devices = account.me?.devices ?? const <ServerDevice>[];
    if (devices.isEmpty) return _emptyRow('tvNoDevicesAccount');
    return ListView.separated(
      scrollDirection: Axis.horizontal,
      itemCount: devices.length,
      separatorBuilder: (_, __) => const SizedBox(width: 12),
      itemBuilder: (context, i) {
        final d = devices[i];
        final active = account.activeDeviceId == d.id.toString();
        return FocusableTile(
          width: 264,
          height: 128,
          selected: active,
          onTap: () => account.setActiveDevice(d.id.toString()),
          child: _DeviceTileBody(
            name: d.name,
            detail: d.ipv4,
            isXray: d.isForeign,
            online: d.online,
          ),
        );
      },
    );
  }

  Widget _tunnelRow(TunnelController tunnels) {
    if (tunnels.tunnels.isEmpty) return _emptyRow('tvNoTunnels');
    // +1 trailing tile: "add by join code" (pushes the TV onboarding).
    final count = tunnels.tunnels.length + 1;
    return ListView.separated(
      scrollDirection: Axis.horizontal,
      itemCount: count,
      separatorBuilder: (_, __) => const SizedBox(width: 12),
      itemBuilder: (context, i) {
        if (i == count - 1) return _addByCodeTile(context);
        final t = tunnels.tunnels[i];
        final active = tunnels.activeTunnelId == t.id;
        return FocusableTile(
          width: 264,
          height: 128,
          selected: active,
          // Pressing a tunnel card connects exactly that tunnel (the
          // button above follows the active one).
          onTap: () async {
            if (active) {
              await tunnels.disconnect();
            } else {
              await tunnels.connect(t);
            }
          },
          child: _DeviceTileBody(
            name: t.name,
            detail: t.serverLabel,
            isXray: false,
            online: false,
          ),
        );
      },
    );
  }

  Widget _addByCodeTile(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return FocusableTile(
      width: 200,
      height: 128,
      onTap: () => Navigator.of(context).push(
        MaterialPageRoute<void>(builder: (_) => const TvJoinScreen()),
      ),
      child: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Icon(Icons.add_circle_outline, size: 36, color: c.secondary),
          const SizedBox(height: 8),
          Text(
            l10n.t('tvAddByCode'),
            textAlign: TextAlign.center,
            style: TextStyle(
              color: c.muted,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }

  Widget _emptyRow(String key) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8),
        child: Text(
          l10n.t(key),
          textAlign: TextAlign.center,
          style: TextStyle(color: c.muted, fontWeight: FontWeight.w600),
        ),
      ),
    );
  }
}

/// Card body shared by both rows: name on top, detail line + pills below.
class _DeviceTileBody extends StatelessWidget {
  const _DeviceTileBody({
    required this.name,
    required this.detail,
    required this.isXray,
    required this.online,
  });

  final String name;
  final String detail;
  final bool isXray;
  final bool online;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          name,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: Theme.of(context)
              .textTheme
              .titleLarge
              ?.copyWith(fontWeight: FontWeight.w800),
        ),
        const Spacer(),
        Text(
          detail,
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(
            color: c.muted,
            fontFamily: 'monospace',
            fontFamilyFallback: ['Courier New'],
            fontSize: 13,
            fontWeight: FontWeight.w600,
          ),
        ),
        const SizedBox(height: 6),
        Row(
          children: [
            StatusPill(
              label: isXray ? 'Xray' : 'WG',
              tone: isXray ? PillTone.live : PillTone.off,
            ),
            if (online) ...[
              const SizedBox(width: 8),
              StatusPill(label: l10n.t('online'), tone: PillTone.ok),
            ],
          ],
        ),
      ],
    );
  }
}
