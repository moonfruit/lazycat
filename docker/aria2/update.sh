#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/docker.sh"

echo " --- === Updating alpine === ---"
ALPINE=$(find-image-latest-version alpine)
sed -e 's|\(alpine:\).*|\1'"$ALPINE"'|' -i Dockerfile
echo "Using version: $ALPINE"
echo

echo " --- === Updating aria2 === ---"
VERSION=$(find-alpine-package-version aria2 "${ALPINE%.*}" community)
sed -e 's|\(ARIA2_VERSION=\).*|\1'"$VERSION"'|' -i Dockerfile
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet Dockerfile; then
        echo " --- === Result === ---"
        git diff Dockerfile
    fi
fi
