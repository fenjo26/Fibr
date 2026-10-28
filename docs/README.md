# Документация Fibr

**Fibr** — self-hosted VPN-платформа на WireGuard: сервер с админкой,
Telegram-ботом и Telegram Mini App. Пользователь открывает Mini App, добавляет
устройство и сканирует QR официальным клиентом WireGuard. Подробный обзор —
в [корневом README](../README.md).

## Гайды

| Документ | О чём |
|---|---|
| [getting-started.md](getting-started.md) | Полный путь новичка: VPS → домен → установка одной командой → бот → первое устройство |
| [devices.md](devices.md) | Подключение Android, iPhone/iPad, Windows, macOS, Linux, Android TV; почему Apple TV и Smart TV — только через роутер |
| [routers.md](routers.md) | OpenWrt, Keenetic и любой роутер с WireGuard: куда какие поля конфига класть |
| [telegram-bot.md](telegram-bot.md) | Команды бота, роли и FIBR_ADMIN_TELEGRAM_IDS, как устроен Mini App |
| [xray.md](xray.md) | Второй протокол VLESS/Reality для сетей с блокировкой WireGuard: включение, клиенты, ограничения |
| [self-enroll.md](self-enroll.md) | Регистрация по приглашениям: ссылки, режим «только по invite», deep-link через бота |
| [roles.md](roles.md) | Три роли: суперадмин, админ-реселлер и пользователь — кто кого видит и создаёт |
| [billing.md](billing.md) | Сроки доступа: продажа суперадмином, каскад на ветку админа, продление, бессрочный режим |
| [awg.md](awg.md) | AmneziaWG — обфусцированный WireGuard: включение (--awg), клиенты AmneziaVPN, ограничения |
| [faq.md](faq.md) | Скорость, смена порта, обновление, перенос на новый сервер, забытый токен, безопасность |

## С чего начать

Коротко — три шага:

1. VPS (Ubuntu 22.04+/Debian 12) с A-записью домена, указывающей на его IP.
2. Одна команда установки (см. [getting-started.md](getting-started.md)).
3. Подключите Telegram-бота и раздайте QR устройствам.

## Быстрые ссылки

- Установщик и опции: `install/install.sh --help`
- Все настройки сервера: `install/.env.example` (рабочий файл — `/opt/fibr/install/.env`)
- REST API: `server/internal/api/api.go`
