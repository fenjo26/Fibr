# Роутеры

VPN на роутере нужен для устройств без клиента WireGuard: Apple TV,
Smart TV, приставки, консоли, устройства умного дома. Всё, что за роутером,
ходит в интернет через туннель автоматически.

Подход один для всех платформ:

1. Создайте в Fibr отдельное устройство — например, «Роутер домой» —
   и получите его конфиг (Mini App → «Конфиг» или админка → «Скачать
   .conf»). Конфиг выглядит так:

   ```ini
   [Interface]
   PrivateKey = <приватный ключ>
   Address = 10.8.0.5/32
   DNS = 1.1.1.1, 1.0.0.1
   MTU = 1420

   [Peer]
   PublicKey = <публичный ключ сервера>
   PresharedKey = <прешаренный ключ>
   AllowedIPs = 0.0.0.0/0, ::/0
   Endpoint = vpn.example.com:51820
   PersistentKeepalive = 25
   ```

2. Перенесите поля конфига в настройки WireGuard роутера (таблицы ниже).
3. Включите NAT (маскарадинг) туннеля на самом роутере: сервер Fibr
   маршрутизирует для роутера только его единственный адрес `/32`,
   поэтому домашняя подсеть должна прятаться за ним.

Важно: роутер — одно устройство Fibr, как телефон или ноутбук. Не
используйте один конфиг на двух роутерах.

## OpenWrt

### Установка пакетов

```sh
opkg update
opkg install wireguard-tools luci-proto-wireguard
```

### LuCI-плагин (проще всего)

Вместо ручного переноса полей по таблицам ниже можно поставить пакет
`luci-app-fibr`: страница **Network → Fibr VPN** сама обменяет join-код на
конфиг и пропишет интерфейс, пира и файрвол (все шаги из UCI-инструкции
ниже — автоматически). Требования: OpenWrt 23.05+ (24.10 рекомендована),
зависимости `wireguard-tools` и `luci-proto-wireguard` ставятся сами.

Установка (когда пакет собран CI и приложен к релизу):

```sh
opkg update
opkg install luci-app-fibr
```

или вручную — скачайте ipk из артефактов CI (Actions → OpenWrt package →
luci-app-fibr-ipk) или релизов и:

```sh
scp luci-app-fibr-*.ipk root@192.168.1.1:/tmp/
ssh root@192.168.1.1 "opkg install /tmp/luci-app-fibr-*.ipk"
```

Затем:

1. Mini App → устройство роутера → кнопка «Код» (join-код, живёт 15 минут).
2. LuCI → **Network → Fibr VPN**: домен сервера (например `vpn.example.com`)
   и код → «Настроить». Можно и без кода — вставьте весь `.conf` в поле
   «… или конфиг».
3. Плагин пишет `network.wg_fibr` (proto wireguard), пир
   `wireguard_wg_fibr` (AllowedIPs из конфига, endpoint, keepalive),
   зону файрвола `fibr` с маскарадингом и forwarding `lan → fibr`.
   DNS из конфига при желании пропишите в Network → DHCP and DNS.
4. Статус туннеля и кнопки «Включить/Выключить» — на той же странице.

Обмен кода идёт из браузера роутера напрямую на
`https://<домен>/api/join/<код>` — код одноразовый и гасится на сервере
как обычно. Для этого сервер должен отдавать `Access-Control-Allow-Origin`
на этой ручке (актуальные версии Fibr делают это); на старом сервере
вставьте конфиг вручную. Xray-устройства (vless) и серверы с движком
AmneziaWG плагин определит и честно предупредит — роутеру нужен обычный
WireGuard-конфиг.

### Через веб-интерфейс (LuCI)

1. **Network → Interfaces → Add new interface…** — имя `fibr`,
   протокол **WireGuard VPN**.
2. На вкладке интерфейса заполните:

   | Поле LuCI | Значение из конфига |
   |---|---|
   | Private Key | `PrivateKey` |
   | IP Addresses (в разделе Interface, No DHCP) | `Address`, т.е. `10.8.0.5/32` |
   | MTU (Advanced Settings) | `1420` |

   Отдельного поля DNS у протокола WireGuard в LuCI нет — DNS из конфига
   (`1.1.1.1, 1.0.0.1`) при желании пропишите в **Network → DHCP and DNS →
   DNS forwardings**, иначе роутер продолжит использовать свои текущие
   резолверы.

3. В разделе **Peers** добавьте пира:

   | Поле LuCI | Значение из конфига |
   |---|---|
   | Public Key | `PublicKey` из `[Peer]` (ключ сервера) |
   | Preshared Key | `PresharedKey` |
   | Allowed IPs | `0.0.0.0/0` и `::/0` (две строки) |
   | Endpoint Host | хост из `Endpoint` (`vpn.example.com`) |
   | Endpoint Port | порт из `Endpoint` (`51820`) |
   | Persistent Keepalive | `25` |
   | Route Allowed IPs | включено (маршрут по умолчанию через туннель) |

4. **Firewall**: Network → Interfaces → вкладка Firewall Settings у `fibr` —
   создайте зону `fibr` (или назначьте туннель в зону `wan`), и в
   Network → Firewall разрешите forwarding `lan → fibr`, у зоны `fibr`
   включите **Masquerading / NAT**. Без маскарадинга трафик домашних
   устройств (адреса вида 192.168.x.x) сервер Fibr не маршрутизирует.
