# HAOS on LightOS Implementation Plan

> **Status (2026-05-23):** Executed and verified. HAOS 17.3 running on 192.168.50.216,
> LAN-reachable. One deviation from this plan: persistent data path was moved from
> `/lzcapp/document/VM/haos/` to `/var/lib/haos/` during Task 12 (the document mount
> is idmapped and unwritable by root). See the spec's Amendments section for details.
> The references to `/lzcapp/document/VM/haos/` below are preserved as the ex-ante
> plan; the shipped code uses `/var/lib/haos/`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a `haos/` subdirectory in the lazycat repo containing all scripts and config templates needed to deploy a Home Assistant OS (HAOS) virtual machine inside the existing LightOS debian instance at 192.168.50.13, giving HAOS a fully LAN-visible identity (independent MAC + DHCP IP) so all smart-home discovery protocols (mDNS / SSDP / HomeKit) work natively.

**Architecture:** systemd-managed `qemu-system-x86_64` running nested KVM inside the LightOS debian container. HAOS attaches via a `macvtap` interface derived from LightOS's macvlan parent (`lzc-debian`), getting an independent MAC and its own DHCP lease from the LAN router. All runtime scripts live under `/opt/haos/`; persistent qcow2 + UEFI vars live in `/lzcapp/document/VM/haos/` (bind-mounted from host through LightOS).

**Tech Stack:** bash, systemd, qemu-system-x86_64 (Debian 13 packaged 9.x), OVMF UEFI firmware, macvtap (Linux kernel), QMP (QEMU Machine Protocol), socat.

**Reference spec:** `docs/superpowers/specs/2026-05-23-haos-on-lightos-design.md`

---

## File Structure

Repository layout to be created:

```
haos/
├── README.md            # End-user deployment guide
├── VERSION              # HAOS upstream version (e.g. 17.3); maintained by update.sh
├── update.sh            # Repo-side: query GitHub for latest HAOS release, update VERSION
├── install.sh           # Deploy entry-point, run inside LightOS as root
├── uninstall.sh         # Remove service; optional --purge / --purge-uefi-only flags
└── lib/                 # Resources copied to LightOS at install time
    ├── haos.conf.example
    ├── haos.service
    ├── haos-network.sh
    ├── haos-launch.sh
    ├── haos-stop.sh
    └── haos-status.sh
```

Each file has one clear responsibility:

| File | Responsibility |
|---|---|
| `VERSION` | Single source of truth for which HAOS release to install |
| `update.sh` | Repo-side automation; never runs on LightOS |
| `install.sh` | All deployment side-effects in LightOS (apt install, copy, enable, download) |
| `uninstall.sh` | Inverse of install.sh; tiered cleanup |
| `lib/haos.conf.example` | User-tunable runtime config template |
| `lib/haos.service` | systemd unit; no logic, only declarative wiring |
| `lib/haos-network.sh` | Just network: macvtap up/down. Single concern |
| `lib/haos-launch.sh` | Just qemu launch: assemble command, exec qemu |
| `lib/haos-stop.sh` | Just graceful shutdown: QMP system_powerdown |
| `lib/haos-status.sh` | Just diagnostics: probe all observability layers |

Test artifacts and runtime files (NOT committed):

```
LightOS internal (created by install.sh):
  /opt/haos/{haos.conf,bin/*,data→symlink}
  /etc/systemd/system/haos.service
  /var/log/haos/install-*.log
  /run/haos/*               (systemd RuntimeDirectory)

Host bind-mount target (created by install.sh via LightOS view):
  /lzcapp/document/VM/haos/{haos_ova.qcow2,OVMF_VARS.fd}
```

## Testing Strategy

This is system/integration code (bash + systemd + qemu), not application logic. There is no isolated unit test framework. Each task validates with:

- **Syntax**: `bash -n <script>` and (where available) `shellcheck`
- **Static contract**: source the script, inspect declared functions/variables
- **Integration**: run the script in a controlled environment, assert observable side effects (file created, interface up, process running, port responding)

Integration tests run inside the **actual LightOS debian** (192.168.50.13). There is no staging copy. To keep tasks reversible, each integration step is paired with a teardown command. The final Task 12 runs the full installer end-to-end and verifies the cardinal deliverable: **`avahi-browse` from another LAN device sees the HAOS instance**.

---

