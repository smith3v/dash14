#!/bin/sh
set -eu
umask 077
trap 'exit 0' INT TERM

while :; do
    recent=$(find /backups -maxdepth 1 -type f \
        -name 'dash14-????????T??????Z-??????.db' -mmin -1440 -print -quit)
    if [ -z "$recent" ] && ! sh /usr/local/bin/dash14-backup; then
        printf '%s Backup failed. Retrying in 60 seconds.\n' "$(date -u +%FT%TZ)" >&2
    fi
    sleep 60 &
    wait "$!"
done
