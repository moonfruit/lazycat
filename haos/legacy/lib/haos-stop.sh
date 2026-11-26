#!/usr/bin/env bash
# haos-stop.sh — graceful powerdown for the HAOS guest.
#
# Called by systemd ExecStop. Sends QMP system_powerdown (ACPI power button)
# and BLOCKS until qemu exits or our internal timeout fires. Blocking is
# required: systemd sends SIGTERM to the main process IMMEDIATELY after
# ExecStop returns — if we returned right away the guest would never get a
# chance to actually shut down.
#
# Timeout budget: we wait up to HAOS_SHUTDOWN_WAIT seconds (default 60).
# Must be less than the unit's TimeoutStopSec (currently 90s) so systemd
# still has room to escalate to SIGTERM/SIGKILL if the guest is wedged.
set -euo pipefail

: "${HAOS_QMP_SOCK:=/run/haos/qmp.sock}"
: "${HAOS_SHUTDOWN_WAIT:=60}"

if [[ ! -S "$HAOS_QMP_SOCK" ]]; then
  echo "haos-stop: $HAOS_QMP_SOCK not present, qemu likely not running" >&2
  exit 0
fi

# 1. Tell the guest to shut down via ACPI power button.
#    -T5 caps how long the QMP write can hang if the socket is unresponsive.
printf '%s\n%s\n' \
  '{"execute":"qmp_capabilities"}' \
  '{"execute":"system_powerdown"}' \
  | socat -T5 - "UNIX-CONNECT:$HAOS_QMP_SOCK" >/dev/null 2>&1 || true
echo "haos-stop: powerdown signal sent, waiting up to ${HAOS_SHUTDOWN_WAIT}s for guest to halt"

# 2. Wait for qemu to exit. Use systemd's notion of the main PID — it tracks
#    the unit's primary process, which IS qemu (haos-launch.sh exec'd it).
MAIN_PID=$(systemctl show -p MainPID --value haos.service 2>/dev/null || echo 0)
if [[ -z "$MAIN_PID" || "$MAIN_PID" == "0" ]]; then
  echo "haos-stop: no main PID — qemu already gone"
  exit 0
fi

waited=0
while (( waited < HAOS_SHUTDOWN_WAIT )); do
  if ! kill -0 "$MAIN_PID" 2>/dev/null; then
    echo "haos-stop: qemu (pid $MAIN_PID) exited cleanly after ${waited}s"
    exit 0
  fi
  sleep 1
  waited=$((waited + 1))
done

echo "haos-stop: guest did not halt within ${HAOS_SHUTDOWN_WAIT}s — letting systemd SIGTERM" >&2
# Return 0 so systemd proceeds with normal stop (SIGTERM main process, then
# SIGKILL after the remaining TimeoutStopSec window). Returning nonzero would
# mark the unit as failed even though stopping is the user's intent.
exit 0
