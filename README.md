# Fibr — Self-Hosted VPN Platform

**English** | [Русский](README.ru.md)

**Fibr** is a turn-key, self-hosted VPN platform: *"rent a VPS → point a domain → run one command → instant VPN for all your devices"*. Built on WireGuard, AmneziaWG, and Xray (VLESS + Reality), featuring a modern web admin dashboard, user self-service cabinet, Telegram bot & Telegram Mini App, QR configs, and live traffic analytics. Open source, MIT licensed.

---

## Highlights

- 🚀 **Zero-Hassle Onboarding:** Users open the Telegram Mini App or Web Cabinet, click "Add Device", and scan a QR code.
- 📱 **Universal Compatibility:** Configurations and QR codes work seamlessly in Fibr's native app (Android, Android TV, Windows, macOS) as well as official WireGuard and AmneziaVPN clients (iOS, Android, Windows, macOS, Linux, OpenWrt, Keenetic).
- 🛡️ **DPI & Censorship Resistance:** Built-in support for obfuscated WireGuard (**AmneziaWG**) and **Xray (VLESS + Reality)** disguised as regular HTTPS traffic to bypass deep packet inspection.
- 🛑 **DNS Protection & Filtering:** Integrated DNS resolver with AdBlock, parental control (Family mode with scheduled access windows), anti-leak rules, and custom AdGuard/hosts format blocklists ([docs/dns-games.md](docs/dns-games.md)).
- 🎮 **Game Mode:** Route only gaming server traffic (CS2, Dota 2, Valorant, Apex, PUBG, etc.) through the low-latency tunnel ([docs/dns-games.md](docs/dns-games.md)).
- 🌐 **Web Cabinet without Telegram:** Full browser-based personal cabinet with invite links, one-time access keys, username/password auth, or Telegram Login Widget ([docs/web-login.md](docs/web-login.md)).
- 📊 **Real-time Admin Dashboard:** Live connection states, total and per-device bandwidth metrics, tunnel speed monitor, server keys, and complete user/device management.
- 👥 **Multi-Tier Roles:** Superadmin (system owner), reseller admin (creates and manages their own user branch), and end-users ([docs/roles.md](docs/roles.md)).
- ⏳ **Access Periods & Monetization:** Set expiration dates (`expires_at`) with automatic cascade revocation on reseller branches, manual renewals, and lifetime mode ([docs/billing.md](docs/billing.md)).
- 💰 **Referral Program:** Custom referral links, two-tier commissions (15% L1, 5% L2), user balances, and manual payout tracking ([docs/referrals.md](docs/referrals.md)).
- 🇷🇺 **Relay inside Russia:** when the server's hosting range is blocked, a small domestic VPS forwards traffic to the main server. One-command install; users get an "I'm in Russia" switch in the cabinet ([docs/relay.md](docs/relay.md)).
- 📡 **Multi-Node Mesh:** Standby node agents, active primary nodes, and instant endpoint failover when IP blocks occur without regenerating client keys ([docs/multinode.md](docs/multinode.md)).
- 🏠 **OpenWrt Router Plugin (LuCI):** Ready-to-install `luci-app-fibr` package for whole-home VPN configuration using a simple join code ([docs/routers.md](docs/routers.md)).
- 🎨 **Adaptive Design System:** Polished dark and light themes (design tokens in [`design/tokens.css`](design/tokens.css), design specification in [`docs/DESIGN.md`](docs/DESIGN.md)), with full Russian and English localization.

---

## Quick Start

