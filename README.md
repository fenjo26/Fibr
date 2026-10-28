# Fibr — self-hosted VPN platform

**Fibr** — это «купил VPS → направил домен → одна команда → VPN для всех
устройств»: WireGuard-сервер с админкой, Telegram-ботом и Telegram Mini App,
QR-конфигами и статистикой. Open source, MIT.

- любой юзер открывает Mini App из бота, добавляет устройство и сканирует QR;
- QR совместим с официальным приложением WireGuard (Android, iOS, Windows,
  macOS, Linux, OpenWrt, Keenetic) — клиент Fibr не обязателен;
- админ видит в панели: кто онлайн, трафик по устройствам и суммарный,
  скорость туннеля, ключи сервера; управляет пользователями и устройствами;
- i18n: интерфейсы на русском и английском (админка, миниапп, бот);
- **три роли** ([docs/roles.md](docs/roles.md)): суперадмин (владелец), админ-реселлер (создаёт и видит только своих пользователей), пользователь (максимально простой экран — устройства и QR);
- **сроки доступа** ([docs/billing.md](docs/billing.md)): суперадмин продаёт доступ — «действует до» с каскадом на ветку админа, по истечении туннель реально отключается; продление вручную, есть бессрочный режим;
- **реферальная программа** ([docs/referrals.md](docs/referrals.md)): у каждого свой код-ссылка, пополнения в панели автоматически начисляют 15% пригласившему и 5% его пригласившему; балансы, ручные выплаты и контакты владельца настраиваются в панели;
- **лимиты устройств на пользователя** с тем же каскадом: «пакет на 5 устройств» для каждого аккаунта, дефолт — `FIBR_MAX_DEVICES`;
- **мультинодовость** ([docs/multinode.md](docs/multinode.md)): запасные серверы-ноды, активная нода и мгновенная подмена эндпоинта при блокировке IP — клиенты перекачивают QR, ключи устройств не меняются;
- тёмная и светлая темы (дизайн-система — см. [`design/tokens.css`](design/tokens.css)
  и `DESIGN-1win.md`).

> Этап 1 (этот репозиторий): сервер + админка + бот + Mini App + WireGuard.
> Этап 2 — Flutter-клиент (`app/README.md`), этап 3 — AmneziaWG/Xray для
> DPI-устойчивости.

## Быстрый старт

На чистом Ubuntu 22.04+/Debian 12 VPS с A-записью `vpn.example.com`:

```bash
curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/install/install.sh \
  | bash -s -- --domain vpn.example.com
```

Скрипт сам: поставит Docker, проверит DNS, сгенерирует секреты в
`/opt/fibr/install/.env`, соберёт контейнеры и поднимет их. В конце напечатает
адрес админки и токен.

Из чек-аута репозитория то же самое: `bash install/install.sh --domain vpn.example.com`.

Опции: `--port` (UDP-порт WG), `--dev` (локальный режим без домена и TLS),
`--skip-dns-check`, `--force`, `--uninstall [--purge]`.

## Скачать клиент

