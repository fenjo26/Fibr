import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../l10n/app_localizations.dart';
import '../services/server_api_service.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';
import 'add_tunnel_screen.dart';
import 'settings_screen.dart';

/// First-launch sign-in (simple mode entry point). Three ways, as cards:
///  1) access key — the server domain + the key from the Mini App profile;
///  2) QR / join code — the pre-existing manual flow, as is;
///  3) "Where do I get a key?" — an expanding hint plate for non-technical
///     users (Mini App → Profile → App key).
///
/// The app root decides who sees this: only when there is no account AND no
/// manually imported tunnels.
class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key});

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  final _domain = TextEditingController();
  final _key = TextEditingController();
  bool _keyFormOpen = false;
  bool _whereOpen = false;
  bool _busy = false;

  // Remember the last domain between attempts (same pref as join codes).
  static const _serverPref = 'fibr.join.server';

  @override
  void initState() {
    super.initState();
    _loadSavedDomain();
  }

  Future<void> _loadSavedDomain() async {
    final prefs = await SharedPreferences.getInstance();
    final saved = prefs.getString(_serverPref);
    if (saved != null && saved.isNotEmpty && mounted) _domain.text = saved;
  }

  @override
  void dispose() {
    _domain.dispose();
    _key.dispose();
    super.dispose();
  }

  Future<void> _signIn() async {
    if (_busy || !mounted) return;
    final l10n = AppLocalizations.of(context);
    setState(() => _busy = true);
    final account = context.read<ServerAccountController>();
    final error = await account.signIn(_domain.text, _key.text);
    if (!mounted) return;
    setState(() => _busy = false);
    if (error == null) {
      // Remember the domain for the next bare-code / re-login entry.
      final prefs = await SharedPreferences.getInstance();
      await prefs.setString(_serverPref, account.domain ?? '');
      _key.clear();
      return; // AppRoot rebuilds into the home screen
    }
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(l10n.t(_loginErrorKey(error)))),
    );
  }

  static String _loginErrorKey(ServerLoginError e) {
    switch (e) {
      case ServerLoginError.badDomain:
        return 'errLoginBadDomain';
      case ServerLoginError.network:
        return 'errLoginNetwork';
      case ServerLoginError.invalidKey:
        return 'errLoginInvalidKey';
      case ServerLoginError.disabled:
        return 'errLoginDisabled';
      case ServerLoginError.expired:
        return 'errLoginExpired';
      case ServerLoginError.deviceLimit:
      case ServerLoginError.server:
        return 'errLoginServer';
    }
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(
        title: Text(l10n.t('loginTitle')),
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
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            _KeyCard(
              domain: _domain,
              keyInput: _key,
              open: _keyFormOpen,
              busy: _busy,
              onToggle: () => setState(() => _keyFormOpen = !_keyFormOpen),
              onSignIn: _signIn,
            ),
            const SizedBox(height: 12),
            _QrCard(onTap: () => Navigator.of(context).push(
                  MaterialPageRoute<void>(
                      builder: (_) => const AddTunnelScreen()),
                )),
            const SizedBox(height: 12),
            _WhereKeyCard(
              open: _whereOpen,
              onToggle: () => setState(() => _whereOpen = !_whereOpen),
            ),
          ],
        ),
      ),
    );
  }
}

/// Card 1: the access-key form (domain + key), collapsible.
class _KeyCard extends StatelessWidget {
  const _KeyCard({
    required this.domain,
    required this.keyInput,
    required this.open,
    required this.busy,
    required this.onToggle,
    required this.onSignIn,
  });

