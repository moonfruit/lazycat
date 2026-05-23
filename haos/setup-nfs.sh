#!/usr/bin/env bash
# setup-nfs.sh — minimal NFSv4 server exposing /opt/haos/share to the LAN.
# Run as root inside LightOS. Idempotent.
#
# HAOS mounts it via Settings → System → Storage → Add Network Storage.
#
# Why /opt/haos/share (not /lzcapp/document/...): /lzcapp/document is an
# idmapped bind-mount from the host, and the Linux kernel's in-kernel nfsd
# refuses to export idmapped mounts ("does not support NFS export" /
# "access denied by server"). /opt/haos/share lives in the LightOS rootfs
# btrfs subvol — exportable, survives service restarts. Backed up via host
# `btrfs subvolume snapshot` if needed across instance rebuild.
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "must be root"; exit 1; }

EXPORT_DIR=/opt/haos/share
SUBNET=192.168.50.0/24
EXPORT_LINE="${EXPORT_DIR} ${SUBNET}(rw,sync,no_subtree_check,no_root_squash,insecure)"
MARKER_BEGIN="# === setup-nfs.sh managed lines below — do not edit ==="
MARKER_END="# === setup-nfs.sh managed lines above ==="

echo "=== installing nfs-kernel-server ==="
apt-get install -y --no-install-recommends nfs-kernel-server

echo "=== preparing export dir $EXPORT_DIR ==="
install -d -m 0777 "$EXPORT_DIR"
ls -ld "$EXPORT_DIR"

echo "=== /etc/exports ==="
touch /etc/exports
# Strip any previously managed block.
sed -i "\|^${MARKER_BEGIN}$|,\|^${MARKER_END}$|d" /etc/exports
# Strip legacy export lines from earlier setup-nfs.sh versions.
sed -i '\|^/srv/haos|d' /etc/exports
sed -i '\|^/lzcapp/document/haos|d' /etc/exports
# Append new managed block.
{
  echo "$MARKER_BEGIN"
  echo "$EXPORT_LINE"
  echo "$MARKER_END"
} >> /etc/exports
exportfs -ra
echo "  $EXPORT_LINE"

echo "=== enabling nfs-kernel-server ==="
systemctl enable --now nfs-kernel-server

# Best-effort cleanup of legacy export dirs left behind by older script versions.
for legacy in /srv/haos /lzcapp/document/haos; do
  [[ -d "$legacy" ]] && rmdir "$legacy" 2>/dev/null || true
done

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
