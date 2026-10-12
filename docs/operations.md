# Эксплуатация экземпляров

Один ресторан — один экземпляр (каталог с `.env` + проект docker compose). Скрипты — в `deploy/scripts/`, `docker-compose.yml` они не меняют. Нужны `bash`, `docker compose`, `curl`.

## Новый ресторан

```
deploy/scripts/new-restaurant.sh cafe-lenina [порт]
```

Создаёт `instances/cafe-lenina/.env` (права 600) с настройками чистого экземпляра (`DEMO_MODE=false`, `SEED_ON_EMPTY=false`, `CLOCK_MODE=real`, `AUTH_REQUIRED=true`), случайными паролями PostgreSQL и admin и свободным портом. Каталог `instances/` содержит секреты: держите его вне репозитория или не коммитьте (`INSTANCES_ROOT` задаёт другое место). Скрипт печатает следующие шаги: запуск, первый вход admin и смена пароля, выпуск ключа адаптера (роль `operator`), запуск адаптера в `ADAPTER_MODE=shadow`.

## Резервные копии

```
deploy/scripts/backup.sh instances/cafe-lenina
```

`pg_dump -Fc` в `instances/cafe-lenina/backups/<YYYY-MM-DD>/`. Копии старше `RETENTION_DAYS` (по умолчанию 14) дней удаляются. `BACKUP_ROOT` меняет каталог; хранить копии стоит на другом диске или хосте.

### Cron

```
# crontab -e: копия каждую ночь, сразу за ней проверка восстановления
15 3 * * * cd /opt/umirhack && deploy/scripts/backup.sh instances/cafe-lenina && deploy/scripts/restore-check.sh instances/cafe-lenina >> /var/log/umirhack-backup.log 2>&1
```

Ненулевой код выхода `restore-check.sh` — повод для оповещения (например, `|| mail ...`). Копия, восстановление которой не проверено, копией не считается.

## Проверка восстановления

`deploy/scripts/restore-check.sh instances/cafe-lenina` берёт последнюю копию, разворачивает её во временную базу в том же PostgreSQL, сравнивает число строк в `batches` и `movements` с рабочей базой и проверяет `GET /healthz` экземпляра. Временная база удаляется. Рабочая база только читается. Если между копией и проверкой в ресторане были поставки или заказы, счётчики разойдутся: запускайте проверку сразу после `backup.sh`.

## Восстановление после сбоя

1. Остановить API: `docker compose -p cafe-lenina -f deploy/docker-compose.yml --env-file instances/cafe-lenina/.env stop api`.
2. Пересоздать базу и развернуть копию:
   ```
   dc="docker compose -p cafe-lenina -f deploy/docker-compose.yml --env-file instances/cafe-lenina/.env"
   source instances/cafe-lenina/.env
   $dc exec -T postgres psql -U $POSTGRES_USER -d postgres -c "DROP DATABASE $POSTGRES_DB" -c "CREATE DATABASE $POSTGRES_DB"
   $dc exec -T -e PGPASSWORD=$POSTGRES_PASSWORD postgres pg_restore --no-owner -U $POSTGRES_USER -d $POSTGRES_DB < instances/cafe-lenina/backups/<дата>/<файл>.dump
   ```
3. Запустить API: `$dc up -d api`, проверить `curl -fsS http://127.0.0.1:<порт>/healthz`.
4. Склад восстановлен на момент копии; поставки и заказы после неё надо перепослать из учётной системы (поставки идемпотентны по id).

## Первый запуск на сервере

Стенд (VPS 1 vCPU / 2 ГБ, Ubuntu 24.04) разворачивается кодом: `deploy/docker-compose.prod.yml`, проект `swapowl`. Чужой контейнер `postgresql` на сервере не трогаем: наш PostgreSQL свой, внутри сети compose, порт не публикуется, том `swapowl_pgdata`. Наружу открыты только 80 и 443 (Caddy).

1. **DNS.** A-запись `SITE_DOMAIN` на IP сервера. Без неё Let's Encrypt не выдаст сертификат.
2. **Секреты GitHub** (Settings → Secrets → Actions): `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_SSH_KEY` (закрытый ключ пользователя с доступом к docker).
3. **Сервер:** `scp deploy/scripts/server-init.sh root@host:/tmp/ && ssh root@host bash /tmp/server-init.sh` — swap-файл 2 ГБ (если нет), ufw 22/80/443, каталог `/opt/swapowl`. Скрипт можно запускать повторно.
4. **`.env`:** `cp deploy/.env.prod.example /opt/swapowl/.env`, `chmod 600`, заполнить `SITE_DOMAIN`, `POSTGRES_PASSWORD`, `ADMIN_PASSWORD`, `DEMO_TOKEN`, `METRICS_TOKEN`, `IIKO_PASSWORD` (общий для fake-iiko и адаптера, должен быть непустым). `ADAPTER_API_KEY` и `NOTIFIER_API_KEY` — ключи интеграции из API (выпускаются после первого запуска, затем `deploy.sh` повторно); пока они пусты, `iiko-adapter` и `notifier` перезапускаются — это нормально.
5. **Первый деплой вручную:** Actions → Deploy → Run workflow, `tag` = sha коммита из `main`, для которого собраны образы (образы собирает запуск после зелёного CI на push в `main`; если их ещё нет, дождитесь первого такого запуска). Дальше выкатка идёт сама.
6. Проверка: `curl -fsS https://<SITE_DOMAIN>/healthz`.

