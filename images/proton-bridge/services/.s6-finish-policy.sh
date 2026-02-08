#!/bin/sh
set -eu

service_name="${1:?service name is required}"
critical="${2:-false}"

exit_code="${3:-0}"
signal_num="${4:-0}"

state_dir="/tmp/proton-bridge-s6"
mkdir -p "${state_dir}"

start_file="${state_dir}/${service_name}.start"
state_file="${state_dir}/${service_name}.state"

now="$(date +%s)"
started_at="${now}"
if [ -f "${start_file}" ]; then
  started_at="$(cat "${start_file}" 2>/dev/null || echo "${now}")"
fi

runtime="$((now - started_at))"
window="${CRASH_LOOP_WINDOW_SECONDS:-20}"
max_restarts="${CRASH_LOOP_MAX_RESTARTS:-5}"
count=0

case "${window}" in
  ''|*[!0-9]*)
    echo "invalid CRASH_LOOP_WINDOW_SECONDS: ${window}" >&2
    exit 1
    ;;
esac

case "${max_restarts}" in
  ''|*[!0-9]*)
    echo "invalid CRASH_LOOP_MAX_RESTARTS: ${max_restarts}" >&2
    exit 1
    ;;
esac

if [ -f "${state_file}" ]; then
  count="$(cat "${state_file}" 2>/dev/null || echo "0")"
fi

if [ "${runtime}" -lt "${window}" ]; then
  count="$((count + 1))"
else
  count=1
fi
printf '%s\n' "${count}" > "${state_file}"

echo "[${service_name}] exited (code=${exit_code} signal=${signal_num} runtime=${runtime}s restart=${count}/${max_restarts})"

if [ "${critical}" = "true" ] && [ "${count}" -ge "${max_restarts}" ]; then
  echo "[${service_name}] crash loop threshold reached, stopping container"
  exec s6-svscanctl -t /app/services
fi

exit 0
