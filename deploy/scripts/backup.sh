#!/usr/bin/env bash
# Резервная копия экземпляра: pg_dump -Fc в <BACKUP_ROOT>/<имя>/<YYYY-MM-DD>/, хранение RETENTION_DAYS дней.
# Использование: backup.sh <каталог экземпляра>
# Переменные: BACKUP_ROOT (по умолчанию <каталог>/backups), RETENTION_DAYS (по умолчанию 14).
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

load_instance "${1:-}"
BACKUP_ROOT="${BACKUP_ROOT:-$INSTANCE_DIR/backups}"
RETENTION_DAYS="${RETENTION_DAYS:-14}"
case "$RETENTION_DAYS" in ''|*[!0-9]*) die "RETENTION_DAYS должно быть числом" ;; esac
[ "$RETENTION_DAYS" -ge 1 ] || die "RETENTION_DAYS должно быть не меньше 1"

day="$(date +%F)"
dir="$BACKUP_ROOT/$day"
file="$dir/$POSTGRES_DB-$(date +%H%M%S).dump"
mkdir -p "$dir"
umask 077

tmp="$file.partial"
trap 'rm -f "$tmp"' EXIT
dc exec -T -e PGPASSWORD="$POSTGRES_PASSWORD" postgres \
  pg_dump -Fc -U "$POSTGRES_USER" -d "$POSTGRES_DB" > "$tmp"
[ -s "$tmp" ] || die "pg_dump вернул пустой файл"
mv "$tmp" "$file"
trap - EXIT
echo "копия: $file ($(wc -c < "$file") байт)"

# Хранение: удаляем только каталоги дат старше RETENTION_DAYS внутри BACKUP_ROOT.
find "$BACKUP_ROOT" -mindepth 1 -maxdepth 1 -type d -name '????-??-??' -mtime "+$RETENTION_DAYS" -print |
while read -r old; do
  echo "удаляю устаревшую копию: $old"
  rm -rf -- "$old"
done
