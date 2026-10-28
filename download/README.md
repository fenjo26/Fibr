# download/

Здесь сервер раздаёт установщики клиентов по адресу
`https://<ваш-домен>/download/` (каталог примонтирован к Caddy только на
чтение). Особенно удобно для Android TV: в приложении **Downloader**
достаточно ввести `ваш-домен/download/Fibr-Android.apk`.

Актуальные файлы сюда заливает скрипт из GitHub Releases:

```bash
bash install/sync-downloads.sh        # разово
# или в cron, чтобы всегда была свежая версия:
# 0 6 * * *  cd /opt/fibr && bash install/sync-downloads.sh >/dev/null
```

Сами бинарники в git не коммитятся (см. .gitignore).
