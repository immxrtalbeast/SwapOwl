#!/usr/bin/env bash
# Выкатка тега на сервере: deploy.sh <tag>. Каталог развёртывания — DEPLOY_DIR (по умолчанию /opt/swapowl):
# в нём .env и клон/копия каталога deploy/. Откат на любой тег — тот же вызов с нужным тегом.
set -euo pipefail

TAG="${1:?использование: deploy.sh <tag>}"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/swapowl}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_FILE="${COMPOSE_FILE:-$SCRIPT_DIR/../docker-compose.prod.yml}"
ENV_FILE="$DEPLOY_DIR/.env"
LAST_GOOD="$DEPLOY_DIR/.last_good"

die() { echo "ошибка: $*" >&2; exit 1; }
# Профили (integrations, ops) берутся из COMPOSE_PROFILES в .env; без них стартуют только caddy, web, api, postgres.
PROFILES=()
if [ -f "$ENV_FILE" ]; then
  IFS=, read -r -a _p <<< "$(sed -n 's/^COMPOSE_PROFILES=//p' "$ENV_FILE" | tail -n 1 | tr -d '"'"'"' ')"
  for _x in ${_p[@]+"${_p[@]}"}; do [ -n "$_x" ] && PROFILES+=(--profile "$_x"); done
fi
dc() { IMAGE_TAG="$1" docker compose -p swapowl ${PROFILES[@]+"${PROFILES[@]}"} -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "${@:2}"; }

# Проверяем сам API внутри сети compose (в обход Caddy: прокси отвечает и при упавшем API).
wait_health() {
  local i
  for ((i = 0; i < HEALTH_TIMEOUT; i += 2)); do
    if dc "$1" exec -T api wget -q -T 3 -O /dev/null http://127.0.0.1:8080/healthz 2>/dev/null; then return 0; fi
    sleep 2
  done
  return 1
}

[ -f "$ENV_FILE" ] || die "нет $ENV_FILE"
grep -Eq '^POSTGRES_PASSWORD=.+' "$ENV_FILE" || die "в $ENV_FILE не задан POSTGRES_PASSWORD"

prev=""
[ -f "$LAST_GOOD" ] && prev="$(cat "$LAST_GOOD")"

# Копия базы перед обновлением; на самом первом запуске базы ещё нет.
if [ -n "$prev" ] && [ -x "$SCRIPT_DIR/backup.sh" ]; then
  echo "резервная копия перед обновлением"
  COMPOSE_FILE="$COMPOSE_FILE" COMPOSE_PROJECT_NAME=swapowl "$SCRIPT_DIR/backup.sh" "$DEPLOY_DIR" ||
    die "резервная копия не удалась, обновление отменено"
fi

echo "выкатываю $TAG (прошлый: ${prev:-нет})"
dc "$TAG" pull
dc "$TAG" up -d --remove-orphans

if wait_health "$TAG"; then
  echo "$TAG" > "$LAST_GOOD"
  echo "готово: $TAG"
  exit 0
fi

echo "/healthz не ответил за ${HEALTH_TIMEOUT} с" >&2
dc "$TAG" logs --tail=50 api >&2 || true
if [ -n "$prev" ] && [ "$prev" != "$TAG" ]; then
  echo "возвращаю $prev" >&2
  dc "$prev" up -d --remove-orphans || true
  wait_health "$prev" && echo "откат на $prev выполнен" >&2 || echo "после отката /healthz тоже не отвечает" >&2
fi
exit 1
