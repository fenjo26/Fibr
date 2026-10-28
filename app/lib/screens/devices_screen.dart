import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../l10n/app_localizations.dart';
import '../services/server_api_service.dart';
import '../services/tunnel_service.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';

/// Simple-mode device list (second level under Home): the account's devices
/// from `GET /api/me`, "+ Device" (created on the server and applied to the
/// engine right away — the config is never shown), the active-device pick
/// for the Home connect button, and per-device actions. Xray devices carry
/// an honest plate: their credential is a vless:// link, which this client
/// cannot tunnel — open it in v2rayNG/Hiddify (fetch it from the Mini App).
class DevicesScreen extends StatefulWidget {
  const DevicesScreen({super.key});

  @override
  State<DevicesScreen> createState() => _DevicesScreenState();
}

class _DevicesScreenState extends State<DevicesScreen> {
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _reload();
  }

  Future<void> _reload() async {
    final account = context.read<ServerAccountController>();
    await account.refreshMe();
    if (!mounted) return;
    setState(() => _loading = false);
    // Signed out mid-session (key rotated/revoked) — fall back to the root.
    if (!account.isSignedIn && Navigator.of(context).canPop()) {
      Navigator.of(context).pop();
    }
  }

  void _showAddSheet() {
    final l10n = AppLocalizations.of(context);
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (sheetContext) => _AddDeviceSheet(
        xrayEnabled: context.read<ServerAccountController>().me?.xrayEnabled ?? false,
        onCreated: (device) async {
          Navigator.of(sheetContext).pop();
          await _applyCreatedDevice(device);
        },
        onCreateFailed: (error) {
          Navigator.of(sheetContext).pop();
          if (!mounted) return;
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text(l10n.t(_createErrorKey(error)))),
          );
        },
      ),
    );
  }

  static String _createErrorKey(Object error) {
    if (error is ServerApiException) {
      switch (error.error) {
        case ServerLoginError.deviceLimit:
          return 'errDeviceLimit';
        case ServerLoginError.network:
          return 'errLoginNetwork';
        case ServerLoginError.expired:
          return 'errLoginExpired';
        case ServerLoginError.disabled:
          return 'errLoginDisabled';
        case ServerLoginError.invalidKey:
          return 'sessionExpired';
        case ServerLoginError.badDomain:
        case ServerLoginError.server:
          return 'errLoginServer';
      }
    }
    return 'errLoginServer';
  }

  /// Create→apply pipeline tail: the device already exists on the server;
  /// fetch its config and land it in the engine without showing it.
  Future<void> _applyCreatedDevice(ServerDevice device) async {
    final l10n = AppLocalizations.of(context);
    final account = context.read<ServerAccountController>();
    final api = account.api;
    final tunnels = context.read<TunnelController>();
    if (api == null) return;
    try {
      if (device.isForeign) {
        // Fetch anyway: stamps config_fetched_at server-side (multi-node
        // staleness signal); the link itself is unusable here.
        await api.fetchDeviceConfig(device.id);
        if (!mounted) return;
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(l10n.t('xrayCreatedPlate'))),
        );
        return;
      }
      final ok = await applyDeviceToEngine(
          api: api, tunnels: tunnels, device: device);
      if (!mounted) return;
      if (!ok) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(l10n.t('errLoginServer'))),
        );
        return;
      }
      await account.setActiveDevice(device.id.toString());
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('deviceAdded'))),
      );
    } on ServerApiException catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t(_createErrorKey(e)))),
      );
    }
  }

  Future<void> _deleteDevice(ServerDevice device) async {
    final l10n = AppLocalizations.of(context);
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(l10n.t('btnDelete')),
        content: Text(l10n.t('confirmDeleteDevice', {'name': device.name})),
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
    final account = context.read<ServerAccountController>();
    final api = account.api;
    if (api == null) return;
    try {
      await api.deleteDevice(device.id);
    } on ServerApiException catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t(_createErrorKey(e)))),
      );
      return;
    }
    // Drop the local mirror of the credential too (wg devices only).
    final tunnels = context.read<TunnelController>();
    final tunnelId = tunnelIdForDevice(device.id);
    if (tunnels.tunnelById(tunnelId) != null) {
      await tunnels.removeTunnel(tunnelId);
    }
    if (account.activeDeviceId == device.id.toString()) {
      await account.setActiveDevice(null);
    }
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(l10n.t('deviceDeleted'))),
    );
    await _reload();
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final account = context.watch<ServerAccountController>();
    final me = account.me;
    final devices = me?.devices ?? const <ServerDevice>[];

    return Scaffold(
      appBar: AppBar(title: Text(l10n.t('homeDevices'))),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _showAddSheet,
        icon: const Icon(Icons.add),
        label: Text(l10n.t('addDevice')),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _reload,
              child: devices.isEmpty
                  ? ListView(
                      children: [
                        Padding(
                          padding: const EdgeInsets.all(32),
                          child: Center(
                            child: Text(
                              l10n.t('devicesEmptySimple'),
                              textAlign: TextAlign.center,
                              style: TextStyle(
                                color: FibrColors.of(context).muted,
                                fontWeight: FontWeight.w600,
                              ),
                            ),
                          ),
                        ),
                      ],
                    )
                  : ListView.separated(
                      padding: const EdgeInsets.fromLTRB(16, 8, 16, 96),
                      itemCount: devices.length,
                      separatorBuilder: (_, __) => const SizedBox(height: 8),
                      itemBuilder: (context, i) => _DeviceCard(
                        device: devices[i],
                        active:
                            account.activeDeviceId == devices[i].id.toString(),
                        onSelect: () => account
                            .setActiveDevice(devices[i].id.toString()),
                        onDelete: () => _deleteDevice(devices[i]),
                      ),
                    ),
            ),
    );
  }
}

