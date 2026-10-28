# DNS-защита и игровой режим — архитектура и контракт

> Восстановлен 2026-10-28 (вторично): файлы пакета были удалены из рабочего
> дерева параллельной сессией и повторно созданы. После завершения этой
> итерации работа коммитится немедленно.

Единая точка правды для трёх фич. Пакеты `internal/dns` и `internal/games`
самодостаточны: сервер подключает их в `cmd/fibr/main.go`, API-ручки и UI —
поверх (§6).

## 1. DNS-защита (блокировка рекламы + родительский контроль)

Резолвер с фильтрацией живёт в бинаре fibr (контейнер имеет NET_ADMIN),
слушает `:53`. Клиенты WireGuard получают `DNS = <шлюз подсети туннеля>`
(напр. `10.8.0.1`).

### Профили устройств

| Профиль | Что делает |
|---|---|
| `off` | только кеш+upstream |
| `adblock` | блокировка рекламы/трекеров (+ блок DoH-обхода) |
| `family` | adblock + adult + SafeSearch + расписание |

### Расписание (только `family`)

```json
{"days": [1,2,3,4,5], "start": "07:00", "end": "22:00", "tz": "Europe/Berlin"}
```

Дни 1=Пн..7=Вс; окно может переходить через полночь. Вне окна — REFUSED на
всё, кроме allowlist (`fibr.local`).

### Синтаксис правил (свой парсер — проект MIT, GPL-библиотеки AdGuard нельзя)

hosts (`0.0.0.0 domain`), plain domain, `||domain^`, `@@`-исключения
(приоритетнее блоков), `$dnsrewrite` — внутри SafeSearch-набора. Матчинг:
map + суффиксный проход. Строки чужих форматов (CSS-правила `##.banner`,
комментарии) пропускаются молча — поэтому кастомные списки AdGuard-формата
совместимы.

### Списки

- Дефолт (BuiltIn, MIT): `https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts`
  и `https://raw.githubusercontent.com/StevenBlack/hosts/master/alternates/porn/hosts`.
- **Кастомные списки «как у AdGuard Home»**: пользователь добавляет любые URL
  (hosts/adblock-формат) — хранение в sqlite (`dns_lists`: name, url, enabled,
  builtin, etag, last_ok, last_check); методы `Lists/AddList/RemoveList/
  SetListEnabled/RefreshAll/RefreshOne` — для UI админки (кнопка «Обновить
  сейчас» = RefreshAll). BuiltIn удалять нельзя (только disable).
- Кастомные списки действуют в профилях adblock+family.
- Автообновление каждые 24ч (ETag/If-None-Match, кэш `/data/dns/lists/`),
  offline — работаем на кэше, старт без сети не фатален.

### SafeSearch (встроенный набор)

CNAME → `forcesafesearch.google.com`, `restrictmoderate.youtube.com`,
`strict.bing.com`, `safe.duckduckgo.com`; Yandex A → `213.180.193.56`.
Плюс встроенный блок-лист ~21 популярного DoH-домена (cloudflare-dns.com,
dns.google, dns.quad9.net, …) — против обхода фильтрации шифрованным DNS.

### Anti-leak (iptables в контейнере, при старте резолвера)

UDP/TCP 53 из подсети туннеля мимо резолвера → DNAT; TCP 853 → REJECT;
`use-application-dns.net` → всегда NXDOMAIN (canary Firefox DoH).

### Upstream

`FIBR_DNS_UPSTREAM` (дефолт `https://1.1.1.1/dns-query,1.1.1.1`). Ядро —
`github.com/AdguardTeam/dnsproxy` v0.86.0 (Apache-2.0), фильтрация — наш
`proxy.Middleware` до кеша. Порядок: canary → расписание → SafeSearch →
блок → upstream.

### Query log

Кольцевой буфер 10k + батч-flush (5с) в `/data/dns/queries.db`
(`dns_query_log(ts, device_ip, qname, qtype, action, profile, list)`,
action ∈ allow|block|safesearch|schedule); prune раз в час, retention 7д;
`Stats24h()` (счётчики + top-50), `Entries(limit, deviceIP)`.

## 2. Игровой режим

Через туннель — ТОЛЬКО игровой трафик: второй WireGuard-туннель из того же
пира, AllowedIPs = CIDR выбранных игр. Серверная часть — `internal/games`.

