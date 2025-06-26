#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/github.sh"

echo " --- === Updating Caddy === ---"
VERSION=$(find-latest-version caddyserver caddy)
sed -e 's|\(image: caddy:\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    echo " --- === Result === ---"
    git diff lzc-manifest.yml
fi