  final TextEditingController domain;
  final TextEditingController keyInput;
  final bool open;
  final bool busy;
  final VoidCallback onToggle;
  final VoidCallback onSignIn;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return FibrCard(
      onTap: onToggle,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.vpn_key_outlined, color: c.secondary),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(l10n.t('loginKeyCard'),
                        style: Theme.of(context).textTheme.titleMedium),
                    Text(
                      l10n.t('loginKeyCardHint'),
                      style: TextStyle(color: c.muted, fontSize: 12),
                    ),
                  ],
                ),
              ),
              Icon(open ? Icons.expand_less : Icons.expand_more, color: c.faint),
            ],
          ),
          if (open) ...[
            const SizedBox(height: 12),
            TextField(
              controller: domain,
              autocorrect: false,
              enableSuggestions: false,
              keyboardType: TextInputType.url,
              decoration: InputDecoration(
                labelText: l10n.t('loginDomainField'),
                hintText: l10n.t('loginDomainHint'),
              ),
              onSubmitted: (_) => onSignIn(),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: keyInput,
              autocorrect: false,
              enableSuggestions: false,
              obscureText: true,
              decoration: InputDecoration(
                labelText: l10n.t('loginKeyField'),
                hintText: l10n.t('loginKeyHint'),
              ),
              onSubmitted: (_) => onSignIn(),
            ),
            const SizedBox(height: 16),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: busy ? null : onSignIn,
                icon: busy
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.login),
                label: Text(busy ? l10n.t('loginBusy') : l10n.t('loginBtn')),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

/// Card 2: the pre-existing QR / join-code flow (opens AddTunnelScreen).
class _QrCard extends StatelessWidget {
  const _QrCard({required this.onTap});

  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return FibrCard(
      onTap: onTap,
      child: Row(
        children: [
          Icon(Icons.qr_code_scanner, color: c.muted),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(l10n.t('loginQrCard'),
                    style: Theme.of(context).textTheme.titleMedium),
                Text(
                  l10n.t('loginQrCardHint'),
                  style: TextStyle(color: c.muted, fontSize: 12),
                ),
              ],
            ),
          ),
          Icon(Icons.chevron_right, color: c.faint),
        ],
      ),
    );
  }
}

/// Card 3: "Where do I get a key?" — a visual step plate for non-technical
/// users (chips with icons, not a wall of text).
class _WhereKeyCard extends StatelessWidget {
  const _WhereKeyCard({required this.open, required this.onToggle});

  final bool open;
  final VoidCallback onToggle;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    return FibrCard(
      onTap: onToggle,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.help_outline, color: c.muted),
              const SizedBox(width: 12),
              Expanded(
                child: Text(l10n.t('loginWhereKey'),
                    style: Theme.of(context).textTheme.titleMedium),
              ),
              Icon(open ? Icons.expand_less : Icons.expand_more, color: c.faint),
            ],
          ),
          if (open) ...[
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(child: _StepChip(icon: Icons.smartphone, label: l10n.t('loginWhereStep1'))),
                Icon(Icons.chevron_right, size: 16, color: c.faint),
                Expanded(child: _StepChip(icon: Icons.person_outline, label: l10n.t('loginWhereStep2'))),
                Icon(Icons.chevron_right, size: 16, color: c.faint),
                Expanded(child: _StepChip(icon: Icons.vpn_key_outlined, label: l10n.t('loginWhereStep3'))),
              ],
            ),
            const SizedBox(height: 8),
            Text(
              l10n.t('loginWhereNote'),
              style: TextStyle(color: c.muted, fontSize: 12),
            ),
          ],
        ],
      ),
    );
  }
}

/// One step of the "where to get a key" plate.
class _StepChip extends StatelessWidget {
  const _StepChip({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) {
    final c = FibrColors.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 10),
      decoration: BoxDecoration(
        color: c.surface2,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: c.hairline),
      ),
      child: Column(
        children: [
          Icon(icon, size: 20, color: c.secondary),
          const SizedBox(height: 4),
          Text(
            label,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            textAlign: TextAlign.center,
            style: TextStyle(
              color: c.ink,
              fontSize: 11,
              fontWeight: FontWeight.w600,
            ),
          ),
        ],
      ),
    );
  }
}
