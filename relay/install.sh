#!/usr/bin/env bash
# Fibr relay installer — run on a small VPS INSIDE the censored country
# (e.g. a Russian VPS). It turns the box into a plain TCP/UDP forwarder
# (nginx "stream", from the OS packages — no Docker needed, Docker Hub is
# often unreachable from Russia) in front of your main Fibr server:
#
#   phone ──► relay (domestic IP) ──► main Fibr server (abroad)
#
# The relay never decrypts anything: VLESS stays TLS end-to-end to the main
# server's Caddy, WireGuard / AmneziaWG stay encrypted end-to-end. See
# docs/relay.md.
#
# Usage:
#   bash install.sh --upstream <MAIN_SERVER_IP>
#   bash install.sh --upstream 201.51.27.151 --awg-port 51821 --wg-port 51820
#   bash install.sh --remove            # take the relay down again
#   bash install.sh --upstream X --print-config   # show the nginx config only
set -euo pipefail

UPSTREAM=""
WG_PORT=51820
AWG_PORT=51821
XRAY_PORT=8443
TLS_PORT=443
FIREWALL=1
PRINT=0
REMOVE=0
CONF=/etc/nginx/fibr-relay.conf

die()  { echo -e "\033[31m✗ $*\033[0m" >&2; exit 1; }
info() { echo -e "\033[36m• $*\033[0m"; }
ok()   { echo -e "\033[32m✔ $*\033[0m"; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --upstream)     UPSTREAM="${2:-}"; shift 2 ;;
    --wg-port)      WG_PORT="${2:-}"; shift 2 ;;
    --awg-port)     AWG_PORT="${2:-}"; shift 2 ;;
    --xray-port)    XRAY_PORT="${2:-}"; shift 2 ;;
    --tls-port)     TLS_PORT="${2:-}"; shift 2 ;;
    --no-firewall)  FIREWALL=0; shift ;;
    --print-config) PRINT=1; shift ;;
    --remove)       REMOVE=1; shift ;;
    -h|--help)      sed -n '2,20p' "$0"; exit 0 ;;
    *) die "unknown option: $1 (see --help)" ;;
  esac
done

render_config() {
  cat <<CEOF
# Fibr relay — written by relay/install.sh, see docs/relay.md.
# Plain forwarding to the main Fibr server $UPSTREAM; nothing is decrypted here.
stream {
    # VLESS over XHTTP: TLS to the Fibr domain passes through untouched to
    # the main server's Caddy (SNI and certificate stay the real domain).
    server {
        listen $TLS_PORT;
        proxy_pass $UPSTREAM:$TLS_PORT;
        proxy_connect_timeout 10s;
        proxy_timeout 1h;
    }
    # VLESS + Reality (fallback transport).
    server {
        listen $XRAY_PORT;
        proxy_pass $UPSTREAM:$XRAY_PORT;
        proxy_connect_timeout 10s;
        proxy_timeout 1h;
    }
    # WireGuard (UDP). Clients send a keepalive every 25 s, well inside the
    # 3-minute idle timeout, so a session keeps its upstream socket.
    server {
        listen $WG_PORT udp;
        proxy_pass $UPSTREAM:$WG_PORT;
        proxy_timeout 3m;
    }
    # AmneziaWG (UDP).
    server {
        listen $AWG_PORT udp;
        proxy_pass $UPSTREAM:$AWG_PORT;
        proxy_timeout 3m;
    }
}
CEOF
}

if [[ $REMOVE -eq 1 ]]; then
  [[ $EUID -eq 0 ]] || die "run as root"
  sed -i '\#include /etc/nginx/fibr-relay.conf;#d' /etc/nginx/nginx.conf 2>/dev/null || true
  rm -f "$CONF"
  nginx -t && systemctl reload nginx
  ok "relay removed (nginx itself is left installed)"
  exit 0
fi

[[ -n "$UPSTREAM" ]] || die "--upstream <MAIN_SERVER_IP> is required (the IP of your main Fibr server; an IP is safer than the domain — no DNS lookups from inside the country)"
for p in "$WG_PORT" "$AWG_PORT" "$XRAY_PORT" "$TLS_PORT"; do
  [[ "$p" =~ ^[0-9]+$ ]] && (( p >= 1 && p <= 65535 )) || die "bad port: $p"
