import 'package:flutter/widgets.dart';

/// Hand-rolled ru/en dictionaries — same wording as `miniapp/app.js`
/// wherever the strings overlap; no codegen, plain Dart maps.
class AppLocalizations {
  const AppLocalizations(this.locale);

  /// Resolved locale (never null-language); falls back to English.
  final Locale locale;

  String get langCode => locale.languageCode == 'ru' ? 'ru' : 'en';

  /// t('key', {vars}) — `{name}` placeholders, like `Fibr.makeT` in design/app.js.
  String t(String key, [Map<String, Object>? vars]) {
    var s = _dicts[langCode]?[key] ?? _en[key] ?? key;
    if (vars != null) {
      for (final e in vars.entries) {
        s = s.replaceAll('{${e.key}}', '${e.value}');
      }
    }
    return s;
  }

  static const Map<String, Map<String, String>> _dicts = {'ru': _ru, 'en': _en};

  static const _ru = {
    'appName': 'Fibr',
    // Tunnels
    'tunnelsTitle': 'Туннели',
    'tunnelsEmpty': 'Пока нет туннелей.\nДобавьте первый — отсканируйте QR-код или вставьте конфиг.',
    'addTunnel': 'Добавить туннель',
    'settings': 'Настройки',
    'serverLabel': 'Сервер',
    'statusConnected': 'подключён',
    'statusConnecting': 'подключение…',
    'statusDisconnecting': 'отключение…',
    'statusDisconnected': 'отключён',
    'statusFailure': 'ошибка',
    // Tunnel detail
    'connect': 'Подключить',
    'disconnect': 'Отключить',
    'connecting': 'Подключение…',
    'session': 'Длительность',
    'rx': '↓ {v}',
    'tx': '↑ {v}',
    'statsUnavailable': 'Статистика недоступна: плагин не отдаёт счётчики',
    'lblEndpoint': 'Endpoint',
    'lblAddress': 'Address',
    'lblDns': 'DNS',
    'lblAllowedIps': 'Allowed IPs',
    'lblServerKey': 'Публичный ключ сервера',
    'errTunnelNotFound': 'Туннель не найден',
    'errConnect': 'Не удалось подключиться: {msg}',
    // Add tunnel
    'addTitle': 'Добавить туннель',
    'importQr': 'Сканировать QR',
    'importPaste': 'Вставить конфиг',
    'scanHint': 'Наведите камеру на QR-код с конфигурацией WireGuard',
    'nameField': 'Название',
    'nameHint': 'Например: Phone',
    'configField': 'Конфигурация wg-quick',
    'configHint': '[Interface]\nPrivateKey = …',
    'save': 'Сохранить',
    'tunnelAdded': 'Туннель добавлен',
    'errInvalidConfig': 'Не удалось разобрать конфиг: {msg}',
    'errQrNotConfig': 'QR не похож на конфиг WireGuard',
    // Common
    'btnConfig': 'Конфиг',
    'btnDelete': 'Удалить',
    'confirmDelete': 'Удалить туннель {name}?',
    'copy': 'Копировать',
    'copied': 'Скопировано',
    'close': 'Закрыть',
    'cancel': 'Отмена',
    // Split tunneling
    'splitTitle': 'Разделение трафика',
    'splitOff': 'Весь трафик в туннеле',
    'splitAllow': 'Только выбранные приложения',
    'splitDeny': 'Кроме выбранных',
    'selectedCount': '{n} выбрано',
    'searchApps': 'Поиск приложений',
    'showSystemApps': 'Системные приложения',
    'iosSplitNote':
        'Per-app VPN на iOS требует MDM-профиль — это ограничение Apple. '
        'В этой версии доступен только полный туннель.',
    // Settings
    'theme': 'Тема',
    'themeSystem': 'Системная',
    'themeDark': 'Тёмная',
    'themeLight': 'Светлая',
    'language': 'Язык',
    'langSystem': 'Как в системе',
    'langRu': 'Русский',
    'langEn': 'English',
    'about': 'О приложении',
    'version': 'Версия',
    'footHint': 'QR также совместим с официальным приложением WireGuard',
    'roadmapNote': 'Синхронизация устройств с сервером Fibr — этап 2.5.',
  };

  static const _en = {
    'appName': 'Fibr',
    // Tunnels
    'tunnelsTitle': 'Tunnels',
    'tunnelsEmpty': 'No tunnels yet.\nAdd one — scan a QR code or paste a config.',
    'addTunnel': 'Add tunnel',
    'settings': 'Settings',
    'serverLabel': 'Server',
    'statusConnected': 'connected',
    'statusConnecting': 'connecting…',
    'statusDisconnecting': 'disconnecting…',
    'statusDisconnected': 'disconnected',
    'statusFailure': 'error',
    // Tunnel detail
    'connect': 'Connect',
    'disconnect': 'Disconnect',
    'connecting': 'Connecting…',
    'session': 'Session',
    'rx': '↓ {v}',
    'tx': '↑ {v}',
    'statsUnavailable': 'Statistics unavailable: the plugin exposes no counters',
    'lblEndpoint': 'Endpoint',
    'lblAddress': 'Address',
    'lblDns': 'DNS',
    'lblAllowedIps': 'Allowed IPs',
    'lblServerKey': 'Server public key',
    'errTunnelNotFound': 'Tunnel not found',
    'errConnect': 'Could not connect: {msg}',
    // Add tunnel
    'addTitle': 'Add tunnel',
    'importQr': 'Scan QR code',
    'importPaste': 'Paste config',
    'scanHint': 'Point the camera at a WireGuard configuration QR code',
    'nameField': 'Name',
    'nameHint': 'For example: Phone',
    'configField': 'wg-quick config',
    'configHint': '[Interface]\nPrivateKey = …',
    'save': 'Save',
    'tunnelAdded': 'Tunnel added',
    'errInvalidConfig': 'Could not parse the config: {msg}',
    'errQrNotConfig': 'The QR code does not look like a WireGuard config',
    // Common
    'btnConfig': 'Config',
    'btnDelete': 'Delete',
    'confirmDelete': 'Delete tunnel {name}?',
    'copy': 'Copy',
    'copied': 'Copied',
    'close': 'Close',
    'cancel': 'Cancel',
    // Split tunneling
    'splitTitle': 'Split tunneling',
    'splitOff': 'All traffic through the tunnel',
    'splitAllow': 'Only selected apps',
    'splitDeny': 'All except selected',
    'selectedCount': '{n} selected',
    'searchApps': 'Search apps',
    'showSystemApps': 'System apps',
    'iosSplitNote':
        'Per-app VPN on iOS requires an MDM profile — an Apple limitation. '
        'Only the full tunnel is available in this version.',
    // Settings
    'theme': 'Theme',
    'themeSystem': 'System',
    'themeDark': 'Dark',
    'themeLight': 'Light',
    'language': 'Language',
    'langSystem': 'System',
    'langRu': 'Русский',
    'langEn': 'English',
    'about': 'About',
    'version': 'Version',
    'footHint': 'The QR also works with the official WireGuard app',
    'roadmapNote': 'Device sync with the Fibr server is stage 2.5.',
  };
}

/// Injected above the Navigator in MaterialApp.builder so every route can do
/// `AppLocalizations.of(context).t('key')` and rebuild on language change.
class L10nProvider extends InheritedWidget {
  const L10nProvider({super.key, required this.l10n, required super.child});

  final AppLocalizations l10n;

  static AppLocalizations of(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<L10nProvider>()!.l10n;

  @override
  bool updateShouldNotify(L10nProvider oldWidget) =>
      oldWidget.l10n.locale != l10n.locale;
}
