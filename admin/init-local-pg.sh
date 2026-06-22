#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"

DOCKER_BIN="${DOCKER_BIN:-docker}"
GO_BIN="${GO_BIN:-go}"
SERVICES_DEPLOY_DIR="${MSSAR_SERVICES_DEPLOY_DIR:-${ROOT_DIR}/../services-deploy}"
BASE_DIR="${SERVICES_DEPLOY_DIR}/base"
POSTGRES_CONTAINER="${MSSAR_POSTGRES_CONTAINER:-services-deploy-postgres}"
ADMIN_NAME="${MSSAR_INIT_ADMIN_NAME:-admin}"
ADMIN_PASSWORD="${MSSAR_INIT_ADMIN_PASSWORD:-dWz@240926!}"
RESET_DATABASE="${RESET_DATABASE:-false}"

. "${SCRIPT_DIR}/services-deploy-env.sh"
resolve_services_deploy_env
load_services_deploy_defaults

TARGET_DSN="${MSSAR_DATABASE_DSN:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:5432/ms_sar_dashboard?sslmode=disable}"

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

if [ "${RESET_DATABASE}" = "true" ]; then
  "${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d postgres -v ON_ERROR_STOP=1 -v db_name="${database_name}" <<'SQL'
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = :'db_name'
  AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS :"db_name";
CREATE DATABASE :"db_name" OWNER postgres;
SQL
else
  database_exists="$("${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '${database_name}'" | tr -d '[:space:]')"
  if [ -z "${database_exists}" ]; then
    "${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d postgres -v ON_ERROR_STOP=1 -v db_name="${database_name}" <<'SQL'
CREATE DATABASE :"db_name" OWNER postgres;
SQL
  fi
fi

seed_required="false"
schema_exists="$("${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d "${database_name}" -tAc "SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 't_admin'" | tr -d '[:space:]')"
if [ "${RESET_DATABASE}" = "true" ] || [ -z "${schema_exists}" ]; then
  "${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d "${database_name}" -v ON_ERROR_STOP=1 < upgrade/sql/schema.sql
  seed_required="true"
else
  admin_exists="$("${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d "${database_name}" -tAc "SELECT 1 FROM t_admin WHERE name = '${ADMIN_NAME}' AND disabled = FALSE LIMIT 1" | tr -d '[:space:]')"
  if [ -z "${admin_exists}" ]; then
    seed_required="true"
  fi
fi

if [ "${seed_required}" = "true" ]; then
  "${DOCKER_BIN}" exec -i "${POSTGRES_CONTAINER}" psql -U postgres -d "${database_name}" \
    -v ON_ERROR_STOP=1 \
    -v admin_name="${ADMIN_NAME}" \
    -v admin_password_hash="${admin_password_hash}" \
    < upgrade/sql/data.sql
fi

printf 'prepared PostgreSQL database %s with admin %s\n' "${database_name}" "${ADMIN_NAME}"
