#!/usr/bin/env bash
# Fibr one-command installer.
#
#   curl -fsSL https://raw.githubusercontent.com/fenjo26/Fibr/main/install/install.sh | bash -s -- --domain vpn.example.com
#
# Or from a checkout:
#   bash install/install.sh --domain vpn.example.com
#
# Options:
#   --domain DOMAIN   public domain pointed at this server (A record)
#   --port PORT       WireGuard UDP port (default 51820)
#   --bot-token TOKEN set up the Telegram bot in the same command (from @BotFather)
#   --admin-tg IDS    your numeric Telegram id(s), comma-separated (makes you superadmin)
#   (all three protocols are on by default: WireGuard udp/--port, AmneziaWG
#    udp/51821 as a per-device choice, VLESS/Xray tcp/--xray-port)
#   --awg             run the WHOLE server on AmneziaWG instead of plain
#                     WireGuard (rarely needed now — see docs/awg.md)
#   --xray            Xray (VLESS + Reality) on TCP --xray-port — ON by default now
#   --no-xray         do not enable Xray (WireGuard only)
#   --xray-sni HOST   Reality camouflage host (default www.microsoft.com; implies --xray)
#   --xray-port PORT  Xray TCP port (default 8443; implies --xray)
#   --with-updater    also enable one-click updates from the admin panel (docs/updates.md)
#   --dev             local dev mode (no domain, no TLS, no DNS check)
#   --skip-dns-check  install even if the DNS check fails (e.g. behind NAT)
#   --force           regenerate .env secrets on an existing install
#   --uninstall       stop and remove containers (keeps /opt/fibr and volumes)
#   --purge           uninstall and delete everything including data
#   --help            this text
set -euo pipefail

FIBR_REPO="${FIBR_REPO:-https://github.com/fenjo26/Fibr.git}"
FIBR_DIR="/opt/fibr"
WG_PORT="51820"
AWG=0
XRAY=1
UPDATER=0
XRAY_SNI="www.microsoft.com"
XRAY_PORT="8443"
DOMAIN=""
DEV=0
SKIP_DNS=0
FORCE=0
UNINSTALL=0
PURGE=0
BOT_TOKEN=""
ADMIN_TG=""

# ---------- output helpers ----------
if [[ -t 2 ]]; then
  C_INFO=$'\e[1;32m'; C_WARN=$'\e[1;33m'; C_ERR=$'\e[1;31m'; C_OFF=$'\e[0m'
else
  C_INFO=""; C_WARN=""; C_ERR=""; C_OFF=""
fi
info() { echo "${C_INFO}[fibr]${C_OFF} $*" >&2; }
warn() { echo "${C_WARN}[fibr]${C_OFF} $*" >&2; }
err()  { echo "${C_ERR}[fibr]${C_OFF} $*" >&2; }
die()  { err "$@"; exit 1; }

usage() { sed -n '2,23p' "$0" 2>/dev/null || grep -A20 '^# Fibr' "$0"; exit "${1:-0}"; }

# ---------- parse args ----------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --domain|-d)      DOMAIN="${2:-}"; shift 2 ;;
    --port|-p)        WG_PORT="${2:-}"; shift 2 ;;
    --awg)            AWG=1; shift ;;
    --xray)           XRAY=1; shift ;;
    --no-xray)        XRAY=0; shift ;;
    --with-updater)   UPDATER=1; shift ;;
    --xray-sni)       XRAY_SNI="${2:-}"; XRAY=1; shift 2 ;;
    --xray-port)      XRAY_PORT="${2:-}"; XRAY=1; shift 2 ;;
    --dev)            DEV=1; shift ;;
    --bot-token)      BOT_TOKEN="${2:-}"; shift 2 ;;
    --admin-tg)       ADMIN_TG="${2:-}"; shift 2 ;;
    --skip-dns-check) SKIP_DNS=1; shift ;;
    --force|-f)       FORCE=1; shift ;;
    --uninstall)      UNINSTALL=1; shift ;;
    --purge)          UNINSTALL=1; PURGE=1; shift ;;
    --help|-h)        usage 0 ;;
    *)                err "unknown option: $1"; usage 1 ;;
  esac
