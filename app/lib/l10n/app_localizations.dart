import 'package:flutter/widgets.dart';

/// Hand-rolled ru/en dictionaries — same wording as `miniapp/app.js`
/// wherever the strings overlap; no codegen, plain Dart maps.
class AppLocalizations {
  const AppLocalizations(this.locale);

  /// Resolved locale (never null-language); falls back to English.
  final Locale locale;

  String get langCode => locale.languageCode == 'ru' ? 'ru' : 'en';

  /// Shorthand for L10nProvider.of so screens write
  /// AppLocalizations.of(context).t('key').
  static AppLocalizations of(BuildContext context) => L10nProvider.of(context);

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
    'importPaste': 'Вставить / файл',
    'importFromClipboard': 'Из буфера',
    'importFromFile': 'Выбрать файл',
    'importAnyHint': 'Подойдёт файл .conf, ссылка-приглашение на устройство, код подключения или текст конфига. Файл из Telegram можно открыть сразу: «Открыть в…» → Fibr.',
    'errImportEmpty': 'Буфер обмена пуст',
    'errImportUnknown': 'Не похоже ни на конфиг, ни на ссылку или код подключения',
    'loginFileCard': 'Файл или вставка',
    'loginFileCardHint': 'Файл .conf из Telegram, скопированная ссылка или конфиг',
    'scanHint': 'Наведите камеру на QR-код с конфигурацией WireGuard',
    'nameField': 'Название',
    'nameHint': 'Например: Phone',
    'configField': 'Конфигурация wg-quick',
    'configHint': '[Interface]\nPrivateKey = …',
    'save': 'Сохранить',
    'tunnelAdded': 'Туннель добавлен',
    'dupTitle': 'Это устройство уже добавлено',
    'dupBody':
        'Туннель «{name}» создан для этого же устройства (совпадает ключ). '
        'Обновить его новым конфигом или добавить отдельный туннель?',
    'dupUpdate': 'Обновить существующий',
    'dupAddNew': 'Добавить новый',
    'tunnelUpdated': 'Туннель «{name}» обновлён',
    'tunnelUpdatedReconnect':
        'Туннель «{name}» обновлён — переподключитесь, чтобы применить новый сервер',
    'errInvalidConfig': 'Не удалось разобрать конфиг: {msg}',
    'errQrNotConfig': 'QR не похож на конфиг WireGuard',
    // Quick-connect (join codes)
    'importCode': 'По коду',
    'joinHint':
        'Вставьте ссылку из Mini App («Код» у устройства) или введите код и адрес сервера.',
    'joinCodeField': 'Ссылка или код',
    'joinCodeHint': 'https://vpn.example.com/join/Ab3xK9',
    'joinServerField': 'Сервер (для ввода только кода)',
    'joinServerHint': 'vpn.example.com',
    'joinBtn': 'Получить конфиг',
    'joinBusy': 'Обмениваю код…',
    'errJoinNotFound': 'Код не найден — проверьте ввод',
    'errJoinGone': 'Код уже использован или истёк — попросите новый',
    'errJoinForbidden': 'Подписка аккаунта истекла или устройство отключено',
    'errJoinNetwork': 'Сервер недоступен — проверьте адрес и сеть',
    'errJoinBad': 'Не похоже на ссылку или код подключения',
    'errJoinVless':
        'Это Xray-устройство: приложение работает с WireGuard, ссылка vless:// — на странице кода',
    // DNS protection
    'dnsTitle': 'DNS-защита',
    'dnsProtected': 'Защита включена',
    'dnsOrdinary': 'Обычный DNS',
    'dnsProfileHint': 'Профиль меняется в Mini App или админке',
    'dnsProfileAdblock': 'adblock — блокировка рекламы и трекеров',
    'dnsProfileFamily': 'family — реклама, взрослый контент и SafeSearch',
    'dnsProfileUnknown': 'Профиль устройства неизвестен',
    // Game mode
    'gameMode': 'Игровой режим',
    'gameNeedBaseTunnel': 'Сначала добавьте и включите основной туннель',
    'gameIntro':
        'Отдельный туннель только для игрового трафика: выберите игры — '
        'и через VPN пойдут только их серверы, всё остальное — напрямую.',
    'gameSwitchHint':
        'Два туннеля с одного пира — нормально для WireGuard; перед игрой '
        'включайте игровой вместо основного',
    'gameSnapshotNote': 'Список игр — офлайн-снапшот; сервер отдаёт актуальный',
    'gameCidrCount': '{n} подсетей',
    'gameCreate': 'Создать игровой туннель',
    'gameCreating': 'Создаю…',
    'gameTunnelCreated': 'Игровой туннель создан',
    'gameNoRoutes': 'Не удалось получить маршруты игр',
    'gameCidrLimit': 'Слишком много подсетей ({n}) — обрезано до 180',
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
    'splitNotAppliedYet':
        'Выбор сохраняется, но пока не влияет на VPN: плагин WireGuard ещё '
        'не даёт API для per-app режимов. Появится в следующей версии.',
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
    // Server login (simple mode)
    'loginTitle': 'Вход',
    'loginKeyCard': 'Ключ доступа',
    'loginKeyCardHint': 'Домен сервера и ключ из мини-приложения',
    'loginDomainField': 'Домен',
    'loginDomainHint': 'vpnoleg.de',
    'loginKeyField': 'Ключ',
    'loginKeyHint': '32 символа',
    'loginBtn': 'Войти',
    'loginBusy': 'Вхожу…',
    'loginQrCard': 'QR / join-код',
    'loginQrCardHint': 'Как раньше — камера или код подключения',
    'loginWhereKey': 'Где взять ключ?',
    'loginWhereStep1': 'Мини-приложение',
    'loginWhereStep2': 'Профиль',
    'loginWhereStep3': 'Ключ для приложения',
    'loginWhereNote': 'Нажмите «Сгенерировать» и скопируйте ключ — он показывается один раз.',
    'errLoginBadDomain': 'Похоже, это не адрес сервера',
    'errLoginNetwork': 'Сервер недоступен — проверьте домен и сеть',
    'errLoginInvalidKey': 'Неверный ключ',
    'errLoginDisabled': 'Аккаунт отключён администратором',
    'errLoginExpired': 'Подписка истекла — свяжитесь с администратором',
    'errLoginServer': 'Ошибка сервера — попробуйте ещё раз',
    // Simple-mode home
    'homeDevices': 'Устройства',
    'homeNoDevice': 'Не выбрано устройство — нажмите «Устройства»',
    'online': 'онлайн',
    'signedInAs': 'Вы вошли как {name}',
    // Devices (simple mode)
    'addDevice': '+ Устройство',
    'addDeviceTitle': 'Новое устройство',
    'deviceAddBusy': 'Создаю…',
    'deviceAdded': 'Устройство добавлено',
    'deviceDeleted': 'Устройство удалено',
    'devicesEmptySimple': 'Пока нет устройств.\nДобавьте первое — конфиг создастся сам.',
    'confirmDeleteDevice': 'Удалить устройство {name}? Конфиг на сервере будет отозван.',
    'errDeviceLimit': 'Лимит устройств исчерпан',
    'sessionExpired': 'Сессия истекла — войдите заново',
    'nameRequired': 'Введите название устройства — например, «Телефон» или «Ноутбук»',
    'btnCreate': 'Создать',
    // Xray honesty plate (no tunnel in this client)
    'xrayPlate': 'Туннель недоступен: это Xray-устройство — откройте его ссылку в v2rayNG/Hiddify',
    'awgPlate': 'Туннель недоступен: это устройство AmneziaWG — его файл открывается в приложении AmneziaVPN',
    'xrayCreatedPlate':
        'Устройство Xray создано. Ссылку vless:// возьмите в мини-приложении (QR у устройства) — приложение Fibr подключает только WireGuard.',
    // Account section in settings
    'accountSection': 'Аккаунт',
    'signOut': 'Выйти из аккаунта',
    'connectOnLaunch': 'Подключаться при запуске',
    'connectOnLaunchHint': 'Активное устройство поднимется сразу после запуска',
    // TV UI (feature 2)
    'tvJoinTitle': 'Подключение по коду',
    'howToTitle': 'Как подключиться',
    'howToIntro': 'Выберите удобный способ и откройте его. Одно устройство = один гаджет: для каждого телефона, ноутбука или ТВ — свой файл или код.',
    'howToCard': 'Как подключиться?',
    'howToCardHint': 'Пошагово: файл, QR, код, AmneziaVPN, телевизор',
    'tvPairTitle': 'Подключить с телефона',
    'tvPairHint': 'Отсканируйте QR камерой телефона — откроется бот Fibr. Или в приложении Fibr в Telegram нажмите «Подключить телевизор» и введите этот код.',
    'tvPairNeedServer': 'Введите адрес сервера ниже один раз — и здесь появится QR для подключения с телефона.',
    'tvPairFailed': 'Не удалось получить QR от сервера. Проверьте адрес сервера и нажмите «Новый QR».',
    'tvPairNew': 'Новый QR',
    'tvJoinHint':
        'Возьмите код в мини-приложении (Устройства → Код) и введите его с пульта. '
        'Сначала — адрес сервера, затем код.',
    'tvServerField': 'Сервер',
    'tvCodeField': 'Код подключения',
    'tvPickDevice': 'Выбрать устройство',
    'tvNoDevicesAccount': 'Нет устройств — добавьте устройство в мини-приложении',
    'tvNoTunnels': 'Нет туннелей — введите код подключения',
    'tvAddByCode': 'Добавить по коду',
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
    'importPaste': 'Paste / file',
    'importFromClipboard': 'From clipboard',
    'importFromFile': 'Choose file',
    'importAnyHint': 'A .conf file, a device join link, a quick-connect code or config text all work. A file from Telegram opens directly: "Open in…" → Fibr.',
    'errImportEmpty': 'The clipboard is empty',
    'errImportUnknown': 'This is neither a config nor a join link or code',
    'loginFileCard': 'File or paste',
    'loginFileCardHint': 'A .conf from Telegram, a copied link or config',
    'scanHint': 'Point the camera at a WireGuard configuration QR code',
    'nameField': 'Name',
    'nameHint': 'For example: Phone',
    'configField': 'wg-quick config',
    'configHint': '[Interface]\nPrivateKey = …',
    'save': 'Save',
    'tunnelAdded': 'Tunnel added',
    'dupTitle': 'This device is already added',
    'dupBody':
        'Tunnel "{name}" was created for this same device (same key). '
        'Update it with the new config, or add a separate tunnel?',
    'dupUpdate': 'Update existing',
    'dupAddNew': 'Add as new',
    'tunnelUpdated': 'Tunnel "{name}" updated',
    'tunnelUpdatedReconnect':
        'Tunnel "{name}" updated — reconnect to apply the new server',
    'errInvalidConfig': 'Could not parse the config: {msg}',
    'errQrNotConfig': 'The QR code does not look like a WireGuard config',
    // Quick-connect (join codes)
    'importCode': 'By code',
    'joinHint':
        'Paste the link from the Mini App (the "Code" button on a device), or type the code and the server address.',
    'joinCodeField': 'Link or code',
    'joinCodeHint': 'https://vpn.example.com/join/Ab3xK9',
    'joinServerField': 'Server (when typing just the code)',
    'joinServerHint': 'vpn.example.com',
    'joinBtn': 'Get config',
    'joinBusy': 'Exchanging the code…',
    'errJoinNotFound': 'Code not found — check the input',
    'errJoinGone': 'The code was already used or has expired — ask for a new one',
    'errJoinForbidden': 'The subscription expired or the device is disabled',
    'errJoinNetwork': 'Server unreachable — check the address and network',
    'errJoinBad': 'This does not look like a join link or code',
    'errJoinVless':
        'This is an Xray device: the app speaks WireGuard; the vless:// link is on the code page',
    // DNS protection
    'dnsTitle': 'DNS protection',
    'dnsProtected': 'Protection on',
    'dnsOrdinary': 'Ordinary DNS',
    'dnsProfileHint': 'The profile is changed in the Mini App or the admin panel',
    'dnsProfileAdblock': 'adblock — blocks ads and trackers',
    'dnsProfileFamily': 'family — ads, adult content and SafeSearch',
    'dnsProfileUnknown': 'Device profile unknown',
    // Game mode
    'gameMode': 'Game mode',
    'gameNeedBaseTunnel': 'First add and connect the base tunnel',
    'gameIntro':
        'A separate tunnel for game traffic only: pick games and only their '
        'servers go through the VPN — everything else stays direct.',
    'gameSwitchHint':
        'Two tunnels from one peer is normal for WireGuard; before playing, '
        'turn on the game tunnel instead of the base one',
    'gameSnapshotNote': 'This game list is an offline snapshot; the server serves the up-to-date one',
    'gameCidrCount': '{n} subnets',
    'gameCreate': 'Create game tunnel',
    'gameCreating': 'Creating…',
    'gameTunnelCreated': 'Game tunnel created',
    'gameNoRoutes': 'Could not fetch game routes',
    'gameCidrLimit': 'Too many subnets ({n}) — trimmed to 180',
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
    'splitNotAppliedYet':
        'The selection is saved but does not shape the VPN yet: the '
        'WireGuard plugin exposes no per-app API. Coming in a later version.',
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
    // Server login (simple mode)
    'loginTitle': 'Sign in',
    'loginKeyCard': 'Access key',
    'loginKeyCardHint': 'The server domain and the key from the Mini App',
    'loginDomainField': 'Domain',
    'loginDomainHint': 'vpnoleg.de',
    'loginKeyField': 'Key',
    'loginKeyHint': '32 characters',
    'loginBtn': 'Sign in',
    'loginBusy': 'Signing in…',
    'loginQrCard': 'QR / join code',
    'loginQrCardHint': 'As before — camera or quick-connect code',
    'loginWhereKey': 'Where do I get a key?',
    'loginWhereStep1': 'Mini App',
    'loginWhereStep2': 'Profile',
    'loginWhereStep3': 'App key',
    'loginWhereNote': 'Tap "Generate" and copy the key — it is shown only once.',
    'errLoginBadDomain': 'This does not look like a server address',
    'errLoginNetwork': 'Server unreachable — check the domain and network',
    'errLoginInvalidKey': 'Invalid key',
    'errLoginDisabled': 'Account disabled by the administrator',
    'errLoginExpired': 'Subscription expired — contact the administrator',
    'errLoginServer': 'Server error — try again',
    // Simple-mode home
    'homeDevices': 'Devices',
    'homeNoDevice': 'No device selected — tap "Devices"',
    'online': 'online',
    'signedInAs': 'Signed in as {name}',
    // Devices (simple mode)
    'addDevice': '+ Device',
    'addDeviceTitle': 'New device',
    'deviceAddBusy': 'Creating…',
    'deviceAdded': 'Device added',
    'deviceDeleted': 'Device deleted',
    'devicesEmptySimple': 'No devices yet.\nAdd one — the config is created for you.',
    'confirmDeleteDevice': 'Delete device {name}? Its server config will be revoked.',
    'errDeviceLimit': 'Device limit reached',
    'sessionExpired': 'Session expired — sign in again',
    'nameRequired': 'Enter a device name — e.g. “Phone” or “Laptop”',
    'btnCreate': 'Create',
    // Xray honesty plate (no tunnel in this client)
    'xrayPlate': 'No tunnel here: this is an Xray device — open its link in v2rayNG/Hiddify',
    'awgPlate': 'No tunnel here: this is an AmneziaWG device — its file opens in the AmneziaVPN app',
    'xrayCreatedPlate':
        'Xray device created. Take the vless:// link from the Mini App (the device QR) — the Fibr app tunnels WireGuard only.',
    // Account section in settings
    'accountSection': 'Account',
    'signOut': 'Sign out',
    'connectOnLaunch': 'Connect on launch',
    'connectOnLaunchHint': 'The active device comes up right after launch',
    // TV UI (feature 2)
    'tvJoinTitle': 'Connect with a code',
    'howToTitle': 'How to connect',
    'howToIntro': 'Pick the way that suits you and open it. One device = one gadget: every phone, laptop or TV gets its own file or code.',
    'howToCard': 'How to connect?',
    'howToCardHint': 'Step by step: file, QR, code, AmneziaVPN, TV',
    'tvPairTitle': 'Connect from your phone',
    'tvPairHint': 'Scan the QR with your phone camera — the Fibr bot opens. Or in the Fibr app in Telegram tap “Connect a TV” and type this code.',
    'tvPairNeedServer': 'Type the server address below once — a QR for connecting from your phone appears here.',
    'tvPairFailed': 'Could not get a QR from the server. Check the server address and press “New QR”.',
    'tvPairNew': 'New QR',
    'tvJoinHint':
        'Take the code from the Mini App (Devices → Code) and type it with the '
        'remote. First the server address, then the code.',
    'tvServerField': 'Server',
    'tvCodeField': 'Join code',
    'tvPickDevice': 'Choose a device',
    'tvNoDevicesAccount': 'No devices yet — add one in the Mini App',
    'tvNoTunnels': 'No tunnels yet — enter a join code',
    'tvAddByCode': 'Add with a code',
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
