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
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
SYNC_COMPOSE_IMAGE="${SYNC_COMPOSE_IMAGE:-true}"

docker build \
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

if [ "${SYNC_COMPOSE_IMAGE}" = "true" ] && [ -f "${COMPOSE_FILE}" ]; then
  tmp_file="$(mktemp)"
  awk -v repo="${IMAGE_REPOSITORY}" -v image="${IMAGE_NAME}" '
    {
      marker = "image: " repo ":"
      pos = index($0, marker)
      if (pos > 0) {
        indent = substr($0, 1, pos - 1)
        $0 = indent "image: " image
      }
      print
    }
  ' "${COMPOSE_FILE}" > "${tmp_file}"
  mv "${tmp_file}" "${COMPOSE_FILE}"
  printf 'updated %s image to %s\n' "${COMPOSE_FILE}" "${IMAGE_NAME}"
fi

printf '%s\n' "${IMAGE_NAME}"
