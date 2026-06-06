#!/bin/sh
set -eu

# Plesk/webspace stacks mount .env.production beside compose. Source it here so auth
# vars survive compose interpolation quirks (empty ${AUTH_*} overrides env_file).
for env_file in /stack/.env.production /stack/.env; do
  if [ -f "$env_file" ]; then
    set -a
    # shellcheck disable=SC1090
    . "$env_file"
    set +a
  fi
done

if [ "$(id -u)" = "0" ]; then
  mkdir -p /data
  # Keep database volume writable for the app user, then drop privileges.
  chown -R app:app /data
  exec su-exec app "$@"
fi

exec "$@"
