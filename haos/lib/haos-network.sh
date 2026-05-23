#!/usr/bin/env bash
# haos-network.sh — manage HAOS macvtap interface.
# Args: up | down
set -euo pipefail

: "${HAOS_PARENT_IF:?HAOS_PARENT_IF must be set}"
: "${HAOS_TAP_IF:?HAOS_TAP_IF must be set}"
: "${HAOS_MAC:?HAOS_MAC must be set}"

IFINDEX_FILE=/run/haos/tap.ifindex

cmd="${1:-}"

case "$cmd" in
  up)
    # 1. Parent must exist and be up
    if ! ip link show "$HAOS_PARENT_IF" >/dev/null 2>&1; then
      echo "haos-network: parent interface $HAOS_PARENT_IF not found" >&2
      exit 1
    fi
    if ! ip -br link show "$HAOS_PARENT_IF" | grep -qE '\<UP\>'; then
      echo "haos-network: parent $HAOS_PARENT_IF is not UP" >&2
      exit 1
    fi

    # 2. Create macvtap if missing (idempotent)
    if ! ip link show "$HAOS_TAP_IF" >/dev/null 2>&1; then
      ip link add link "$HAOS_PARENT_IF" name "$HAOS_TAP_IF" \
        type macvtap mode bridge
    fi

    # 3. Set MAC and bring up (no-op if already correct)
    ip link set "$HAOS_TAP_IF" address "$HAOS_MAC"
    ip link set "$HAOS_TAP_IF" up

    # 4. Wait for /dev/tap<ifindex> to appear (udev)
    IFINDEX=$(cat "/sys/class/net/$HAOS_TAP_IF/ifindex")
    DEV="/dev/tap$IFINDEX"
    for _ in $(seq 1 50); do
      [[ -c "$DEV" ]] && break
      sleep 0.1
    done
    if [[ ! -c "$DEV" ]]; then
      echo "haos-network: $DEV did not appear within 5s" >&2
      exit 1
    fi

    # 5. Grant kvm group access (defensive; current setup runs qemu as root)
    if getent group kvm >/dev/null 2>&1; then
      chgrp kvm "$DEV"
      chmod 660 "$DEV"
    fi

    # 6. Publish ifindex for haos-launch.sh
    mkdir -p "$(dirname "$IFINDEX_FILE")"
    echo "$IFINDEX" > "$IFINDEX_FILE"
    echo "haos-network: $HAOS_TAP_IF up, ifindex=$IFINDEX, dev=$DEV"
    ;;

  down)
    if ip link show "$HAOS_TAP_IF" >/dev/null 2>&1; then
      ip link delete "$HAOS_TAP_IF"
    fi
    rm -f "$IFINDEX_FILE"
    echo "haos-network: $HAOS_TAP_IF down"
    ;;

  *)
    echo "usage: $0 {up|down}" >&2
    exit 2
    ;;
esac
