#!/bin/sh
set -eu
umask 077

backup_dir=/backups
backup_pattern='dash14-????????T??????Z-??????.db'

log() {
    printf '%s %s\n' "$(date -u +%FT%TZ)" "$*"
}

# Serialize scheduled and manual backups, including cleanup and rotation.
exec 9>"$backup_dir/.lock"
flock -w 300 9

database=$(yq -e -r '.sqlite.path | select(tag == "!!str")' /config/config.yaml)
case "$database" in
    /data/*) ;;
    *) log 'sqlite.path must point to a database under /data' >&2; exit 1 ;;
esac
if [ ! -f "$database" ] || [ ! -s "$database" ]; then
    log "Waiting for SQLite database at $database" >&2
    exit 1
fi

# Clean up staging directories left by interrupted runs under the lock.
find "$backup_dir" -maxdepth 1 -type d -name '.dash14-partial.*' \
    -exec rm -rf {} +
staging=$(mktemp -d "$backup_dir/.dash14-partial.XXXXXX")
trap 'rm -rf "$staging"' EXIT
snapshot="$staging/dash14.db"

timeout 300 sqlite3 -batch -bail -readonly "$database" \
    '.timeout 5000' ".backup $snapshot"
# The backup can inherit WAL mode. Seal it as a standalone database.
result=$(timeout 300 sqlite3 -batch -bail "$snapshot" \
    'PRAGMA journal_mode=DELETE; PRAGMA quick_check;')
if [ "$result" != "$(printf 'delete\nok')" ]; then
    log "Snapshot check failed: $result" >&2
    exit 1
fi

name="dash14-$(date -u +%Y%m%dT%H%M%SZ)-${staging##*.}.db"
touch "$snapshot"
mv "$snapshot" "$backup_dir/$name"
log "Verified backup saved as $name"

find "$backup_dir" -maxdepth 1 -type f -name "$backup_pattern" \
    -mtime +13 -exec rm {} +
