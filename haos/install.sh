#!/usr/bin/env bash
# install.sh — deploy HAOS into the LightOS debian instance.
# Run as root inside LightOS. Idempotent.
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
LIB_DIR="$SCRIPT_DIR/lib"
LOG_DIR=/var/log/haos
mkdir -p "$LOG_DIR"
LOG="$LOG_DIR/install-$(date +%Y%m%d-%H%M%S).log"
exec > >(tee -a "$LOG") 2>&1

echo "=== haos install starting at $(date -Iseconds) ==="

# --- 0. Preflight ----------------------------------------------------------
[[ $EUID -eq 0 ]] || { echo "must be root"; exit 1; }
[[ -e /dev/kvm ]] || { echo "/dev/kvm missing — LightOS not exposing KVM"; exit 1; }
[[ -r /proc/cpuinfo ]] || { echo "/proc/cpuinfo unreadable"; exit 1; }
grep -qE '^flags.*\b(vmx|svm)\b' /proc/cpuinfo \
  || { echo "host CPU lacks vmx/svm"; exit 1; }

. /etc/os-release
[[ "$ID" == "debian" && "$VERSION_ID" == "13" ]] \
  || { echo "expected Debian 13 (got $ID $VERSION_ID)"; exit 1; }

PARENT_IF=lzc-debian
ip link show "$PARENT_IF" >/dev/null 2>&1 \
  || { echo "parent interface $PARENT_IF missing — LightOS not in macvlan mode?"; exit 1; }

# --- 1. apt packages --------------------------------------------------------
apt-get update -qq
apt-get install -y --no-install-recommends \
  qemu-system-x86 ovmf qemu-utils socat curl xz-utils

# --- 2. Directory layout ---------------------------------------------------
install -d -m 0755 /opt/haos/bin
install -d -m 0755 /var/lib/haos
# symlink /opt/haos/data → persistent target.
# NOTE: data lives in the LightOS rootfs btrfs subvol — survives LightOS service
# restarts but not instance rebuild. /lzcapp/document/ would survive rebuild
# too, but it's an idmapped mount and root inside LightOS can't write to it.
if [[ ! -L /opt/haos/data ]]; then
  ln -sfn /var/lib/haos /opt/haos/data
fi

# --- 3. Scripts ------------------------------------------------------------
install -m 0755 "$LIB_DIR/haos-network.sh" /opt/haos/bin/
install -m 0755 "$LIB_DIR/haos-launch.sh"  /opt/haos/bin/
install -m 0755 "$LIB_DIR/haos-stop.sh"    /opt/haos/bin/
install -m 0755 "$LIB_DIR/haos-status.sh"  /opt/haos/bin/

# --- 4. Config -------------------------------------------------------------
# Deterministic MAC = 52:54:00 + first 6 hex of md5(hostname)
gen_mac() {
  local hex
  hex=$(hostname | md5sum | head -c 6)
  echo "52:54:00:${hex:0:2}:${hex:2:2}:${hex:4:2}"
}

if [[ ! -f /opt/haos/haos.conf ]]; then
  mac=$(gen_mac)
  sed "s|@AUTOGEN@|$mac|" "$LIB_DIR/haos.conf.example" > /opt/haos/haos.conf
  chmod 0644 /opt/haos/haos.conf
  echo "wrote /opt/haos/haos.conf (HAOS_MAC=$mac)"
else
  echo "keeping existing /opt/haos/haos.conf — diff against template:"
  diff "$LIB_DIR/haos.conf.example" /opt/haos/haos.conf || true
fi

# --- 5. systemd unit -------------------------------------------------------
install -m 0644 "$LIB_DIR/haos.service" /etc/systemd/system/haos.service
systemctl daemon-reload

# --- 6. HAOS image (first install only) ------------------------------------
VERSION=$(cat "$SCRIPT_DIR/VERSION")
IMG=/opt/haos/data/haos_ova.qcow2
BAK="/opt/haos/data/haos_ova-${VERSION}.qcow2.bak"

if [[ ! -f "$IMG" ]]; then
  URL="https://github.com/home-assistant/operating-system/releases/download/${VERSION}/haos_ova-${VERSION}.qcow2.xz"
  echo "downloading $URL"
  tmpxz=$(mktemp --suffix=.qcow2.xz)
  trap 'rm -f "$tmpxz"' EXIT
  curl -fL -o "$tmpxz" "$URL"
  echo "decompressing to $IMG"
  xz -dc "$tmpxz" > "$IMG"
  cp -a "$IMG" "$BAK"
  rm -f "$tmpxz"
  trap - EXIT
  echo "image ready: $(du -h "$IMG" | cut -f1) at $IMG"
else
  echo "image exists, skipping download: $IMG"
fi

# --- 7. UEFI vars (per-instance copy) --------------------------------------
VARS=/opt/haos/data/OVMF_VARS.fd
if [[ ! -f "$VARS" ]]; then
  cp /usr/share/OVMF/OVMF_VARS_4M.fd "$VARS"
  echo "wrote $VARS"
fi

# --- 8. Enable service -----------------------------------------------------
systemctl enable haos.service

echo "=== install complete ==="
echo
echo "Next steps:"
echo "  systemctl start haos.service"
echo "  /opt/haos/bin/haos-status.sh"
echo "  journalctl -u haos.service -f"
