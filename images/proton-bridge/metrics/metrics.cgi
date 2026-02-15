#!/bin/sh
set -eu

state_dir="/tmp/proton-bridge-s6"

metric_bool() {
  name="$1"
  value="$2"
  printf '%s %s\n' "${name}" "${value}"
}

service_up() {
  service="$1"
  if s6-svstat "/app/services/${service}" 2>/dev/null | grep -q '^up '; then
    value=1
  else
    value=0
  fi
  printf 'proton_bridge_service_up{service="%s"} %s\n' "${service}" "${value}"
}

service_restart_count() {
  service="$1"
  state_file="${state_dir}/${service}.state"
  count=0
  if [ -f "${state_file}" ]; then
    count="$(cat "${state_file}" 2>/dev/null || echo 0)"
  fi
  printf 'proton_bridge_service_restart_count{service="%s"} %s\n' "${service}" "${count}"
}

service_start_time() {
  service="$1"
  start_file="${state_dir}/${service}.start"
  if [ -f "${start_file}" ]; then
    started_at="$(cat "${start_file}" 2>/dev/null || echo 0)"
  else
    started_at=0
  fi
  printf 'proton_bridge_service_start_time_seconds{service="%s"} %s\n' "${service}" "${started_at}"
}

port_listening() {
  name="$1"
  port="$2"
  if netstat -ltn 2>/dev/null | grep -q "[.:]${port}[[:space:]]"; then
    value=1
  else
    value=0
  fi
  printf 'proton_bridge_port_listening{listener="%s",port="%s"} %s\n' "${name}" "${port}" "${value}"
}

smtp_probe_up() {
  if printf 'QUIT\r\n' | nc -w 3 127.0.0.1 "${CONTAINER_SMTP_PORT}" 2>/dev/null | grep -Eq '^220 '; then
    value=1
  else
    value=0
  fi
  metric_bool proton_bridge_smtp_banner_probe_up "${value}"
}

imap_probe_up() {
  if printf 'a1 LOGOUT\r\n' | nc -w 3 127.0.0.1 "${CONTAINER_IMAP_PORT}" 2>/dev/null | grep -Eq '^\* (OK|PREAUTH|BYE) '; then
    value=1
  else
    value=0
  fi
  metric_bool proton_bridge_imap_banner_probe_up "${value}"
}

gpg_agent_up() {
  if gpg-connect-agent /bye >/dev/null 2>&1; then
    value=1
  else
    value=0
  fi
  metric_bool proton_bridge_gpg_agent_up "${value}"
}

pass_entry_count() {
  count=0
  if [ -d "${HOME}/.password-store" ]; then
    count="$(find "${HOME}/.password-store" -type f -name '*.gpg' 2>/dev/null | wc -l | tr -d ' ')"
  fi
  metric_bool proton_bridge_pass_entry_count "${count}"
}

print_metrics() {
  printf 'Content-Type: text/plain; version=0.0.4\r\n\r\n'

  printf '# HELP proton_bridge_service_up Whether an s6 service is currently up (1) or down (0).\n'
  printf '# TYPE proton_bridge_service_up gauge\n'
  service_up bridge
  service_up gpg-agent
  service_up socat-smtp
  service_up socat-imap
  service_up metrics

  printf '# HELP proton_bridge_service_restart_count Number of rapid restarts seen by the s6 finish policy per service.\n'
  printf '# TYPE proton_bridge_service_restart_count gauge\n'
  service_restart_count bridge
  service_restart_count gpg-agent
  service_restart_count socat-smtp
  service_restart_count socat-imap
  service_restart_count metrics

  printf '# HELP proton_bridge_service_start_time_seconds Last recorded service start time as Unix epoch seconds.\n'
  printf '# TYPE proton_bridge_service_start_time_seconds gauge\n'
  service_start_time bridge
  service_start_time gpg-agent
  service_start_time socat-smtp
  service_start_time socat-imap
  service_start_time metrics

  printf '# HELP proton_bridge_gpg_agent_up Whether gpg-agent responds to gpg-connect-agent.\n'
  printf '# TYPE proton_bridge_gpg_agent_up gauge\n'
  gpg_agent_up

  printf '# HELP proton_bridge_port_listening Whether the expected listener port is open inside the container.\n'
  printf '# TYPE proton_bridge_port_listening gauge\n'
  port_listening smtp "${CONTAINER_SMTP_PORT}"
  port_listening imap "${CONTAINER_IMAP_PORT}"
  port_listening metrics "${CONTAINER_METRICS_PORT}"

  printf '# HELP proton_bridge_smtp_banner_probe_up Whether SMTP banner probe on localhost succeeds.\n'
  printf '# TYPE proton_bridge_smtp_banner_probe_up gauge\n'
  smtp_probe_up

  printf '# HELP proton_bridge_imap_banner_probe_up Whether IMAP banner probe on localhost succeeds.\n'
  printf '# TYPE proton_bridge_imap_banner_probe_up gauge\n'
  imap_probe_up

  printf '# HELP proton_bridge_pass_entry_count Number of pass entries in the mounted bridge password store.\n'
  printf '# TYPE proton_bridge_pass_entry_count gauge\n'
  pass_entry_count
}

print_metrics
