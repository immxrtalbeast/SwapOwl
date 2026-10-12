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

### Сторож в прод-compose

Сервис `watchdog` уже описан в `deploy/docker-compose.prod.yml` под профилем `ops` (в одиночном режиме не стартует без токена Telegram, иначе перезапускался бы в цикле): `docker compose -p swapowl --profile ops ... up -d watchdog`. В `deploy/.env.prod.example` есть `WATCHDOG_TG_TOKEN`, `WATCHDOG_TG_CHAT`. Образы `hub` и `watchdog` собирает `deploy.yml` вместе с остальными.

## Демо-стенд и интеграции (профиль integrations)

Демо-стенд работает **без интеграций**: по умолчанию стартуют только `caddy`, `web`, `api`, `postgres`. Сервисы `fake-iiko`, `iiko-adapter` и `notifier` вынесены в профиль `integrations` (как `ops` и `fleet`). Причина: адаптер с ключом API заливает в БД справочники эмулятора iiko и пытается залить продажи по реальным часам эмулятора, а у стенда часы симулированные. Демо-сид перезаписывается, и сценарий перестаёт воспроизводиться (помогает только сброс). `notifier` без токена Telegram перезапускается каждые несколько секунд, что даёт лишнюю нагрузку на 1 vCPU.

Профили берутся из `COMPOSE_PROFILES` в `.env` точки (`/opt/swapowl/.env` для `deploy.sh`, `points/<slug>/.env` для `fleet.sh`). В `deploy/.env.prod.example` значение пустое: `COMPOSE_PROFILES=`.

Интеграцию показывайте на отдельной точке: `fleet.sh up integ`, затем в `points/integ/.env` задайте `COMPOSE_PROFILES=integrations` (и `ADAPTER_API_KEY`, `NOTIFIER_API_KEY`) и повторите `fleet.sh up integ`. У точки своя БД и реальные часы, демо-стенд не затрагивается.

После выкатки этого изменения на уже работающем стенде остановите и удалите лишние контейнеры вручную: `docker compose -p swapowl -f deploy/docker-compose.prod.yml --env-file /opt/swapowl/.env rm -sf iiko-adapter fake-iiko notifier`.

## Много точек на одном сервере

Одиночный режим (`deploy.sh <sha>`, проект `swapowl`) остаётся по умолчанию и не меняется. Несколько ресторанов на одном сервере ведёт `deploy/scripts/fleet.sh`.

**Устройство.** Точка — отдельный compose-проект `swapowl-<slug>` из того же `docker-compose.prod.yml`: свой postgres, свой volume, свой `.env` в `/opt/swapowl/points/<slug>/` (создаёт `new-restaurant.sh` с `PROJECT_PREFIX=swapowl-`, пароли случайные). Общие настройки сервера — `/opt/swapowl/edge/.env` (`SITE_DOMAIN`, `IMAGE_REPO`, `IMAGE_TAG`, `HUB_TOKEN`, токены сторожа, лимиты памяти). Один Caddy и один hub — проект `swapowl-edge`, они видят точки по общей docker-сети `swapowl-edge` (алиасы `api-<slug>`, `web-<slug>`). Caddyfile и конфиг hub генерирует `fleet.sh` по списку точек.

**Маршрут: `https://<slug>.<SITE_DOMAIN>`** (не `/<slug>/`). Причина: фронтенд ссылается на `/assets`, `/api`, `/events` от корня; путь-префикс потребовал бы пересборки фронтенда с base-путём и правок API. Поддомен ничего не меняет в образах, а у каждой точки свои cookie и CORS. Цена: нужен wildcard DNS (`*.example.com`) или записи на каждую точку; для `sslip.io` поддомены работают (`SITE_DOMAIN=1-2-3-4.sslip.io` даёт `cafe.1-2-3-4.sslip.io`). Сертификат Let's Encrypt выпускается на каждый поддомен (лимит — 50 имён в неделю на домен). Hub: `https://hub.<SITE_DOMAIN>`, авторизация `Authorization: Bearer $HUB_TOKEN`.

**Команды** (`FLEET_ROOT` по умолчанию `/opt/swapowl`):

```sh
fleet.sh up cafe-lenina              # создать точку и поднять, обновить маршруты и hub
fleet.sh list                        # точки
fleet.sh status                      # тег и /healthz каждой точки
fleet.sh deploy <tag> cafe-lenina    # одну или несколько точек
fleet.sh deploy <tag> --all          # все, по очереди
fleet.sh hub-key cafe-lenina <<<"$KEY"   # viewer-ключ точки для hub
```

`deploy` без `--all` и без списка точек не запускается. Каждая точка по очереди: `backup.sh` → `pull` → `up -d` → `/healthz` до `HEALTH_TIMEOUT` (60 с). Не ответил — эта точка возвращается на прошлый тег (`points/<slug>/.last_good`), раскатка **останавливается**, остальные не тронуты, код выхода 1. Если бэкап не удался, точка не обновляется и раскатка тоже останавливается.

**Первая точка.** `SITE_DOMAIN=<домен> fleet.sh up cafe-lenina` (создаёт `edge/.env` с `HUB_TOKEN`; нужен `docker` и доступ к образам). Затем ключ viewer для hub: войти admin-ом точки (пароль в `points/cafe-lenina/.env`), `POST /api/api-keys` с ролью `viewer`, передать `fleet.sh hub-key`. Hub включает в сводку только точки с ключом и стартует, только когда есть хотя бы одна (до этого `hub.<домен>` отдаёт 502); проверка: `curl -H "Authorization: Bearer $HUB_TOKEN" https://hub.<домен>/api/network/summary` (подробности — `docs/network.md`). Точки получают чистые экземпляры (`DEMO_MODE=false`), без сида.

**Сторож.** Если в `edge/.env` заданы `WATCHDOG_TG_TOKEN` и `WATCHDOG_TG_CHAT`, на каждую точку поднимается свой `watchdog` (профиль `ops`, 32 МБ). Сообщение пока не содержит имени точки (в `cmd/watchdog` такого поля нет) — по тексту отличить точку нельзя; это пробел, нужна отдельная задача.

**Сколько точек влезает (1 vCPU, 2 ГБ).** Лимиты на точку в `edge/.env`: `PG_MEM=320m`, `API_MEM=192m`, `WEB_MEM=32m`, `SIDECAR_MEM=32m` на каждый из `fake-iiko`, `iiko-adapter`, `notifier` (+ `watchdog` 32m). Итого по лимитам ≈ 700 МБ на точку (≈ 730 с watchdog), общие `edge` 128 МБ + `hub` 32 МБ. Остаток на ОС и swap: 2048 − 160 − ~300 ≈ 1,5 ГБ, то есть **по лимитам — 2 точки с запасом, 3 впритык**. Фактическое потребление ниже лимитов, но замер `docker stats` на двух точках на целевом сервере **не выполнен** (в среде разработки не было Docker): прогоните `docker stats --no-stream` на двух точках под нагрузкой и поправьте это число. Для точек, где адаптер не нужен, останавливайте `fake-iiko` (в проде у реальной точки это демо-эмулятор). CPU один: 3 точки и больше — только при низкой нагрузке; Postgres на точку — главный потребитель, при росте переходите на второй сервер.

**Ограничения.** `fleet.sh` не удаляет точки и тома (удаление — вручную, после бэкапа). Миграции откатом тега не отменяются (как и в одиночном режиме). Раскатка последовательная, окно обновления растёт с числом точек.
