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