done
[[ -n "$WG_PORT" ]] || die "--port needs a value"
[[ -n "$XRAY_SNI" ]] || die "--xray-sni needs a value"
[[ -n "$XRAY_PORT" ]] || die "--xray-port needs a value"
[[ -n "$BOT_TOKEN" ]] || die "--bot-token needs a value"
[[ -n "$ADMIN_TG" ]] || die "--admin-tg needs a value"

# ---------- must be root ----------
if [[ $EUID -ne 0 ]]; then
  info "re-running as root via sudo..."
  exec sudo -E "$0" "$@"
fi

# ---------- uninstall path ----------
if [[ $UNINSTALL -eq 1 ]]; then
  if [[ ! -d "$FIBR_DIR/install" ]]; then
    die "nothing to uninstall at $FIBR_DIR"
  fi
  cd "$FIBR_DIR/install"
  # --profile xray/updater so previously started optional containers are
  # removed too.
  if [[ $PURGE -eq 1 ]]; then
    info "purging containers, volumes and $FIBR_DIR..."
    docker compose --profile xray --profile updater down -v --remove-orphans || true
    rm -rf "$FIBR_DIR"
  else
    info "stopping containers (data volumes kept; rerun installer to update)..."
    docker compose --profile xray --profile updater down --remove-orphans || true
  fi
  info "done."
  exit 0
fi

# ---------- prerequisites ----------
if [[ $DEV -eq 1 ]]; then
  DOMAIN="localhost"
  SKIP_DNS=1
  info "dev mode: domain=localhost, plain HTTP, no DNS check"
else
  [[ -n "$DOMAIN" ]] || die "specify --domain vpn.example.com (or --dev for local testing)"
fi

if [[ -r /etc/os-release ]]; then
  # shellcheck disable=SC1091
  . /etc/os-release
  case "${ID:-unknown}" in
    ubuntu|debian) info "OS: ${PRETTY_NAME:-$ID}" ;;
    *) warn "untested OS: ${PRETTY_NAME:-unknown} — installer assumes a Debian-like system" ;;
  esac
else
  warn "cannot detect OS (/etc/os-release missing)"
fi

command -v curl >/dev/null || die "curl is required"
command -v docker >/dev/null || {
  info "installing Docker..."
  curl -fsSL https://get.docker.com | sh
}
docker compose version >/dev/null 2>&1 || die "docker compose plugin is required (see https://docs.docker.com/compose/install/)"
systemctl is-active docker >/dev/null 2>&1 || systemctl start docker || true

# ---------- get the repo ----------
# When piped (curl | bash) there is no script path — guard against unbound
# BASH_SOURCE so the fallback git-clone branch is taken cleanly.
SCRIPT_SRC=""
if [[ -n "${BASH_SOURCE[0]:-}" ]]; then
  SCRIPT_SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." 2>/dev/null && pwd || true)"
fi
if [[ -n "$SCRIPT_SRC" && -f "$SCRIPT_SRC/install/docker-compose.yml" ]]; then
  if [[ "$SCRIPT_SRC" != "$FIBR_DIR" ]]; then
    info "copying repo from $SCRIPT_SRC to $FIBR_DIR..."
    mkdir -p "$FIBR_DIR"
    # .git is normally skipped (a checkout copy does not need history), but
    # the --with-updater sidecar pulls updates with git, so keep it then.
    if [[ $UPDATER -eq 1 && -d "$SCRIPT_SRC/.git" ]]; then
      tar -C "$SCRIPT_SRC" \
        --exclude=./data --exclude=./install/.env \
        --exclude=./server/bin -cf - . | tar -C "$FIBR_DIR" -xf -
    else
      tar -C "$SCRIPT_SRC" \
        --exclude=./.git --exclude=./data --exclude=./install/.env \
        --exclude=./server/bin -cf - . | tar -C "$FIBR_DIR" -xf -
    fi
  fi
