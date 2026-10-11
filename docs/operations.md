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
