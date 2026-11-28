#!/usr/bin/env bash
set -euo pipefail

image=$1
version=$2
docker run --rm --platform linux/amd64 --network none "$image" sh -ec '
    system=$(qemu-system-x86_64 --version)
    img=$(qemu-img --version)
    echo "$system" >&2
    echo "$img" >&2
    test "$(echo "$system" | awk "NR == 1 {print \$4}")" = "$1"
    test "$(echo "$img" | awk "NR == 1 {print \$3}")" = "$1"
    qemu-img create -q -f qcow2 /tmp/smoke.qcow2 1M
    qemu-img check -q /tmp/smoke.qcow2
' sh "$version"
# TCG is available on hosted runners without /dev/kvm. Verify that the machine answers QMP.
docker run --rm -i --platform linux/amd64 --network none "$image" \
    timeout 30 qemu-system-x86_64 -machine q35,accel=tcg -m 64M -S -nodefaults -display none -qmp stdio <<'QMP' |
{"execute":"qmp_capabilities"}
{"execute":"query-status","id":"status"}
{"execute":"quit"}
QMP
    jq -es 'any(.[]; .id == "status" and .return.status == "prelaunch")' >/dev/null
