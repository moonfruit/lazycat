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

LazyCat exposes each LightOS instance via mDNS at `<instance-name>.<owner>.heiyu.space`
(the懒猫 NSS shim resolves these — `ping` may not see them but `ssh` does).
Sync the haos/ tree to the instance and run installer as moon via sudo:

```sh
# From this directory on your dev machine:
rsync -a --delete ./ moon@<instance>.<owner>.heiyu.space:~/haos/

# Then on the LightOS instance:
ssh moon@<instance>.<owner>.heiyu.space
sudo ~/haos/install.sh
sudo systemctl start haos.service
```

Example for the canonical setup in this repo:

```sh
rsync -a --delete ./ moon@debian.dkmooncat.heiyu.space:~/haos/
ssh moon@debian.dkmooncat.heiyu.space 'sudo ~/haos/install.sh && sudo systemctl start haos.service'
```

`install.sh` is idempotent — safe to re-run.

## macvtap-helper & Crash-loop Hardening

### macvtap-helper (recommended)

Install the companion LazyCat application `macvtap-helper` (lives in `../macvtap-helper/`
in this repo) **on the LazyCat box** (not inside LightOS):

```sh
cd ../macvtap-helper && make install
```

`macvtap-helper` runs on the LazyCat host on every boot and:

1. Loads the `macvtap` kernel module before HAOS starts — the module is absent at
   the moment LazyCat creates devices during a cold reboot, so HAOS's first startup
   attempt (triggered by instance auto-start) would otherwise fail with a device
   cgroup whitelist error.
2. Automatically restarts HAOS once the module is loaded.

**Instance auto-start strategy**: keep the debian/lightos instance auto-start **ON**
(the default). After a cold reboot the sequence is: instance starts → HAOS first
attempt fails (macvtap not yet present) → `macvtap-helper` loads module and restarts
the service → HAOS comes up normally. Turning auto-start off would prevent HAOS from
starting at all without manual intervention.

### StartLimit hardening

`haos.service` declares `StartLimitIntervalSec=600` / `StartLimitBurst=5` in its
`[Unit]` section (systemd requires StartLimit* in `[Unit]`, not `[Service]`). This
prevents a crash-storm: without a wide accounting window, rapid restart failures
during the cold-boot macvtap-absent window cause systemd to permanently deactivate
the unit before `macvtap-helper` gets a chance to intervene. With a 600 s window the
unit stays restartable long enough for the helper to recover it.

`haos.service` is also **enabled** (`systemctl enable`) by `install.sh` so it
persists across instance reboots automatically.

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

## Optional: expose HAOS via the Lazycat hostname

Without any extra setup HAOS lives on `http://<HAOS LAN IP>:8123`, which means
your phone/laptop must be on the same LAN to reach it. If this LightOS instance
is registered in the Lazycat network and reachable as
`<instance>.<owner>.heiyu.space`, you can route HAOS through it:

```sh
sudo ~/haos/setup-proxy.sh
```

The script installs `nginx-light` and writes a single proxy site on `:80`
forwarding to HAOS (auto-discovered from `haos.conf` + ARP, override via
`HAOS_PROXY_UPSTREAM=<ip>`). WebSocket upgrade is enabled — HA's live UI works.

Then in any browser **inside the Lazycat network** (the laptop also needs
hclient-cli running):

```
https://<instance>.<owner>.heiyu.space/
```

The Lazycat platform gateway intercepts that hostname, requires a login to
your Lazycat account, then transparently forwards the request to this
LightOS instance's port 80 — where nginx reverse-proxies to HAOS:8123.

**Caveats**:

- The default nginx config does **not** send `X-Forwarded-*` headers — HA
  rejects them unless the proxy IP is listed in `http.trusted_proxies`. HA
  will see every request as coming from this LightOS's LAN IP. To get the
  real client IP visible inside HA, configure trusted_proxies in HA's
  `/config/configuration.yaml`, then uncomment the X-Forwarded-* lines in
  `/etc/nginx/conf.d/haos-proxy.conf` and `systemctl reload nginx`.
- The hostname path goes through the Lazycat account-login gateway. Direct
  LAN access via `http://<HAOS LAN IP>:8123` is still the no-auth fast path.

## Optional: share a directory to HAOS via NFS

HAOS 13+ has built-in network storage support. To let HAOS mount a directory
from this LightOS instance (e.g. for backups or shared media), run on LightOS:

```sh
sudo ~/haos/setup-nfs.sh
```

The script installs `nfs-kernel-server`, creates `/opt/haos/share`,
exports it to `192.168.50.0/24` (NFSv4, rw, no_root_squash), and enables
the systemd unit.

Then in HAOS Web UI: **Settings → System → Storage → Add Network Storage**
(pick NFS, point at `<lightos-ip>:/opt/haos/share`).

**Why /opt/haos/share** (not under /lzcapp/document/): `/lzcapp/document/`
is an idmapped bind-mount and the Linux kernel's in-kernel nfsd refuses to
export idmapped mounts. /opt/haos/share lives in the LightOS rootfs btrfs
subvol — survives service restarts, but rebuilding the LightOS instance
drops it. Back up via host-side `btrfs subvolume snapshot` if needed.

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
| `/opt/haos/data/` | persistent qcow2 + UEFI vars (real directory) |
| `/opt/haos/log/install-*.log` | install transcripts |
| `/run/haos/` | qmp.sock, monitor.sock, serial.sock, tap.ifindex |

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

## Last verified deployment

- **Date**: 2026-05-23
- **Host OS**: Debian 12, kernel 6.5, machine lzcbox-052d6a70 at 192.168.50.11
- **LightOS instance OS**: Debian 13 (trixie), instance `cloud.lazycat.lightos.entry--debian`, macvlan 192.168.50.13
- **HAOS version**: 17.3
- **HAOS MAC**: 52:54:00:c7:22:46 (auto-generated from md5(hostname))
- **HAOS IP**: 192.168.50.15 (DHCP — for stable IP, bind HAOS_MAC on your router)
- **Verified**: HTTP 200 on :8123 from another LAN host; `haos-status.sh` all-green
  (service active, guest running, tap UP, arp-scan finds HAOS, :8123 reachable)
