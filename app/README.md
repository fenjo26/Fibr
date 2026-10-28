# Fibr client (Flutter) — этап 2

Здесь появится мобильный/десктопный клиент на Flutter. Один код на
Android, iOS, Android TV, Windows, macOS, Linux.

## План

| Компонент | Решение |
|---|---|
| UI | Flutter + `intl` (ARB-файлы; те же ключи, что в `miniapp/app.js`) |
| VPN-движок (Android) | `com.wireguard.android:tunnel` (+ форк AmneziaWG на этапе 3) |
| VPN-движок (iOS/macOS) | `WireGuardKit` + `NetworkExtension` |
| QR-импорт | `mobile_scanner` |
| Профиль-ссылка | deep link `fibr://...` (как `vpn://` у Amnezia) |
| Split tunneling по приложениям | Android `VpnService.addAllowedApplication`; desktop — свои правила; iOS — только домены/исключения (per-app VPN требует MDM, это ограничение Apple) |

## Что уже работает без клиента

QR-коды Fibr — это обычные wg-конфиги: их импортирует официальное
приложение WireGuard (Android, iOS, Windows, macOS, Linux, OpenWrt,
Keenetic). Клиент нужен для UX: список серверов, per-app туннель,
статистика, обновление ключей.

## Дизайн-токены

Источник — [`../design/tokens.css`](../design/tokens.css) (тёмная тема из
`DESIGN-1win.md` + светлая). В Flutter перенести в `ThemeData`:

| Токен | Dark | Light |
|---|---|---|
| primary | `#00b24b` | `#00a044` |
| secondary | `#0075ff` | `#0075ff` |
| canvas | `#141415` | `#f4f5f7` |
| surface | `#2e3035` | `#ffffff` |
| surface-2 | `#26282c` | `#edeff2` |
| hairline | `#2d2f33` | `#e2e5ea` |
| ink | `#f0f2f5` | `#191b1f` |
| muted | `#9aa1b1` | `#5d6470` |
| faint | `#53575f` | `#9aa1b1` |
| danger | `#ff4d4f` | `#d92c2c` |

Шрифт — Inter, веса только 600/800. Радиусы: кнопки 10–12, карточки 16,
пилюли 999. Глубина — ступенью поверхности (canvas → surface), не тенями.

## Лицензия на движки

При встраивании кода Amnezia (AmneziaWG — GPLv3) клиент обязан быть
открытым. WireGuard — MIT/Apache-2.0, ограничений нет.
