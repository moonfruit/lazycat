#!/usr/bin/env bash
# setup-nfs.sh — minimal NFSv4 server exposing /srv/haos to the LAN.
# Run as root inside LightOS. Idempotent.
#
# HAOS mounts it via Settings → System → Storage → Add Network Storage.
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "must be root"; exit 1; }

EXPORT_DIR=/srv/haos
SUBNET=192.168.50.0/24
EXPORT_LINE="${EXPORT_DIR} ${SUBNET}(rw,sync,no_subtree_check,no_root_squash,insecure)"

echo "=== installing nfs-kernel-server ==="
apt-get install -y --no-install-recommends nfs-kernel-server

echo "=== preparing export dir $EXPORT_DIR ==="
install -d -m 0777 -o nobody -g nogroup "$EXPORT_DIR"

echo "=== /etc/exports ==="
touch /etc/exports
if ! grep -qxF "$EXPORT_LINE" /etc/exports; then
  echo "$EXPORT_LINE" >> /etc/exports
fi
exportfs -ra
echo "  $EXPORT_LINE"

echo "=== enabling nfs-kernel-server ==="
systemctl enable --now nfs-kernel-server

echo
echo "=== NFS ready ==="
SERVER_IP=$(ip -4 -o addr show lzc-debian 2>/dev/null | awk '{print $4}' | cut -d/ -f1)
echo "  server:  ${SERVER_IP:-<lzc-debian IP>}"
echo "  share:   $EXPORT_DIR"
echo "  network: $SUBNET (rw, no_root_squash)"
echo
echo "In HAOS Web UI:"
echo "  Settings → System → Storage → '+ Add Network Storage'"
echo "  Name:     lightos-share        (or whatever)"
echo "  Usage:    media | backup | share"
echo "  Protocol: NFS"
echo "  Server:   ${SERVER_IP:-<above>}"
echo "  Path:     $EXPORT_DIR"
echo "  Version:  v4"
