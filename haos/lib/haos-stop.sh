#!/usr/bin/env bash
# haos-stop.sh — send graceful powerdown to HAOS guest via QMP.
# Called by systemd ExecStop. Returns immediately; systemd's
# TimeoutStopSec controls how long to wait before SIGTERM.
set -euo pipefail

: "${HAOS_QMP_SOCK:=/run/haos/qmp.sock}"

if [[ ! -S "$HAOS_QMP_SOCK" ]]; then
  echo "haos-stop: $HAOS_QMP_SOCK not present, qemu likely not running" >&2
  exit 0
fi

# Two JSON messages on one connection. socat with - reads stdin and sends it.
printf '%s\n%s\n' \
  '{"execute":"qmp_capabilities"}' \
  '{"execute":"system_powerdown"}' \
  | socat - "UNIX-CONNECT:$HAOS_QMP_SOCK" >/dev/null 2>&1 || true

echo "haos-stop: powerdown sent"
