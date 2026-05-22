#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"

DOCKER_BIN="${DOCKER_BIN:-docker}"
GO_BIN="${GO_BIN:-go}"
CONFIG_PATH="${CONFIG_PATH:-${ROOT_DIR}/configs/dev.yaml}"
SERVICES_DEPLOY_DIR="${MSSAR_SERVICES_DEPLOY_DIR:-/Users/kely/Desktop/code/services-deploy}"
BASE_DIR="${SERVICES_DEPLOY_DIR}/base"
POSTGRES_CONTAINER="${MSSAR_POSTGRES_CONTAINER:-services-deploy-postgres}"
REDIS_CONTAINER="${MSSAR_REDIS_CONTAINER:-services-deploy-redis}"
ELASTICSEARCH_CONTAINER="${MSSAR_ELASTICSEARCH_CONTAINER:-services-deploy-elasticsearch}"
DATABASE_NAME="${MSSAR_DATABASE_NAME:-ms_sar_dashboard}"
REDIS_DB="${MSSAR_REDIS_DB:-8}"
RESET_DATABASE="${RESET_DATABASE:-false}"

. "${SCRIPT_DIR}/services-deploy-env.sh"
resolve_services_deploy_env
load_services_deploy_defaults

REDIS_EFFECTIVE_PASSWORD="${REDIS_PASSWORD}"
TARGET_DSN="${MSSAR_DATABASE_DSN:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@127.0.0.1:5432/${DATABASE_NAME}?sslmode=disable}"

if ! command -v "${DOCKER_BIN}" >/dev/null 2>&1; then
  printf 'docker executable not found: %s\n' "${DOCKER_BIN}" >&2
  exit 1
fi

if ! command -v "${GO_BIN}" >/dev/null 2>&1; then
  printf 'go executable not found: %s\n' "${GO_BIN}" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  printf 'curl executable not found\n' >&2
  exit 1
fi

ensure_docker_daemon

ensure_container() {
  name="$1"
  if "${DOCKER_BIN}" inspect "${name}" >/dev/null 2>&1; then
    if [ "$("${DOCKER_BIN}" inspect -f '{{.State.Running}}' "${name}")" != "true" ]; then
      "${DOCKER_BIN}" start "${name}" >/dev/null
    fi
    return 0
  fi

  if [ -f "${BASE_DIR}/docker-compose.yml" ]; then
    services_compose_up postgres redis elasticsearch
    return 0
  fi

  printf 'services-deploy container not found: %s\n' "${name}" >&2
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

wait_for_redis() {
  attempt=0
  while [ "${attempt}" -lt 60 ]; do
    if [ -n "${REDIS_PASSWORD}" ] && redis_cli_with_password "${REDIS_PASSWORD}" ping >/dev/null 2>&1; then
      REDIS_EFFECTIVE_PASSWORD="${REDIS_PASSWORD}"
      return 0
    fi
    if redis_cli_without_password ping >/dev/null 2>&1; then
      REDIS_EFFECTIVE_PASSWORD=""
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
  printf 'redis container is not ready\n' >&2
  exit 1
}

redis_cli_with_password() {
  password="$1"
  shift
  "${DOCKER_BIN}" exec -i "${REDIS_CONTAINER}" redis-cli -a "${password}" --no-auth-warning "$@"
}

redis_cli_without_password() {
  "${DOCKER_BIN}" exec -i "${REDIS_CONTAINER}" redis-cli "$@"
}

redis_cli() {
  if [ -n "${REDIS_EFFECTIVE_PASSWORD}" ]; then
    redis_cli_with_password "${REDIS_EFFECTIVE_PASSWORD}" "$@"
    return $?
  fi
  redis_cli_without_password "$@"
}

wait_for_elasticsearch() {
  attempt=0
  while [ "${attempt}" -lt 90 ]; do
    if curl --noproxy '*' -fsS http://127.0.0.1:9200 >/dev/null 2>&1; then
      return 0
    fi
    attempt=$((attempt + 1))
    sleep 1
  done
  printf 'elasticsearch is not ready\n' >&2
  exit 1
}

clear_app_auth_cache() {
  if [ "${RESET_DATABASE}" != "true" ]; then
    return 0
  fi

  keys="$(redis_cli -n "${REDIS_DB}" --raw KEYS 'app_auth_*' | tr -d '\r')"
  if [ -z "${keys}" ]; then
    return 0
  fi

  printf '%s\n' "${keys}" | while IFS= read -r key; do
    if [ -n "${key}" ]; then
      redis_cli -n "${REDIS_DB}" DEL "${key}" >/dev/null
    fi
  done
}

ensure_container "${POSTGRES_CONTAINER}"
ensure_container "${REDIS_CONTAINER}"
ensure_container "${ELASTICSEARCH_CONTAINER}"
wait_for_postgres
wait_for_redis
wait_for_elasticsearch

export MSSAR_DATABASE_DSN="${TARGET_DSN}"
export MSSAR_REDIS_MODE="${MSSAR_REDIS_MODE:-standalone}"
export MSSAR_REDIS_ADDRS="${MSSAR_REDIS_ADDRS:-127.0.0.1:6379}"
export MSSAR_REDIS_DB="${MSSAR_REDIS_DB:-${REDIS_DB}}"
if [ -z "${MSSAR_REDIS_PASSWORD+x}" ]; then
  export MSSAR_REDIS_PASSWORD="${REDIS_EFFECTIVE_PASSWORD}"
fi
export MSSAR_INIT_ADMIN_NAME="${MSSAR_INIT_ADMIN_NAME:-admin}"
export MSSAR_INIT_ADMIN_PASSWORD="${MSSAR_INIT_ADMIN_PASSWORD:-dWz@240926!}"
export RESET_DATABASE

"${SCRIPT_DIR}/init-local-pg.sh"
clear_app_auth_cache

cd "${ROOT_DIR}"
exec "${GO_BIN}" run ./cmd/ms-sar-dashboard -config "${CONFIG_PATH}" "$@"
