import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../l10n/app_localizations.dart';
import '../services/server_api_service.dart';
import '../services/settings_service.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';

/// Kept in sync with pubspec.yaml `version`.
const appVersion = '0.2.0';

class SettingsScreen extends StatelessWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final settings = context.watch<SettingsController>();
    final account = context.watch<ServerAccountController>();
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);

    return Scaffold(
      appBar: AppBar(title: Text(l10n.t('settings'))),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          // Simple-mode account card (server login): who is signed in, the
          // connect-on-launch toggle and sign-out. Hidden for manual-mode
          // users (no account).
          if (account.isSignedIn) ...[
            FibrCard(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(12, 8, 12, 0),
                    child: Text(l10n.t('accountSection'),
                        style: Theme.of(context).textTheme.titleSmall),
                  ),
                  Padding(
                    padding: const EdgeInsets.fromLTRB(12, 4, 12, 0),
                    child: Text(
                      '${l10n.t('signedInAs', {
                        'name': account.me?.username ?? account.domain ?? ''
                      })}\n${account.domain ?? ''}',
                      style: TextStyle(color: c.muted, fontSize: 12),
                    ),
                  ),
                  SwitchListTile(
                    value: settings.connectOnLaunch,
                    onChanged: (v) => settings.setConnectOnLaunch(v),
                    title: Text(l10n.t('connectOnLaunch')),
                    subtitle: Text(
                      l10n.t('connectOnLaunchHint'),
                      style: TextStyle(color: c.faint, fontSize: 12),
                    ),
                  ),
                  Divider(height: 1, color: c.hairline),
                  ListTile(
                    leading: Icon(Icons.logout, color: c.danger),
                    title: Text(l10n.t('signOut'),
                        style: TextStyle(color: c.danger)),
                    onTap: () async {
                      await account.signOut();
                      // AppRoot swaps to Login/TunnelList; leave this pushed
                      // screen so nothing dangles above the new root.
                      if (!context.mounted) return;
                      Navigator.of(context).popUntil((r) => r.isFirst);
                    },
                  ),
                ],
              ),
            ),
            const SizedBox(height: 8),
          ],
          FibrCard(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(12, 8, 12, 0),
                  child: Text(l10n.t('theme'),
                      style: Theme.of(context).textTheme.titleSmall),
                ),
                RadioListTile<ThemeMode>(
                  value: ThemeMode.system,
                  groupValue: settings.themeMode,
                  onChanged: (v) => settings.setThemeMode(v ?? ThemeMode.system),
                  title: Text(l10n.t('themeSystem')),
                ),
                RadioListTile<ThemeMode>(
                  value: ThemeMode.dark,
                  groupValue: settings.themeMode,
                  onChanged: (v) => settings.setThemeMode(v ?? ThemeMode.dark),
                  title: Text(l10n.t('themeDark')),
                ),
                RadioListTile<ThemeMode>(
                  value: ThemeMode.light,
                  groupValue: settings.themeMode,
                  onChanged: (v) => settings.setThemeMode(v ?? ThemeMode.light),
                  title: Text(l10n.t('themeLight')),
                ),
              ],
            ),
          ),
          const SizedBox(height: 8),
          FibrCard(
            padding: const EdgeInsets.symmetric(vertical: 8),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(12, 8, 12, 0),
                  child: Text(l10n.t('language'),
                      style: Theme.of(context).textTheme.titleSmall),
                ),
                RadioListTile<AppLanguageMode>(
                  value: AppLanguageMode.system,
                  groupValue: settings.language,
                  onChanged: (v) =>
                      settings.setLanguage(v ?? AppLanguageMode.system),
                  title: Text(l10n.t('langSystem')),
                ),
                RadioListTile<AppLanguageMode>(
                  value: AppLanguageMode.ru,
                  groupValue: settings.language,
                  onChanged: (v) => settings.setLanguage(v ?? AppLanguageMode.ru),
                  title: Text(l10n.t('langRu')),
                ),
                RadioListTile<AppLanguageMode>(
                  value: AppLanguageMode.en,
                  groupValue: settings.language,
                  onChanged: (v) => settings.setLanguage(v ?? AppLanguageMode.en),
                  title: Text(l10n.t('langEn')),
                ),
              ],
            ),
          ),
          const SizedBox(height: 8),
          FibrCard(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(l10n.t('about'),
                    style: Theme.of(context).textTheme.titleSmall),
                const SizedBox(height: 8),
                Row(
                  children: [
                    Expanded(
                        child: Text(l10n.t('version'),
                            style: TextStyle(color: c.muted))),
                    const Text(appVersion),
                  ],
                ),
                const SizedBox(height: 8),
                Text(
                  l10n.t('footHint'),
                  style: TextStyle(color: c.muted, fontWeight: FontWeight.w600),
                ),
                const SizedBox(height: 4),
                Text(
                  l10n.t('roadmapNote'),
                  style: TextStyle(color: c.faint, fontSize: 12),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
