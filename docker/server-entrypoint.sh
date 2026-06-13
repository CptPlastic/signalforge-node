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

mkdir -p /data /data/call-archive /etc/signalforge

# Generate s3cmd config for DigitalOcean Spaces when credentials are provided.
# https://docs.digitalocean.com/products/spaces/reference/s3cmd/
if [ -n "${SPACES_ACCESS_KEY:-}" ] && [ -n "${SPACES_SECRET_KEY:-}" ] && [ -n "${SPACES_ENDPOINT:-}" ]; then
  s3cfg_path="${CALL_ARCHIVE_S3CFG:-/etc/signalforge/s3cfg}"
  mkdir -p "$(dirname "$s3cfg_path")"
  cat >"$s3cfg_path" <<EOF
[default]
access_key = ${SPACES_ACCESS_KEY}
secret_key = ${SPACES_SECRET_KEY}
host_base = ${SPACES_ENDPOINT}
host_bucket = %(bucket)s.${SPACES_ENDPOINT}
use_https = True
signature_v2 = False
EOF
  chmod 600 "$s3cfg_path"
fi

if [ "$(id -u)" = "0" ]; then
  # Keep database and archive volumes writable for the app user, then drop privileges.
  chown -R app:app /data /etc/signalforge
  exec su-exec app "$@"
fi

exec "$@"
