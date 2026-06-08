#!/bin/sh
set -eu

append_no_proxy_host() {
  var_name="$1"
  host="$2"
  current_value="$(printenv "$var_name" 2>/dev/null || true)"
  case ",${current_value}," in
    *,"${host}",*)
      return 0
      ;;
  esac
  if [ -n "${current_value}" ]; then
    export "${var_name}=${current_value},${host}"
    return 0
  fi
  export "${var_name}=${host}"
}

append_no_proxy_host "NO_PROXY" "server.muguayun.top"
append_no_proxy_host "no_proxy" "server.muguayun.top"

exec /app/ms-sar-dashboard "$@"