### Task 1: Repository skeleton + VERSION

**Files:**
- Create: `haos/.gitkeep`
- Create: `haos/VERSION`
- Create: `haos/lib/.gitkeep`

- [ ] **Step 1: Create directory layout and VERSION file**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat
mkdir -p haos/lib
echo '17.3' > haos/VERSION
touch haos/.gitkeep haos/lib/.gitkeep
```

- [ ] **Step 2: Verify**

```bash
ls -la haos/
cat haos/VERSION
```

Expected:
```
17.3
```

- [ ] **Step 3: Commit**

```bash
git add haos/
git commit -m "haos: scaffold directory and pin VERSION to 17.3"
```

---

### Task 2: `lib/haos.conf.example`

**Files:**
- Create: `haos/lib/haos.conf.example`

- [ ] **Step 1: Write the config template**

```bash
cat > haos/lib/haos.conf.example <<'EOF'
# Home Assistant OS — runtime configuration
# Loaded by systemd EnvironmentFile=. Lines are KEY=VALUE.
# Comments start with '#'. No bash expansion is performed by systemd.

# --- Image and firmware paths ---
HAOS_IMAGE_PATH=/opt/haos/data/haos_ova.qcow2
HAOS_OVMF_VARS=/opt/haos/data/OVMF_VARS.fd

# --- Resources ---
HAOS_RAM_MB=4096
HAOS_VCPUS=2

# --- Networking ---
# Parent network interface in the LightOS container view.
# When LightOS is running in macvlan mode, this is the lzc-<instance-name> interface.
HAOS_PARENT_IF=lzc-debian
HAOS_TAP_IF=haos-mvtap0

# Stable MAC for HAOS. install.sh replaces the @AUTOGEN@ placeholder
# with a deterministic value derived from MD5(hostname). KVM-reserved prefix 52:54:00.
# You may overwrite manually after install if needed.
HAOS_MAC=@AUTOGEN@

# --- Management sockets (under systemd RuntimeDirectory) ---
HAOS_QMP_SOCK=/run/haos/qmp.sock
EOF
```

- [ ] **Step 2: Verify it parses as shell-style env file**

```bash
( set -a; . haos/lib/haos.conf.example 2>/dev/null || true; set +a;
  echo "PARENT_IF=$HAOS_PARENT_IF MAC=$HAOS_MAC RAM=$HAOS_RAM_MB" )
