#!/usr/bin/env bash
# Проверка восстановления: последняя копия -> временная база в том же postgres, затем
#   1) число партий и движений во временной базе совпадает с рабочей;
#   2) /healthz живого экземпляра отвечает 200.
# Рабочая база только читается. Временная база удаляется при выходе.
# Использование: restore-check.sh <каталог экземпляра>
# Если рабочая база успела измениться между копией и проверкой, счётчики могут разойтись:
# запускайте сразу после backup.sh (так делает пример из docs/operations.md).
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

load_instance "${1:-}"
BACKUP_ROOT="${BACKUP_ROOT:-$INSTANCE_DIR/backups}"

latest="$(find "$BACKUP_ROOT" -type f -name '*.dump' 2>/dev/null | sort | tail -n 1)"
[ -n "$latest" ] || die "в $BACKUP_ROOT нет копий"
echo "проверяю копию: $latest"

tmpdb="restore_check_$(date +%s)"
cleanup() { psql_in postgres -qc "DROP DATABASE IF EXISTS $tmpdb" >/dev/null 2>&1 || true; }
trap cleanup EXIT

psql_in postgres -qc "CREATE DATABASE $tmpdb"
dc exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres \
  pg_restore --no-owner -U "$POSTGRES_USER" -d "$tmpdb" < "$latest" || die "pg_restore не прошёл"

count() { psql_in "$1" -tA -c "SELECT count(*) FROM $2" | tr -d '[:space:]'; }
fail=0
for t in batches movements; do
  src="$(count "$POSTGRES_DB" "$t")"
  dst="$(count "$tmpdb" "$t")"
  if [ "$src" = "$dst" ]; then
    echo "OK   $t: $dst"
  else
    echo "FAIL $t: рабочая $src, восстановленная $dst" >&2
    fail=1
  fi
done

code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "http://127.0.0.1:$API_PORT/healthz" || true)"
if [ "$code" = "200" ]; then
  echo "OK   /healthz: 200"
else
  echo "FAIL /healthz: $code" >&2
  fail=1
fi

[ "$fail" -eq 0 ] || exit 1
echo "восстановление проверено"
