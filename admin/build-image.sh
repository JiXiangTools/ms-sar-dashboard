#!/usr/bin/env sh
set -eu

DATE_TAG="$(date +%y%m%d)"
COMMIT_ID="${COMMIT_ID:-$(git rev-parse --short HEAD 2>/dev/null || printf '%s' 'nogit')}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
IMAGE_REPOSITORY="${IMAGE_REPOSITORY:-dockerhub.seobot.cc/ms/sar-dashboard}"
IMAGE_TAG="${IMAGE_TAG:-${DATE_TAG}_${COMMIT_ID}}"
IMAGE_NAME="${IMAGE_NAME:-${IMAGE_REPOSITORY}:${IMAGE_TAG}}"
DOCKERFILE_PATH="${DOCKERFILE_PATH:-Dockerfile}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
DOCKER_BIN="${DOCKER_BIN:-docker}"
COMPOSE_ENV_FILE="${COMPOSE_ENV_FILE:-.compose-image.env}"
SYNC_COMPOSE_IMAGE="${SYNC_COMPOSE_IMAGE:-true}"

"${DOCKER_BIN}" build \
  --build-arg COMMIT_ID="${COMMIT_ID}" \
  --build-arg BUILD_TIME="${BUILD_TIME}" \
  --build-arg GOPROXY="${GOPROXY}" \
  --build-arg HTTP_PROXY= \
  --build-arg HTTPS_PROXY= \
  --build-arg ALL_PROXY= \
  --build-arg NO_PROXY= \
  --build-arg http_proxy= \
  --build-arg https_proxy= \
  --build-arg all_proxy= \
  --build-arg no_proxy= \
  -f "${DOCKERFILE_PATH}" \
  -t "${IMAGE_NAME}" \
  .

if [ "${SYNC_COMPOSE_IMAGE}" = "true" ]; then
  tmp_file="$(mktemp "${COMPOSE_ENV_FILE}.tmp.XXXXXX")"
  trap 'rm -f "${tmp_file}"' EXIT HUP INT TERM
  printf 'MS_SAR_DASHBOARD_IMAGE=%s\n' "${IMAGE_NAME}" > "${tmp_file}"
  mv "${tmp_file}" "${COMPOSE_ENV_FILE}"
  tmp_file=""
  trap - EXIT HUP INT TERM
  printf 'updated local compose image env %s to %s\n' "${COMPOSE_ENV_FILE}" "${IMAGE_NAME}"
fi

printf '%s\n' "${IMAGE_NAME}"