```

Expected:
```
PARENT_IF=lzc-debian MAC=@AUTOGEN@ RAM=4096
```

- [ ] **Step 3: Commit**

```bash
git add haos/lib/haos.conf.example
git commit -m "haos: add config template"
```

---

### Task 3: `lib/haos-network.sh`

**Files:**
- Create: `haos/lib/haos-network.sh`

**Contract:**
- Args: `up` or `down`
- Env: `HAOS_PARENT_IF`, `HAOS_TAP_IF`, `HAOS_MAC`
- `up`: ensure macvtap exists, MAC is set, link is up; persist ifindex to `/run/haos/tap.ifindex`
- `down`: delete the link; ignore if absent
- Idempotent: repeated `up` does not break; repeated `down` does not fail

- [ ] **Step 1: Write the script**

```bash
cat > haos/lib/haos-network.sh <<'EOF'
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
EOF
chmod +x haos/lib/haos-network.sh
```

- [ ] **Step 2: Verify syntax**

```bash
bash -n haos/lib/haos-network.sh && echo "syntax OK"
```

Expected: `syntax OK`

- [ ] **Step 3: Verify usage banner**

```bash
haos/lib/haos-network.sh 2>&1 || true
```

Expected: `usage: ... {up|down}` and exit 2.

- [ ] **Step 4: Integration test on LightOS debian (192.168.50.13)**

Copy the script to LightOS via stdin, set test env, run `up` then `down`, assert ifindex file and `/dev/tap<N>` lifecycle:

```bash
scp haos/lib/haos-network.sh root@192.168.50.13:/tmp/haos-network.sh
ssh root@192.168.50.13 'bash -c "
set -e
export HAOS_PARENT_IF=lzc-debian
export HAOS_TAP_IF=haos-mvtap-test
export HAOS_MAC=52:54:00:99:99:99
/tmp/haos-network.sh up
test -f /run/haos/tap.ifindex && echo ifindex=\$(cat /run/haos/tap.ifindex)
ls -l /dev/tap\$(cat /run/haos/tap.ifindex)
ip -br addr show haos-mvtap-test
/tmp/haos-network.sh down
! ip link show haos-mvtap-test 2>/dev/null && echo cleaned-up
"'
```

Expected: ifindex printed, `/dev/tapN` exists, interface UP, then `cleaned-up`.

Note: if direct `ssh root@192.168.50.13` does not work (LightOS may not expose root SSH directly), fall back to the host nsenter pattern:

```bash
ssh 192.168.50.11 'DPID=$(runc --root /lzcsys/run/lightos/.runc state cloud.lazycat.lightos.entry--debian | jq -r .pid); nsenter -t $DPID -m -n -u -i -p bash -s' < /tmp/test-cmds.sh
```

- [ ] **Step 5: Commit**

```bash
git add haos/lib/haos-network.sh
git commit -m "haos: add macvtap network up/down script"
```

---

### Task 4: `lib/haos-launch.sh`

**Files:**
- Create: `haos/lib/haos-launch.sh`

**Contract:**
- Reads env: `HAOS_*` from systemd EnvironmentFile
- Reads `/run/haos/tap.ifindex` (set by haos-network.sh)
- Opens `/dev/tap<N>` as fd 3, then exec qemu
- Never returns (exec replaces process)

- [ ] **Step 1: Write the script**

```bash
cat > haos/lib/haos-launch.sh <<'EOF'
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
EOF
chmod +x haos/lib/haos-launch.sh
```

- [ ] **Step 2: Syntax check**

```bash
bash -n haos/lib/haos-launch.sh && echo OK
```

Expected: `OK`

- [ ] **Step 3: Dry-run env-validation (assert it fails fast without env)**

```bash
haos/lib/haos-launch.sh 2>&1 | head -5
```

Expected: `haos-launch: HAOS_IMAGE_PATH: required` (or similar — first required-but-missing variable). Exit nonzero.

- [ ] **Step 4: Commit**

```bash
git add haos/lib/haos-launch.sh
git commit -m "haos: add qemu launch script with macvtap fd handoff"
```

---

### Task 5: `lib/haos-stop.sh`

**Files:**
- Create: `haos/lib/haos-stop.sh`

**Contract:**
- No args, reads `HAOS_QMP_SOCK` from env
- Sends `qmp_capabilities` then `system_powerdown` via socat
- Exits 0 always (idempotent; systemd's TimeoutStopSec handles waiting)

- [ ] **Step 1: Write the script**

```bash
cat > haos/lib/haos-stop.sh <<'EOF'
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
EOF
chmod +x haos/lib/haos-stop.sh
```

- [ ] **Step 2: Syntax check + idle invocation**

```bash
bash -n haos/lib/haos-stop.sh && echo OK
HAOS_QMP_SOCK=/tmp/nonexistent.sock haos/lib/haos-stop.sh
```

Expected:
```
OK
haos-stop: /tmp/nonexistent.sock not present, qemu likely not running
```
Exit 0.

- [ ] **Step 3: Commit**

```bash
git add haos/lib/haos-stop.sh
git commit -m "haos: add graceful QMP shutdown helper"
```

---

### Task 6: `lib/haos-status.sh`

**Files:**
- Create: `haos/lib/haos-status.sh`

**Contract:**
- No args, reads `/opt/haos/haos.conf` if available
- Prints a 5-line status report covering: systemd service, QMP guest status, tap interface, HAOS MAC's LAN IP (best-effort via local arp), HAOS :8123 port
- Always exits 0 (it's a probe, not a gate)

- [ ] **Step 1: Write the script**

```bash
cat > haos/lib/haos-status.sh <<'EOF'
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

# 2. QMP guest status
if [[ -S "$HAOS_QMP_SOCK" ]] && command -v socat >/dev/null; then
  resp=$(printf '%s\n%s\n' \
    '{"execute":"qmp_capabilities"}' \
    '{"execute":"query-status"}' \
    | socat -T2 - "UNIX-CONNECT:$HAOS_QMP_SOCK" 2>/dev/null | tr -d '\r')
  guest=$(echo "$resp" | grep -oE '"status":"[^"]+"' | head -1 | cut -d'"' -f4)
  echo "2. guest status:  ${guest:-unknown}"
