#!/usr/bin/env bash
# Общие функции скриптов эксплуатации. Подключается через source.
# Экземпляр = каталог с .env (его создаёт new-restaurant.sh). Compose-файл — deploy/docker-compose.yml.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$SCRIPT_DIR/../docker-compose.yml}"

die() { echo "ошибка: $*" >&2; exit 1; }

# load_instance <каталог экземпляра>: читает .env, задаёт INSTANCE_DIR, PROJECT и функцию dc.
load_instance() {
  INSTANCE_DIR="$(cd "${1:?укажите каталог экземпляра}" 2>/dev/null && pwd)" || die "нет каталога экземпляра: $1"
  [ -f "$INSTANCE_DIR/.env" ] || die "нет $INSTANCE_DIR/.env"
  set -a
  # shellcheck disable=SC1091
  . "$INSTANCE_DIR/.env"
  set +a
  PROJECT="${COMPOSE_PROJECT_NAME:-$(basename "$INSTANCE_DIR")}"
  POSTGRES_USER="${POSTGRES_USER:-umirhack}"
  POSTGRES_DB="${POSTGRES_DB:-umirhack}"
  API_PORT="${API_PORT:-8080}"
}

dc() { docker compose -p "$PROJECT" -f "$COMPOSE_FILE" --env-file "$INSTANCE_DIR/.env" "$@"; }

# psql_in <база> [аргументы psql]: psql внутри контейнера postgres.
psql_in() {
  local db="$1"; shift
  dc exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$db" "$@"
}
