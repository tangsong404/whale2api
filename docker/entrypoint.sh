#!/bin/sh
# whale2api/poolui container entrypoint.
#
# The container starts as root so that a bind-mounted /data directory created by
# Docker (owned by root) can be chowned to the unprivileged service user before
# SQLite opens the pool database. Privileges are then dropped via gosu.
set -e

APP_UID=999
APP_GID=999

if [ "$(id -u)" = "0" ]; then
    mkdir -p /data
    chown -R "$APP_UID:$APP_GID" /data
    exec gosu "$APP_UID:$APP_GID" "$@"
fi

exec "$@"
