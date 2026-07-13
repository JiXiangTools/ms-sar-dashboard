#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"

DOCKER_BIN="${DOCKER_BIN:-docker}"
GO_BIN="${GO_BIN:-go}"
SERVICES_DEPLOY_DIR="${MSSAR_SERVICES_DEPLOY_DIR}"
BASE_DIR="${SERVICES_DEPLOY_DIR}/base"
POSTGRES_CONTAINER="${MSSAR_POSTGRES_CONTAINER:-services-deploy-postgres}"
ADMIN_NAME="${MSSAR_INIT_ADMIN_NAME:?MSSAR_INIT_ADMIN_NAME is required}"
ADMIN_PASSWORD="${MSSAR_INIT_ADMIN_PASSWORD:?MSSAR_INIT_ADMIN_PASSWORD is required}"
RESET_DATABASE="${RESET_DATABASE:-false}"
UPDATE_ADMIN="${MSSAR_UPDATE_ADMIN:-false}"

. "${SCRIPT_DIR}/services-deploy-env.sh"
resolve_services_deploy_env
load_services_deploy_defaults

TARGET_DSN="${MSSAR_DATABASE_DSN:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:5432/ms_sar_dashboard?sslmode=disable}"

if [ "${RESET_DATABASE}" = "true" ]; then
  printf 'RESET_DATABASE is no longer supported because migrations never delete an existing database; use a separate disposable database for destructive testing\n' >&2
  exit 2
fi

if ! command -v "${DOCKER_BIN}" >/dev/null 2>&1; then
  printf 'docker executable not found: %s\n' "${DOCKER_BIN}" >&2
  exit 1
fi

if ! command -v "${GO_BIN}" >/dev/null 2>&1; then
  printf 'go executable not found: %s\n' "${GO_BIN}" >&2
  exit 1
fi

ensure_docker_daemon

ensure_postgres_container() {
  if "${DOCKER_BIN}" inspect "${POSTGRES_CONTAINER}" >/dev/null 2>&1; then
    if [ "$("${DOCKER_BIN}" inspect -f '{{.State.Running}}' "${POSTGRES_CONTAINER}")" != "true" ]; then
      "${DOCKER_BIN}" start "${POSTGRES_CONTAINER}" >/dev/null
    fi
    return 0
  fi

  if [ -f "${BASE_DIR}/docker-compose.yml" ]; then
    services_compose_up postgres
    return 0
  fi

  printf 'services-deploy postgres container not found: %s\n' "${POSTGRES_CONTAINER}" >&2
  exit 1
}

wait_for_postgres() {
  attempt=0
  while [ "${attempt}" -lt 60 ]; do
    if "${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d postgres -tAc 'SELECT 1' >/dev/null 2>&1; then
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
  printf 'postgres container is not ready\n' >&2
  exit 1
}

database_name="$(printf '%s\n' "${TARGET_DSN}" | sed -E 's#^[^:]+://[^/]+/([^?]+).*$#\1#')"

cd "${ROOT_DIR}"

ensure_postgres_container
wait_for_postgres

admin_password_hash="$("${GO_BIN}" run ./cmd/hash-password -password "${ADMIN_PASSWORD}")"

database_exists="$(
  "${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d postgres -v ON_ERROR_STOP=1 -v db_name="${database_name}" -tA <<'SQL' | tr -d '[:space:]'
SELECT 1 FROM pg_database WHERE datname = :'db_name';
SQL
)"
if [ -z "${database_exists}" ]; then
  "${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d postgres -v ON_ERROR_STOP=1 -v db_name="${database_name}" <<'SQL'
CREATE DATABASE :"db_name" OWNER postgres;
SQL
fi

set -- \
  -timeout "${MSSAR_DATABASE_MIGRATION_TIMEOUT:-60s}"
if [ "${UPDATE_ADMIN}" = "true" ]; then
  set -- "$@" -update-admin
fi
MSSAR_DATABASE_DSN="${TARGET_DSN}" \
MSSAR_INIT_ADMIN_NAME="${ADMIN_NAME}" \
MSSAR_INIT_ADMIN_PASSWORD_HASH="${admin_password_hash}" \
  "${GO_BIN}" run ./cmd/ms-sar-dbtool "$@"

printf 'prepared PostgreSQL database %s with admin %s\n' "${database_name}" "${ADMIN_NAME}"