### Prerequisites
1. A clean VPS with **Ubuntu 22.04+** or **Debian 12** (1 vCPU, 1 GB RAM minimum; installer will offer to configure swap if RAM is lower).
2. A domain with an **A record** pointing to your VPS public IP.
3. *(Optional)* For Telegram integration: bot token from [@BotFather](https://t.me/BotFather) (`/newbot`) and your numeric Telegram ID from [@userinfobot](https://t.me/userinfobot).

### One-Command Installation

Deploy everything (including the Telegram bot and superadmin account) with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/install/install.sh \
  | bash -s -- --domain vpn.example.com \
      --bot-token 1234567890:AAF-your-token-from-botfather \
      --admin-tg 123456789
```

Or deploy without Telegram (Web Cabinet & Admin Panel only):

```bash
curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/install/install.sh \
  | bash -s -- --domain vpn.example.com
```

### What the installer does:
1. Installs Docker and Docker Compose (if not already installed).
2. Verifies DNS propagation (checks that your domain points to the server's public IP).
3. Generates secure cryptographic secrets (`FIBR_ADMIN_TOKEN`, `FIBR_SECRET`, WireGuard & Xray Reality keys).
4. Configures firewall rules (`ufw` ports 80/tcp, 443/tcp, WireGuard UDP, and Xray TCP).
5. Starts the stack (`fibr` + `caddy` + `xray`) and prints credentials and access links.

> 💡 **Idempotent:** Running the installer again is safe — it updates configuration and settings in `.env` without overwriting existing data. See [docs/getting-started.md](docs/getting-started.md) for details.

---

## Can't connect from Russia? Use a relay

Symptom: a user in Russia cannot even open `https://your-domain/cabinet/` without a VPN (timeout), while other sites open. Then the provider blocks your hoster's IP range, and no protocol will help. The fix is a relay on a VPS **inside Russia**. It only forwards traffic to the main server and decrypts nothing.

```
phone ──► VPS in Russia (nginx forwarder) ──► main Fibr server ──► internet
```

1. **On the Russian VPS** (clean Ubuntu/Debian, port 443 free):
   ```bash
   curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/relay/install.sh -o relay.sh
   bash relay.sh --upstream MAIN_SERVER_IP
   ```
2. **On the main server:**
   ```bash
   echo "FIBR_RELAY_HOST=RELAY_IP" >> /opt/fibr/install/.env
   cd /opt/fibr/install && docker compose up -d
   ```
3. **Users:** keep "🇷🇺 I am in Russia" on in the cabinet (on by default) and take the QR / file / link again.

Details, checks and limitations (in Russian): [docs/relay.md](docs/relay.md).

---

## Download Fibr Client

Pre-built binaries are available on the [Releases](https://github.com/fenjo26/Fibr/releases) page and built automatically by GitHub Actions CI/CD.

### Host Direct Downloads
After running `bash install/sync-downloads.sh` on your server, short download links are hosted directly on your domain (convenient for Android TV using the Downloader app):

| Short Link | Target |
|---|---|
| `https://your-domain/tv.apk` | Android APK (phones, tablets, and Android TV) |
| `https://your-domain/app.apk` | Mirror APK link for easy sharing in messengers |
| `https://your-domain/windows.zip` | Windows desktop client bundle |
| `https://your-domain/macos.zip` | macOS desktop client bundle |

### GitHub Releases:

| Platform | Download |
|---|---|
| **Android / Android TV (APK)** | [Fibr-Android.apk](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-Android.apk) |
| **Windows (.zip)** | [Fibr-Windows.zip](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-Windows.zip) |
| **macOS (.zip)** | [Fibr-macOS.zip](https://github.com/fenjo26/Fibr/releases/latest/download/Fibr-macOS.zip) |
| **iOS / iPadOS** | Use official [WireGuard](https://apps.apple.com/app/wireguard/id1441195209) or AmneziaVPN |

> ℹ️ **Notice:** All device configs can also be loaded into the official WireGuard and AmneziaVPN apps via QR or `.conf` file.

---

## Updates

### Via SSH (One command):
```bash
cd /opt/fibr && git pull && docker compose -f install/docker-compose.yml up -d --build
```

### One-Click Updates via Admin Panel:
When installed with `--with-updater`, an update notification badge and an "Update" button appear in the admin dashboard. The updater sidecar pulls the latest commit from GitHub and rebuilds containers automatically ([docs/updates.md](docs/updates.md)).

---

## Telegram Bot & Mini App Configuration

If you installed without `--bot-token`, you can enable the bot at any time by re-running the installer:

```bash
curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/install/install.sh \
  | bash -s -- --domain vpn.example.com \
      --bot-token <TOKEN_FROM_BOTFATHER> \
      --admin-tg <YOUR_TELEGRAM_ID>
```

Then in [@BotFather](https://t.me/BotFather):
1. Send `/mybots` → select your bot → **Bot Settings** → **Menu Button**.
2. Enter the URL: `https://vpn.example.com/miniapp/`.
3. To enable web login widget: **Bot Settings** → **Domain** → `vpn.example.com`.

Opening your bot in Telegram and tapping **Start** displays the Mini App button right next to the chat input field.

---

## Accessing the Interfaces

- **User Web Cabinet:** `https://vpn.example.com/cabinet/` — browser client accessible without Telegram (supports invite links, access keys, or username/password).
- **Telegram Mini App:** The menu button in your Telegram bot.
- **Admin Dashboard:** `https://vpn.example.com/admin/` — log in using the `FIBR_ADMIN_TOKEN` found in `/opt/fibr/install/.env`.

---

## Repository Structure

```
├── server/          # Go backend: REST API, WireGuard/AWG/Xray manager, Telegram bot
│   ├── cmd/fibr/    # Primary server binary
│   ├── cmd/fibr-node/    # Multi-node agent (docs/multinode.md)
│   └── cmd/fibr-updater/ # One-click update sidecar (docs/updates.md)
├── install/         # Deployment scripts, Caddyfile, docker-compose configuration
├── relay/           # Relay installer for a VPS inside Russia (nginx stream, docs/relay.md)
├── admin/           # Admin panel (Vanilla JS, zero build step, responsive)
├── miniapp/         # Telegram Mini App client (Vanilla JS)
├── cabinet/         # Browser-based user cabinet
├── design/          # Shared CSS tokens, brand assets, helper utilities
├── app/             # Cross-platform Flutter client (Android, iOS, Desktop)
├── luci-app-fibr/   # LuCI plugin for OpenWrt routers
└── docs/            # Guides, manuals, and architectural specifications
```

---

## Architecture

```
                       ┌──────────────────────────── VPS ────────────────────────────┐
  Telegram ⇄ Bot (long polling)                                      │
  Mini App ⇄ Caddy ──TLS── /api/* ──► fibr-api (Go) ── wgctrl ──► wg0 (kernel)
  (HTTPS)         ├─ /admin/*  static       │                     │ iptables NAT
                  ├─ /cabinet/* static      ├── SQLite (/data)    ▼
                  └─ /shared/*  tokens      └── /data/xray/config.json → xray (VLESS+Reality)
                                                                Internet
```

- **fibr-api:** Manages the Linux kernel `wg0` interface via `wgctrl`, dynamically synchronizes peers, samples traffic and handshake statistics, and serves the REST API.
- **caddy:** Automates Let's Encrypt TLS certificates, hosts static web apps, and securely reverse-proxies `/api/*`.
- **xray:** Handles VLESS + Reality traffic with TLS camouflage against deep packet inspection.
- **SQLite:** Lightweight, embedded transactional database storing users, devices, access keys, and analytics.

---

## REST API Overview

Authentication: `POST /api/auth/telegram` for Mini App (validates initData), `POST /api/auth/key` or `/password` for web cabinet, and `Authorization: Bearer <token>` for admin endpoints.

| Endpoint | Method | Description |
|---|---|---|
| `/api/me` | `GET` | Current user profile, device quotas, and status |
| `/api/me/devices` | `GET`, `POST` | List and create client devices |
| `/api/me/devices/{id}` | `DELETE` | Remove a device |
| `/api/me/devices/{id}/config` | `GET` | Download wg-quick or AWG configuration |
| `/api/me/devices/{id}/qr.png` | `GET` | Generate configuration QR code |
| `/api/dns/status` | `GET` | DNS resolver status and default filtering profile |
| `/api/games` | `GET` | Gaming catalog routes and IP CIDR lists |
| `/api/admin/overview` | `GET` | Online peers, aggregate bandwidth, speed, server keys |
| `/api/admin/users` | `GET`, `POST`, `PATCH`, `DELETE` | Manage users, roles, and expiration dates |
| `/api/admin/devices` | `GET`, `PATCH`, `DELETE` | Global device list, enable/disable, QR codes |

*For the complete API schema, see `server/internal/api/api.go`.*

---

## Security Best Practices

- **Device Private Keys:** Private keys are stored on the server to allow displaying QR codes directly in the user cabinet (standard pattern for web-managed WireGuard like wg-easy). Ensure your VPS is secured, SSH password authentication is disabled, and root login uses keys.
- **API Isolation:** The internal REST API binds only to local interfaces and is exposed only via Caddy over HTTPS.
- **Revocation:** Disabling or deleting a user immediately purges all associated peer devices from the active WireGuard interface, terminating ongoing sessions instantly.
- **Firewall:** Only necessary ports (HTTP/HTTPS, WireGuard UDP, and Xray TCP) are opened.

---

## Development

```bash
make build   # Compile bin/fibr
make test    # Run Go unit test suite
make run     # Run locally in passive mode (mocked WireGuard for macOS/testing)
```

In passive mode, the server operates on macOS or local workstations without requiring root kernel WireGuard capabilities, allowing frontend and API development on `http://127.0.0.1:8080`.

---

## Documentation Index

Explore comprehensive guides in [docs/](docs/README.md):

- [Getting Started Guide](docs/getting-started.md)
- [Connecting Client Devices](docs/devices.md)
- [Router Configuration (OpenWrt, Keenetic)](docs/routers.md)
- [Telegram Bot Setup & Commands](docs/telegram-bot.md)
- [Web Cabinet & Auth Without Telegram](docs/web-login.md)
- [DNS Protection, AdBlock & Game Mode](docs/dns-games.md)
- [Xray Protocol (VLESS + Reality)](docs/xray.md)
- [AmneziaWG Obfuscated WireGuard](docs/awg.md)
- [Multi-Node Failover & Clusters](docs/multinode.md)
- [Relay Inside Russia (when the server IP is blocked)](docs/relay.md)
- [Billing & Expiration Cascade](docs/billing.md)
- [Referral Program & Commissions](docs/referrals.md)
- [User Roles & Permissions](docs/roles.md)
- [Self-Enrollment & Invites](docs/self-enroll.md)
- [Quick Connect via Join Codes](docs/join.md)
- [One-Click Server Updates](docs/updates.md)
- [Frequently Asked Questions (FAQ)](docs/faq.md)
- [Fibr Design Language](docs/DESIGN.md)

---

## License

This project is licensed under the **MIT License** — see [LICENSE](LICENSE) for details.