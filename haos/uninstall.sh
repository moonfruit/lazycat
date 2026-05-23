#!/usr/bin/env bash
# uninstall.sh — remove HAOS deployment.
# Default: keep config and data. Use --purge or --purge-uefi-only to remove more.
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "must be root"; exit 1; }

MODE=keep
case "${1:-}" in
  --purge) MODE=purge ;;
  --purge-uefi-only) MODE=purge-uefi ;;
  '') MODE=keep ;;
  *) echo "usage: $0 [--purge|--purge-uefi-only]" >&2; exit 2 ;;
esac

echo "=== haos uninstall: mode=$MODE ==="

# Stop and disable service (gracefully via QMP if running)
if systemctl is-active --quiet haos.service; then
  systemctl stop haos.service
fi
systemctl disable haos.service 2>/dev/null || true

# Remove unit
rm -f /etc/systemd/system/haos.service
systemctl daemon-reload

# Remove scripts
rm -rf /opt/haos/bin

case "$MODE" in
  keep)
    echo "kept /opt/haos/haos.conf and /opt/haos/data/"
    ;;
  purge-uefi)
    rm -f /opt/haos/data/OVMF_VARS.fd
    echo "removed UEFI vars; kept qcow2 and config"
    ;;
  purge)
    rm -f /opt/haos/haos.conf
    rm -rf /opt/haos/data
    rmdir /opt/haos 2>/dev/null || true
    echo "purged everything"
    ;;
esac

echo "=== uninstall complete ==="
