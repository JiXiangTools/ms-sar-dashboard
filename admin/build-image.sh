#!/usr/bin/env sh
set -eu

DATE_TAG="$(date +%y%m%d)"
COMMIT_ID="${COMMIT_ID:-$(git rev-parse --short HEAD 2>/dev/null || printf '%s' 'nogit')}"
BUILD_TIME="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
IMAGE_NAME="${IMAGE_NAME:-dockerhub.seobot.cc/ms/sar-dashboard:${DATE_TAG}_${COMMIT_ID}}"
DOCKERFILE_PATH="${DOCKERFILE_PATH:-Dockerfile}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

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

printf '%s\n' "${IMAGE_NAME}"