else
  echo "2. guest status:  $(yellow "QMP socket unavailable")"
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
EOF
chmod +x haos/lib/haos-status.sh
```

- [ ] **Step 2: Syntax check**

```bash
bash -n haos/lib/haos-status.sh && echo OK
```

Expected: `OK`

- [ ] **Step 3: Run with no conf (handles missing gracefully)**

```bash
haos/lib/haos-status.sh
```

Expected: 5 lines, all annotated; exit 0. systemd service likely "inactive" or "unknown" on the dev machine — that's fine.

- [ ] **Step 4: Commit**

```bash
git add haos/lib/haos-status.sh
git commit -m "haos: add status probe script"
```

---

### Task 7: `lib/haos.service`

**Files:**
- Create: `haos/lib/haos.service`

- [ ] **Step 1: Write the unit**

```bash
cat > haos/lib/haos.service <<'EOF'
[Unit]
Description=Home Assistant OS (KVM nested in LightOS)
Documentation=https://www.home-assistant.io/installation/linux
Wants=network-online.target
After=network-online.target

[Service]
Type=exec
EnvironmentFile=/opt/haos/haos.conf
RuntimeDirectory=haos
RuntimeDirectoryMode=0750

ExecStartPre=/opt/haos/bin/haos-network.sh up
ExecStart=/opt/haos/bin/haos-launch.sh
ExecStop=/opt/haos/bin/haos-stop.sh
ExecStopPost=/opt/haos/bin/haos-network.sh down

KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=90s

Restart=on-failure
RestartSec=10s

ProtectSystem=strict
# /opt/haos contains the data symlink, /lzcapp/document/VM/haos is its real target,
# /run/haos is RuntimeDirectory, /var/log/haos receives install logs.
ReadWritePaths=/opt/haos /lzcapp/document/VM/haos /run/haos /var/log/haos

[Install]
WantedBy=multi-user.target
EOF
```

- [ ] **Step 2: Verify with systemd-analyze (locally if available, otherwise on LightOS in Task 8 integration)**

```bash
if command -v systemd-analyze >/dev/null 2>&1; then
  systemd-analyze verify haos/lib/haos.service 2>&1 || true
fi
echo "(verify may print warnings about referenced paths not existing on dev host — those check at install time on LightOS)"
```

Expected: no syntax errors. Warnings about `/opt/haos/...` not existing are acceptable on the dev host.

- [ ] **Step 3: Commit**

```bash
git add haos/lib/haos.service
git commit -m "haos: add systemd unit"
```

---

### Task 8: `install.sh`

**Files:**
- Create: `haos/install.sh`

**Contract:**
- Run as root inside LightOS debian
- Idempotent: any step re-run produces no harm
- All paths absolute; reads its own location to find `lib/`

- [ ] **Step 1: Write the script**

```bash
cat > haos/install.sh <<'EOF'
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
install -d -m 0755 /lzcapp/document/VM/haos
# symlink /opt/haos/data → persistent target
if [[ ! -L /opt/haos/data ]]; then
  ln -sfn /lzcapp/document/VM/haos /opt/haos/data
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
EOF
chmod +x haos/install.sh
```

- [ ] **Step 2: Syntax check**

```bash
bash -n haos/install.sh && echo OK
```

Expected: `OK`

- [ ] **Step 3: Integration — dry-run preflight in LightOS**

Copy in just the preflight portion (sections 0 + bash header) and run it. This validates that the preflight asserts the right things in the real environment without actually installing.

```bash
ssh 192.168.50.11 'DPID=$(runc --root /lzcsys/run/lightos/.runc state cloud.lazycat.lightos.entry--debian | jq -r .pid); nsenter -t $DPID -m -n -u -i -p bash -c "
set -e
[[ \$EUID -eq 0 ]] && echo root: yes
[[ -e /dev/kvm ]] && echo kvm: yes
grep -qE \"^flags.*\\\\b(vmx|svm)\\\\b\" /proc/cpuinfo && echo vmx: yes
. /etc/os-release
echo os: \$ID \$VERSION_ID
ip link show lzc-debian >/dev/null && echo parent: yes
"'
```

Expected:
```
root: yes
kvm: yes
vmx: yes
os: debian 13
parent: yes
```

- [ ] **Step 4: Commit**

```bash
git add haos/install.sh
git commit -m "haos: add idempotent install entry-point"
```

---

### Task 9: `uninstall.sh`

**Files:**
- Create: `haos/uninstall.sh`

- [ ] **Step 1: Write the script**

```bash
cat > haos/uninstall.sh <<'EOF'
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

