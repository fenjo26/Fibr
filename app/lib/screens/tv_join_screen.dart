import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../l10n/app_localizations.dart';
import '../models/tunnel.dart';
import '../services/join_service.dart';
import '../services/tunnel_service.dart';
import '../services/wg_config_parser.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';
import '../widgets/tv_keyboard.dart';

/// TV onboarding without a camera or a phone (feature 2): type the server
/// domain and the 8-char join code with the on-screen keyboard driven by
/// the D-pad. Focus on a field tile selects it as the keyboard's target;
/// the segment cursor auto-advances per typed char, and when the last
/// segment fills, focus jumps to the Connect button. The code is exchanged
/// for a wg-quick config (JoinService) and the tunnel lands in the engine
/// — the AppRoot then swaps to the TV home screen by itself.
///
/// The second TV path ("pair from the phone", stage 2.6) is NOT here —
/// it needs a server-side pairing endpoint and is out of scope.
class TvJoinScreen extends StatefulWidget {
  const TvJoinScreen({super.key, this.initialJoinUrl});

  /// A fibr://join/... or https://host/join/<code> deep link: the domain
  /// and the code segments are prefilled from it, focus lands on Connect.
  final String? initialJoinUrl;

  @override
  State<TvJoinScreen> createState() => _TvJoinScreenState();
}

enum _TvInputTarget { domain, code }

class _TvJoinScreenState extends State<TvJoinScreen> {
  final _connectNode = FocusNode();
  final _code = TvCodeEntry();

  String _domain = '';
  _TvInputTarget _target = _TvInputTarget.domain;
  bool _busy = false;

  // Same pref as the phone join tab / login: the domain typed once is
  // reused everywhere.
  static const _serverPref = 'fibr.join.server';

  @override
  void initState() {
    super.initState();
    // A deep link carries its own domain — do not let a stale saved one
    // clobber it (the async load below would win the race otherwise).
    final link = widget.initialJoinUrl;
    if (link != null) {
      _prefillFromLink(link);
      return;
    }
    _loadSavedDomain();
  }

  /// fibr://join/... or https://host/join/<code>: split it back into the
  /// domain field and the segment row. Runs from initState only — plain
  /// field writes, no setState (nothing is built yet).
  void _prefillFromLink(String raw) {
    final join = JoinService.extractJoinUrl(raw);
    if (join == null) return;
    final uri = Uri.tryParse(join);
    if (uri == null) return;
    final parts = uri.pathSegments;
    if (parts.length != 2 || uri.host.isEmpty) return;
    _domain = uri.hasPort ? '${uri.host}:${uri.port}' : uri.host;
    _code.clear();
    for (final ch in parts[1].split('')) {
      if (!_code.append(ch)) break;
    }
  }

  Future<void> _loadSavedDomain() async {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString(_serverPref);
    if (saved != null && saved.isNotEmpty && mounted) {
      setState(() => _domain = saved);
    }
  }

  @override
  void dispose() {
    _connectNode.dispose();
    super.dispose();
  }

  void _onKey(String ch) {
    setState(() {
      if (_target == _TvInputTarget.domain) {
        if (_domain.length < 60) _domain += ch;
      } else {
        final wasComplete = _code.isComplete;
        _code.append(ch);
        if (!wasComplete && _code.isComplete) {
          // Auto-advance: the code is full — hand the cursor to Connect.
          _connectNode.requestFocus();
        }
      }
    });
  }

  void _onDelete() {
    setState(() {
      if (_target == _TvInputTarget.domain) {
        if (_domain.isNotEmpty) {
          _domain = _domain.substring(0, _domain.length - 1);
        }
      } else {
        _code.backspace();
      }
    });
  }

