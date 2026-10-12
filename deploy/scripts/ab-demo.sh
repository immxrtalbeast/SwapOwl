#!/usr/bin/env bash
# Живое A/B на одном сервере рядом с одиночным стендом (deploy.sh, проект swapowl):
#   основной стенд — точка «Со SwapOwl»; рядом поднимается точка «Без подсказок» (проект swapowl-nohints,
#   своя БД и часы, SIM_FOLLOW_HOT=false), hub опрашивает обе, экран «Сеть» основного стенда показывает сравнение.
# fleet.sh для этого не годится: его общий Caddy занимает 80/443 и не знает основного домена.
#
#   ab-demo.sh setup              поднять точку, hub и маршруты (повторный запуск безопасен)
#   ab-demo.sh start [скорость]   сброс обеих точек, одна скорость (по умолчанию 600), одновременный старт генератора
#   ab-demo.sh stop               пауза обеих: генератор выключен, часы стоят
#   ab-demo.sh status             время стенда, скорость и генератор обеих точек
#   ab-demo.sh teardown           убрать точку, hub и маршруты; основной стенд — как до setup
#
# Запуск от root на сервере. Всё серверное — вне deploy/ (автодеплой его не затирает):
#   $DEPLOY_DIR/compose.local.yml — подключение основного стенда к сети swapowl-edge (его подхватывает deploy.sh);
#   $DEPLOY_DIR/caddy-extra/ab.caddy — сайт точки; $DEPLOY_DIR/ab/{nohints,edge} — .env точки и hub.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/swapowl}"
PROD_COMPOSE="$SCRIPT_DIR/../docker-compose.prod.yml"
MAIN_ENV="$DEPLOY_DIR/.env"
AB_DIR="$DEPLOY_DIR/ab"
POINT_DIR="$AB_DIR/nohints"
EDGE_DIR="$AB_DIR/edge"
EXTRA_DIR="$DEPLOY_DIR/caddy-extra"
LOCAL_FILE="$DEPLOY_DIR/compose.local.yml"
NETWORK=swapowl-edge
POINT_HOST_PREFIX="${POINT_HOST_PREFIX:-without-owl}"
# root не залогинен в GHCR — берём вход пользователя deploy, если он есть.
[ -d /home/deploy/.docker ] && export DOCKER_CONFIG="${DOCKER_CONFIG:-/home/deploy/.docker}"

die() { echo "ошибка: $*" >&2; exit 1; }
rand() { head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c "$1"; }
envval() { sed -n "s/^$2=//p" "$1" 2>/dev/null | tail -n 1; }
setenv() { # setenv <файл> <ключ> <значение>: заменить или дописать
  if grep -q "^$2=" "$1"; then sed -i "s|^$2=.*|$2=$3|" "$1"; else printf '%s=%s\n' "$2" "$3" >> "$1"; fi
}
tag() { cat "$DEPLOY_DIR/.last_good" 2>/dev/null || die "нет $DEPLOY_DIR/.last_good: сначала обычный деплой"; }
domain() { envval "$MAIN_ENV" SITE_DOMAIN; }
point_url() { echo "https://$POINT_HOST_PREFIX.$(domain)"; }
main_url() { echo "https://$(domain)"; }

dc_main() { IMAGE_TAG="$(tag)" docker compose -p swapowl -f "$PROD_COMPOSE" -f "$LOCAL_FILE" --env-file "$MAIN_ENV" "$@"; }
dc_point() { IMAGE_TAG="$(tag)" docker compose -p swapowl-nohints -f "$PROD_COMPOSE" -f "$POINT_DIR/compose.override.yml" --env-file "$POINT_DIR/.env" "$@"; }
dc_hub() { EDGE_DIR="$EDGE_DIR" IMAGE_TAG="$(tag)" docker compose -p swapowl-edge --profile fleet -f "$PROD_COMPOSE" --env-file "$EDGE_DIR/.env" "$@"; }

wait_url() { # wait_url <url> <сек>
  local i
  for ((i = 0; i < $2; i += 2)); do curl -fsS -o /dev/null --max-time 3 "$1" && return 0; sleep 2; done
  return 1
}

# viewer-ключ для hub: вход админом точки и POST /api/api-keys.
viewer_key() { # viewer_key <base_url> <env-файл точки>
  local jar login pass key
  jar="$(mktemp)"; login="$(envval "$2" ADMIN_LOGIN)"; pass="$(envval "$2" ADMIN_PASSWORD)"
  curl -fsS -c "$jar" -o /dev/null -H 'Content-Type: application/json' \
    -d "{\"login\":\"$login\",\"password\":\"$pass\"}" "$1/api/auth/login" || { rm -f "$jar"; die "$1: вход админом не удался"; }
  key="$(curl -fsS -b "$jar" -H 'Content-Type: application/json' -d '{"name":"hub-ab","role":"viewer"}' "$1/api/api-keys" \
    | python3 -c 'import sys,json;print(json.load(sys.stdin)["key"])')"
  rm -f "$jar"
  [ -n "$key" ] || die "$1: ключ не выпущен"
  echo "$key"
}