elif [[ -d "$FIBR_DIR/install" ]]; then
  info "reusing existing checkout at $FIBR_DIR"
else
  command -v git >/dev/null || { apt-get update -qq && apt-get install -y -qq git; }
  info "cloning $FIBR_REPO into $FIBR_DIR..."
  git clone --depth 1 "$FIBR_REPO" "$FIBR_DIR"
fi
cd "$FIBR_DIR/install"

# ---------- dns check ----------
if [[ $SKIP_DNS -eq 0 ]]; then
  PUBLIC_IP="$(curl -fsS4 --max-time 8 https://api.ipify.org 2>/dev/null || curl -fsS4 --max-time 8 https://ifconfig.me 2>/dev/null || true)"
  DNS_IP="$(getent hosts "$DOMAIN" 2>/dev/null | awk '{print $1; exit}' || true)"
  if [[ -z "$DNS_IP" ]]; then
    die "domain $DOMAIN does not resolve. Create an A record pointing to this server, or pass --skip-dns-check."
  fi
  if [[ -n "$PUBLIC_IP" && "$DNS_IP" != "$PUBLIC_IP" ]]; then
    err "DNS mismatch: $DOMAIN resolves to $DNS_IP, but this server's public IP is $PUBLIC_IP."
    err "If the domain is behind Cloudflare proxy (orange cloud), switch the record to DNS-only."
    err "If you know what you are doing, rerun with --skip-dns-check."
    exit 1
  fi
  info "DNS ok: $DOMAIN -> $DNS_IP"
fi

# ---------- firewall ----------
if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
  ufw allow 80/tcp  >/dev/null
  ufw allow 443/tcp >/dev/null
  ufw allow "$WG_PORT/udp" >/dev/null
  ufw allow 51821/udp >/dev/null # AmneziaWG tunnel (FIBR_AWG_PORT)
  # Xray speaks TCP (Reality masquerades as TLS), unlike WireGuard's UDP.
  ufw allow "$XRAY_PORT/tcp" >/dev/null
  info "ufw: opened 80,443/tcp, $WG_PORT/udp, 51821/udp and $XRAY_PORT/tcp"
fi

# ---------- env ----------
rand_hex() {
  if command -v openssl >/dev/null; then openssl rand -hex "$1"
  else head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'; fi
}

# compose_profile_add ensures <profile> is listed in the ACTIVE
# COMPOSE_PROFILES of <file>, preserving the other profiles already enabled
# (e.g. "xray" when adding "updater", and vice versa). A commented
# #COMPOSE_PROFILES= hint is replaced, but its value is NOT inherited — a
# dormant "#COMPOSE_PROFILES=xray" must not silently start the xray
# container. The result is exactly one active line at the end of the file
# (dotenv semantics: the last assignment wins, duplicates are a foot-gun).
compose_profile_add() {
  local file="$1" want="$2" cur="" line
  line="$(grep -E '^COMPOSE_PROFILES=' "$file" | tail -1 || true)"
  if [[ -n "$line" ]]; then cur="${line#COMPOSE_PROFILES=}"; fi
  case ",${cur}," in
    *",${want},"*) return 0 ;; # already enabled — nothing to do
  esac
  sed -i.bak -E '/^#?COMPOSE_PROFILES=/d' "$file"
  printf 'COMPOSE_PROFILES=%s\n' "${cur:+${cur},}${want}" >> "$file"
  rm -f "$file.bak"
}

