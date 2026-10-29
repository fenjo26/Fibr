# Fibr — self-hosted VPN платформа

[English](README.md) | **Русский**

**Fibr** — это решение «купил VPS → направил домен → одна команда → VPN для всех устройств»: WireGuard/AmneziaWG/Xray-сервер с веб-админкой, личным кабинетом, Telegram-ботом и Telegram Mini App, QR-конфигами и детальной аналитикой. Полностью открытый исходный код (Open Source, MIT).

- 🚀 **Мгновенный старт:** пользователь открывает Mini App или веб-кабинет, нажимает «Добавить устройство» и сканирует QR-код.
- 📱 **Универсальная совместимость:** конфиги и QR работают как в собственном клиенте Fibr (Android, Android TV, Windows, macOS), так и в официальных приложениях WireGuard и AmneziaVPN (iOS, Android, Windows, macOS, Linux, OpenWrt, Keenetic).
- 🛡️ **Защита от блокировок (DPI):** обфусцированный WireGuard (**AmneziaWG 2.0**) и **VLESS через XHTTP на 443** с сертификатом вашего домена (снаружи выглядит как обычный HTTPS к сайту), плюс запасной VLESS + Reality ([docs/xray.md](docs/xray.md), [docs/awg.md](docs/awg.md)).
- 🇷🇺 **Ретранслятор в России:** если IP сервера заблокирован, маленький российский VPS пересылает трафик на основной сервер. Ставится одной командой, а в кабинете у пользователя появляется переключатель «Я в России» ([docs/relay.md](docs/relay.md)).
- 🛑 **DNS-защита и фильтрация:** встроенный DNS-резолвер с AdBlock, родительским контролем (Family), расписанием доступа, защитой от утечек (anti-leak) и поддержкой кастомных списков в формате AdGuard/hosts ([docs/dns-games.md](docs/dns-games.md)).
- 🎮 **Игровой режим:** маршрутизация трафика популярных онлайн-игр (CS2, Dota 2, Valorant, Apex и др.) через туннель с минимальным пингом ([docs/dns-games.md](docs/dns-games.md)).
- 🌐 **Веб-кабинет без Telegram:** полноценный браузерный личный кабинет с регистрацией по инвайтам, входом по разовым кодам, логину/паролю или виджету Telegram ([docs/web-login.md](docs/web-login.md)).
- 📊 **Мощная панель управления:** мониторинг онлайна в реальном времени, графики трафика, скорость туннеля, ключи сервера, управление пользователями и устройствами.
- 👥 **Трёхуровневые роли:** суперадмин (владелец), админ-реселлер (создаёт и управляет только своими клиентами) и конечный пользователь ([docs/roles.md](docs/roles.md)).
- ⏳ **Сроки доступа и монетизация:** установка срока действия (`expires_at`) с каскадным отключением ветки реселлера; ручное продление или бессрочный режим ([docs/billing.md](docs/billing.md)).
- 💰 **Реферальная программа:** персональные ссылки, двухуровневая система начислений (15% L1, 5% L2), балансы и выплаты ([docs/referrals.md](docs/referrals.md)).
- 📡 **Мультинодовость:** подключение резервных нод-агентов, активные ноды и мгновенное переключение эндпоинта при блокировке IP без смены клиентских ключей ([docs/multinode.md](docs/multinode.md)).
- 🏠 **Плагин для OpenWrt (LuCI):** пакет `luci-app-fibr` для настройки VPN на весь дом по короткому join-коду ([docs/routers.md](docs/routers.md)).
- 🎨 **Современный UI/UX:** тёмная и светлая темы, адаптивный дизайн (токены в [`design/tokens.css`](design/tokens.css), гайдлайны в [`docs/DESIGN.md`](docs/DESIGN.md)), полная локализация на русский и английский языки.

---

## Быстрый старт

