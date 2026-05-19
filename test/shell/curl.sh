#!/usr/bin/env sh
set -eu

BASE_URL="${BASE_URL:-http://127.0.0.1:8081}"
ADMIN_NAME="${ADMIN_NAME:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-dWz@240926!}"

if ! command -v curl >/dev/null 2>&1; then
  printf 'curl executable not found\n' >&2
  exit 1
fi

login_response="$(curl -sS -X POST "${BASE_URL}/api/v1/admin/auth/login" \
  -H 'content-type: application/json' \
  -H 'x-request-id: sar-admin-login-1' \
  -d "{\"name\":\"${ADMIN_NAME}\",\"password\":\"${ADMIN_PASSWORD}\"}")"

printf '%s\n' "${login_response}"

if command -v jq >/dev/null 2>&1; then
  token="$(printf '%s\n' "${login_response}" | jq -r '.data.access_token // empty')"
  if [ -n "${token}" ]; then
    curl -sS "${BASE_URL}/api/v1/admin/app?page=1&page_size=10" \
      -H "authorization: Bearer ${token}" \
      -H 'x-request-id: sar-admin-app-list-1'
    printf '\n'
  fi
fi
