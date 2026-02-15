#!/bin/sh
set -eu

method=""
path=""
protocol=""
IFS=' ' read -r method path protocol || true

while IFS= read -r header; do
  case "${header}" in
    ''|$'\r')
      break
      ;;
  esac
done

if [ "${method}" = "GET" ] && [ "${path}" = "/cgi-bin/metrics" ]; then
  printf 'HTTP/1.1 200 OK\r\n'
  printf 'Content-Type: text/plain; version=0.0.4\r\n'
  printf 'Connection: close\r\n'
  printf '\r\n'
  exec /app/metrics/metrics.cgi
fi

printf 'HTTP/1.1 404 Not Found\r\n'
printf 'Content-Type: text/plain\r\n'
printf 'Connection: close\r\n'
printf '\r\n'
printf 'not found\n'
