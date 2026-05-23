#!/usr/bin/env bash
# haos-status.sh — diagnostic snapshot of the HAOS deployment.
set -u

CONF=/opt/haos/haos.conf
[[ -r "$CONF" ]] && . "$CONF"

: "${HAOS_TAP_IF:=haos-mvtap0}"
: "${HAOS_MAC:=}"
: "${HAOS_QMP_SOCK:=/run/haos/qmp.sock}"

green() { printf '\033[32m%s\033[0m' "$1"; }
red()   { printf '\033[31m%s\033[0m' "$1"; }
yellow(){ printf '\033[33m%s\033[0m' "$1"; }

# 1. systemd service state
state=$(systemctl is-active haos.service 2>/dev/null || true)
case "$state" in
  active)   echo "1. service:       $(green active)" ;;
  inactive) echo "1. service:       $(yellow inactive)" ;;
  *)        echo "1. service:       $(red "$state")" ;;
esac

# 2. QMP guest status — /run/haos is 0750 root:root (systemd RuntimeDirectory),
#    so non-root users cannot even stat the socket. Detect that explicitly so
#    the message is actionable.
if [[ -S "$HAOS_QMP_SOCK" ]] && command -v socat >/dev/null; then
  resp=$(printf '%s\n%s\n' \
    '{"execute":"qmp_capabilities"}' \
    '{"execute":"query-status"}' \
    | socat -T2 - "UNIX-CONNECT:$HAOS_QMP_SOCK" 2>/dev/null | tr -d '\r')
  guest=$(echo "$resp" | grep -oE '"status":"[^"]+"' | head -1 | cut -d'"' -f4)
  echo "2. guest status:  ${guest:-unknown}"
elif (( EUID != 0 )); then
  echo "2. guest status:  $(yellow "needs sudo (qmp.sock under /run/haos is root-only)")"
elif ! command -v socat >/dev/null; then
  echo "2. guest status:  $(red "socat not installed")"
else
  echo "2. guest status:  $(yellow "QMP socket not present (service stopped?)")"
fi

# 3. tap interface
if ip link show "$HAOS_TAP_IF" >/dev/null 2>&1; then
  link=$(ip -br link show "$HAOS_TAP_IF" | awk '{print $2}')
  echo "3. tap iface:     $HAOS_TAP_IF $link"
else
  echo "3. tap iface:     $(red "missing")"
fi

# 4. HAOS LAN IP via local arp cache (best-effort; macvlan host isolation
#    means this often won't resolve — check from another LAN device instead)
ip_addr=""
if [[ -n "$HAOS_MAC" ]]; then
  mac_lc=$(echo "$HAOS_MAC" | tr '[:upper:]' '[:lower:]')
  ip_addr=$(ip neigh 2>/dev/null | awk -v m="$mac_lc" 'tolower($5)==m {print $1; exit}')
fi
echo "4. HAOS IP (arp): ${ip_addr:-not in local arp — query router or another LAN host}"

# 5. HAOS :8123 reachability (only attempt if we resolved an IP)
if [[ -n "$ip_addr" ]] && command -v curl >/dev/null; then
  code=$(curl -k -s -o /dev/null -w '%{http_code}' --max-time 3 \
    "http://$ip_addr:8123/" 2>/dev/null || echo "000")
  echo "5. HA :8123:      HTTP $code"
else
  echo "5. HA :8123:      skip (no IP)"
fi
