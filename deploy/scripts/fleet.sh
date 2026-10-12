#!/usr/bin/env bash
# Много точек на одном сервере: compose-проект swapowl-<slug> на каждый ресторан, один Caddy и один hub на сервер.
#   fleet.sh list                          точки сервера
#   fleet.sh up <slug>                     создать точку (если нет) и поднять; обновить маршруты и конфиг hub
#   fleet.sh deploy <tag> [--all|slug...]  выкатить тег по очереди: бэкап, pull+up, /healthz; ошибка -> откат точки и стоп
#   fleet.sh status                        тег и здоровье каждой точки
#   fleet.sh hub-key <slug>                сохранить viewer-ключ точки для hub (из stdin) и обновить hub
#   fleet.sh edge                          пересобрать Caddyfile/hub.yml и перезапустить общие сервисы
# Каталог: FLEET_ROOT (по умолчанию /opt/swapowl): points/<slug>/{.env,hub.key,compose.override.yml,.last_good}, edge/{.env,Caddyfile,hub.yml}.
# Одиночный режим (deploy.sh, проект swapowl) не затрагивается.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FLEET_ROOT="${FLEET_ROOT:-/opt/swapowl}"
PROD_COMPOSE="${PROD_COMPOSE:-$SCRIPT_DIR/../docker-compose.prod.yml}"
HEALTH_TIMEOUT="${HEALTH_TIMEOUT:-60}"
POINTS="$FLEET_ROOT/points"
EDGE_DIR="$FLEET_ROOT/edge"
NETWORK=swapowl-edge
SLUG_RE='^[a-z0-9][a-z0-9-]{0,30}$'

