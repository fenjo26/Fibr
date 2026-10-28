#!/usr/bin/env bash
# Pulls the latest client binaries from GitHub Releases into download/,
# where Caddy serves them at https://<domain>/download/ .
#
#   bash install/sync-downloads.sh            # from a repo checkout
#
# Run it after each release, or from cron to keep the server in sync:
#   0 6 * * *  cd /opt/fibr && bash install/sync-downloads.sh >/dev/null 2>&1
set -euo pipefail

REPO="${FIBR_REPO:-fenjo26/Fibr}"
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../download" && pwd)"
BASE="https://github.com/${REPO}/releases/latest/download"

mkdir -p "$DIR"
ok=0
for f in Fibr-Android.apk Fibr-Windows.zip Fibr-macOS.zip; do
  echo "fetching $f ..."
  if curl -fSL --retry 3 -o "$DIR/$f.tmp" "$BASE/$f"; then
    mv "$DIR/$f.tmp" "$DIR/$f"
    ok=$((ok + 1))
  else
    rm -f "$DIR/$f.tmp"
    echo "  skipped (no such release asset yet?): $f" >&2
  fi
done

echo "done: $ok file(s) in $DIR — available at https://<your-domain>/download/"
