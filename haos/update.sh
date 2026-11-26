#!/usr/bin/env bash
set -euo pipefail
if [[ -z ${PROXY_ENABLED:-} ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

cd "$(dirname "$0")"

# HAOS sometimes publishes releases for just a subset of boards.
version=$(curl -fsSL https://version.home-assistant.io/stable.json |
    jq -er '.hassos.ova | select(test("^[0-9]+(\\.[0-9]+)+$"))')
archive="haos_ova-$version.qcow2.xz"
digest=$(gh api "repos/home-assistant/operating-system/releases/tags/$version" |
    jq -er --arg name "$archive" '
        first(.assets[] | select(.name == $name) | .digest | strings
              | select(test("^sha256:[0-9a-f]{64}$")) | ltrimstr("sha256:"))
        // error("missing SHA-256 for " + $name)
    ')

qemu=$(skopeo list-tags docker://ghcr.io/moonfruit/qemu |
    jq -er '.Tags[] | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+-r[1-9][0-9]*$"))' |
    sort -V | tail -n 1)

storage=src/cmd/haos-vm/storage.go
sed -E -i \
    -e "s|^const imageVersion = .*$|const imageVersion = \"$version\"|" \
    -e "s|^([[:space:]]+digest:[[:space:]]*)\"[0-9a-f]{64}\"|\\1\"$digest\"|" "$storage"
sed -E -i "s|^(    image: ghcr.io/moonfruit/qemu:).*|\\1$qemu|" lzc-manifest.yml
sed -E -i "s|haos_ova-[0-9.]+\\.qcow2\\.xz|$archive|g" lzc-deploy-params.yml
# The acceptance records later in the README describe specific builds.
sed -E -i "/^### 预先下载 HAOS 镜像/,/^## /{
    s|haos_ova-[0-9.]+\\.qcow2\\.xz|$archive|g
    s|(operating-system/releases/download/)[0-9.]+/|\\1$version/|g
    s|[0-9a-f]{64}|$digest|g
}" README.md

echo "HAOS OVA $version ($digest), QEMU $qemu"
if [[ ${1:-} != -N ]]; then
    git diff -- "$storage" lzc-manifest.yml lzc-deploy-params.yml README.md
fi
