import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:provider/provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../l10n/app_localizations.dart';
import '../models/tunnel.dart';
import '../services/join_service.dart';
import '../services/tunnel_service.dart';
import '../services/wg_config_parser.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';

/// Add-tunnel flow: scan a QR code, paste a wg-quick config, exchange a
/// quick-connect code, or arrive pre-filled from a fibr:// deep link. After
/// a successful scan the screen switches to a confirmation form with the
/// parsed summary.
class AddTunnelScreen extends StatefulWidget {
  const AddTunnelScreen({super.key, this.initialConfig, this.initialJoinUrl});

  final String? initialConfig;

  /// A fibr://join/... or https://.../join/<code> link: the screen exchanges
  /// it for the config right away (quick-connect; docs/join.md on server).
  final String? initialJoinUrl;

  @override
  State<AddTunnelScreen> createState() => _AddTunnelScreenState();
}

/// What the user picked in the "this device is already added" dialog.
enum _SameDeviceAction { update, addNew }

class _AddTunnelScreenState extends State<AddTunnelScreen> {
  int _tab = 0; // 0 — scan, 1 — code, 2 — paste
  bool _handlingScan = false;
  bool _joining = false; // quick-connect exchange in flight
  String? _pendingConfig; // scanned, awaiting confirmation

  late final TextEditingController _name =
      TextEditingController(text: widget.initialConfig == null ? '' : _suggest(widget.initialConfig!));
  late final TextEditingController _config = TextEditingController(text: widget.initialConfig ?? '');
  late final TextEditingController _joinInput = TextEditingController();
  late final TextEditingController _joinServer = TextEditingController();
  static const _serverPref = 'fibr.join.server';

  @override
  void initState() {
    super.initState();
    _loadServerPref();
    if (widget.initialConfig != null) _tab = 2;
    if (widget.initialJoinUrl != null) {
      _joinInput.text = widget.initialJoinUrl!;
      _tab = 1;
      // Exchange as soon as the first frame settles (context for l10n/snacks).
      WidgetsBinding.instance.addPostFrameCallback((_) => _exchange());
    }
  }

  Future<void> _loadServerPref() async {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString(_serverPref);
    if (saved != null && saved.isNotEmpty && mounted) {
      _joinServer.text = saved;
    }
  }

  @override
  void dispose() {
    _name.dispose();
    _config.dispose();
    _joinInput.dispose();
    _joinServer.dispose();
    super.dispose();
  }

  static String _suggest(String config) {
    final parsed = WgConfigParser.tryParse(config);
    return parsed == null ? '' : WgConfigParser.suggestName(parsed);
  }

