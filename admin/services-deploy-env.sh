#!/usr/bin/env sh

resolve_services_deploy_env() {
  COMPOSE_ENV_FILE="${MSSAR_SERVICES_DEPLOY_ENV_FILE:-}"
  if [ -z "${COMPOSE_ENV_FILE}" ] && [ -f "${BASE_DIR}/.env" ]; then
    COMPOSE_ENV_FILE="${BASE_DIR}/.env"
  fi
  if [ -z "${COMPOSE_ENV_FILE}" ] && [ -f "${SERVICES_DEPLOY_DIR}/.env" ]; then
    COMPOSE_ENV_FILE="${SERVICES_DEPLOY_DIR}/.env"
  fi
  if [ -n "${COMPOSE_ENV_FILE}" ] && [ ! -f "${COMPOSE_ENV_FILE}" ]; then
    printf 'services-deploy env file not found: %s\n' "${COMPOSE_ENV_FILE}" >&2
    exit 1
  fi
}

services_env_file_value() {
  name="$1"
  if [ -z "${COMPOSE_ENV_FILE:-}" ] || [ ! -f "${COMPOSE_ENV_FILE}" ]; then
    return 0
  fi

  value="$(
    awk -v name="${name}" '
      /^[[:space:]]*#/ { next }
      /^[[:space:]]*$/ { next }
      {
        key = $0
        sub(/=.*/, "", key)
        gsub(/^[[:space:]]+|[[:space:]]+$/, "", key)
        if (key == name) {
          sub(/^[^=]*=/, "")
          value = $0
        }
      }
      END { print value }
    ' "${COMPOSE_ENV_FILE}" | tr -d '\r'
  )"

  case "${value}" in
    \"*\")
      value="${value#\"}"
      value="${value%\"}"
      ;;
    \'*\')
      value="${value#\'}"
      value="${value%\'}"
      ;;
  esac
  printf '%s' "${value}"
}

services_compose_value() {
  name="$1"
  fallback="$2"
  eval "current=\${${name}:-}"
  if [ -n "${current}" ] && [ "${current}" != "***" ]; then
    printf '%s' "${current}"
    return 0
  fi

  from_file="$(services_env_file_value "${name}")"
  if [ -n "${from_file}" ] && [ "${from_file}" != "***" ]; then
    printf '%s' "${from_file}"
    return 0
  fi

  printf '%s' "${fallback}"
}

load_services_deploy_defaults() {
  DOCKER_PLATFORM="$(services_compose_value DOCKER_PLATFORM linux/amd64)"
  POSTGRES_IMAGE="$(services_compose_value POSTGRES_IMAGE dockerhub.seobot.cc/library/postgres:18.3)"
  POSTGRES_DB="$(services_compose_value POSTGRES_DB postgres)"
  POSTGRES_USER="${MSSAR_POSTGRES_USER:-$(services_compose_value POSTGRES_USER postgres)}"
  POSTGRES_PASSWORD="${MSSAR_POSTGRES_PASSWORD:-$(services_compose_value POSTGRES_PASSWORD postgres)}"
  POSTGRES_PORT="$(services_compose_value POSTGRES_PORT 5432)"
  REDIS_IMAGE="$(services_compose_value REDIS_IMAGE dockerhub.seobot.cc/library/redis:8.6.1)"
  REDIS_PORT="$(services_compose_value REDIS_PORT 6379)"
  REDIS_PASSWORD="${MSSAR_REDIS_PASSWORD:-$(services_compose_value REDIS_PASSWORD redis)}"
  ELASTICSEARCH_IMAGE="$(services_compose_value ELASTICSEARCH_IMAGE dockerhub.seobot.cc/library/elasticsearch:8.13.4)"
  ELASTICSEARCH_PORT="$(services_compose_value ELASTICSEARCH_PORT 9200)"
  ES_JAVA_OPTS="$(services_compose_value ES_JAVA_OPTS '-Xms512m -Xmx512m')"
  KAFKA_IMAGE="$(services_compose_value KAFKA_IMAGE dockerhub.seobot.cc/library/kafka:3.9.1)"
  KAFKA_CLUSTER_ID="$(services_compose_value KAFKA_CLUSTER_ID 4L6g3nShT-eMCtK--X86sw)"
  KAFKA_PORT="$(services_compose_value KAFKA_PORT 9092)"
  SERVICE_DEPLOY_NET_SUBNET="$(services_compose_value SERVICE_DEPLOY_NET_SUBNET 10.251.0.0/16)"
}

ensure_docker_daemon() {
  if ! "${DOCKER_BIN}" info >/dev/null 2>&1; then
    printf 'docker daemon is not running. Please start Docker Desktop and retry.\n' >&2
    exit 1
  fi
}

services_compose_up() {
  (
    export DOCKER_PLATFORM
    export POSTGRES_IMAGE POSTGRES_DB POSTGRES_USER POSTGRES_PASSWORD POSTGRES_PORT
    export REDIS_IMAGE REDIS_PORT REDIS_PASSWORD
    export ELASTICSEARCH_IMAGE ELASTICSEARCH_PORT ES_JAVA_OPTS
    export KAFKA_IMAGE KAFKA_CLUSTER_ID KAFKA_PORT
    export SERVICE_DEPLOY_NET_SUBNET
    cd "${BASE_DIR}"
    if [ -n "${COMPOSE_ENV_FILE:-}" ]; then
      "${DOCKER_BIN}" compose --env-file "${COMPOSE_ENV_FILE}" up -d "$@"
    else
      "${DOCKER_BIN}" compose up -d "$@"
    fi
  )
}