Готовые сборки публикуются в [Releases](https://github.com/fenjo26/Fibr/releases) и собираются автоматически из тегов — конвейер в `.github/workflows/release.yml`.

**Проще всего — со своего сервера** (после `bash install/sync-downloads.sh` на нём): короткие ссылки удобно диктовать и набирать с пульта ТВ (приложение Downloader):

| Короткая ссылка | Куда ведёт |
|---|---|
| `https://ваш-домен/tv.apk` | Android-приложение (телефон и Android TV) |
| `https://ваш-домен/app.apk` | то же — удобно давать в чатах |
| `https://ваш-домен/windows.zip` · `/macos.zip` | десктопные сборки |

Прямые ссылки на GitHub (всегда последняя версия):

| Платформа | Скачать |
|---|---|
| Android (APK) | [Fibr-Android.apk](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-Android.apk) |
| Windows (zip) | [Fibr-Windows.zip](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-Windows.zip) |
| macOS (zip) | [Fibr-macOS.zip](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-macOS.zip) |
| Google Play / App Store | — (добавим после публикации) |

Примечания:

- Один и тот же APK работает на телефоне и Android TV; требуется Android 7.0+ (minSdk 24). На ТВ интерфейс пока телефонный — полноценный TV-UI в планах.
- Пока клиент ранний, сборки экспериментальные. Для всех платформ по-прежнему работает связка «QR из Mini App + официальное приложение WireGuard».
- APK подписан CI-ключом: устанавливается как «неизвестный источник»; перед массовой раздачей настроим собственный keystore.
- macOS-сборка без подписи разработчика: первый запуск — правый клик по приложению → «Открыть».

### Подключение Telegram-бота и Mini App

1. В [@BotFather](https://t.me/BotFather): `/newbot` → получите токен.
2. Впишите в `/opt/fibr/install/.env`:
   - `FIBR_TELEGRAM_BOT_TOKEN=<токен>`
   - `FIBR_ADMIN_TELEGRAM_IDS=<ваш числовой id; узнать — @userinfobot или команда /id у бота после старта>`
3. `docker compose -f /opt/fibr/install/docker-compose.yml up -d`
4. В BotFather: `/mybots` → ваш бот → *Bot Settings* → *Menu Button* →
   `https://vpn.example.com/miniapp/`
5. Откройте бота в Telegram → **Start** — кнопка «Открыть Fibr».

Mini App требует HTTPS и домен — это ограничение платформы Telegram.
После шага 4 миниапп подтягивается из бота **автоматически**: кнопка слева от
поля ввода сообщения открывает приложение, ставить ничего не нужно.

Два способа зайти без Telegram:

- **веб-кабинет** `https://vpn.example.com/cabinet/` — тот же личный кабинет
  в обычном браузере, вход кнопкой «Log in with Telegram». Чтобы Telegram
  авторизовал вход с домена, в BotFather сделайте ещё `/mybots` → *Bot
  Settings* → *Domain* → `vpn.example.com`;
- **админ-панель** `/admin/` — по токену из `.env` (только суперадмин).

## Состав репозитория

```
server/   Go-сервер: REST API + WireGuard-менеджер + Telegram-бот (один бинарник)
          + fibr-node — агент дополнительной ноды (docs/multinode.md)
install/  install.sh, docker-compose.yml, Caddyfile, .env.example
admin/    Админ-панель (vanilla JS, без сборки)
miniapp/  Telegram Mini App (vanilla JS)
cabinet/  Веб-кабинет: миниапп для браузера, вход Telegram Login Widget
design/   Общие дизайн-токены и хелперы (отдаются на /shared/*)
app/      Flutter-клиент
```

## Архитектура

```
                      ┌──────────────────────────── VPS ────────────────────────────┐
 Telegram ⇄ бот (long polling)                                     │
 Mini App ⇄ Caddy ──TLS── /api/* ──► fibr-api (Go) ── wgctrl ──► wg0 (kernel)
 (https)         ├─ /admin/*  статика      │                     │ iptables NAT
                 └─ /miniapp/, /shared/*   ├── SQLite (/data)    ▼
                                          └── /data/xray/config.json → xray (VLESS+Reality, opt-in)
                                                                Интернет
```

- **fibr** (контейнер, `NET_ADMIN`): сам поднимает `wg0`, хранит ключи и
  пиров в SQLite, раз в 30 секунд собирает счётчики трафика пиров
  (`rx/tx`, handshake) и считает скорость интерфейса.
- **caddy**: TLS Let's Encrypt, раздаёт админку/миниапп, проксирует `/api/*`.
  Порт API наружу не торчит.
- Пир = «устройство» пользователя. Отключение устройства = исключение пира
  из конфигурации wg0 (сеанс рвётся).

## API (кратко)

`POST /api/auth/telegram` — вход Mini App (проверка подписи initData);
далее `Authorization: Bearer <session>`.

| Endpoint | Описание |
|---|---|
| `GET /api/me`, `POST /api/me/devices`, `DELETE /api/me/devices/{id}` | свои устройства |
| `GET /api/me/devices/{id}/config` · `/qr.png` | конфиг wg-quick · QR |
| `GET /api/admin/overview` | онлайн, трафик, скорость, ключ сервера |
| `GET/POST/PATCH/DELETE /api/admin/users…` | пользователи (Bearer = админ-токен или админ-сессия) |
| `POST /api/admin/users/{id}/devices` | выдать устройство вручную |
| `GET/PATCH/DELETE /api/admin/devices…` · `/qr.png` | все устройства |

Полный список — в `server/internal/api/api.go`.

## Безопасность (прочитайте)

- **Приватные ключи устройств хранятся на сервере** — так же, как в wg-easy:
  иначе панель не сможет показать QR. Компрометация VPS = компрометация
  ключей. Ставьте Fibr только на свой сервер, закрывайте SSH.
- Админ-токен даёт полный доступ к панели — храните его в секретах.
- Отключённый пользователь не может войти, но его уже выданные пиры живут,
  пока не отключены устройства (см. «Отключение устройства» выше) — при
  блокировке отключите его устройства или удалите пользователя.
- Сервер слушает только UDP/WG и HTTP за Caddy; API наружу не опубликован.
- `AllowedIPs = 0.0.0.0/0, ::/0` — полный туннель; DNS из `.env`
  (`FIBR_DNS`, по умолчанию 1.1.1.1/1.0.0.1).

## Разработка

```bash
make build   # собрать bin/fibr
make test    # юнит-тесты (Go)
make run     # локальный запуск в пассивном режиме (без kernel WireGuard)
```

Пассивный режим: на macOS/CI сервер не управляет интерфейсом, но ключи,
конфиги, QR и API работают — удобно для фронтенда:
`bin/fibr` + открыть `http://127.0.0.1:8080/api/health`.

Фронтенды без сборки: статикой их отдаёт Caddy (`/shared/*`, `/admin/*`,
`/miniapp/*`). Общие токены/хелперы — `design/tokens.css`, `design/app.js`.

## Дорожная карта

- [x] Этап 1 — сервер, админка, бот, Mini App, WireGuard, QR, i18n (ru/en)
- [x] Этап 2 — Flutter-клиент (`app/`): парсер конфигов, QR/deep-link импорт, темы
- [x] Xray (VLESS-Reality) как второй протокол устройств — [docs/xray.md](docs/xray.md)
- [x] AmneziaWG — обфусцированный WireGuard, движок на весь сервер — [docs/awg.md](docs/awg.md)
- [x] Self-enroll по приглашениям — [docs/self-enroll.md](docs/self-enroll.md)
- [x] IPv6 dual-stack (ULA /64, адрес устройства выводится из IPv4)
- [x] Мультинодовость — ноды-агенты, активная нода, подмена при блокировке — [docs/multinode.md](docs/multinode.md)
- [ ] OpenWrt/LuCI-плагин

## Лицензия

MIT — см. [LICENSE](LICENSE). Если в будущем вы встроите код Amnezia
(GPLv3), соответствующие части обязаны остаться открытыми.