  void _onScanned(String raw) {
    if (_handlingScan || !mounted) return;
    // A quick-connect QR: the Mini App renders the https join link (and the
    // landing page button is fibr://join/...). Exchange instead of parsing.
    final joinUrl = JoinService.extractJoinUrl(raw);
    if (joinUrl != null) {
      _handlingScan = true;
      _joinInput.text = joinUrl;
      setState(() => _tab = 1);
      _exchange();
      return;
    }
    _handlingScan = true;
    final parsed = WgConfigParser.tryParse(raw);
    if (parsed == null) {
      final l10n = AppLocalizations.of(context);
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text(l10n.t('errQrNotConfig'))));
      Future.delayed(const Duration(milliseconds: 800), () => _handlingScan = false);
      return;
    }
    setState(() {
      _pendingConfig = raw;
      _name.text = WgConfigParser.suggestName(parsed);
    });
  }

  /// Quick-connect exchange: code/link (+ optional server host) -> config,
  /// then the same confirmation form as a scanned QR.
  Future<void> _exchange() async {
    if (_joining || !mounted) return;
    final l10n = AppLocalizations.of(context);
    final input = _joinInput.text;
    if (input.trim().isEmpty) return;
    setState(() => _joining = true);
    try {
      final config = await JoinService.exchange(input, server: _joinServer.text);
      final parsed = WgConfigParser.tryParse(config);
      if (parsed == null) {
        // Most likely a vless:// link for an xray device — this client only
        // speaks WireGuard.
        ScaffoldMessenger.of(context)
            .showSnackBar(SnackBar(content: Text(l10n.t('errJoinVless'))));
        return;
      }
      // Remember the server host for the next bare-code entry.
      final uri = Uri.tryParse(input.trim());
      if (uri != null && uri.host.isNotEmpty) {
        final prefs = await SharedPreferences.getInstance();
        await prefs.setString(_serverPref, uri.host);
        if (mounted) _joinServer.text = uri.host;
      }
      setState(() {
        _pendingConfig = config;
        _name.text = WgConfigParser.suggestName(parsed);
      });
    } on JoinException catch (e) {
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text(l10n.t(_joinErrorKey(e.error)))));
    } finally {
      if (mounted) setState(() => _joining = false);
      _handlingScan = false; // re-arm the scanner either way
    }
  }

  static String _joinErrorKey(JoinError e) {
    switch (e) {
      case JoinError.notFound:
        return 'errJoinNotFound';
      case JoinError.gone:
        return 'errJoinGone';
      case JoinError.forbidden:
        return 'errJoinForbidden';
      case JoinError.network:
        return 'errJoinNetwork';
      case JoinError.badRequest:
        return 'errJoinBad';
      case JoinError.badResponse:
        return 'errJoinNetwork';
    }
  }

  /// The most recently updated existing tunnel of the SAME device: the
  /// [Interface] private key is the device identity and survives server
  /// swaps (endpoint and even the server's own public key change when the
  /// operator switches nodes — see docs/multinode.md — but the device key
  /// pair does not). Null when nothing matches.
  Tunnel? _sameDeviceMatch(TunnelController ctrl, String deviceKey) {
    if (deviceKey.isEmpty) return null;
    final never = DateTime.fromMillisecondsSinceEpoch(0); // legacy null stamps
    Tunnel? match;
    for (final t in ctrl.tunnels) {
      if (t.parsed?.iface.privateKey != deviceKey) continue;
      final best = match?.updatedAt ?? never;
      final current = t.updatedAt ?? never;
      if (match == null || current.isAfter(best)) match = t;
    }
    return match;
  }

  /// Server-swap fork: this device already has a tunnel — offer to update
  /// it in place instead of creating a duplicate. Returns null when
  /// dismissed (stay on this screen).
  Future<_SameDeviceAction?> _askSameDevice(AppLocalizations l10n, String name) {
    return showDialog<_SameDeviceAction>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text(l10n.t('dupTitle')),
        content: Text(l10n.t('dupBody', {'name': name})),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: Text(l10n.t('cancel')),
          ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(_SameDeviceAction.addNew),
            child: Text(l10n.t('dupAddNew')),
          ),
          FilledButton(
            onPressed: () => Navigator.of(dialogContext).pop(_SameDeviceAction.update),
            child: Text(l10n.t('dupUpdate')),
          ),
        ],
      ),
    );
  }

  Future<void> _save(String configText) async {
    final l10n = AppLocalizations.of(context);
    try {
      final parsed = WgConfigParser.parse(configText);
      if (parsed.primaryEndpoint == null) {
        throw const WgConfigFormatException('no peer with Endpoint');
      }
      final ctrl = context.read<TunnelController>();

      // Re-import of a device the user already has (operator swapped the
      // server/node) — offer an in-place update of the existing tunnel.
      final match = _sameDeviceMatch(ctrl, parsed.iface.privateKey);
      if (match != null) {
        final action = await _askSameDevice(l10n, match.name);
        if (!mounted) return;
        if (action == _SameDeviceAction.update) {
          final wasActive = ctrl.activeTunnelId == match.id;
          await ctrl.updateTunnelConfig(match.id, configText);
          if (!mounted) return;
          ScaffoldMessenger.of(context).showSnackBar(SnackBar(
            content: Text(l10n.t(
              wasActive ? 'tunnelUpdatedReconnect' : 'tunnelUpdated',
              {'name': match.name},
            )),
          ));
          Navigator.of(context).pop();
          return;
        }
        if (action != _SameDeviceAction.addNew) return; // dismissed
      }

      await ctrl.addTunnel(Tunnel(
        id: Tunnel.newId(),
        name: _name.text.trim().isEmpty ? WgConfigParser.suggestName(parsed) : _name.text.trim(),
        config: configText,
      ));
      if (!mounted) return;
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text(l10n.t('tunnelAdded'))));
      Navigator.of(context).pop();
    } on WgConfigFormatException catch (e) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('errInvalidConfig', {'msg': e.message}))),
      );
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(l10n.t('addTitle'))),
      body: _pendingConfig != null ? _buildConfirm(l10n) : _buildTabs(l10n),
    );
  }

  Widget _buildTabs(AppLocalizations l10n) {
    return Column(
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
          child: SegmentedButton<int>(
            segments: [
              ButtonSegment(value: 0, icon: const Icon(Icons.qr_code_scanner), label: Text(l10n.t('importQr'))),
              ButtonSegment(value: 1, icon: const Icon(Icons.password), label: Text(l10n.t('importCode'))),
              ButtonSegment(value: 2, icon: const Icon(Icons.content_paste), label: Text(l10n.t('importPaste'))),
            ],
            selected: {_tab},
            onSelectionChanged: (s) => setState(() => _tab = s.first),
          ),
        ),
        Expanded(child: switch (_tab) {
          0 => _buildScanner(l10n),
          1 => _buildCode(l10n),
          _ => _buildPaste(l10n),
        }),
      ],
    );
  }

  Widget _buildScanner(AppLocalizations l10n) {
    final c = FibrColors.of(context);
    return Stack(
      alignment: Alignment.center,
      children: [
        MobileScanner(
          onDetect: (capture) {
            final barcodes = capture.barcodes;
            if (barcodes.isEmpty) return;
            final raw = barcodes.first.rawValue;
            if (raw == null || raw.isEmpty) return;
            _onScanned(raw);
          },
        ),
        Positioned(
          bottom: 24,
          left: 16,
          right: 16,
          child: FibrCard(
            child: Text(
              l10n.t('scanHint'),
              textAlign: TextAlign.center,
              style: TextStyle(color: c.ink, fontWeight: FontWeight.w600),
            ),
          ),
        ),
      ],
    );
  }

  /// Quick-connect tab: paste the link the owner sent (or the bare code plus
  /// the server host) and get the config in one tap.
  Widget _buildCode(AppLocalizations l10n) {
    final c = FibrColors.of(context);
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        FibrCard(
          child: Text(
            l10n.t('joinHint'),
            style: TextStyle(color: c.ink, fontWeight: FontWeight.w600),
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _joinInput,
          autocorrect: false,
          enableSuggestions: false,
          keyboardType: TextInputType.url,
          decoration: InputDecoration(
            labelText: l10n.t('joinCodeField'),
            hintText: l10n.t('joinCodeHint'),
          ),
          onSubmitted: (_) => _exchange(),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _joinServer,
          autocorrect: false,
          enableSuggestions: false,
          keyboardType: TextInputType.url,
          decoration: InputDecoration(
            labelText: l10n.t('joinServerField'),
            hintText: l10n.t('joinServerHint'),
          ),
          onSubmitted: (_) => _exchange(),
        ),
        const SizedBox(height: 16),
        FilledButton.icon(
          onPressed: _joining ? null : () => _exchange(),
          icon: _joining
              ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
              : const Icon(Icons.bolt_outlined),
          label: Text(_joining ? l10n.t('joinBusy') : l10n.t('joinBtn')),
        ),
      ],
    );
  }

  Widget _buildPaste(AppLocalizations l10n) {
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        TextField(
          controller: _name,
          decoration: InputDecoration(labelText: l10n.t('nameField'), hintText: l10n.t('nameHint')),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _config,
          maxLines: 12,
          style: const TextStyle(
            fontFamily: 'monospace',
            fontFamilyFallback: ['Courier New'],
            fontSize: 12,
            fontWeight: FontWeight.w600,
          ),
          decoration: InputDecoration(
            labelText: l10n.t('configField'),
            hintText: l10n.t('configHint'),
            alignLabelWithHint: true,
          ),
        ),
        const SizedBox(height: 16),
        FilledButton.icon(
          onPressed: () => _save(_config.text),
          icon: const Icon(Icons.save_outlined),
          label: Text(l10n.t('save')),
        ),
      ],
    );
  }

  Widget _buildConfirm(AppLocalizations l10n) {
    final parsed = WgConfigParser.tryParse(_pendingConfig!);
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        FibrCard(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              InfoRow(label: l10n.t('lblEndpoint'), value: parsed?.primaryEndpoint ?? '—'),
              InfoRow(label: l10n.t('lblAddress'), value: parsed?.iface.addresses.join(', ') ?? '—'),
              InfoRow(
                label: l10n.t('lblDns'),
                value: parsed != null && parsed.iface.dns.isNotEmpty
                    ? parsed.iface.dns.join(', ')
                    : '—',
              ),
              InfoRow(label: l10n.t('lblAllowedIps'), value: parsed?.allowedIpsJoined ?? '—'),
            ],
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _name,
          autofocus: true,
          decoration: InputDecoration(labelText: l10n.t('nameField'), hintText: l10n.t('nameHint')),
        ),
        const SizedBox(height: 16),
        FilledButton.icon(
          onPressed: () => _save(_pendingConfig!),
          icon: const Icon(Icons.save_outlined),
          label: Text(l10n.t('save')),
        ),
        const SizedBox(height: 8),
        TextButton(
          onPressed: () => setState(() {
            _pendingConfig = null;
            _handlingScan = false; // re-arm the scanner
          }),
          child: Text(l10n.t('close')),
        ),
      ],
    );
  }
}
