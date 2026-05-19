#!/bin/sh
set -eu

if [ "$(id -u)" = "0" ]; then
  mkdir -p /data
  # Keep database volume writable for the app user, then drop privileges.
  chown -R app:app /data
  exec su-exec app "$@"
fi

exec "$@"