5. Сохраните и примените (Save & Apply).

### Через UCI (терминал)

```sh
uci set network.fibr=interface
uci set network.fibr.proto='wireguard'
uci set network.fibr.private_key='<PrivateKey>'
uci add_list network.fibr.addresses='10.8.0.5/32'
uci set network.fibr.mtu='1420'

uci add network wireguard_fibr
uci set network.@wireguard_fibr[-1].public_key='<PublicKey сервера>'
uci set network.@wireguard_fibr[-1].preshared_key='<PresharedKey>'
uci add_list network.@wireguard_fibr[-1].allowed_ips='0.0.0.0/0'
uci add_list network.@wireguard_fibr[-1].allowed_ips='::/0'
uci set network.@wireguard_fibr[-1].endpoint_host='vpn.example.com'
uci set network.@wireguard_fibr[-1].endpoint_port='51820'
uci set network.@wireguard_fibr[-1].persistent_keepalive='25'
uci set network.@wireguard_fibr[-1].route_allowed_ips='1'

# зона файрвола с маскарадингом и пробросом lan -> fibr
uci add firewall zone
uci set firewall.@zone[-1].name='fibr'
uci set firewall.@zone[-1].input='REJECT'
uci set firewall.@zone[-1].output='ACCEPT'
uci set firewall.@zone[-1].forward='REJECT'
uci set firewall.@zone[-1].masq='1'
uci set firewall.@zone[-1].mtu_fix='1'
uci add_list firewall.@zone[-1].network='fibr'

uci add firewall forwarding
uci set firewall.@forwarding[-1].src='lan'
uci set firewall.@forwarding[-1].dest='fibr'

uci commit
reload_config
```

## Keenetic (KeeneticOS)

В Keenetic есть родной клиент WireGuard — отдельные приложения не нужны.

1. Веб-интерфейс: **Интернет → Другие подключения → VPN-подключения →
   WireGuard → Добавить подключение**.
2. Заполните поля из конфига Fibr:

   | Поле Keenetic | Значение из конфига |
   |---|---|
   | Название | произвольное, например `Fibr` |
   | Приватный ключ (Private key) | `PrivateKey` из `[Interface]` |
   | Адрес (Address / IP) | `10.8.0.5/32` из `Address` |
   | DNS-серверы | `1.1.1.1, 1.0.0.1` из `DNS` |
   | Публичный ключ (Peer public key) | `PublicKey` из `[Peer]` — ключ сервера |
   | Предварительный ключ / Preshared key | `PresharedKey` |
   | Разрешённые адреса (Allowed IPs) | `0.0.0.0/0` и `::/0` |
   | Адрес узла / Endpoint | `vpn.example.com` из `Endpoint` |
   | Порт узла | `51820` из `Endpoint` |
   | Поддержка активности (Keepalive) | `25` |
   | MTU (дополнительно/расширенные) | `1420` |

3. Отметьте **«Использовать для выхода в интернет»** — Keenetic сам
   направит трафик домашней сети в туннель и выполнит NAT (п. 3 общей
   схемы выполняется автоматически).
4. Примените и включите подключение.

Если нужных полей в вашей версии прошивки нет, обновите KeeneticOS до
актуальной — поддержка WireGuard есть во всех текущих версиях.

## Любой роутер с поддержкой WireGuard

Универсальная таблица соответствия. Слева — секции `.conf` Fibr, справа —
типовые названия настроек в прошивках (ASUS, Mikrotik, TP-Link, Ubiquiti,
pfSense/OPNsense и др.).

| Конфиг Fibr | Где в настройках роутера |
|---|---|
| `[Interface] PrivateKey` | Private key интерфейса WireGuard |
| `[Interface] Address = 10.8.0.5/32` | Local IP / Tunnel address / Address интерфейса (вносить с маской `/32`) |
| `[Interface] DNS` | DNS-серверы туннеля (если прошивка их поддерживает) |
| `[Interface] MTU = 1420` | MTU интерфейса (если поле есть; иначе оставить по умолчанию) |
| `[Peer] PublicKey` | Peer public key / ключ удалённой стороны |
| `[Peer] PresharedKey` | Preshared key / Pre-shared key пира |
| `[Peer] AllowedIPs = 0.0.0.0/0, ::/0` | Allowed IPs / Destination networks пира — именно это включает «весь трафик через VPN» |
| `[Peer] Endpoint = vpn.example.com:51820` | Endpoint / Peer address — хост и UDP-порт сервера |
| `[Peer] PersistentKeepalive = 25` | Keepalive / Persistent keepalive, секунд |

Обязательно проверьте после настройки:

- **NAT за туннелем**: локальная подсеть должна маскарадиться в адрес
  `10.8.0.5` (см. п. 3 общей схемы).
- **UDP-порт открыт наружу** на пути до сервера — Fibr слушает
  `FIBR_WG_PORT` (по умолчанию 51820/udp).
- **Онлайн-статус**: через минуту устройство «Роутер» должно показать
  «онлайн» в админке Fibr (handshake за последние 3 минуты).