/// One device row: active-device radio, name + protocol pill + status, the
/// xray plate when the tunnel is unavailable here, delete.
class _DeviceCard extends StatelessWidget {
  const _DeviceCard({
    required this.device,
    required this.active,
    required this.onSelect,
    required this.onDelete,
  });

  final ServerDevice device;
  final bool active;
  final VoidCallback onSelect;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return FibrCard(
      onTap: onSelect,
      child: Column(
        children: [
          Row(
            children: [
              Radio<bool>(
                value: true,
                groupValue: active,
                onChanged: (_) => onSelect(),
              ),
              const SizedBox(width: 4),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      device.name,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context)
                          .textTheme
                          .titleSmall
                          ?.copyWith(fontWeight: FontWeight.w800),
                    ),
                    const SizedBox(height: 2),
                    Row(
                      children: [
                        Text(
                          device.ipv4,
                          style: TextStyle(
                            color: c.muted,
                            fontFamily: 'monospace',
                            fontFamilyFallback: ['Courier New'],
                            fontSize: 12,
                            fontWeight: FontWeight.w600,
                          ),
                        ),
                        const SizedBox(width: 8),
                        StatusPill(
                          label: device.isAwg ? 'AWG' : device.isXray ? 'Xray' : 'WG',
                          tone: device.isForeign ? PillTone.live : PillTone.off,
                        ),
                        if (device.online) ...[
                          const SizedBox(width: 8),
                          StatusPill(
                              label: l10n.t('online'), tone: PillTone.ok),
                        ],
                      ],
                    ),
                  ],
                ),
              ),
              IconButton(
                icon: Icon(Icons.delete_outline, color: c.danger),
                tooltip: l10n.t('btnDelete'),
                onPressed: onDelete,
              ),
            ],
          ),
          if (device.isForeign) ...[
            const SizedBox(height: 8),
            Row(
              children: [
                Icon(Icons.info_outline, size: 16, color: c.faint),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    l10n.t(device.plateKey),
                    style:
                        TextStyle(color: c.faint, fontSize: 12, fontWeight: FontWeight.w600),
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

/// "+ Device" bottom sheet: name + protocol (xray only when the server has
/// it on). The config the server renders is never displayed — creation
/// applies it to the engine immediately (see _applyCreatedDevice).
class _AddDeviceSheet extends StatefulWidget {
  const _AddDeviceSheet({
    required this.xrayEnabled,
    required this.onCreated,
    required this.onCreateFailed,
  });

  final bool xrayEnabled;
  final void Function(ServerDevice device) onCreated;
  final void Function(Object error) onCreateFailed;

  @override
  State<_AddDeviceSheet> createState() => _AddDeviceSheetState();
}

class _AddDeviceSheetState extends State<_AddDeviceSheet> {
  final _name = TextEditingController();
  String _protocol = 'wg';
  bool _busy = false;

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  Future<void> _create() async {
    if (_busy || !mounted) return;
    final l10n = AppLocalizations.of(context);
    final account = context.read<ServerAccountController>();
    final api = account.api;
    if (api == null) return;
    final name = _name.text.trim();
    if (name.isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('nameRequired'))),
      );
      return;
    }
    setState(() => _busy = true);
    try {
      final device = await api.createDevice(name, _protocol);
      widget.onCreated(device);
    } catch (e) {
      widget.onCreateFailed(e);
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(l10n.t('addDeviceTitle'),
              style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 12),
          TextField(
            controller: _name,
            autofocus: true,
            maxLength: 32,
            decoration: InputDecoration(
              labelText: l10n.t('nameField'),
              hintText: l10n.t('nameHint'),
            ),
            onSubmitted: (_) => _create(),
          ),
          const SizedBox(height: 12),
          if (widget.xrayEnabled)
            SegmentedButton<String>(
              segments: [
                ButtonSegment(value: 'wg', label: const Text('WireGuard')),
                ButtonSegment(value: 'xray', label: const Text('Xray')),
              ],
              selected: {_protocol},
              onSelectionChanged: (s) => setState(() => _protocol = s.first),
            ),
          const SizedBox(height: 16),
          FilledButton.icon(
            onPressed: _busy ? null : _create,
            icon: _busy
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.add),
            label: Text(_busy ? l10n.t('deviceAddBusy') : l10n.t('btnCreate')),
          ),
        ],
      ),
    );
  }
}