ENV_FILE="$FIBR_DIR/install/.env"
if [[ -f "$ENV_FILE" && $FORCE -eq 0 ]]; then
  info "keeping existing .env (use --force to regenerate secrets)"
  # adopt a possibly changed port/domain into the running env
  sed -i.bak -e "s#^FIBR_DOMAIN=.*#FIBR_DOMAIN=$DOMAIN#" -e "s#^FIBR_WG_PORT=.*#FIBR_WG_PORT=$WG_PORT#" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
  # Upgrade path: a .env written before AmneziaWG existed has no
  # FIBR_WG_ENGINE key.
  if ! grep -q '^FIBR_WG_ENGINE=' "$ENV_FILE"; then
    cat >> "$ENV_FILE" <<'AEOF'

# WireGuard engine (kernel/awg) — added by installer upgrade; see docs/awg.md.
FIBR_WG_ENGINE=kernel
AEOF
    info "appended FIBR_WG_ENGINE=kernel to existing .env"
  fi
  # --awg switches the engine on an existing install too; without the flag a
  # manually edited engine value is left untouched.
  if [[ $AWG -eq 1 ]]; then
    sed -i.bak -E "s|^FIBR_WG_ENGINE=.*|FIBR_WG_ENGINE=awg|" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
    info "amneziawg engine enabled in existing .env (recreate: docker compose up -d)"
  fi
  # Upgrade path: a .env written before Xray existed has no FIBR_XRAY_* keys.
  if ! grep -q '^FIBR_XRAY_ENABLED=' "$ENV_FILE"; then
    cat >> "$ENV_FILE" <<'XEOF'

# Xray (VLESS + Reality) — added by installer upgrade; see .env.example.
FIBR_XRAY_ENABLED=true
FIBR_XRAY_PORT=8443
FIBR_XRAY_SNI=www.microsoft.com
FIBR_XRAY_SHORT_ID=
XEOF
    info "appended default xray settings to existing .env"
  fi
  # Upgrade path: DNS protection and the game catalog run with code
  # defaults (enabled) even without these keys; writing them keeps the
  # knobs visible to the operator.
  if ! grep -q '^FIBR_DNS_ENABLED=' "$ENV_FILE"; then
    cat >> "$ENV_FILE" <<'DEOF'

# Protected DNS + game catalog — added by installer upgrade; see docs/dns-games.md.
FIBR_DNS_ENABLED=true
FIBR_DNS_PROFILE_DEFAULT=adblock
FIBR_GAMES_ENABLED=true
DEOF
    info "appended default dns/games settings to existing .env"
  fi
  # --xray explicitly enables/updates xray on an existing install too.
  # Without the flag, manual xray edits in .env are left untouched.
  if [[ $XRAY -eq 1 ]]; then
    if [[ -z "$(grep '^FIBR_XRAY_SHORT_ID=' "$ENV_FILE" | cut -d= -f2)" ]]; then
      sed -i.bak -E "s|^FIBR_XRAY_SHORT_ID=.*|FIBR_XRAY_SHORT_ID=$(rand_hex 4)|" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
    fi
    sed -i.bak -E \
      -e "s|^FIBR_XRAY_ENABLED=.*|FIBR_XRAY_ENABLED=true|" \
      -e "s|^FIBR_XRAY_PORT=.*|FIBR_XRAY_PORT=$XRAY_PORT|" \
      -e "s|^FIBR_XRAY_SNI=.*|FIBR_XRAY_SNI=$XRAY_SNI|" \
      "$ENV_FILE" && rm -f "$ENV_FILE.bak"
    compose_profile_add "$ENV_FILE" xray
    info "xray enabled in existing .env (remember: docker compose up -d)"
  fi
  # --with-updater turns on the one-click updater sidecar on an existing
  # install as well (merged into COMPOSE_PROFILES, see docs/updates.md).
  if [[ $UPDATER -eq 1 ]]; then
    compose_profile_add "$ENV_FILE" updater
    info "updater profile enabled in existing .env (remember: docker compose up -d)"
  fi
