#!/usr/bin/env sh
set -eu

ADMIN_NAME="${MSSAR_INIT_ADMIN_NAME:?MSSAR_INIT_ADMIN_NAME is required}"
ADMIN_PASSWORD_HASH="${MSSAR_INIT_ADMIN_PASSWORD_HASH:?MSSAR_INIT_ADMIN_PASSWORD_HASH is required}"

psql -v ON_ERROR_STOP=1 --username "${POSTGRES_USER}" --dbname "${POSTGRES_DB}" -f /app/upgrade/sql/schema.sql
psql \
  -v ON_ERROR_STOP=1 \
  -v admin_name="${ADMIN_NAME}" \
  -v admin_password_hash="${ADMIN_PASSWORD_HASH}" \
  --username "${POSTGRES_USER}" \
  --dbname "${POSTGRES_DB}" \
  -f /app/upgrade/sql/data.sql
