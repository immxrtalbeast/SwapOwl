#!/usr/bin/env bash
# Живое A/B на одном сервере рядом с одиночным стендом (deploy.sh, проект swapowl).
# Две отдельные точки с одинаковым сидом и генератором — «Со SwapOwl» (SIM_FOLLOW_HOT=true) и «Без подсказок»
# (false), у каждой своя БД и часы. Демо-стенд в сравнении не участвует: гонку можно запустить до показа,
# пока ведущий идёт по сценарию на демо-стенде. hub опрашивает все три, экран «Сеть» демо-стенда показывает сравнение.
# fleet.sh для этого не годится: его общий Caddy занимает 80/443 и не знает основного домена.
#
#   ab-demo.sh setup              поднять точки, hub и маршруты (повторный запуск безопасен)
#   ab-demo.sh start [скорость]   сброс обеих точек сравнения, одна скорость (по умолчанию 600, максимум 3600),
#                                 одновременный старт генератора
#   ab-demo.sh stop               пауза обеих точек: генератор выключен, часы стоят
#   ab-demo.sh status             время стенда, скорость и генератор всех точек
#   ab-demo.sh teardown           убрать точки, hub и маршруты; основной стенд — как до setup
#
# Запуск от root на сервере. Всё серверное — вне deploy/ (автодеплой его не затирает):
#   $DEPLOY_DIR/compose.local.yml — подключение основного стенда к сети swapowl-edge (его подхватывает deploy.sh);
#   $DEPLOY_DIR/caddy-extra/ab.caddy — сайты точек; $DEPLOY_DIR/ab/{<точка>,edge} — .env точек и hub.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/swapowl}"
PROD_COMPOSE="$SCRIPT_DIR/../docker-compose.prod.yml"
MAIN_ENV="$DEPLOY_DIR/.env"
AB_DIR="$DEPLOY_DIR/ab"
EDGE_DIR="$AB_DIR/edge"
EXTRA_DIR="$DEPLOY_DIR/caddy-extra"
LOCAL_FILE="$DEPLOY_DIR/compose.local.yml"
NETWORK=swapowl-edge
# Точки сравнения: каталог | поддомен | SIM_FOLLOW_HOT | подпись в hub.
POINTS=("withhints|with-owl|true|Со SwapOwl" "nohints|without-owl|false|Без подсказок")
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
main_url() { echo "https://$(domain)"; }
field() { local IFS='|'; local -a f; read -r -a f <<< "$1"; echo "${f[$2]}"; } # field <точка> <номер поля>
pdir() { echo "$AB_DIR/$(field "$1" 0)"; }
purl() { echo "https://$(field "$1" 1).$(domain)"; }

dc_main() { IMAGE_TAG="$(tag)" docker compose -p swapowl -f "$PROD_COMPOSE" -f "$LOCAL_FILE" --env-file "$MAIN_ENV" "$@"; }
dc_point() { local p="$1"; shift
  IMAGE_TAG="$(tag)" docker compose -p "swapowl-$(field "$p" 0)" -f "$PROD_COMPOSE" -f "$(pdir "$p")/compose.override.yml" \
    --env-file "$(pdir "$p")/.env" "$@"; }
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

setup_point() { # setup_point <точка>
  local p="$1" d slug host hot
  d="$(pdir "$p")"; slug="$(field "$p" 0)"; host="$(field "$p" 1)"; hot="$(field "$p" 2)"
  mkdir -p "$d"
  if [ ! -f "$d/.env" ]; then
    (umask 077; grep -vE '^(POSTGRES_PASSWORD|COMPOSE_PROFILES|ADAPTER_API_KEY|NOTIFIER_API_KEY|API_UPSTREAM|WEB_UPSTREAM|HUB_TOKEN|CADDY_EXTRA_DIR)=' "$MAIN_ENV" > "$d/.env")
    setenv "$d/.env" POSTGRES_PASSWORD "$(rand 24)"
    setenv "$d/.env" SITE_DOMAIN "$host.$(domain)"
    setenv "$d/.env" COMPOSE_PROFILES ""
  fi
  setenv "$d/.env" SIM_FOLLOW_HOT "$hot"
  cat > "$d/compose.override.yml" <<YML
# Генерирует ab-demo.sh. Свой Caddy у точки отключён: сайт держит Caddy основного стенда (caddy-extra/ab.caddy).
services:
  caddy:
    profiles: ["disabled"]
  api:
    mem_limit: 192m
    networks:
      default: {}
      edge:
        aliases: [api-$slug]
  web:
    networks:
      default: {}
      edge:
        aliases: [web-$slug]
  postgres:
    mem_limit: 320m
YML
  echo "== точка «$(field "$p" 3)» ($host, тег $(tag))"
  dc_point "$p" up -d --remove-orphans
}