# Always remove the data symlink (it's just a pointer)
rm -f /opt/haos/data

case "$MODE" in
  keep)
    echo "kept /opt/haos/haos.conf and /lzcapp/document/VM/haos/"
    ;;
  purge-uefi)
    rm -f /lzcapp/document/VM/haos/OVMF_VARS.fd
    echo "removed UEFI vars; kept qcow2 and config"
    ;;
  purge)
    rm -f /opt/haos/haos.conf
    rm -rf /lzcapp/document/VM/haos
    rmdir /opt/haos 2>/dev/null || true
    echo "purged everything"
    ;;
esac

echo "=== uninstall complete ==="
EOF
chmod +x haos/uninstall.sh
```

- [ ] **Step 2: Syntax check**

```bash
bash -n haos/uninstall.sh && echo OK
```

Expected: `OK`

- [ ] **Step 3: Argument parsing test**

```bash
haos/uninstall.sh --bogus 2>&1 || echo "exit=$?"
```

Expected: prints usage, `exit=2`.

- [ ] **Step 4: Commit**

```bash
git add haos/uninstall.sh
git commit -m "haos: add tiered uninstall script"
```

---

### Task 10: `update.sh`

**Files:**
- Create: `haos/update.sh`

**Contract:**
- Follow CLAUDE.md "版本更新约定": first line may `exec proxy`, source user shell libs, sed VERSION in place
- Updates the `haos/VERSION` file only; does not download images
- Prints diff unless invoked with `-N`

- [ ] **Step 1: Write the script**

```bash
cat > haos/update.sh <<'EOF'
#!/usr/bin/env bash
# update.sh — bump VERSION to upstream HAOS latest stable release.
# Per the repo convention, sources user shell helpers from $ENV/lib/bash.
set -euo pipefail

# Hop through proxy if available (used by other update.sh in this repo)
if [[ -z "${IN_PROXY:-}" ]] && command -v proxy >/dev/null 2>&1; then
  export IN_PROXY=1
  exec proxy "$0" "$@"
fi

cd "$(dirname "$0")"

# Use repo's shared helpers if present
if [[ -n "${ENV:-}" && -d "$ENV/lib/bash" ]]; then
  # shellcheck disable=SC1091
  . "$ENV/lib/bash/github.sh"
  NEW=$(find-latest-version home-assistant operating-system)
else
  # Fallback: GitHub redirect of /releases/latest points at the latest tag.
  NEW=$(curl -sLI "https://github.com/home-assistant/operating-system/releases/latest" \
        | awk -F'/' 'tolower($1) ~ /^location:/ { print $NF }' | tr -d '\r')
fi

[[ -n "$NEW" ]] || { echo "could not determine latest version" >&2; exit 1; }

sed -i.bak "s|^[0-9.]\\+$|$NEW|" VERSION && rm -f VERSION.bak
echo "VERSION = $(cat VERSION)"

if [[ "${1:-}" != "-N" ]]; then
  git --no-pager diff VERSION || true
fi
EOF
chmod +x haos/update.sh
```

- [ ] **Step 2: Syntax check**

```bash
bash -n haos/update.sh && echo OK
```

Expected: `OK`

- [ ] **Step 3: Run it (fallback path will execute because the user lib path is not present in this dev shell)**

```bash
( cd haos && ./update.sh -N )
cat haos/VERSION
```

Expected: VERSION printed, value is a semver-looking string (e.g. `17.3` or later). Exit 0.

- [ ] **Step 4: Commit (only if VERSION actually changed; otherwise skip)**

```bash
if ! git diff --quiet haos/VERSION; then
  git add haos/VERSION haos/update.sh
  git commit -m "haos: add update.sh and bump VERSION"
else
  git add haos/update.sh
  git commit -m "haos: add update.sh"
fi
```

---

### Task 11: `README.md`

**Files:**
- Create: `haos/README.md`

- [ ] **Step 1: Write the README**

```bash
cat > haos/README.md <<'EOF'
# HAOS on LightOS

