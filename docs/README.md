# Fibr Documentation & Wiki Hub

**English** | [Русский](README.ru.md) | [Back to Main README](../README.md)

Welcome to the comprehensive documentation directory for the **Fibr** self-hosted VPN platform.

---

## Documentation Directory

### 🚀 Getting Started & Client Setup

| Document | Description |
|---|---|
| [getting-started.md](getting-started.md) | **Step-by-step beginner guide:** VPS selection, DNS setup, one-command installation, admin panel login, and initial Telegram bot configuration. |
| [devices.md](devices.md) | **Connecting devices:** Instructions for Android, iPhone/iPad, Windows, macOS, Linux, and Android TV; configuring official WireGuard and AmneziaVPN apps. |
| [web-login.md](web-login.md) | **Web cabinet without Telegram:** Browser access via invite links, one-time access keys, username/password credentials, or Telegram widget. |
| [faq.md](faq.md) | **Frequently Asked Questions:** Performance tuning, port modification, server migration, forgotten admin credentials, and MTU optimization. |

### 🛡️ Protocols, Censorship Resistance & DNS

| Document | Description |
|---|---|
| [awg.md](awg.md) | **AmneziaWG (Obfuscated WireGuard):** Defeating deep packet inspection (DPI) with junk packets (Jc, Jmin-Jmax), header morphing (H1–H4), and handshake padding (S1–S2). |
| [xray.md](xray.md) | **Xray (VLESS + Reality):** Secondary anti-censorship protocol disguising VPN traffic as genuine TLS connections to major CDN hosts. |
| [dns-games.md](dns-games.md) | **DNS Protection & Game Mode:** AdBlock, parental control (Family filter with schedule), anti-leak rules, custom AdGuard lists, and game-specific routing (low ping for CS2, Dota 2, Valorant). |
| [routers.md](routers.md) | **Routers & Whole-Home VPN:** Configuring OpenWrt with `luci-app-fibr`, Keenetic, Mikrotik, and routing setup for Apple TV and Smart TVs. |

### 👥 Users, Access Control & Monetization

| Document | Description |
|---|---|
| [roles.md](roles.md) | **Role-Based Access Control:** Superadmin (system owner), reseller admin (isolated user branch management), and end-users. |
| [billing.md](billing.md) | **Access Expiration & Subscriptions:** Setting `expires_at`, branch cascade revocation, manual renewals, and lifetime mode. |
| [referrals.md](referrals.md) | **Referral Program:** Referral codes and links, 2-tier commissions (15% L1 / 5% L2), user balance ledger, and manual payout processing. |
| [self-enroll.md](self-enroll.md) | **Self-Enrollment:** User onboarding via invite links, invite-only server mode, and bot deep links. |
| [join.md](join.md) | **Fast Connect (Join Codes):** One-time short codes and `/join` links for instant onboarding in the Fibr client app. |

### ⚙️ Infrastructure & Operations

| Document | Description |
|---|---|
| [telegram-bot.md](telegram-bot.md) | **Telegram Bot:** Commands reference, `FIBR_ADMIN_TELEGRAM_IDS` superadmin privileges, and Mini App integration. |
| [multinode.md](multinode.md) | **Multi-Node Clusters:** Adding secondary worker nodes, instant endpoint IP hot-swapping during provider blocks without regenerating keys. |
| [updates.md](updates.md) | **Server Updates:** One-click automated updates from the web panel (`--with-updater`) and manual SSH upgrade workflows. |
| [DESIGN.md](DESIGN.md) | **Design Specification:** UI tokens, color palettes (dark and light themes), typography rules (Inter 600/800), and card layout principles. |

---

## Architecture at a Glance

```
                      ┌──────────────────────────── VPS ────────────────────────────┐
 Telegram ⇄ Bot (long polling)                                      │
 Mini App ⇄ Caddy ──TLS── /api/* ──► fibr-api (Go) ── wgctrl ──► wg0 (kernel)
 (HTTPS)         ├─ /admin/*  static       │                     │ iptables NAT
                 ├─ /cabinet/* static      ├── SQLite (/data)    ▼
                 └─ /shared/*  tokens      └── /data/xray/config.json → xray (VLESS+Reality)
                                                               Internet
```

---

## Quick Reference Links

- **Main Project Readme:** [README.md](../README.md)
- **Russian Readme:** [README.ru.md](../README.ru.md)
- **Flutter Client Readme:** [app/README.md](../app/README.md)
- **Downloads Directory Guide:** [download/README.md](../download/README.md)
- **Installer CLI Options:** `bash install/install.sh --help`
- **Configuration Defaults:** [`install/.env.example`](../install/.env.example)