else
  ADMIN_TOKEN="$(rand_hex 24)"
  SECRET="$(rand_hex 32)"
  cp .env.example "$ENV_FILE"
  sed -i.bak \
    -e "s#^FIBR_DOMAIN=.*#FIBR_DOMAIN=$DOMAIN#" \
    -e "s#^FIBR_WG_PORT=.*#FIBR_WG_PORT=$WG_PORT#" \
    -e "s#^FIBR_ADMIN_TOKEN=.*#FIBR_ADMIN_TOKEN=$ADMIN_TOKEN#" \
    -e "s#^FIBR_SECRET=.*#FIBR_SECRET=$SECRET#" \
    "$ENV_FILE" && rm -f "$ENV_FILE.bak"
  if [[ $AWG -eq 1 ]]; then
    sed -i.bak -E "s|^FIBR_WG_ENGINE=.*|FIBR_WG_ENGINE=awg|" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
  fi
  if [[ $XRAY -eq 1 ]]; then
    sed -i.bak -E \
      -e "s|^FIBR_XRAY_ENABLED=.*|FIBR_XRAY_ENABLED=true|" \
      -e "s|^FIBR_XRAY_PORT=.*|FIBR_XRAY_PORT=$XRAY_PORT|" \
      -e "s|^FIBR_XRAY_SNI=.*|FIBR_XRAY_SNI=$XRAY_SNI|" \
      -e "s|^FIBR_XRAY_SHORT_ID=.*|FIBR_XRAY_SHORT_ID=$(rand_hex 4)|" \
      "$ENV_FILE" && rm -f "$ENV_FILE.bak"
    compose_profile_add "$ENV_FILE" xray
  fi
  if [[ $UPDATER -eq 1 ]]; then
    compose_profile_add "$ENV_FILE" updater
  fi
  chmod 600 "$ENV_FILE"
  info "generated .env with fresh secrets"
fi

# --bot-token / --admin-tg write the Telegram settings into .env so the whole
# setup fits into one command — no manual file editing. Works both on fresh
# installs and on already-running ones (just re-run the installer).
if [[ -n "$BOT_TOKEN" || -n "$ADMIN_TG" ]]; then
  if [[ -n "$BOT_TOKEN" ]]; then
    sed -i.bak -E "s|^FIBR_TELEGRAM_BOT_TOKEN=.*|FIBR_TELEGRAM_BOT_TOKEN=$BOT_TOKEN|" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
  fi
  if [[ -n "$ADMIN_TG" ]]; then
    sed -i.bak -E "s|^FIBR_ADMIN_TELEGRAM_IDS=.*|FIBR_ADMIN_TELEGRAM_IDS=$ADMIN_TG|" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
  fi
  chmod 600 "$ENV_FILE"
  info "telegram settings written to .env"
fi

# ---------- low-memory preflight ----------
# Compiling the Go server (modernc.org/sqlite in particular) peaks at
# ~1.5-2 GB. On tiny VPS plans the build gets OOM-killed after many minutes;
# fail fast with the exact fix instead.
TOTAL_MB="$(free -m 2>/dev/null | awk '/^Mem:/{print $2}')"
SWAP_MB="$(free -m 2>/dev/null | awk '/^Swap:/{print $2}')"
TOTAL_MB="${TOTAL_MB:-4096}" # free missing? do not block
SWAP_MB="${SWAP_MB:-0}"
if (( TOTAL_MB + SWAP_MB < 2000 )); then
  err "not enough memory to compile: ${TOTAL_MB}MB RAM + ${SWAP_MB}MB swap (< 2GB total)."
  err "Create a swap file (as root) and re-run this installer:"
  cat >&2 <<'SWAPEOF'
    fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
    echo '/swapfile none swap sw 0 0' >> /etc/fstab   # survives reboots
SWAPEOF
  exit 1
fi
if (( TOTAL_MB + SWAP_MB < 3000 )); then
  warn "low memory (${TOTAL_MB}MB RAM + ${SWAP_MB}MB swap): the first build may take 10-15 minutes"
fi

# ---------- build & run ----------
info "building and starting containers (first build downloads Go modules, be patient)..."
docker compose up -d --build

