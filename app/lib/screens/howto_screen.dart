import 'package:flutter/material.dart';

import '../l10n/app_localizations.dart';
import '../theme/app_theme.dart';
import '../widgets/fibr_widgets.dart';

/// "How to connect" — the same plain-language walkthrough the bot (/howto),
/// the Mini App and the web cabinet show, for people with no technical
/// background. Each way is a collapsible section with numbered steps.
class HowToScreen extends StatelessWidget {
  const HowToScreen({super.key});

  static const _ru = <(String, List<String>)>[
    ('📱 Этот телефон — файлом (проще всего)', [
      'В Telegram откройте бота Fibr и напишите /new — придёт файл .conf.',
      'Нажмите на файл → «Открыть в…» → Fibr. Он добавится сам.',
      'Нажмите «Сохранить», затем включите туннель и разрешите VPN.',
    ]),
    ('🔑 По ключу доступа', [
      'В боте нажмите «Открыть Fibr» → Профиль → «Ключ для приложения».',
      'Здесь на экране входа откройте «Ключ доступа», введите адрес сервера и вставьте ключ.',
      'Нажмите «Войти» — устройства подтянутся сами.',
    ]),
    ('📋 Вставить или выбрать файл', [
      'Скопируйте конфиг, ссылку или код подключения (или сохраните файл .conf).',
      'Здесь: «Добавить» → «Вставить / файл».',
      '«Из буфера» — если скопировали; «Выбрать файл» — если сохранили файл.',
    ]),
    ('🔳 По QR-коду', [
      'На другом экране откройте бота → «Открыть Fibr» → у устройства «QR-код».',
      'Здесь: «Добавить» → «Сканировать QR» и наведите камеру.',
    ]),
    ('🔢 По коду подключения', [
      'В боте: «Открыть Fibr» → у устройства кнопка «Код».',
      'Здесь: «Добавить» → «По коду» → вставьте ссылку или введите код и адрес сервера.',
    ]),
    ('🅰️ Если удобнее AmneziaVPN или WireGuard', [
      'Установите AmneziaVPN или WireGuard из Google Play.',
      'В боте напишите /new — придёт файл.',
      'AmneziaVPN: «Файл с настройками подключения» → выберите файл. WireGuard: «+» → «Импорт из файла».',
    ]),
    ('📺 Телевизор', [
      'Откройте Fibr на телевизоре — на экране появится QR и короткий код.',
      'Отсканируйте QR камерой телефона → откроется бот → «Новое устройство для ТВ».',
      'Без камеры: в боте «Открыть Fibr» → «📺 Подключить телевизор» → введите код с ТВ.',
    ]),
    ('✅ Проверить и если не работает', [
      'Откройте в браузере 2ip.ru — должен быть IP сервера, а не домашний.',
      'Не работает — выключите и включите туннель.',
      'Один файл не работает на двух телефонах сразу: для второго сделайте свой через /new.',
      'Не получилось — напишите администратору.',
    ]),
  ];

  static const _en = <(String, List<String>)>[
    ('📱 This phone — with a file (easiest)', [
      'In Telegram open the Fibr bot and send /new — a .conf file arrives.',
      'Tap the file → “Open in…” → Fibr. It is added automatically.',
      'Tap “Save”, then switch the tunnel on and allow the VPN.',
    ]),
    ('🔑 With an access key', [
      'In the bot tap “Open Fibr” → Profile → “App key”.',
      'Here on the sign-in screen open “Access key”, type the server address and paste the key.',
      'Tap “Sign in” — your devices load by themselves.',
    ]),
    ('📋 Paste or pick a file', [
      'Copy a config, a link or a quick-connect code (or save a .conf file).',
      'Here: “Add” → “Paste / file”.',
      '“From clipboard” if you copied; “Choose file” if you saved a file.',
    ]),
    ('🔳 With a QR code', [
      'On another screen open the bot → “Open Fibr” → “QR code” on the device.',
      'Here: “Add” → “Scan QR” and point the camera.',
    ]),
    ('🔢 With a quick-connect code', [
      'In the bot: “Open Fibr” → “Code” on the device.',
      'Here: “Add” → “By code” → paste the link or type the code and server.',
    ]),
    ('🅰️ Prefer AmneziaVPN or WireGuard', [
      'Install AmneziaVPN or WireGuard from Google Play.',
      'Send /new to the bot — a file arrives.',
      'AmneziaVPN: “File with connection settings” → pick the file. WireGuard: “+” → “Import from file”.',
    ]),
    ('📺 TV', [
      'Open Fibr on the TV — a QR and a short code appear.',
      'Scan the QR with your phone → the bot opens → “New device for the TV”.',
      'No camera: in the bot “Open Fibr” → “📺 Connect a TV” → type the code from the TV.',
    ]),
    ('✅ Check it, and if it does not work', [
      'Open 2ip.ru in a browser — it should show the server IP, not your home one.',
      'Not working — switch the tunnel off and on.',
      'One file does not work on two phones at once: make another with /new.',
      'Still stuck — message the administrator.',
    ]),
  ];

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final c = FibrColors.of(context);
    final sections = l10n.langCode == 'ru' ? _ru : _en;
    return Scaffold(
      appBar: AppBar(title: Text(l10n.t('howToTitle'))),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            Text(
              l10n.t('howToIntro'),
              style: TextStyle(color: c.muted, fontWeight: FontWeight.w600),
            ),
            const SizedBox(height: 12),
            for (var i = 0; i < sections.length; i++) ...[
              FibrCard(
                padding: EdgeInsets.zero,
                child: Theme(
                  // No divider lines from ExpansionTile inside the card.
                  data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
                  child: ExpansionTile(
                    initiallyExpanded: i == 0,
                    title: Text(
                      sections[i].$1,
                      style: const TextStyle(fontWeight: FontWeight.w700),
                    ),
                    childrenPadding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
                    expandedCrossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      for (var j = 0; j < sections[i].$2.length; j++)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 8),
                          child: Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              SizedBox(
                                width: 24,
                                child: Text(
                                  '${j + 1}.',
                                  style: TextStyle(
                                    color: c.secondary,
                                    fontWeight: FontWeight.w800,
                                  ),
                                ),
                              ),
                              Expanded(
                                child: Text(
                                  sections[i].$2[j],
                                  style: TextStyle(color: c.ink, height: 1.35),
                                ),
                              ),
                            ],
                          ),
                        ),
                    ],
                  ),
                ),
              ),
              const SizedBox(height: 10),
            ],
          ],
        ),
      ),
    );
  }
}
