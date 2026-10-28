# Fibr client (Flutter) — этап 2

Мобильный клиент Fibr: standalone-менеджер WireGuard-туннелей — QR-импорт,
вставка конфига, deep link, подключение/отключение, split tunneling
(Android), тёмная/светлая тема, ru/en. Без логина на сервер в этой итерации:
любой валидный wg-конфиг (в т.ч. QR из Mini App или админки Fibr — это
обычный wg-quick) импортируется и работает.

> **Честное предупреждение:** код написан вручную и **не собирался** — на
> машине-авторе нет Flutter SDK. Цель — полный связный проект; возможны
> мелкие несовпадения сигнатур плагинов/Gradle, чинятся по месту.
> Структура платформенных папок минимальная — недостающие файлы допишет
> `flutter create .` (см. ниже).

## Что внутри

| Возможность | Статус |
|---|---|
| Список туннелей (имя, сервер, статус) | готово |
| Импорт: QR камерой (`mobile_scanner`) | готово |
| Импорт: вставка текста wg-конфига | готово |
| Импорт: `fibr://import/<base64>` | готово (Android — из коробки; iOS — вставить сниппет в AppDelegate, см. ниже) |
| Парсер wg-quick `[Interface]/[Peer]` | свой, с unit-тестами (`test/wg_config_parser_test.dart`) |
| Подключение/отключение | через `wireguard_flutter`, за интерфейсом `TunnelEngine` |
| Статусы (connecting/connected/…) | готово, поток из плагина |
| rx/tx и длительность сессии | **заглушка**: длительность считается локально, rx/tx — `—` (плагин не отдаёт счётчики; TODO в `WireGuardFlutterEngine.readStats`) |
| Split tunneling | экран и хранение выбора (allow/deny) готовы; **применение к VPN — заглушка** (у `wireguard_flutter` 0.1.x нет per-app API; TODO в `WireGuardFlutterEngine.connect`). iOS — информационное сообщение про MDM |
| DNS-защита (статус) | карточка в туннеле: вердикт по DNS конфига (шлюз подсети) + уточнение `GET /api/dns/status` через шлюз (`docs/dns-games.md`); профиль меняется в Mini App/админке |
| Игровой режим | второй туннель с AllowedIPs выбранных игр: каталог `GET /api/games` + офлайн-снапшот, объединение CIDR с лимитом 180 (`game_mode_screen.dart`) |
| Настройки: тема/язык | готово, `shared_preferences` |
| Синхронизация с сервером | этап 2.5 (см. дорожную карту) |

Ограничение плагина: подключение идёт через **первого пира с Endpoint**;
конфиги с несколькими пирами парсятся и хранятся полностью, но активен
только основной сервер.

## Структура

```
lib/
  main.dart                      точка входа, провайдеры, deep-link роутинг
  l10n/app_localizations.dart    словари ru/en (карты, без codegen) + L10nProvider
  theme/app_theme.dart           FibrColors (токены из design/tokens.css) + ThemeData
  models/tunnel.dart             модель туннеля (+JSON для shared_preferences)
  models/game.dart               каталог игр (модель + офлайн-снапшот CIDR)
  services/
    wg_config_parser.dart        парсер wg-quick (свой) + render() для
                                 производных конфигов, шлюз подсети/DNS-вердикт
    tunnel_service.dart          TunnelEngine (абстракция) + WireGuardFlutterEngine
                                 + TunnelController (список, статусы, persistence)
    settings_service.dart        SettingsController: тема/язык
    deep_link_service.dart       fibr://import/<base64> (MethodChannel/EventChannel)
    app_list_service.dart        список приложений для split tunneling (Android)
    server_api_service.dart      публичные ручки сервера через шлюз:
                                 /api/dns/status, /api/games, /api/games/routes
                                 (fail-soft: ошибки → null/пусто)
  screens/
    tunnel_list_screen.dart      список + FAB добавления + кнопка игрового режима
    add_tunnel_screen.dart       QR / вставка / подтверждение с разбором
    tunnel_detail_screen.dart    статус, сессия, инфо, DNS-защита, конфиг, удаление
    split_apps_screen.dart       выбор приложений (Android) / инфо (iOS)
    game_mode_screen.dart        игровой туннель из CIDR выбранных игр
    settings_screen.dart         тема, язык, about
  widgets/fibr_widgets.dart      FibrCard (16px, hairline), StatusPill (999), StatusDot, InfoRow
test/
  wg_config_parser_test.dart     обычный конфиг, «грязный», несколько [Peer], ошибки;
                                 шлюз подсети + DNS-вердикт, render (round-trip)
android/                         минимальный скаффолдинг (см. ниже) — gradle, манифест,
                                 MainActivity.kt (deep link + список приложений), res
ios/Runner/                      Info.plist (fibr-scheme, камера) + Runner.entitlements
```

