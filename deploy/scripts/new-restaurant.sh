#!/usr/bin/env bash
# Новый ресторан: каталог экземпляра с .env чистого экземпляра (спецификация, 8.1).
# Использование: new-restaurant.sh <имя> [порт]
# Переменные: INSTANCES_ROOT (по умолчанию ./instances), PROJECT_PREFIX (префикс compose-проекта, по умолчанию пусто;
# fleet.sh задаёт swapowl-). Пароли случайные, в репозиторий не попадают.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

name="${1:-}"
[[ "$name" =~ ^[a-z0-9][a-z0-9-]{0,40}$ ]] || die "имя: строчные латинские буквы, цифры и дефис, например: cafe-lenina"
INSTANCES_ROOT="${INSTANCES_ROOT:-./instances}"
dir="$INSTANCES_ROOT/$name"
[ ! -e "$dir" ] || die "$dir уже существует"

rand() { head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c "$1"; }

port="${2:-}"
if [ -z "$port" ]; then
  # Первый свободный порт после самого большого уже выданного экземплярам.
  last="$(cat "$INSTANCES_ROOT"/*/.env 2>/dev/null | sed -n 's/^API_PORT=//p' | sort -n | tail -n 1 || true)"
  port=$(( ${last:-8079} + 1 ))
fi
case "$port" in ''|*[!0-9]*) die "порт должен быть числом" ;; esac

admin_password="$(rand 20)"
pg_password="$(rand 32)"

mkdir -p "$dir"
umask 077
cat > "$dir/.env" <<ENV
# Экземпляр $name. Содержит секреты: не коммитить, права 600.
COMPOSE_PROJECT_NAME=${PROJECT_PREFIX:-}$name
POSTGRES_USER=umirhack
POSTGRES_PASSWORD=$pg_password
POSTGRES_DB=umirhack
API_PORT=$port
RESTAURANT_TZ=Europe/Moscow
OPEN_AT=10:00
CLOSE_AT=23:00
LOG_LEVEL=info
DB_POOL_SIZE=20
# Чистый экземпляр (solution.md, 8.1)
DEMO_MODE=false
SEED_ON_EMPTY=false
CLOCK_MODE=real
AUTH_REQUIRED=true
ADMIN_LOGIN=admin
ADMIN_PASSWORD=$admin_password
ENV
chmod 600 "$dir/.env"

cat <<EOT
Экземпляр создан: $dir/.env (порт $port)
Пароль admin записан только в .env; сменить: после первого входа (шаг 2).

Дальше:
  1. Запуск:
       docker compose -p $name -f deploy/docker-compose.yml --env-file $dir/.env up -d --build
       curl -fsS http://127.0.0.1:$port/healthz
  2. Первый вход admin: POST /api/auth/login {"login":"admin","password":"<ADMIN_PASSWORD из $dir/.env>"},
     затем смените пароль (docs/api.md, раздел доступа). Часовой пояс и часы работы — в $dir/.env.
  3. Ключ адаптера (роль operator), показывается один раз:
       curl -fsS -c /tmp/$name.cookie -H 'Content-Type: application/json' \\
         -d '{"login":"admin","password":"..."}' http://127.0.0.1:$port/api/auth/login
       curl -fsS -b /tmp/$name.cookie -H 'Content-Type: application/json' \\
         -d '{"name":"iiko-adapter","role":"operator"}' http://127.0.0.1:$port/api/api-keys
     Удалите куку: rm /tmp/$name.cookie
  4. Запуск адаптера: передайте ему ключ и адрес http://127.0.0.1:$port; начните с ADAPTER_MODE=shadow
     (docs/iiko.md, цикл подключения — solution.md, 8.4).
  5. Резервные копии: docs/operations.md (cron: backup.sh и restore-check.sh).
EOT
