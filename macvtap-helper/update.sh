#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/docker.sh"

echo " --- === Updating BusyBox === ---"
VERSION=$(find-image-latest-version busybox)
sed -e 's|\(image: busybox:\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

echo " --- === Updating packages === ---"
go -C src get -u github.com/vishvananda/netlink
go -C src mod tidy
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet lzc-manifest.yml src/go.mod src/go.sum; then
        echo " --- === Result === ---"
        git diff lzc-manifest.yml src/go.mod src/go.sum
    fi
fi