Deploy [Home Assistant OS](https://www.home-assistant.io/installation/linux/)
as a KVM virtual machine inside the LightOS Debian instance, with full
LAN visibility (independent MAC + DHCP IP) so all smart-home discovery
protocols work natively.

Not a `.lpk` application — HAOS is a complete OS and integrates poorly
with the LazyCat application framework. This subdirectory is deployed
manually via `install.sh` running on the target LightOS instance.

## Prerequisites

- LightOS Debian instance with `network_mode: macvlan` (verify via the
  host's `/lzcsys/run/lightos/<owner>--<name>/lightos.json`).
- `/dev/kvm` visible inside the LightOS container (default for
  `security_level: system`).
- Host CPU with `vmx` or `svm`; host kernel with nested KVM enabled
  (`/sys/module/kvm_{intel,amd}/parameters/nested = Y`).
- At least 4 GiB free RAM and 32 GiB free disk on the host.

## Deploy

From this directory on your dev machine:

```sh
rsync -a --delete ./ root@<lightos-ip>:/root/haos/
ssh root@<lightos-ip> 'cd /root/haos && ./install.sh'
ssh root@<lightos-ip> 'systemctl start haos.service'
```

If your LightOS instance does not expose root SSH directly, run install.sh
via host nsenter:

```sh
ssh <host-ip> 'DPID=$(runc --root /lzcsys/run/lightos/.runc \
  state cloud.lazycat.lightos.entry--debian | jq -r .pid); \
  nsenter -t $DPID -m -n -u -i -p bash -c "cd /root/haos && ./install.sh"'
```

`install.sh` is idempotent — safe to re-run.

## Configure

After first install, edit `/opt/haos/haos.conf` to change:

- `HAOS_RAM_MB`, `HAOS_VCPUS` — guest resources
- `HAOS_MAC` — override the auto-generated MAC (rarely needed)
- `HAOS_PARENT_IF` — must match your LightOS network interface name

Then `systemctl restart haos.service`.

## Verify

```sh
# On the LightOS instance:
systemctl status haos.service
/opt/haos/bin/haos-status.sh
journalctl -u haos.service -f
```

The key deliverable — HAOS visible on the LAN — is verified from
**another LAN device** (not the LightOS host itself; macvlan host
isolation blocks self-discovery):

```sh
# From your laptop on the same LAN:
arp -a | grep '<HAOS MAC, lowercase>'                # MAC appears
ping <HAOS IP>                                       # responds
curl -kI http://<HAOS IP>:8123                       # HTTP 200
avahi-browse -art | grep -i home-assistant           # mDNS announces
```

mDNS visibility is the cardinal test — it proves the macvtap path works
end-to-end and HAOS is a first-class LAN citizen.

## Lifecycle

- **Update**: HAOS updates itself via Settings → System → Updates.
  This repo's `VERSION` file only controls the **initial** image
  downloaded by a fresh install.
- **Backup**: use HAOS's built-in Backup feature; do NOT copy
  `haos_ova.qcow2` while running.
- **Restart**: `systemctl restart haos.service` — sends QMP
  `system_powerdown`, waits up to 90 s, then re-launches.
- **Uninstall**: `./uninstall.sh` (keeps data/config) or
  `--purge` (removes everything) or `--purge-uefi-only` (drops
  corrupted UEFI vars but keeps the qcow2).

## Files

| Location | Purpose |
|---|---|
| `/etc/systemd/system/haos.service` | systemd unit |
| `/opt/haos/haos.conf` | runtime config |
| `/opt/haos/bin/` | the four lifecycle scripts |
| `/opt/haos/data/` (symlink) | persistent qcow2 + UEFI vars |
| `/lzcapp/document/VM/haos/` | symlink target on the LightOS host bind-mount |
| `/run/haos/` | qmp.sock, monitor.sock, serial.sock, tap.ifindex |
| `/var/log/haos/install-*.log` | install transcript |

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `parent interface lzc-debian missing` | LightOS not in macvlan mode | edit `lightos.json`, set `network_mode: macvlan`, restart instance |
| qemu reports `KVM: not available` | nested KVM off on host | check `/sys/module/kvm_intel/parameters/nested` |
| HAOS not visible on LAN | router blocking unfamiliar MAC | check router admin / add MAC to allow-list |
| Web UI loads then 502s | HAOS still booting (first boot ~3 min) | wait; watch `socat - UNIX-CONNECT:/run/haos/serial.sock` |
| qcow2 won't open | torn write or wrong path | restore from `haos_ova-<ver>.qcow2.bak` or rerun install.sh |

## See also

- Design spec: `../docs/superpowers/specs/2026-05-23-haos-on-lightos-design.md`
EOF
```

- [ ] **Step 2: Verify it renders**

```bash
wc -l haos/README.md
head -5 haos/README.md
```

Expected: ~100 lines; first line is `# HAOS on LightOS`.

- [ ] **Step 3: Commit**

```bash
git add haos/README.md
git commit -m "haos: add user-facing README"
```

---

### Task 12: End-to-end integration test on LightOS

This task validates the entire deployment on the real LightOS debian instance. It is destructive (downloads ~555 MB, creates VMs) but reversible via `uninstall.sh --purge`.

**Files:** (none changed — this is purely verification)

- [ ] **Step 1: Push the haos/ tree to LightOS**

Decide the transport based on which works in your environment:

Option A — direct rsync (if root SSH to LightOS is open):

```bash
rsync -a --delete /Users/moon/Workspace.localized/lazycat/lazycat/haos/ \
  root@192.168.50.13:/root/haos/
```

Option B — host-mediated push (recommended if LightOS only takes user SSH):

```bash
rsync -a /Users/moon/Workspace.localized/lazycat/lazycat/haos/ \
  192.168.50.11:/tmp/haos-stage/
ssh 192.168.50.11 'DPID=$(runc --root /lzcsys/run/lightos/.runc \
  state cloud.lazycat.lightos.entry--debian | jq -r .pid); \
  nsenter -t $DPID -m -n -u -i -p bash -c "mkdir -p /root/haos && cp -r /tmp/haos-stage/. /root/haos/"'
```

- [ ] **Step 2: Run install.sh**

```bash
ssh 192.168.50.11 'DPID=$(runc --root /lzcsys/run/lightos/.runc \
  state cloud.lazycat.lightos.entry--debian | jq -r .pid); \
  nsenter -t $DPID -m -n -u -i -p bash -c "cd /root/haos && ./install.sh"'
```

Expected: progresses through 8 numbered sections, downloads HAOS image,
finishes with "install complete". Exit 0.

- [ ] **Step 3: Start service and check status**

```bash
ssh 192.168.50.11 'DPID=$(runc --root /lzcsys/run/lightos/.runc \
  state cloud.lazycat.lightos.entry--debian | jq -r .pid); \
  nsenter -t $DPID -m -n -u -i -p bash -c "
    systemctl start haos.service
    sleep 3
    systemctl status haos.service --no-pager | head -15
    /opt/haos/bin/haos-status.sh
  "'
```

Expected: service `active (running)`; tap interface UP; QMP guest status
`running` (HAOS just booting); HA :8123 likely 000 (not yet reachable;
HAOS first boot takes ~3 min).

- [ ] **Step 4: Wait for HAOS first boot then re-check**

```bash
sleep 180
ssh 192.168.50.11 'DPID=$(runc --root /lzcsys/run/lightos/.runc \
  state cloud.lazycat.lightos.entry--debian | jq -r .pid); \
  nsenter -t $DPID -m -n -u -i -p bash -c "/opt/haos/bin/haos-status.sh"'
```

Expected: guest `running`. HAOS LAN IP may or may not appear in local arp
(macvlan host isolation) — proceed to step 5 to verify from another LAN
device.

- [ ] **Step 5: Verify LAN visibility from a different LAN device**

Find HAOS's MAC (`grep HAOS_MAC /opt/haos/haos.conf` via nsenter), lower-
case it, then on your laptop (different machine on the same LAN):

```bash
# Replace 52:54:00:xx:xx:xx with the actual HAOS MAC
HAOS_MAC=52:54:00:xx:xx:xx
arp -a | grep -i "$HAOS_MAC"
```

Expected: one entry showing the IP the router gave HAOS.

- [ ] **Step 6: HTTP and mDNS reachability**

From the same laptop:

```bash
HAOS_IP=<ip from step 5>
curl -kI "http://$HAOS_IP:8123"            # HTTP/1.1 200 OK after HAOS finishes booting
avahi-browse -art | grep -i home-assistant # — the cardinal mDNS test
```

Expected: HTTP 200; mDNS browse reveals a `Home Assistant` service. **This is
the project's success criterion.**

- [ ] **Step 7: Graceful restart cycle**

```bash
ssh 192.168.50.11 'DPID=$(runc --root /lzcsys/run/lightos/.runc \
  state cloud.lazycat.lightos.entry--debian | jq -r .pid); \
  nsenter -t $DPID -m -n -u -i -p bash -c "
    systemctl restart haos.service
    sleep 5
    systemctl is-active haos.service
  "'
```

Expected: prints `active`. (Internally: ExecStop sends QMP powerdown,
guest takes some seconds, ExecStopPost removes tap, ExecStartPre recreates
tap, ExecStart re-execs qemu.)

- [ ] **Step 8: Persistence across LightOS restart**

Restart the LightOS instance itself via the host:

```bash
ssh 192.168.50.11 'runc --root /lzcsys/run/lightos/.runc \
  kill cloud.lazycat.lightos.entry--debian SIGTERM'
# wait for LazyCat to restart the instance — observable via lightos-core
sleep 30
ssh 192.168.50.11 'DPID=$(runc --root /lzcsys/run/lightos/.runc \
  state cloud.lazycat.lightos.entry--debian | jq -r .pid); \
  nsenter -t $DPID -m -n -u -i -p bash -c "
    systemctl is-active haos.service
    test -f /opt/haos/data/haos_ova.qcow2 && echo data preserved
  "'
```

Expected: `active`; `data preserved`. HAOS auto-starts because
`systemctl enable` was run during install.

- [ ] **Step 9: Document the integration run**

Append a short transcript to the repo so future reruns have a known-good
baseline. Capture the actual HAOS IP, MAC, and any deviations.

```bash
cat >> /Users/moon/Workspace.localized/lazycat/lazycat/haos/README.md <<EOF

## Last verified deployment

- Date: $(date +%Y-%m-%d)
- LightOS: 192.168.50.13 (macvlan)
- HAOS MAC: <fill from /opt/haos/haos.conf>
- HAOS IP: <fill from arp on laptop>
- HAOS version installed: $(cat /Users/moon/Workspace.localized/lazycat/lazycat/haos/VERSION)
EOF
```

- [ ] **Step 10: Commit final notes**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat
git add haos/README.md
git commit -m "haos: record first known-good deployment in README"
```

---

## Plan Self-Review

Spec coverage check:

| Spec section | Implemented by |
|---|---|
| §Architecture | Task 12 verifies end-to-end |
| §File layout (repo) | Task 1 (skeleton), Tasks 2-11 (files) |
| §File layout (LightOS) | Task 8 install.sh, verified Task 12 |
| §haos.conf fields | Task 2 |
| §haos-network.sh contract | Task 3 |
| §haos-launch.sh contract | Task 4 |
| §haos-stop.sh contract | Task 5 |
| §haos.service | Task 7 |
| §install.sh steps 1–9 | Task 8 |
| §uninstall.sh modes | Task 9 |
| §update.sh | Task 10 |
| §Fault matrix | install.sh preflight + journalctl coverage; Task 12 step 8 |
| §Verification checklist | Task 12 steps 3–8 |
| §haos-status.sh | Task 6 |

All spec sections have a corresponding task. No spec gaps.

Type/signature consistency:

- Script paths are stable: `/opt/haos/bin/{haos-network,haos-launch,haos-stop,haos-status}.sh`
- env var names consistent across Tasks 2, 3, 4, 5, 6, 7, 8: HAOS_IMAGE_PATH, HAOS_OVMF_VARS, HAOS_RAM_MB, HAOS_VCPUS, HAOS_PARENT_IF, HAOS_TAP_IF, HAOS_MAC, HAOS_QMP_SOCK
- The `/run/haos/tap.ifindex` contract is written by Task 3 and read by Task 4 ✓
- `@AUTOGEN@` placeholder is written by Task 2 (template) and substituted by Task 8 (install.sh) ✓
- `data` symlink target `/lzcapp/document/VM/haos` is consistent across Tasks 7 (ReadWritePaths), 8 (mkdir + symlink), 9 (uninstall purge) ✓
- HAOS version 17.3 is set in Task 1, referenced by URL in Task 8, can be bumped by Task 10 ✓

No placeholders remain.