site_block() { # site_block <точка>
  local slug host; slug="$(field "$1" 0)"; host="$(field "$1" 1)"
  cat <<CF

$host.{\$SITE_DOMAIN} {
	encode zstd gzip
	handle /events {
		reverse_proxy api-$slug:8080 {
			flush_interval -1
		}
	}
	handle /healthz {
		reverse_proxy api-$slug:8080
	}
	handle /api/* {
		reverse_proxy api-$slug:8080
	}
	handle {
		reverse_proxy web-$slug:80
	}
}
CF
}

cmd_setup() {
  [ -f "$MAIN_ENV" ] || die "нет $MAIN_ENV"
  [ "$(envval "$MAIN_ENV" CLOCK_MODE)" = simulated ] || die "у основного стенда CLOCK_MODE должен быть simulated"
  mkdir -p "$EDGE_DIR" "$EXTRA_DIR"
  docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null

  # Основной стенд в сети точек. В ней имена api/web есть у всех точек, поэтому Caddy стенда
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

  { echo "# Генерирует ab-demo.sh: точки живого A/B."; for p in "${POINTS[@]}"; do site_block "$p"; done; } > "$EXTRA_DIR/ab.caddy"

  for p in "${POINTS[@]}"; do setup_point "$p"; done
  echo "== основной стенд: api и caddy в сети точек"
  dc_main up -d --no-deps --pull never api caddy
  docker exec swapowl-caddy-1 caddy reload --config /etc/caddy/Caddyfile >/dev/null 2>&1 || true
  wait_url "$(main_url)/healthz" 90 || die "основной стенд не ответил"
  for p in "${POINTS[@]}"; do wait_url "$(purl "$p")/healthz" 120 || die "точка не ответила на $(purl "$p")"; done

  touch "$EDGE_DIR/keys"; chmod 600 "$EDGE_DIR/keys"
  [ -n "$(envval "$EDGE_DIR/keys" MAIN)" ] || printf 'MAIN=%s\n' "$(viewer_key "$(main_url)" "$MAIN_ENV")" >> "$EDGE_DIR/keys"
  local p k
  for p in "${POINTS[@]}"; do
    k="KEY_$(field "$p" 0)"
    [ -n "$(envval "$EDGE_DIR/keys" "$k")" ] || printf '%s=%s\n' "$k" "$(viewer_key "$(purl "$p")" "$(pdir "$p")/.env")" >> "$EDGE_DIR/keys"
  done
  {
    echo "# Генерирует ab-demo.sh."
    echo "points:"
    printf '  - id: demo\n    name: Демо-стенд\n    url: http://api-main:8080\n    api_key: %s\n' "$(envval "$EDGE_DIR/keys" MAIN)"
    for p in "${POINTS[@]}"; do
      printf '  - id: %s\n    name: %s\n    url: http://api-%s:8080\n    api_key: %s\n    label: "%s"\n    group: ab\n' \
        "$(field "$p" 1)" "$(field "$p" 3)" "$(field "$p" 0)" "$(envval "$EDGE_DIR/keys" "KEY_$(field "$p" 0)")" "$(field "$p" 3)"
    done
  } > "$EDGE_DIR/hub.yml"
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
pair() { # оба запроса параллельно в точки сравнения; ненулевой код, если упал хоть один
  local a b p1="${POINTS[0]}" p2="${POINTS[1]}"
  demo "$(purl "$p1")" "$1" "$2" "$(pdir "$p1")/.env" & a=$!
  demo "$(purl "$p2")" "$1" "$2" "$(pdir "$p2")/.env" & b=$!
  wait "$a" && wait "$b"
}

cmd_start() {
  local speed="${1:-600}"
  [ -f "$(pdir "${POINTS[0]}")/.env" ] || die "сначала ab-demo.sh setup"
  # Профиль сида для сравнения (FS-146): склад, где горячим предложениям есть что спасать. Без поддержки профиля
  # в API (старая версия) — обычный сброс.
  if ! pair /api/demo/reset "{\"profile\":\"${AB_PROFILE:-ab}\"}"; then
    echo "сброс с профилем ${AB_PROFILE:-ab} не принят — обычный сброс"
    pair /api/demo/reset '{}'
  fi
  pair /api/demo/clock/speed "{\"speed\":$speed}"
  pair /api/demo/simulator '{"running":true}'
  echo "старт: ×$speed, генератор идёт на обеих точках сравнения (демо-стенд не тронут). Экран: $(main_url)/#/network"
  echo "неделя стенда пройдёт за $((168 * 3600 / speed)) с; пауза: ab-demo.sh stop"
}

cmd_stop() {
  pair /api/demo/simulator '{"running":false}'
  pair /api/demo/clock/speed '{"speed":0}'
  echo "пауза обеих точек сравнения"
}

cmd_status() {
  local p
  printf '%-15s' "Демо-стенд:"; curl -fsS "$(main_url)/api/clock"; echo
  for p in "${POINTS[@]}"; do printf '%-15s' "$(field "$p" 3):"; curl -fsS "$(purl "$p")/api/clock"; echo; done
}

cmd_teardown() {
  local p k
  dc_hub rm -sf hub || true
  for p in "${POINTS[@]}"; do [ -f "$(pdir "$p")/compose.override.yml" ] && dc_point "$p" down -v --remove-orphans || true; done
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
  *) sed -n '2,14p' "$0"; exit 1 ;;
esac
