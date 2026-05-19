#!/usr/bin/env sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"

GO_BIN="${GO_BIN:-go}"
CONFIG_PATH="${CONFIG_PATH:-${ROOT_DIR}/configs/dev.yaml}"

if ! command -v "${GO_BIN}" >/dev/null 2>&1; then
  printf 'go executable not found: %s\n' "${GO_BIN}" >&2
  exit 1
fi

cd "${ROOT_DIR}"
exec "${GO_BIN}" run ./cmd/ms-sar-dashboard -config "${CONFIG_PATH}" "$@"