  Future<void> _connect() async {
    if (_busy || !mounted) return;
    final server = _domain.trim();
    if (server.isEmpty || !_code.isComplete) return;
    final l10n = AppLocalizations.of(context);
    setState(() => _busy = true);
    try {
      final config = await JoinService.exchange(_code.text, server: server);
      if (!mounted) return;
      final parsed = WgConfigParser.tryParse(config);
      if (parsed == null) {
        // A vless:// link for an xray device — this client speaks WireGuard.
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text(l10n.t('errJoinVless'))),
        );
        return;
      }
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_serverPref, server);
      if (!mounted) return;
      await context
          .read<TunnelController>()
          .addTunnel(Tunnel(
            id: Tunnel.newId(),
            name: WgConfigParser.suggestName(parsed),
            config: config,
          ));
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t('tunnelAdded'))),
      );
      if (Navigator.of(context).canPop()) Navigator.of(context).pop();
      // Not pushed (first TV launch): AppRoot swaps to the TV home itself.
    } on JoinException catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(l10n.t(_joinErrorKey(e.error)))),
      );
    } finally {
      if (mounted) setState(() => _busy = false);
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

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    final pad = AppTheme.tvPad;

    return Scaffold(
      appBar: AppBar(title: Text(l10n.t('tvJoinTitle'))),
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: EdgeInsets.all(pad(16)),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  l10n.t('tvJoinHint'),
                  style: TextStyle(
                    color: c.muted,
                    fontWeight: FontWeight.w600,
                  ),
                ),
                SizedBox(height: pad(12)),
                FocusableTile(
                  autofocus: true,
                  selected: _target == _TvInputTarget.domain,
                  onFocusChange: (focused) {
                    if (focused) {
                      setState(() => _target = _TvInputTarget.domain);
                    }
                  },
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        l10n.t('tvServerField'),
                        style: TextStyle(
                          color: c.muted,
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 6),
                      _DomainDisplay(
                        text: _domain,
                        hint: l10n.t('loginDomainHint'),
                        caret: _target == _TvInputTarget.domain,
                      ),
                    ],
                  ),
                ),
                SizedBox(height: pad(12)),
                FocusableTile(
                  selected: _target == _TvInputTarget.code,
                  onFocusChange: (focused) {
                    if (focused) {
                      setState(() => _target = _TvInputTarget.code);
                    }
                  },
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        l10n.t('tvCodeField'),
                        style: TextStyle(
                          color: c.muted,
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      const SizedBox(height: 10),
                      _CodeSegments(
                        entry: _code,
                        active: _target == _TvInputTarget.code,
                      ),
                    ],
                  ),
                ),
                SizedBox(height: pad(16)),
                TvOnScreenKeyboard(
                  layout: _target == _TvInputTarget.domain
                      ? TvKeyboardLayout.domain
                      : TvKeyboardLayout.code,
                  onKey: _onKey,
                  onDelete: _onDelete,
                ),
                SizedBox(height: pad(16)),
                SizedBox(
                  height: 64,
                  child: FilledButton.icon(
                    focusNode: _connectNode,
                    style: FilledButton.styleFrom(
                      padding: EdgeInsets.symmetric(
                        horizontal: AppTheme.tvPad(32),
                      ),
                    ),
                    onPressed:
                        _busy || !_code.isComplete || _domain.trim().isEmpty
                            ? null
                            : _connect,
                    icon: _busy
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.bolt),
                    label: Text(
                      _busy ? l10n.t('connecting') : l10n.t('connect'),
                      style: const TextStyle(
                        fontSize: 20,
                        fontWeight: FontWeight.w800,
                      ),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// The typed domain with a block caret while the field is the keyboard
/// target; a muted placeholder when empty.
class _DomainDisplay extends StatelessWidget {
  const _DomainDisplay({
    required this.text,
    required this.hint,
    required this.caret,
  });

  final String text;
  final String hint;
  final bool caret;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    final mono = const TextStyle(
      fontFamily: 'monospace',
      fontFamilyFallback: ['Courier New'],
      fontSize: 18,
      fontWeight: FontWeight.w600,
    );
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Flexible(
          child: text.isEmpty
              ? Text(hint, style: mono.copyWith(color: c.faint))
              : Text(
                  text,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: mono.copyWith(color: c.ink),
                ),
        ),
        if (caret) ...[
          const SizedBox(width: 2),
          Container(
            width: 3,
            height: 24,
            decoration: BoxDecoration(color: c.secondary),
          ),
        ],
      ],
    );
  }
}

/// The eight one-char segments of the join code. The segment under the
/// cursor (the next to be typed) carries the secondary focus color while
/// the code field is the keyboard target.
class _CodeSegments extends StatelessWidget {
  const _CodeSegments({required this.entry, required this.active});

  final TvCodeEntry entry;
  final bool active;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (var i = 0; i < entry.length; i++) ...[
          if (i > 0) const SizedBox(width: 8),
          Container(
            width: 48,
            height: 64,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: c.surface2,
              borderRadius: BorderRadius.circular(10),
              border: Border.all(
                color: active && i == entry.cursor ? c.secondary : c.hairline,
                width: active && i == entry.cursor ? 3 : 1,
              ),
            ),
            child: Text(
              entry.charAt(i),
              style: TextStyle(
                fontFamily: 'monospace',
                fontFamilyFallback: ['Courier New'],
                fontSize: 30,
                fontWeight: FontWeight.w800,
                color: c.ink,
              ),
            ),
          ),
        ],
      ],
    );
  }
}
