#!/usr/bin/env bash
set -euo pipefail

manifest=$1
platform=$2
version=$3
lpk=$4
[[ $platform == linux/amd64 ]]
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/content"

# The release is built from a checkout which can contain old disks, local
# configuration and legacy deployment notes. Check both archive boundaries.
python3 - "$lpk" "$work" <<'PY'
import sys
import tarfile
from pathlib import Path

package, output = sys.argv[1], Path(sys.argv[2])
outer = {"compose.override.yml", "content.tar", "deploy_params.yml", "icon.png", "manifest.yml", "package.yml"}
payload = {"haos-vm", "hass-oidc-auth.LICENSE.md", "hass-oidc-auth.zip", "lan-http.nft", "prepare.sh"}

def check(archive, expected):
    members = [m for m in archive if not (m.name == "." and m.isdir())]
    if sorted(m.name for m in members) != sorted(expected) or any(not m.isfile() for m in members):
        sys.exit(f"Unexpected LPK members: {[m.name for m in members]}")

with tarfile.open(package) as archive:
    check(archive, outer)
    for name in ("package.yml", "manifest.yml", "compose.override.yml", "deploy_params.yml"):
        (output / name).write_bytes(archive.extractfile(name).read())
    with tarfile.open(fileobj=archive.extractfile("content.tar")) as content:
        check(content, payload)
        for name in payload:
            destination = output / "content" / name
            destination.write_bytes(content.extractfile(name).read())
            destination.chmod(content.getmember(name).mode & 0o777)
PY

cmp "$manifest" "$work/manifest.yml"
[[ $(yq -er .version "$work/package.yml") == "$version" ]]
[[ $(yq -er .application.kvm_accel "$manifest") == true ]]
[[ $(yq -er '.application.upstreams[] | select(.location == "/") | .backend' "$manifest") == http://qemu:8123/ ]]
[[ $(yq -er .services.qemu.command "$manifest") == /lzcapp/pkg/content/haos-vm ]]
[[ $(yq -er .networks.haoslan.driver_opts.parent "$work/compose.override.yml") == enp2s0 ]]
yq -e '.params[] | select(.id == "Owner" and .type == "lzc_uid" and .default_value == null)' "$work/deploy_params.yml" >/dev/null
grep -Fq '"HAOS_OWNER=%s" .U.Owner' "$manifest"
grep -Fq "LAZYCAT_AUTH_OIDC_CLIENT_SECRET=\${LAZYCAT_AUTH_OIDC_CLIENT_SECRET}" "$manifest"

content="$work/content"
file "$content/haos-vm" | grep -E 'ELF 64-bit LSB executable, x86-64,.*statically linked'
cmp haos/src/internal/vm/guest/prepare.sh "$content/prepare.sh"
cmp haos/src/internal/vm/guest/lan-http.nft "$content/lan-http.nft"
cmp haos/hass-oidc-auth.LICENSE.md "$content/hass-oidc-auth.LICENSE.md"
read -r plugin digest < haos/hass-oidc-auth.lock
printf '%s  %s\n' "$digest" "$content/hass-oidc-auth.zip" | shasum -a 256 --check --quiet
unzip -p "$content/hass-oidc-auth.zip" manifest.json |
    jq -e --arg version "$plugin" '.domain == "auth_oidc" and .version == $version' >/dev/null

image=$(yq -er .services.qemu.image "$manifest")
[[ $image =~ ^ghcr\.io/moonfruit/qemu:([0-9]+\.[0-9]+\.[0-9]+)-r[1-9][0-9]*$ ]]
qemu_version=${BASH_REMATCH[1]}
docker run --rm -i --pull=always --platform "$platform" --network none \
    -v "$content:/lzcapp/pkg/content:ro" "$image" bash -seuo pipefail -- "$qemu_version" <<'BASH'
vm=/lzcapp/pkg/content/haos-vm
"$vm" vm --help >/dev/null
mkdir -p /mnt/data/supervisor/homeassistant
"$vm" oidc --recover
qemu-system-x86_64 --version | head -1 | grep -F "QEMU emulator version $1 "
qemu-system-x86_64 -netdev help | grep -Fx vde
test -s /usr/share/OVMF/OVMF_CODE_4M.fd
cp /usr/share/OVMF/OVMF_VARS_4M.fd /tmp/vars.fd
qemu-img create -f qcow2 /tmp/test.qcow2 16M
qemu-img check -f qcow2 /tmp/test.qcow2
printf '%s\n' '{"execute":"qmp_capabilities"}' '{"execute":"cont"}' '{"execute":"query-status"}' '{"execute":"quit"}' |
    timeout 30 qemu-system-x86_64 -machine q35,accel=tcg -S -m 128 -display none -nodefaults \
        -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
        -drive if=pflash,format=raw,file=/tmp/vars.fd \
        -drive if=virtio,format=qcow2,file=/tmp/test.qcow2 -qmp stdio >/tmp/qmp.json
grep -E '"status" *: *"running"' /tmp/qmp.json
BASH
echo "HAOS LPK $version on $platform: verified packaged companion, auth_oidc $plugin and QEMU $qemu_version"
