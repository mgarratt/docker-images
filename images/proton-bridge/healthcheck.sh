#!/usr/bin/env sh
set -eu

for service in bridge gpg-agent socat-smtp socat-imap metrics; do
  s6-svstat "/app/services/${service}" | grep -q '^up '
done

gpg-connect-agent /bye >/dev/null 2>&1

netstat -ltn 2>/dev/null | grep -q "[.:]${CONTAINER_SMTP_PORT}[[:space:]]"
netstat -ltn 2>/dev/null | grep -q "[.:]${CONTAINER_IMAP_PORT}[[:space:]]"
netstat -ltn 2>/dev/null | grep -q "[.:]${CONTAINER_METRICS_PORT}[[:space:]]"

printf 'QUIT\r\n' | nc -w 3 127.0.0.1 "${CONTAINER_SMTP_PORT}" | grep -Eq '^220 '
printf 'a1 LOGOUT\r\n' | nc -w 3 127.0.0.1 "${CONTAINER_IMAP_PORT}" | grep -Eq '^\* (OK|PREAUTH|BYE) '
printf 'GET /cgi-bin/metrics HTTP/1.0\r\nHost: localhost\r\n\r\n' \
  | nc -w 3 127.0.0.1 "${CONTAINER_METRICS_PORT}" \
  | grep -Eq '^HTTP/[0-9.]+ 200 '