done

if [[ $PRINT -eq 1 ]]; then
  render_config
  exit 0
fi

[[ $EUID -eq 0 ]] || die "run as root (sudo bash install.sh ...)"
command -v apt-get >/dev/null || die "this installer supports Debian/Ubuntu (apt) only"

# The relayed ports must be free: the relay takes them over completely.
busy() { # $1 = t|u, $2 = port -> prints the owner when taken by something else
  ss -l"$1"np 2>/dev/null | grep -E "[:.]$2\s" | grep -v '"nginx"' || true
}
if command -v ss >/dev/null; then
  for spec in "t:$TLS_PORT" "t:$XRAY_PORT" "u:$WG_PORT" "u:$AWG_PORT"; do
    owner="$(busy "${spec%%:*}" "${spec#*:}")"
    if [[ -n "$owner" ]]; then
      echo "$owner"
      die "port ${spec#*:}/$([[ ${spec%%:*} == t ]] && echo tcp || echo udp) is already used by another program — use a clean VPS for the relay (or move it with --xray-port / --wg-port / --awg-port)"
    fi
  done
fi

info "installing nginx with the stream module"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq nginx libnginx-mod-stream >/dev/null

info "writing $CONF"
render_config > "$CONF"
# stream{} lives at the top level of nginx.conf, next to http{}.
grep -q 'include /etc/nginx/fibr-relay.conf;' /etc/nginx/nginx.conf || \
  printf '\n# Fibr relay (docs/relay.md)\ninclude /etc/nginx/fibr-relay.conf;\n' >> /etc/nginx/nginx.conf
# The relay box is dedicated: drop the stock "default" web site. It is not
# needed, and its "listen [::]:80" makes nginx fail to start on VPSes with
# IPv6 disabled.
rm -f /etc/nginx/sites-enabled/default

nginx -t
systemctl enable nginx >/dev/null 2>&1 || true
systemctl restart nginx
ok "nginx relay running"

if [[ $FIREWALL -eq 1 ]] && command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
  ufw allow "$TLS_PORT/tcp" >/dev/null
  ufw allow "$XRAY_PORT/tcp" >/dev/null
  ufw allow "$WG_PORT/udp" >/dev/null
  ufw allow "$AWG_PORT/udp" >/dev/null
  ok "ufw: opened $TLS_PORT,$XRAY_PORT/tcp and $WG_PORT,$AWG_PORT/udp"
fi

# Self-check: can this box reach the main server at all?
if timeout 6 bash -c "</dev/tcp/$UPSTREAM/$TLS_PORT" 2>/dev/null; then
  ok "main server $UPSTREAM:$TLS_PORT is reachable from the relay"
else
  echo -e "\033[33m! cannot open TCP $UPSTREAM:$TLS_PORT from here — check the IP and the main server's firewall\033[0m"
fi

MYIP=""
for u in https://api.ipify.org https://ifconfig.me https://ipv4.icanhazip.com; do
  MYIP="$(curl -4 -fsS --max-time 5 "$u" 2>/dev/null | tr -d '[:space:]')" && [[ -n "$MYIP" ]] && break
done
MYIP="${MYIP:-<IP of this relay>}"

cat <<DONE

──────────────────────────────────────────────────────────────
  Relay is up:  $MYIP  ──►  $UPSTREAM
    tcp/$TLS_PORT   VLESS over XHTTP (TLS passes through)
    tcp/$XRAY_PORT  VLESS + Reality (fallback)
    udp/$WG_PORT    WireGuard
    udp/$AWG_PORT   AmneziaWG

  Now on the MAIN Fibr server:
    echo "FIBR_RELAY_HOST=$MYIP" >> /opt/fibr/install/.env
    cd /opt/fibr/install && docker compose up -d

  Then users take a fresh config / QR / link in the cabinet
  ("I'm in Russia" switch on). Details: docs/relay.md
──────────────────────────────────────────────────────────────
DONE
