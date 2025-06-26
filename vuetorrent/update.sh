#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/github.sh"

echo " --- === Updating VueTorrent-backend === ---"
VERSION=$(find-latest-version VueTorrent vuetorrent-backend)
sed -e 's/^version:.*/version: '"$VERSION"'/;' \
    -e 's|\(image: ghcr.io/vuetorrent/vuetorrent-backend:\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

echo " --- === Updating qBittorrent === ---"
VERSION=$(find-latest-version linuxserver docker-qbittorrent)
sed -e 's|\(image: lscr.io/linuxserver/qbittorrent:\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    echo " --- === Result === ---"
    git diff lzc-manifest.yml
fi
