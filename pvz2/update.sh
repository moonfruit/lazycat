#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/docker.sh"

echo " --- === Updating PvZ2 Gardendless === ---"
VERSION=$(find-image-latest-version gaozih/pvzge)
sed -e 's|\(image: gaozih/pvzge:\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet lzc-manifest.yml; then
        echo " --- === Result === ---"
        git diff lzc-manifest.yml
    fi
fi