### Что понадобится:
1. Чистый VPS под управлением **Ubuntu 22.04+** или **Debian 12** (от 1 ГБ RAM; при меньшем объёме установщик предложит добавить swap).
2. Домен с **A-записью**, указывающей на публичный IP сервера.
3. *(Опционально)* Для Telegram-бота: токен от [@BotFather](https://t.me/BotFather) (`/newbot`) и ваш числовой Telegram ID от [@userinfobot](https://t.me/userinfobot).

### Установка одной командой

С подключением Telegram-бота и назначением суперадмина:

```bash
curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/install/install.sh \
  | bash -s -- --domain vpn.example.com \
      --bot-token 1234567890:AAF-токен-от-BotFather \
      --admin-tg 123456789
```

Только веб-кабинет и админка (без бота):

```bash
curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/install/install.sh \
  | bash -s -- --domain vpn.example.com
```

> **Что сделает скрипт:** установит Docker, проверит DNS-записи, сгенерирует криптографические ключи и пароли, настроит файрвол (ufw), запустит контейнеры (fibr + caddy + xray) и выведет в консоль URL панели управления, токен доступа и ссылки для подключения.

Повторный запуск скрипта безопасен: он обновляет настройки и дописывает флаги в `.env`, не перезатирая существующие данные. Подробнее — в [docs/getting-started.md](docs/getting-started.md).

---

## Если из России не подключается: ретранслятор

Признак: у пользователя в России без VPN не открывается даже `https://ваш-домен/cabinet/` (таймаут), а остальные сайты открываются. Значит, провайдер блокирует IP или подсеть вашего хостера, и протоколы тут ни при чём. Решение: ретранслятор на VPS **в России**. Он просто пересылает трафик на основной сервер и ничего не расшифровывает.

```
телефон ──► VPS в России (nginx, пересылка) ──► основной сервер Fibr ──► интернет
```

1. **На российском VPS** (чистый Ubuntu/Debian, свободный порт 443):
   ```bash
   curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/relay/install.sh -o relay.sh
   bash relay.sh --upstream IP_ОСНОВНОГО_СЕРВЕРА
   ```
2. **На основном сервере:**
   ```bash
   echo "FIBR_RELAY_HOST=IP_РЕТРАНСЛЯТОРА" >> /opt/fibr/install/.env
   cd /opt/fibr/install && docker compose up -d
   ```
3. **Пользователю:** в кабинете включить «🇷🇺 Я в России» (по умолчанию уже включено) и заново взять QR, файл или ссылку.

Подробно, с проверками и ограничениями: [docs/relay.md](docs/relay.md).

---

## Скачать клиент Fibr

Готовые сборки приложений доступны на странице [Releases](https://github.com/fenjo26/Fibr/releases) и компилируются автоматически через GitHub Actions CI/CD.

### Загрузка напрямую с вашего сервера
После выполнения `bash install/sync-downloads.sh` на сервере клиенты доступны по коротким ссылкам (удобно вводить на Android TV в приложении Downloader):

| Короткая ссылка | Описание |
|---|---|
| `https://ваш-домен/tv.apk` | Приложение для Android и Android TV |
| `https://ваш-домен/app.apk` | Зеркало APK для быстрой отправки в мессенджерах |
| `https://ваш-домен/windows.zip` | Клиент для Windows (распаковать и запустить) |
| `https://ваш-домен/macos.zip` | Клиент для macOS |

### Официальные релизы на GitHub:

| Платформа | Ссылка на загрузку |
|---|---|
| **Android / Android TV (APK)** | [Fibr-Android.apk](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-Android.apk) |
| **Windows (.zip)** | [Fibr-Windows.zip](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-Windows.zip) |
| **macOS (.zip)** | [Fibr-macOS.zip](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-macOS.zip) |
| **iOS / iPadOS** | Используйте официальный [WireGuard](https://apps.apple.com/app/wireguard/id1441195209) или AmneziaVPN (импорт QR / файла) |

> ℹ️ **Примечание:** для всех платформ подключение возможно как через клиент Fibr, так и через официальные клиенты WireGuard или AmneziaVPN.

---

## Обновление системы

### Через SSH (одна команда):
```bash
cd /opt/fibr && git pull && docker compose -f install/docker-compose.yml up -d --build
```

### В один клик из веб-админки:
При установке с флагом `--with-updater` в панели управления появится индикатор новых версий и кнопка «Обновить». Сайдкар автоматически подтянет обновления из GitHub и перезапустит сервисы ([docs/updates.md](docs/updates.md)).

---

## Настройка Telegram-бота и Mini App

Если бот не был подключён при первой установке, перезапустите установщик с параметрами:

```bash
curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/install/install.sh \
  | bash -s -- --domain vpn.example.com \
      --bot-token <ТОКЕН_ОТ_BOTFATHER> \
      --admin-tg <ВАШ_TELEGRAM_ID>
```

Затем в [@BotFather](https://t.me/BotFather):
1. Отправьте `/mybots` → выберите вашего бота → **Bot Settings** → **Menu Button**.
2. Укажите URL: `https://vpn.example.com/miniapp/`.
3. Для авторизации через браузерный виджет: **Bot Settings** → **Domain** → `vpn.example.com`.

Теперь при открытии бота в Telegram кнопка рядом с полем ввода мгновенно открывает Fibr Mini App!

---

## Вход в систему

- **Веб-кабинет пользователя:** `https://vpn.example.com/cabinet/` — доступен с любого браузера без Telegram (вход по инвайт-ссылке, коду доступа или логину/паролю).
- **Telegram Mini App:** кнопка «Меню» в вашем Telegram-боте.
- **Панель администратора:** `https://vpn.example.com/admin/` — вход по секретному токену `FIBR_ADMIN_TOKEN`.

---

## Структура репозитория

```
├── server/          # Go-бэкенд: REST API, WireGuard/AWG/Xray контроллер, Telegram-бот
│   ├── cmd/fibr/    # Основной бинарник сервера
│   ├── cmd/fibr-node/    # Агент дополнительной ноды (docs/multinode.md)
│   └── cmd/fibr-updater/ # Сайдкар обновления в 1 клик (docs/updates.md)
├── install/         # Скрипты развёртывания, Caddyfile, docker-compose
├── relay/           # Ретранслятор для VPS в России (nginx stream, docs/relay.md)
├── admin/           # Панель управления администратора (Vanilla JS, адаптивная)
├── miniapp/         # Telegram Mini App клиентский интерфейс
├── cabinet/         # Браузерный личный кабинет пользователя
├── design/          # Дизайн-система, токены CSS, общие скрипты
├── app/             # Кроссплатформенный клиент Flutter (Android, iOS, Desktop)
├── luci-app-fibr/   # Плагин LuCI для роутеров OpenWrt
└── docs/            # Полная документация и руководства
```

---

## Архитектура системы

```
                       ┌──────────────────────────── VPS ────────────────────────────┐
  Telegram ⇄ бот (long polling)                                     │
  Mini App ⇄ Caddy ──TLS── /api/* ──► fibr-api (Go) ── wgctrl ──► wg0 (kernel)
  (HTTPS)         ├─ /admin/*  статика      │                     │ iptables NAT
                  ├─ /cabinet/* статика     ├── SQLite (/data)    ▼
                  └─ /shared/*  токены      └── /data/xray/config.json → xray (VLESS+Reality)
                                                                Интернет
```

- **fibr-api:** управляет сетевым интерфейсом `wg0`, динамически настраивает пиры через `wgctrl`, ведёт учёт трафика и рукопожатий (handshakes), обслуживает REST API и бота.
- **caddy:** обеспечивает автоматический выпуск TLS Let's Encrypt сертификатов, раздаёт статику веб-интерфейсов и защищает доступ к API.
- **xray:** обеспечивает устойчивый к DPI транспорт VLESS+Reality с маскировкой под TLS популярных CDN.
- **SQLite:** надёжная локальная база данных для хранения информации о пользователях, устройствах, сроках и тарифах.

---

## Безопасность

- **Приватные ключи устройств:** генерируются и сохраняются на сервере для обеспечения возможности показа QR-кода в личном кабинете. Используйте надёжный пароль для SSH и закрывайте неиспользуемые порты.
- **Изоляция API:** REST API слушает исключительно локальный адрес и доступен снаружи только через Caddy.
- **Блокировка пользователей:** при блокировке пользователя все его устройства немедленно удаляются из конфигурации активного интерфейса WireGuard, прерывая текущие сессии.

---

## Локальная разработка

```bash
make build   # Сборка бинарника bin/fibr
make test    # Запуск юнит-тестов Go
make run     # Запуск в пассивном режиме (эмуляция wg для macOS/тестирования)
```

В пассивном режиме сервер работает на macOS без доступа к ядру Linux WireGuard, сохраняя полную работоспособность базы данных, API и веб-интерфейсов на `http://127.0.0.1:8080`.

---

## Документация и руководства

Подробные инструкции по всем компонентам доступны в каталоге [docs/](docs/README.md):

- [Быстрый старт с нуля](docs/getting-started.md)
- [Подключение клиентских устройств](docs/devices.md)
- [Настройка роутеров (OpenWrt, Keenetic)](docs/routers.md)
- [Настройка Telegram-бота](docs/telegram-bot.md)
- [Веб-кабинет и вход без Telegram](docs/web-login.md)
- [DNS-защита, фильтрация и игровой режим](docs/dns-games.md)
- [Протокол Xray (VLESS через XHTTP на 443, Reality)](docs/xray.md)
- [Протокол AmneziaWG 2.0](docs/awg.md)
- [Ретранслятор в России (если IP сервера заблокирован)](docs/relay.md)
- [Мультинодовость и распределённая сеть](docs/multinode.md)
- [Биллинг и ограничение доступа по времени](docs/billing.md)
- [Реферальная программа](docs/referrals.md)
- [Роли пользователей и реселлеры](docs/roles.md)
- [Система саморегистрации и инвайты](docs/self-enroll.md)
- [Быстрое подключение по Join-кодам](docs/join.md)
- [Механизм обновлений сервера](docs/updates.md)
- [Часто задаваемые вопросы (FAQ)](docs/faq.md)
- [Дизайн-система Fibr](docs/DESIGN.md)

---

## Лицензия

Проект распространяется под лицензией **MIT** — см. файл [LICENSE](LICENSE).