Снапшот (2026-10-28): cs2 31 + dota2 31 (Valve: denniskupec `old%20list.md` —
прежде mm-range.txt, 404; + ryad-lindner; кросс-чек AS32590; Steam-CDN
205.196.6.0/24, 208.64.200.0–203.0/24 исключены), lol 15 + valorant 15
(Riot AS6507 через RIPEstat). Fortnite/Minecraft не включены (Fortnite —
широкие AWS-блоки = пол-интернета в туннеле; у Minecraft нет фиксированных
серверов). Обновление с GitHub раз в 24ч, failover источников, кэш
`/data/games.json`, форматы plain-CIDR / SSTap / markdown-списки.

## 3. API

Публичные (как /api/health): `GET /api/dns/status` →
`{"enabled":true,"default_profile":"adblock"}`; `GET /api/games` →
`[{id,name,name_ru,cidr_count,updated}]`; `GET /api/games/routes?ids=` →
`{game_id: [cidr...]}`.

Авторизованные (после ролевой сессии): `GET /api/dns/stats`,
`GET /api/dns/querylog`, `PUT /api/me/devices/{id}/dns`
(`{"profile":"family","schedule":{...}|null,"allow":["example.com"]}`,
`allow` — личный никогда-не-блокировать список устройства, до 64 доменов,
приоритетнее любых серверных списков), управление списками
(`GET/POST/DELETE /api/dns/lists`, `PATCH /api/dns/lists/{name}`,
`POST /api/dns/lists/refresh`), игровой туннель
`GET /api/me/devices/{id}/game-config?games=` и
`GET /api/me/devices/{id}/game-qr.png?games=` (второй конфиг того же пира
с AllowedIPs выбранных игр). В WG-конфиге при `profile != off`:
`DNS = <шлюз>`.

## 4. Env

| Переменная | Дефолт | Смысл |
|---|---|---|
| `FIBR_DNS_ENABLED` | `true` | резолвер запускается |
| `FIBR_DNS_PROFILE_DEFAULT` | `adblock` | профиль новых устройств |
| `FIBR_DNS_UPSTREAM` | `https://1.1.1.1/dns-query,1.1.1.1` | upstream |
| `FIBR_DNS_LISTS_ADBLOCK` | URL StevenBlack | override BuiltIn-списка |
| `FIBR_DNS_LISTS_ADULT` | URL StevenBlack porn | override BuiltIn-списка |
| `FIBR_GAMES_ENABLED` | `true` | каталог игр активен |

## 5. Store-модель

`devices.dns_profile TEXT NULL`, `devices.dns_schedule TEXT NULL` (JSON),
`devices.dns_allowlist TEXT NULL` (JSON-массив личных исключений);
`ProfileLookup` и `AllowlistLookup` пакета dns читают их по туннельному IP
устройства.

## 6. Интеграция — ВЫПОЛНЕНА

1. ✅ `cmd/fibr/main.go`: `dns.NewServer` + `games.NewCatalog` + `go cat.Run(ctx)`
   (+ anti-leak для подсети AmneziaWG);
2. ✅ `internal/config`: env §4;
3. ✅ `internal/api`: публичные и авторизованные ручки §3 + списки +
   игровой туннель (`internal/api/dnsgames.go`);
4. ✅ `internal/store`: миграция §5 + ProfileLookup/AllowlistLookup;
5. ✅ `admin/`: вкладка «DNS» — статистика, лог, профили устройств,
   управление списками (добавить URL / вкл-выкл / кнопка «Обновить»);
6. ✅ `miniapp/` + `cabinet/`: кнопка «🛡 Защита» на устройстве — профиль
   (off/adblock/family + расписание) и личные исключения; карточка
   «🎮 Игровой режим» с QR второго туннеля;
7. ✅ `install/install.sh`: env в генерацию `.env` (апгрейд-путь);
8. ✅ WG-конфиг при `profile != off`: `DNS = <шлюз>` (`wg.Manager.DNSLine`,
   включается только после успешного старта резолвера — `SetDNSResolver`).

Клиент (`app/`) готов: DNS-карточка статуса в туннеле + экран игрового
режима (каталог с сервера + fallback-снапшот, создание игрового туннеля,
лимит 180 CIDR), l10n ru/en.
