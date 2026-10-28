import 'dart:async';

import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import 'l10n/app_localizations.dart';
import 'screens/add_tunnel_screen.dart';
import 'screens/home_screen.dart';
import 'screens/login_screen.dart';
import 'screens/tunnel_list_screen.dart';
import 'services/deep_link_service.dart';
import 'services/join_service.dart';
import 'services/server_api_service.dart';
import 'services/settings_service.dart';
import 'services/tunnel_service.dart';
import 'theme/app_theme.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();

  final settings = SettingsController();
  await settings.load();

  final account = ServerAccountController();
  await account.load();

  final tunnels = TunnelController();
  await tunnels.load();
  await tunnels.start(); // engine init; failures land in tunnels.lastError

  // Simple mode: bring the active device up right after launch (the toggle
  // lives in Settings). Failures are silent here — the home screen shows
  // the disconnected state and its own retry.
  if (account.isSignedIn && settings.connectOnLaunch) {
    unawaited(account.connectActiveDevice(tunnels));
  }

  runApp(FibrApp(
    settings: settings,
    tunnels: tunnels,
    account: account,
  ));
}

class FibrApp extends StatefulWidget {
  const FibrApp({
    super.key,
    required this.settings,
    required this.tunnels,
    required this.account,
  });

  final SettingsController settings;
  final TunnelController tunnels;
  final ServerAccountController account;

  @override
  State<FibrApp> createState() => _FibrAppState();
}

class _FibrAppState extends State<FibrApp> {
  final _navKey = GlobalKey<NavigatorState>();
  StreamSubscription<String>? _linkSub;

  @override
  void initState() {
    super.initState();
    // fibr://import/<base64> — from a cold start and while running.
    WidgetsBinding.instance.addPostFrameCallback((_) async {
      final initial = await DeepLinkService.initialLink();
      if (initial != null) _handleLink(initial);
    });
    _linkSub = DeepLinkService.links().listen(_handleLink);
  }

  @override
  void dispose() {
    _linkSub?.cancel();
    widget.settings.dispose();
    widget.account.dispose();
    widget.tunnels.dispose();
    super.dispose();
  }

  void _handleLink(String url) {
    // fibr://join/<base64 url> — quick-connect landing button: exchange the
    // code for the config (docs/join.md). The https join link itself may
    // also arrive here from other apps.
    final joinUrl =
        DeepLinkService.decodeJoinLink(url) ?? JoinService.extractJoinUrl(url);
    if (joinUrl != null) {
      _navKey.currentState?.push(
        MaterialPageRoute<void>(
          builder: (_) => AddTunnelScreen(initialJoinUrl: joinUrl),
        ),
      );
      return;
    }
    final config = DeepLinkService.decodeImportLink(url);
    if (config == null) return;
    _navKey.currentState?.push(
      MaterialPageRoute<void>(
        builder: (_) => AddTunnelScreen(initialConfig: config),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    return MultiProvider(
      providers: [
        ChangeNotifierProvider<SettingsController>.value(value: widget.settings),
        ChangeNotifierProvider<TunnelController>.value(value: widget.tunnels),
        ChangeNotifierProvider<ServerAccountController>.value(
            value: widget.account),
      ],
      child: Consumer<SettingsController>(
        builder: (context, settings, _) {
          final locale = settings.resolvedLocale;
          final l10n = AppLocalizations(locale ?? const Locale('en'));
          return MaterialApp(
            title: 'Fibr',
            debugShowCheckedModeBanner: false,
            theme: AppTheme.light(),
            darkTheme: AppTheme.dark(),
            themeMode: settings.themeMode,
            locale: locale,
            supportedLocales: const [Locale('en'), Locale('ru')],
            navigatorKey: _navKey,
            builder: (context, child) =>
                L10nProvider(l10n: l10n, child: child ?? const SizedBox.shrink()),
            home: const AppRoot(),
          );
        },
      ),
    );
  }
}

/// Decides what the app opens into:
///  - a signed-in account → the simple-mode Home screen;
///  - otherwise manually imported tunnels → the classic tunnel list
///    (existing users keep their app);
///  - otherwise (first launch) → the sign-in screen.
class AppRoot extends StatelessWidget {
  const AppRoot({super.key});

  @override
  Widget build(BuildContext context) {
    final account = context.watch<ServerAccountController>();
    if (account.isSignedIn) return const HomeScreen();
    if (context.watch<TunnelController>().tunnels.isNotEmpty) {
      return const TunnelListScreen();
    }
    return const LoginScreen();
  }
}
