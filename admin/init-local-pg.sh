#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"

PSQL_BIN="${PSQL_BIN:-psql}"
GO_BIN="${GO_BIN:-go}"
TARGET_DSN="${MSSAR_DATABASE_DSN:-postgres://postgres:postgres@127.0.0.1:5432/ms_sar_dashboard_test?sslmode=disable}"
ADMIN_NAME="${MSSAR_INIT_ADMIN_NAME:-admin}"
ADMIN_PASSWORD="${MSSAR_INIT_ADMIN_PASSWORD:-dWz@240926!}"
RESET_DATABASE="${RESET_DATABASE:-false}"

if ! command -v "${PSQL_BIN}" >/dev/null 2>&1; then
  printf 'psql executable not found: %s\n' "${PSQL_BIN}" >&2
  exit 1
fi

if ! command -v "${GO_BIN}" >/dev/null 2>&1; then
  printf 'go executable not found: %s\n' "${GO_BIN}" >&2
  exit 1
fi

database_name="$(printf '%s\n' "${TARGET_DSN}" | sed -E 's#^[^:]+://[^/]+/([^?]+).*$#\1#')"
admin_dsn="$(printf '%s\n' "${TARGET_DSN}" | sed -E 's#(://[^/]+/)[^?]+#\1postgres#')"

cd "${ROOT_DIR}"

if [ "${RESET_DATABASE}" = "true" ]; then
  "${PSQL_BIN}" "${admin_dsn}" -v ON_ERROR_STOP=1 <<SQL
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '${database_name}'
  AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS "${database_name}";
CREATE DATABASE "${database_name}";
SQL
fi

admin_password_hash="$("${GO_BIN}" run ./cmd/hash-password -password "${ADMIN_PASSWORD}")"

"${PSQL_BIN}" "${TARGET_DSN}" -v ON_ERROR_STOP=1 -f upgrade/sql/schema.sql
"${PSQL_BIN}" "${TARGET_DSN}" \
  -v ON_ERROR_STOP=1 \
  -v admin_name="${ADMIN_NAME}" \
  -v admin_password_hash="${admin_password_hash}" \
  -f upgrade/sql/data.sql

printf 'initialized PostgreSQL database %s with admin %s\n' "${database_name}" "${ADMIN_NAME}"