# ---------- health wait ----------
info "waiting for the API to come up..."
OK=0
for _ in $(seq 1 30); do
  # </dev/null is load-bearing: with `curl | bash` the script itself arrives
  # via stdin, and `docker compose exec` attaches to that same stdin — without
  # the redirect it drains the rest of the script and bash exits silently,
  # skipping the summary. (Same class of bug as ssh/docker -i eating input.)
  if docker compose exec -T fibr wget -qO- http://127.0.0.1:8080/api/health >/dev/null 2>&1 </dev/null; then
    OK=1; break
  fi
  sleep 2
done
[[ $OK -eq 1 ]] || { err "API did not become healthy; logs:"; docker compose logs --tail 50 fibr; exit 1; }

# ---------- summary ----------
ADMIN_TOKEN_SHOW="$(grep '^FIBR_ADMIN_TOKEN=' "$ENV_FILE" | cut -d= -f2)"
if [[ $DEV -eq 1 ]]; then
  URL="http://localhost"
else
  URL="https://$DOMAIN"
fi

cat <<EOF

${C_INFO}──────────────────────────────────────────────────────────────${C_OFF}
  Fibr is up.

  Admin panel : $URL/admin/
  Admin token : $ADMIN_TOKEN_SHOW
  Mini App    : $URL/miniapp/   (needs the Telegram bot, see below)
  Web cabinet : $URL/cabinet/   (main door for users: invite link -> own
                login & password; Telegram sign-in is optional)
  Protocols   : every user picks per device —
                WireGuard  udp/$WG_PORT
                AmneziaWG  udp/51821   (for AmneziaVPN, masked WireGuard)
                VLESS      tcp/$XRAY_PORT  (Reality, SNI $XRAY_SNI)
  Firewall    : if your hosting has its own firewall panel, open the same
                ports there too (80,443/tcp, $WG_PORT+51821/udp, $XRAY_PORT/tcp).
EOF
if [[ $AWG -eq 1 ]]; then
  cat <<EOF
  Engine      : AmneziaWG (udp/$WG_PORT, DPI-obfuscated) — QR configs must be
                imported into AmneziaVPN apps, not the official WireGuard app
EOF
fi
if [[ $XRAY -eq 0 ]]; then
  cat <<EOF
  Xray        : off (--no-xray) — the VLESS option is greyed out for users
EOF
fi
if [[ $UPDATER -eq 1 ]]; then
  cat <<EOF
  Updater     : one-click updates on (compose profile "updater") — the
                "Update" button in the panel; see docs/updates.md
EOF
fi
if grep -q '^FIBR_TELEGRAM_BOT_TOKEN=..*' "$ENV_FILE"; then
  cat <<EOF

  Bot         : configured ✔ — open your bot in Telegram and press Start.
                (Optional, BotFather: /setdomain -> $DOMAIN enables the
                 "Sign in with Telegram" button in the web cabinet.)
EOF
else
  cat <<EOF

  Next steps — Telegram bot & Mini App (or re-run the installer with flags):
    bash $FIBR_DIR/install/install.sh --domain $DOMAIN \\
      --bot-token < BotFather token > --admin-tg < your numeric id >

    (@BotFather -> /newbot gives the token; your numeric id — send /id to the
    bot after Start, or ask @userinfobot. After that: Bot Settings -> Menu
    Button -> $URL/miniapp/)
EOF
fi
if [[ $XRAY -eq 1 ]]; then
  cat <<EOF

  Xray note: fibr rewrites the server config when xray devices change, but
  the xray container only reads it at startup. After adding/removing xray
  devices in the panel, apply with:
    docker compose -f $FIBR_DIR/install/docker-compose.yml restart xray
EOF
fi
cat <<EOF
  Logs        : docker compose -f $FIBR_DIR/install/docker-compose.yml logs -f fibr
  Uninstall   : bash $FIBR_DIR/install/install.sh --uninstall [--purge]
${C_INFO}──────────────────────────────────────────────────────────────${C_OFF}
EOF
