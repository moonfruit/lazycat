#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/docker.sh"

echo " --- === Updating Seq === ---"
VERSION=$(find-image-latest-version datalust/seq)
sed -e 's|\(image: datalust/seq:\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

echo " --- === Updating Seq Input GELF === ---"
VERSION=$(find-image-latest-version datalust/seq-input-gelf)
sed -e 's|\(image: datalust/seq-input-gelf:\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet lzc-manifest.yml; then
        echo " --- === Result === ---"
        git diff lzc-manifest.yml
    fi
fi