die() { echo "ошибка: $*" >&2; exit 1; }
rand() { head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c "$1"; }
envval() { sed -n "s/^$2=//p" "$1" 2>/dev/null | tail -n 1; }
slugs() { local d; for d in "$POINTS"/*/; do [ -f "${d}.env" ] && basename "$d"; done; return 0; }
check_slug() { [[ "${1:-}" =~ $SLUG_RE ]] || die "slug: строчные латинские буквы, цифры, дефис (например cafe-lenina)"; }

ensure_edge() {
  mkdir -p "$EDGE_DIR" "$POINTS"
  docker network inspect "$NETWORK" >/dev/null 2>&1 || docker network create "$NETWORK" >/dev/null
  if [ ! -f "$EDGE_DIR/.env" ]; then
    (umask 077; cat > "$EDGE_DIR/.env" <<ENV
# Общие настройки сервера. Секреты: права 600, не коммитить.
SITE_DOMAIN=${SITE_DOMAIN:-localhost}
IMAGE_REPO=${IMAGE_REPO:-ghcr.io/immxrtalbeast/umirhack-2026}
IMAGE_TAG=${IMAGE_TAG:-latest}
HUB_TOKEN=$(rand 32)
WATCHDOG_TG_TOKEN=
WATCHDOG_TG_CHAT=
# Лимиты памяти на точку (docs/operations.md, «Много точек»)
API_MEM=192m
PG_MEM=320m
WEB_MEM=32m
SIDECAR_MEM=32m
ENV
    )
    echo "создан $EDGE_DIR/.env (HUB_TOKEN сгенерирован)"
  fi
}

# dc <slug> <tag> <аргументы compose>: compose-проект точки; профили берутся из COMPOSE_PROFILES в .env точки,
# профиль ops добавляется, если задан токен сторожа.
dc() {
  local slug="$1" tag="$2"; shift 2
  local prof=() p
  local -a want
  IFS=, read -r -a want <<< "$(envval "$POINTS/$slug/.env" COMPOSE_PROFILES | tr -d '"'"'"' ')"
  for p in ${want[@]+"${want[@]}"}; do [ -n "$p" ] && prof+=(--profile "$p"); done
  if [ -n "$(envval "$EDGE_DIR/.env" WATCHDOG_TG_TOKEN)" ] && [ -n "$(envval "$EDGE_DIR/.env" WATCHDOG_TG_CHAT)" ]; then prof+=(--profile ops); fi
  IMAGE_TAG="$tag" docker compose -p "swapowl-$slug" ${prof[@]+"${prof[@]}"} -f "$PROD_COMPOSE" -f "$POINTS/$slug/compose.override.yml" \
    --env-file "$EDGE_DIR/.env" --env-file "$POINTS/$slug/.env" "$@"
}

write_override() {
  local slug="$1"
  cat > "$POINTS/$slug/compose.override.yml" <<YML
# Генерируется fleet.sh. Свой Caddy у точки отключён: маршруты держит общий edge.
services:
  caddy:
    profiles: ["disabled"]
  api:
    networks:
      default: {}
      edge:
        aliases: [api-$slug]
  web:
    networks:
      default: {}
      edge:
        aliases: [web-$slug]
YML
}

wait_health() {
  local slug="$1" tag="$2" i
  for ((i = 0; i < HEALTH_TIMEOUT; i += 2)); do
    if dc "$slug" "$tag" exec -T api wget -q -T 3 -O /dev/null http://127.0.0.1:8080/healthz 2>/dev/null; then return 0; fi
    sleep 2
  done
  return 1
}

backup() { COMPOSE_FILE="$PROD_COMPOSE" "$SCRIPT_DIR/backup.sh" "$POINTS/$1"; }

# Caddyfile (маршрут <slug>.<SITE_DOMAIN>) и конфиг hub по списку точек.
gen_edge() {
  local domain scheme="" slug key
  domain="$(envval "$EDGE_DIR/.env" SITE_DOMAIN)"; domain="${domain:-localhost}"
  [ "$domain" = localhost ] && scheme="http://"
  {
    cat <<'CF'
# Генерируется fleet.sh. Не править руками.
(point) {
	encode zstd gzip
	header {
		Strict-Transport-Security "max-age=31536000"
		X-Content-Type-Options nosniff
		-Server
	}
	handle /events {
		reverse_proxy {args[0]}:8080 {
			flush_interval -1
		}
	}
	handle /healthz {
		reverse_proxy {args[0]}:8080
	}
	@metrics_outside {
		path /metrics
		not remote_ip 127.0.0.1 ::1 172.16.0.0/12
	}
	handle @metrics_outside {
		respond "not found" 404
	}
	handle /metrics {
		reverse_proxy {args[0]}:8080
	}
	handle /api/* {
		reverse_proxy {args[0]}:8080
	}
	handle {
		reverse_proxy {args[1]}:80
	}
}
CF
    for slug in $(slugs); do
      printf '\n%s%s.%s {\n\timport point api-%s web-%s\n}\n' "$scheme" "$slug" "$domain" "$slug" "$slug"
    done
    printf '\n%shub.%s {\n\treverse_proxy hub:8090\n}\n' "$scheme" "$domain"
  } > "$EDGE_DIR/Caddyfile"

  {
    echo "# Генерируется fleet.sh. Только точки с ключом (fleet.sh hub-key <slug>)."
    echo "points:"
    for slug in $(slugs); do
      key="$(cat "$POINTS/$slug/hub.key" 2>/dev/null || true)"
      [ -n "$key" ] || continue
      printf '  - id: %s\n    name: %s\n    url: http://api-%s:8080\n    api_key: %s\n' "$slug" "$slug" "$slug" "$key"
    done
  } > "$EDGE_DIR/hub.yml"
  # hub работает от uid 10001: отдаём ему файл с ключами, иначе читаемым для всех.
  if chown 10001 "$EDGE_DIR/hub.yml" 2>/dev/null; then chmod 600 "$EDGE_DIR/hub.yml"; else chmod 644 "$EDGE_DIR/hub.yml"; fi
}

edge_up() {
  gen_edge
  local svc=(edge) compose=(docker compose -p swapowl-edge --profile fleet -f "$PROD_COMPOSE" --env-file "$EDGE_DIR/.env")
  # hub без точек с ключом завершается с ошибкой конфига и крутился бы в рестартах: поднимаем только при наличии точек.
  if grep -q '^  - id:' "$EDGE_DIR/hub.yml"; then svc+=(hub); else EDGE_DIR="$EDGE_DIR" "${compose[@]}" stop hub 2>/dev/null || true; fi
  EDGE_DIR="$EDGE_DIR" "${compose[@]}" up -d --no-deps --force-recreate "${svc[@]}"
}

cmd_list() { slugs; }

cmd_up() {
  local slug="${1:-}" tag
  check_slug "$slug"
  ensure_edge
  if [ ! -f "$POINTS/$slug/.env" ]; then
    INSTANCES_ROOT="$POINTS" PROJECT_PREFIX=swapowl- "$SCRIPT_DIR/new-restaurant.sh" "$slug" >/dev/null
    printf 'IIKO_PASSWORD=%s\nMETRICS_TOKEN=%s\n' "$(rand 24)" "$(rand 32)" >> "$POINTS/$slug/.env"
    echo "точка $slug создана: $POINTS/$slug/.env"
  fi
  write_override "$slug"
  tag="$(cat "$POINTS/$slug/.last_good" 2>/dev/null || envval "$EDGE_DIR/.env" IMAGE_TAG)"
  dc "$slug" "$tag" up -d --remove-orphans
  wait_health "$slug" "$tag" || die "$slug: /healthz не ответил за ${HEALTH_TIMEOUT} с"
  echo "$tag" > "$POINTS/$slug/.last_good"
  edge_up
  echo "точка $slug: $slug.$(envval "$EDGE_DIR/.env" SITE_DOMAIN) (тег $tag)"
}

deploy_one() {
  local slug="$1" tag="$2" prev
  prev="$(cat "$POINTS/$slug/.last_good" 2>/dev/null || true)"
  echo "== $slug: $tag (прошлый: ${prev:-нет})"
  write_override "$slug"
  if [ -n "$prev" ]; then backup "$slug" || { echo "$slug: бэкап не удался, точка не тронута" >&2; return 1; }; fi
  # deploy_one вызывается из условия if: set -e здесь не действует, коды возврата проверяем явно.
  if ! dc "$slug" "$tag" pull || ! dc "$slug" "$tag" up -d --remove-orphans; then
    echo "$slug: pull/up $tag не удался" >&2
    if [ -n "$prev" ] && [ "$prev" != "$tag" ]; then dc "$slug" "$prev" up -d --remove-orphans || true; fi
    return 1
  fi
  if wait_health "$slug" "$tag"; then echo "$tag" > "$POINTS/$slug/.last_good"; echo "$slug: готово"; return 0; fi
  echo "$slug: /healthz не ответил за ${HEALTH_TIMEOUT} с" >&2
  dc "$slug" "$tag" logs --tail=30 api >&2 || true
  if [ -n "$prev" ] && [ "$prev" != "$tag" ]; then
    echo "$slug: возвращаю $prev" >&2
    dc "$slug" "$prev" up -d --remove-orphans || true
    if wait_health "$slug" "$prev"; then echo "$slug: откат на $prev выполнен" >&2; else echo "$slug: после отката /healthz тоже не отвечает" >&2; fi
  fi
  return 1
}

cmd_deploy() {
  local tag="${1:-}" targets=() s ok=()
  [ -n "$tag" ] || die "использование: fleet.sh deploy <tag> [--all | slug...]"
  shift
  [ "$#" -gt 0 ] || die "укажите --all или список точек (случайно обновить все нельзя)"
  ensure_edge
  if [ "$1" = "--all" ]; then mapfile -t targets < <(slugs); else targets=("$@"); fi
  [ "${#targets[@]}" -gt 0 ] || die "точек нет"
  for s in "${targets[@]}"; do check_slug "$s"; [ -f "$POINTS/$s/.env" ] || die "нет точки $s"; done
  for s in "${targets[@]}"; do
    if deploy_one "$s" "$tag"; then
      ok+=("$s")
    else
      echo "раскатка остановлена на $s. Обновлены: ${ok[*]:-нет}. Остальные не тронуты." >&2
      exit 1
    fi
  done
  echo "обновлено точек: ${#ok[@]} ($tag)"
}

cmd_status() {
  local slug cid img tag health
  printf '%-24s %-22s %s\n' POINT TAG HEALTH
  for slug in $(slugs); do
    cid="$(dc "$slug" latest ps -q api 2>/dev/null | head -n 1 || true)"
    if [ -n "$cid" ]; then
      img="$(docker inspect --format '{{.Config.Image}}' "$cid" 2>/dev/null || true)"; tag="${img##*:}"
      if dc "$slug" "$tag" exec -T api wget -q -T 3 -O /dev/null http://127.0.0.1:8080/healthz 2>/dev/null; then health=ok; else health=DOWN; fi
    else
      tag="$(cat "$POINTS/$slug/.last_good" 2>/dev/null || echo -)"; health=stopped
    fi
    printf '%-24s %-22s %s\n' "$slug" "$tag" "$health"
  done
}

cmd_hub_key() {
  local slug="${1:-}" key
  check_slug "$slug"
  [ -f "$POINTS/$slug/.env" ] || die "нет точки $slug"
  IFS= read -r key || true
  [ -n "$key" ] || die "ключ (роль viewer) читается из stdin: fleet.sh hub-key $slug <<<\"\$KEY\""
  (umask 077; printf '%s\n' "$key" > "$POINTS/$slug/hub.key")
  edge_up
}

case "${1:-}" in
  list) cmd_list ;;
  up) shift; cmd_up "$@" ;;
  deploy) shift; cmd_deploy "$@" ;;
  status) cmd_status ;;
  hub-key) shift; cmd_hub_key "$@" ;;
  edge) ensure_edge; edge_up ;;
  *) sed -n '2,10p' "${BASH_SOURCE[0]}"; exit 2 ;;
esac
