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

source "$ENV/lib/bash/fs.sh"
create-temp-file TEMP

find-package-version() {
    # shellcheck disable=SC2153
    if [[ ! -s "$TEMP" ]]; then
        curl -fSL -o "$TEMP" \
            "https://dl-cdn.alpinelinux.org/alpine/v${ALPINE%.*}/community/x86_64/APKINDEX.tar.gz"
    fi
    tar -xzOf "$TEMP" |
        awk '$1 == "P:'"$1"'" { found = 1 }; found && $1 ~ /^V:/ { print substr($1, 3); exit }'
}

echo " --- === Updating aria2 === ---"
VERSION=$(find-package-version aria2)
sed -e 's|\(ARIA2_VERSION=\).*|\1'"$VERSION"'|' -i Dockerfile
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet Dockerfile; then
        echo " --- === Result === ---"
        git diff Dockerfile
    fi
fi
