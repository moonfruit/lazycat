#!/usr/bin/env bash
# haos-launch.sh — assemble qemu command and exec it.
set -euo pipefail

: "${HAOS_IMAGE_PATH:?required}"
: "${HAOS_OVMF_VARS:?required}"
: "${HAOS_RAM_MB:?required}"
: "${HAOS_VCPUS:?required}"
: "${HAOS_TAP_IF:?required}"
: "${HAOS_MAC:?required}"
: "${HAOS_QMP_SOCK:?required}"

OVMF_CODE=/usr/share/OVMF/OVMF_CODE_4M.fd
if [[ ! -r "$OVMF_CODE" ]]; then
  echo "haos-launch: $OVMF_CODE missing (apt install ovmf?)" >&2
  exit 1
fi

IFINDEX_FILE=/run/haos/tap.ifindex
if [[ ! -r "$IFINDEX_FILE" ]]; then
  echo "haos-launch: $IFINDEX_FILE missing — was ExecStartPre skipped?" >&2
  exit 1
fi
TAPIDX=$(cat "$IFINDEX_FILE")
TAPDEV="/dev/tap$TAPIDX"
if [[ ! -c "$TAPDEV" ]]; then
  echo "haos-launch: $TAPDEV missing" >&2
  exit 1
fi

# Open macvtap as fd 3 then exec qemu. The fd 3 redirection MUST be on the
# `exec` line so qemu inherits it.
exec 3<>"$TAPDEV"
exec qemu-system-x86_64 \
  -name haos,process=haos \
  -enable-kvm -cpu host \
  -machine q35,accel=kvm \
  -smp "$HAOS_VCPUS" -m "$HAOS_RAM_MB" \
  -drive if=pflash,format=raw,readonly=on,file="$OVMF_CODE" \
  -drive if=pflash,format=raw,file="$HAOS_OVMF_VARS" \
  -drive file="$HAOS_IMAGE_PATH",if=virtio,cache=none,aio=native,discard=unmap \
  -netdev tap,id=n0,fd=3,vhost=on \
  -device virtio-net-pci,netdev=n0,mac="$HAOS_MAC" \
  -device i6300esb -action watchdog=pause \
  -display none \
  -serial unix:/run/haos/serial.sock,server,nowait \
  -monitor unix:/run/haos/monitor.sock,server,nowait \
  -qmp unix:"$HAOS_QMP_SOCK",server,nowait
