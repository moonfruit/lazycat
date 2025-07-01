#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

echo " --- === Updating alpine === ---"
VERSION=$(skopeo list-tags docker://alpine | jq -r '.Tags
    | map(select(test("^[0-9]+(\\.[0-9]+){1,2}$")))
    | sort_by(split(".") | map(tonumber))
    | last')
sed -e 's|\(alpine:\).*|\1'"$VERSION"'|' -i docker/Dockerfile
echo "Using version: $VERSION"
echo

echo " --- === Updating aria2 === ---"
VERSION=$(curl -fsSL "https://dl-cdn.alpinelinux.org/alpine/v${VERSION%.*}/community/x86_64/APKINDEX.tar.gz" 2>/dev/null |
    tar -xzO |
    awk '$1 == "P:aria2" { found = 1 }; found && $1 ~ /^V:/ { print substr($1, 3); exit }')
sed -e 's|\(VERSION=\).*|\1'"$VERSION"'|' -i docker/Dockerfile
echo "Using version: $VERSION"
echo

source "$ENV/lib/bash/github.sh"

echo " --- === Updating ariang === ---"
download-latest-release dist/ariang mayswind AriaNg -AllInOne.zip
echo "Using url: $(cat dist/ariang.url)"
echo

# FIXME: Remove this when 1.3.8
echo " --- === Updating Caddy === ---"
VERSION=$(find-latest-version caddyserver caddy)
sed -e 's|\(image: caddy:\).*|\1'"$VERSION"'|' -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet; then
        echo " --- === Result === ---"
        if git diff --quiet lzc-manifest.yml; then
            echo "No changes in lzc-manifest.yml"
        else
            git diff lzc-manifest.yml
        fi
        git diff
    fi
fi
