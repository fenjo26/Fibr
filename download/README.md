# Client Downloads Directory (`download/`)

[English](README.md) | [Русский](../README.ru.md)

This directory hosts pre-built client installers served directly by the web server at `https://<your-domain>/download/` (mounted read-only into Caddy with directory browsing enabled).

---

## Short Download URLs

To make it easy to download clients on TV remotes or share in chat apps, Caddy redirects the following convenient short URLs:

| Short URL | Redirects To | Target Platform |
|---|---|---|
| `https://<domain>/tv.apk` | `/download/Fibr-Android.apk` | Android TV & Phone APK |
| `https://<domain>/app.apk` | `/download/Fibr-Android.apk` | Android APK mirror |
| `https://<domain>/windows.zip` | `/download/Fibr-Windows.zip` | Windows desktop archive |
| `https://<domain>/macos.zip` | `/download/Fibr-macOS.zip` | macOS desktop archive |

---

## Syncing Release Binaries

To fetch the latest pre-compiled binaries from the GitHub Releases page onto your server:

```bash
bash install/sync-downloads.sh
```

### Automatic Sync via Cron (Recommended)
To keep downloads up-to-date automatically every day at 06:00:

```bash
# Add to server crontab (crontab -e):
0 6 * * * cd /opt/fibr && bash install/sync-downloads.sh >/dev/null 2>&1
```

> ⚠️ Note: Binary files (`*.apk`, `*.zip`, `*.exe`) are excluded from git tracking via `.gitignore`.