## Сборка

### 1. Установить Flutter SDK

Stable-канал, **3.27+** (Dart ≥ 3.4; Gradle-файлы соответствуют шаблону
3.27+: AGP 8.7.3 — если ваш Flutter старее, понизьте `com.android.application`
в `android/settings.gradle`, напр. до 8.1.0):

```bash
# macOS (или см. https://docs.flutter.dev/get-started/install)
brew install --cask flutter
flutter doctor
```

### 2. Дописать платформенные папки

`android/` уже содержит ключевые файлы (манифест, gradle, MainActivity).
`flutter create .` **не перезаписывает существующие файлы** (без
`--overwrite`) и допишет недостающее: gradle wrapper, иконки, тестовый
gradle-файл, Xcode-проект и т.д.

```bash
cd app
flutter pub get
flutter create --platforms=android,ios .
```

Проверьте после генерации, что `android/app/src/main/AndroidManifest.xml`
и `ios/Runner/Info.plist` — наши (с CAMERA/VPN_SERVICE и CFBundleURLTypes
соответственно). Если что-то затёрлось — diff с git.

### 3. Шрифты (Inter 600/800)

Скачайте Inter (например, https://github.com/rsms/inter/releases) и положите
`Inter-SemiBold.ttf` и `Inter-ExtraBold.ttf` в `app/assets/fonts/`, затем
раскомментируйте секцию `fonts:` в `pubspec.yaml`. Без ассетов приложение
работает на системном шрифте.

### 4. Android

```bash
flutter run             # девайс/эмулятор с камерой для QR
flutter build apk --release
```

Манифест уже содержит: `CAMERA` (QR), `INTERNET`, `VPN_SERVICE`,
`QUERY_ALL_PACKAGES` (список приложений для split tunneling; для
Play Store это чувствительное разрешение — читайте политику, для
self-hosted/sideload-дистрибуции Fibr это нормально), intent-filter
`fibr://import`, `launchMode=singleTask`. `BIND_VPN_SERVICE` вешается на
сервис самого плагина через manifest-merge.

Если Gradle wrapper не создался автоматически: открыть проект в Android
Studio один раз или `cd android && gradle wrapper --gradle-version 8.9`.

### 5. iOS (ручная настройка в Xcode)

Требуется macOS + Xcode. После `flutter create` (шаг 2):

1. **Deployment target 15.0+** — iOS-часть `wireguard_flutter` требует
   iOS 15 (поставьте в Xcode: Runner target → General → Minimum
   Deployments, и в Podfile `platform :ios, '15.0'`).
2. **Entitlements**: в `ios/Runner/` лежит готовый `Runner.entitlements`
   (`com.apple.developer.network-extension` → `packet-tunnel-provider`).
   В Xcode: Runner target → Signing & Capabilities → убедитесь, что
   указан этот файл (Build Settings → Code Signing Entitlements =
   `Runner/Runner.entitlements`), и добавьте capability *Network
   Extensions* → *Packet Tunnel*. Нужен платный Apple ID-аккаунт.
3. **Network Extension target**: `wireguard_flutter` на iOS поднимает
   туннель через WireGuardKit в packet-tunnel-провайдере. Создайте target
   *Network Extension → Packet Tunnel Provider* (bundle id вида
   `com.fibr.app.tunnel`) и свяжите его с плагином по инструкции
   примера плагина: https://github.com/Caqil/wireguard_flutter —
   id провайдера передаётся из Dart: константа
   `WireGuardFlutterEngine.providerBundleIdentifier`
   (`lib/services/tunnel_service.dart`). **Этот шаг автор не проверял на
   устройстве** — ориентируйтесь на example плагина.
4. **Deep link**: в `ios/Runner/Info.plist` CFBundleURLTypes уже
   зарегистрирован. Для доставки ссылок в Dart добавьте в
   `ios/Runner/AppDelegate.swift` (после `GeneratedPluginRegistrant`):

```swift
import UIKit
import Flutter

@main
@objc class AppDelegate: FlutterAppDelegate {
  private var linkSink: FlutterEventSink?
  private var initialLink: String?

  override func application(
    _ application: UIApplication,
    didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?
  ) -> Bool {
    GeneratedPluginRegistrant.register(with: self)
    if let controller = window?.rootViewController as? FlutterViewController {
      let messenger = controller.binaryMessenger
      MethodChannel(name: "fibr/deeplink", binaryMessenger: messenger)
        .setMethodCallHandler { [weak self] call, result in
          if call.method == "getInitialLink" {
            result(self?.initialLink)
          } else {
            result(FlutterMethodNotImplemented)
          }
        }
      EventChannel(name: "fibr/deeplink/events", binaryMessenger: messenger)
        .setStreamHandler { [weak self] _, sink in
          self?.linkSink = sink
          return nil
        }
    }
    return super.application(application, didFinishLaunchingWithOptions: launchOptions)
  }

  // fibr:// приходит сюда и на cold start (сохраните его в initialLink,
  // если Dart ещё не слушал), и на warm start (отдаём в поток).
  override func application(
    _ app: UIApplication,
    open url: URL,
    options: [UIApplication.OpenURLOptionsKey: Any] = [:]
  ) -> Bool {
    if let sink = linkSink {
      sink(url.absoluteString)
    } else {
      initialLink = url.absoluteString
    }
    return super.application(app, open: url, options: options)
  }
}
```

   (Сниппет ориентировочный: доведите под свой сценарий cold start —
   сохраните `url.absoluteString` в свойство и верните его из
   `getInitialLink`, если ссылка пришла до старта Dart-стороны. Пока
   сниппета нет, deep link на iOS просто не срабатывает — приложение
   работает как обычно.)

### 6. Тесты

```bash
flutter test test/wg_config_parser_test.dart
```

Парсер покрывает: обычный конфиг; лишние пробелы/комментарии/CRLF/регистр
ключей; несколько `[Peer]`; wg-quick-опции (Table/PostUp), которые
игнорируются; ошибки (нет секций/ключей, мусорные строки, дубли
`[Interface]`).

## Deep link: формат

```
fibr://import/<base64 wg-quick конфига>
```

- base64 — стандартный или URL-safe алфавит (URL-safe без паддинга
  рекомендуется), можно percent-encode; декодер принимает оба (`DeepLinkService.decodeImportLink`).
- Сгенерировать:

```bash
B64=$(base64 -w0 < phone.conf | tr '/+' '_-' | tr -d '=')
echo "fibr://import/$B64"
```

- Открытие ссылки: Android — через браузер/мессенджер (intent-filter
  `fibr` + host `import`); iOS — CFBundleURLTypes. Приложение показывает
  экран добавления с разобранным конфигом и предложенным именем (хост
  endpoint).

## Дизайн

Токены — [`../design/tokens.css`](../design/tokens.css) и спецификация [`../docs/DESIGN.md`](../docs/DESIGN.md), перенесены в
`lib/theme/app_theme.dart` (`FibrColors.dark` / `FibrColors.light`):
canvas `#141415` / `#f4f5f7`, surface `#2e3035` / `#fff`, primary
`#00b24b` / `#00a044` (только главные действия — «Подключить»), secondary
`#0075ff` (вторичные/ссылки). Inter 600/800, радиусы: кнопки 10–12,
карточки 16 (виджет `FibrCard`: surface + hairline 1px, без теней),
пилюли 999 (`StatusPill`). Форматтеры байт/времени портированы из
`design/app.js`, формулировки ru/en — из `miniapp/app.js` где пересекаются.

## Дорожная карта клиента

- **2.5 — синхронизация с сервером**: вход по URL сервера + device-token
  (или Telegram Web Auth в отдельном окне), список своих устройств из
  `GET /api/me` (см. корневой README: API уже готово: `POST /api/me/devices`,
  `GET /api/me/devices/{id}/config|qr.png`, `DELETE /api/me/devices/{id}`),
  refresh конфига по кнопке, push при отключении устройства. Клиентская
  часть уже готова к этому: `TunnelEngine` и `TunnelController` не знают,
  откуда взялся конфиг.
- **Следующие шаги**: rx/tx через собственный MethodChannel к
  WireGuardKit / `com.wireguard.android:tunnel`; применение split
  tunneling (`VpnService.Builder.addAllowedApplication`); активация
  туннеля при старте системы (Android BootReceiver); kill-switch.
- **Этап 3** — AmneziaWG-форк движка (лицензия GPLv3 — см. конец файла).

## Лицензия на движки

При встраивании кода Amnezia (AmneziaWG — GPLv3) клиент обязан быть
открытым. WireGuard — MIT/Apache-2.0, ограничений нет.