`/metrics` доступен только с loopback/частных адресов и дополнительно требует `Authorization: Bearer $METRICS_TOKEN`.

`deploy.sh <tag>`: запоминает текущий тег в `/opt/swapowl/.last_good`, перед обновлением делает `backup.sh`, выполняет `pull` и `up -d`, до 60 с ждёт `/healthz`. Не дождался — возвращает прошлый тег и выходит с кодом 1 (workflow красный).

## Откат

Actions → Deploy → Run workflow, `tag` = нужный sha (или тег предыдущего успешного деплоя). Либо на сервере: `/opt/swapowl/deploy/scripts/deploy.sh <tag>`. Миграции базы откатом не отменяются; при несовместимой схеме восстановите копию (см. «Восстановление после сбоя»).

## Оповещения

Отдельный процесс `watchdog` (`cmd/watchdog`, образ `deploy/Dockerfile.watchdog`) раз в `WATCHDOG_EVERY` опрашивает API и шлёт сообщения в Telegram **инженеру** (отдельные бот и чат, не те, что у управляющего в `notifier`). Одно сообщение при входе в тревогу, одно при выходе; пока тревога держится, повтор не чаще `WATCHDOG_REPEAT`. Если Telegram недоступен, состояние не меняется и отправка повторится на следующей проверке.

| Правило | Порог | Источник |
|---|---|---|
| API недоступен | 2 проверки подряд | `GET /healthz` |
| Нет синхронизации адаптера | `WATCHDOG_SYNC_MAX` (15 мин) | `/metrics`: `adapter_last_sync_age_seconds` |
| Старое неподтверждённое сообщение outbox | `WATCHDOG_OUTBOX_MAX` (10 мин) | `/metrics`: `outbox_oldest_unacked_age_seconds` (+ `outbox_undelivered` в тексте) |
| Адаптер недоступен (необязательно) | 1 проверка | `GET $WATCHDOG_ADAPTER_URL/healthz` |
| Мало места на диске | свободно < 10 % (`WATCHDOG_DISK_MIN_FREE`) | `statfs` каталога `WATCHDOG_DATA_DIR` |

Правила по метрикам работают, только если задан `WATCHDOG_METRICS_TOKEN` (= `METRICS_TOKEN` API) и метрика есть в ответе; нет метрики — правило молча пропускается. **Сейчас API отдаёт только `outbox_undelivered`**; `outbox_oldest_unacked_age_seconds` и `adapter_last_sync_age_seconds` нужно добавить в `internal/metrics`/`cmd/api` отдельной задачей.

**Диск.** Сторож видит только то, что смонтировано в его контейнер. Чтобы проверять диск PostgreSQL, смонтируйте в него тот же volume (только чтение) и укажите `WATCHDOG_DATA_DIR`. Без монтирования и без `WATCHDOG_DATA_DIR` правило пропускается.

Переменные: `WATCHDOG_API_URL`, `WATCHDOG_TG_TOKEN`, `WATCHDOG_TG_CHAT` обязательны; `WATCHDOG_EVERY=30s`, `WATCHDOG_SYNC_MAX=15m`, `WATCHDOG_OUTBOX_MAX=10m`, `WATCHDOG_REPEAT=1h` по умолчанию.

### Строки для `deploy/docker-compose.prod.yml`

Сервис (рядом с `notifier`):

```yaml
  watchdog:
    image: ${IMAGE_REPO:-ghcr.io/immxrtalbeast/umirhack-2026}/watchdog:${IMAGE_TAG:-latest}
    depends_on:
      - api
    environment:
      WATCHDOG_API_URL: http://api:8080
      WATCHDOG_ADAPTER_URL: ${WATCHDOG_ADAPTER_URL:-}
      WATCHDOG_METRICS_TOKEN: ${METRICS_TOKEN:-}
      WATCHDOG_TG_TOKEN: ${WATCHDOG_TG_TOKEN:-}
      WATCHDOG_TG_CHAT: ${WATCHDOG_TG_CHAT:-}
      WATCHDOG_EVERY: ${WATCHDOG_EVERY:-30s}
      WATCHDOG_SYNC_MAX: ${WATCHDOG_SYNC_MAX:-15m}
      WATCHDOG_OUTBOX_MAX: ${WATCHDOG_OUTBOX_MAX:-10m}
      WATCHDOG_REPEAT: ${WATCHDOG_REPEAT:-1h}
      WATCHDOG_DATA_DIR: ${WATCHDOG_DATA_DIR:-}
    mem_limit: 32m
    restart: unless-stopped
    logging: *logging
```

В `deploy/.env.prod.example`: `WATCHDOG_TG_TOKEN=`, `WATCHDOG_TG_CHAT=`. Сборку образа `watchdog` добавить в тот же CI/`deploy.sh`, что собирает `notifier`.