cmd_setup() {
  [ -f "$MAIN_ENV" ] || die "нет $MAIN_ENV"
  [ "$(envval "$MAIN_ENV" CLOCK_MODE)" = simulated ] || die "у основного стенда CLOCK_MODE должен быть simulated"
  mkdir -p "$POINT_DIR" "$EDGE_DIR" "$EXTRA_DIR"
  docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null

  # Точка «Без подсказок»: копия настроек стенда, своя БД, генератор не слушается рекомендаций.
  if [ ! -f "$POINT_DIR/.env" ]; then
    (umask 077; grep -vE '^(POSTGRES_PASSWORD|COMPOSE_PROFILES|ADAPTER_API_KEY|NOTIFIER_API_KEY|API_UPSTREAM|WEB_UPSTREAM|HUB_TOKEN|CADDY_EXTRA_DIR)=' "$MAIN_ENV" > "$POINT_DIR/.env")
    setenv "$POINT_DIR/.env" POSTGRES_PASSWORD "$(rand 24)"
    setenv "$POINT_DIR/.env" SITE_DOMAIN "$POINT_HOST_PREFIX.$(domain)"
    setenv "$POINT_DIR/.env" COMPOSE_PROFILES ""
  fi
  setenv "$POINT_DIR/.env" SIM_FOLLOW_HOT false
  cat > "$POINT_DIR/compose.override.yml" <<'YML'
# Генерирует ab-demo.sh. Свой Caddy у точки отключён: сайт держит Caddy основного стенда (caddy-extra/ab.caddy).
services:
  caddy:
    profiles: ["disabled"]
  api:
    mem_limit: 192m
    networks:
      default: {}
      edge:
        aliases: [api-nohints]
  web:
    networks:
      default: {}
      edge:
        aliases: [web-nohints]
  postgres:
    mem_limit: 320m
YML

  # Основной стенд в сети точек. В ней имена api/web есть у обеих точек, поэтому Caddy стенда
  # ходит по уникальным именам контейнеров (API_UPSTREAM/WEB_UPSTREAM).
  cat > "$LOCAL_FILE" <<'YML'
# Генерирует ab-demo.sh (живое A/B). Подхватывается deploy.sh; удаляется ab-demo.sh teardown.
services:
  caddy:
    networks: [default, edge]
  api:
    networks:
      default: {}
      edge:
        aliases: [api-main]
YML
  [ -f "$EDGE_DIR/.env" ] || (umask 077; printf 'HUB_TOKEN=%s\nHUB_POLL=5s\nIMAGE_REPO=%s\n' "$(rand 32)" \
    "$(envval "$MAIN_ENV" IMAGE_REPO)" > "$EDGE_DIR/.env")
  setenv "$MAIN_ENV" API_UPSTREAM swapowl-api-1
  setenv "$MAIN_ENV" WEB_UPSTREAM swapowl-web-1
  setenv "$MAIN_ENV" HUB_TOKEN "$(envval "$EDGE_DIR/.env" HUB_TOKEN)"
  setenv "$MAIN_ENV" CADDY_EXTRA_DIR "$EXTRA_DIR"

  cat > "$EXTRA_DIR/ab.caddy" <<CF
# Генерирует ab-demo.sh: точка «Без подсказок».
$POINT_HOST_PREFIX.{\$SITE_DOMAIN} {
	encode zstd gzip
	handle /events {
		reverse_proxy api-nohints:8080 {
			flush_interval -1
		}
	}
	handle /healthz {
		reverse_proxy api-nohints:8080
	}
	handle /api/* {
		reverse_proxy api-nohints:8080
	}
	handle {
		reverse_proxy web-nohints:80
	}
}
CF

  echo "== точка «Без подсказок» (тег $(tag))"
  dc_point up -d --remove-orphans
  echo "== основной стенд: api и caddy в сети точек"
  dc_main up -d --no-deps --pull never api caddy
  wait_url "$(main_url)/healthz" 90 || die "основной стенд не ответил"
  wait_url "$(point_url)/healthz" 120 || die "точка не ответила на $(point_url)"

  if [ ! -s "$EDGE_DIR/keys" ]; then
    (umask 077; printf 'MAIN=%s\nPOINT=%s\n' "$(viewer_key "$(main_url)" "$MAIN_ENV")" "$(viewer_key "$(point_url)" "$POINT_DIR/.env")" > "$EDGE_DIR/keys")
  fi
  cat > "$EDGE_DIR/hub.yml" <<YML
# Генерирует ab-demo.sh.
points:
  - id: with-owl
    name: Демо-стенд
    url: http://api-main:8080
    api_key: $(envval "$EDGE_DIR/keys" MAIN)
    label: "Со SwapOwl"
    group: ab
  - id: without-owl
    name: Точка без подсказок
    url: http://api-nohints:8080
    api_key: $(envval "$EDGE_DIR/keys" POINT)
    label: "Без подсказок"
    group: ab
YML
  chown 10001 "$EDGE_DIR/hub.yml" 2>/dev/null && chmod 600 "$EDGE_DIR/hub.yml" || chmod 644 "$EDGE_DIR/hub.yml"
  echo "== hub"
  local img; img="$(envval "$EDGE_DIR/.env" IMAGE_REPO)"; img="${img:-ghcr.io/immxrtalbeast/umirhack-2026}/hub:$(tag)"
  if ! docker image inspect "$img" >/dev/null 2>&1 && ! docker pull -q "$img" >/dev/null 2>&1; then
    # Вход deploy в GHCR живёт один запуск Actions: собираем образ из исходников рядом с deploy/ (HUB_SRC).
    local src="${HUB_SRC:-/root/src}"
    [ -f "$src/deploy/Dockerfile.hub" ] || die "нет образа $img и исходников в $src (tar репозитория в $src и повторить)"
    echo "образа в GHCR нет — собираю из $src"
    docker build -q -f "$src/deploy/Dockerfile.hub" -t "$img" "$src" >/dev/null
  fi
  dc_hub up -d --no-deps --force-recreate --pull never hub
  sleep 8
  curl -fsS -o /dev/null "$(main_url)/api/network/summary" && echo "готово: $(main_url)/#/network" \
    || die "сводка сети не отвечает: docker logs swapowl-edge-hub-1"
}

demo() { # demo <base_url> <путь> <json> <env-файл>
  curl -fsS -o /dev/null -H "Authorization: Bearer $(envval "$4" DEMO_TOKEN)" -H 'Content-Type: application/json' -d "$3" "$1$2"
}
both() { # оба запроса параллельно; код возврата — ненулевой, если упал хоть один
  local a b
  demo "$(main_url)" "$1" "$2" "$MAIN_ENV" & a=$!
  demo "$(point_url)" "$1" "$2" "$POINT_DIR/.env" & b=$!
  wait "$a" && wait "$b"
}

cmd_start() {
  local speed="${1:-600}"
  [ -f "$POINT_DIR/.env" ] || die "сначала ab-demo.sh setup"
  # Профиль сида для сравнения (FS-146): склад, где горячим предложениям есть что спасать. Без поддержки профиля
  # в API (старая версия) — обычный сброс.
  if ! both /api/demo/reset "{\"profile\":\"${AB_PROFILE:-ab}\"}"; then
    echo "сброс с профилем ${AB_PROFILE:-ab} не принят — обычный сброс"
    both /api/demo/reset '{}'
  fi
  both /api/demo/clock/speed "{\"speed\":$speed}"
  both /api/demo/simulator '{"running":true}'
  echo "после показа вернуть демо-стенд: ab-demo.sh stop, затем «Сброс» на демо-панели"
  echo "старт: ×$speed, генератор идёт на обеих точках. Экран: $(main_url)/#/network. Пауза: ab-demo.sh stop"
}

cmd_stop() {
  both /api/demo/simulator '{"running":false}'
  both /api/demo/clock/speed '{"speed":0}'
  echo "пауза обеих точек"
}

cmd_status() {
  printf 'Со SwapOwl:    '; curl -fsS "$(main_url)/api/clock"; echo
  printf 'Без подсказок: '; curl -fsS "$(point_url)/api/clock"; echo
}

cmd_teardown() {
  dc_hub rm -sf hub || true
  [ -f "$POINT_DIR/compose.override.yml" ] && dc_point down -v --remove-orphans || true
  rm -f "$EXTRA_DIR/ab.caddy"
  for k in API_UPSTREAM WEB_UPSTREAM HUB_TOKEN; do sed -i "/^$k=/d" "$MAIN_ENV"; done
  rm -f "$LOCAL_FILE"
  IMAGE_TAG="$(tag)" docker compose -p swapowl -f "$PROD_COMPOSE" --env-file "$MAIN_ENV" up -d --no-deps --pull never api caddy
  rm -rf "$AB_DIR"
  echo "A/B убран, основной стенд — одиночный"
}

case "${1:-}" in
  setup) cmd_setup ;;
  start) cmd_start "${2:-}" ;;
  stop) cmd_stop ;;
  status) cmd_status ;;
  teardown) cmd_teardown ;;
  *) sed -n '2,13p' "$0"; exit 1 ;;
esac
