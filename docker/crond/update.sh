#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

echo " --- === Updating alpine === ---"
VERSION=$(skopeo list-tags docker://alpine | jq -r '.Tags
    | map(select(test("^[0-9]+(\\.[0-9]+){1,2}$")))
    | sort_by(split(".") | map(tonumber))
    | last')
sed -e 's|\(alpine:\).*|\1'"$VERSION"'|' -i Dockerfile
echo "Using version: $VERSION"
echo

source "$ENV/lib/bash/fs.sh"
create-temp-file TEMP

echo " --- === Updating busybox === ---"
# shellcheck disable=SC2153
curl -fsSL "https://dl-cdn.alpinelinux.org/alpine/v${VERSION%.*}/main/x86_64/APKINDEX.tar.gz" >"$TEMP"
VERSION=$(tar -xzOf "$TEMP" |
    awk '$1 == "P:busybox-suid" { found = 1 }; found && $1 ~ /^V:/ { print substr($1, 3); exit }')
sed -e 's|\(BUSYBOX_VERSION=\)[^ ]*|\1'"$VERSION"'|' -i Dockerfile
echo "Using version: $VERSION"
echo

echo " --- === Updating curl === ---"
VERSION=$(tar -xzOf "$TEMP" |
    awk '$1 == "P:curl" { found = 1 }; found && $1 ~ /^V:/ { print substr($1, 3); exit }')
sed -e 's|\(CURL_VERSION=\)[^ ]*|\1'"$VERSION"'|' -i Dockerfile
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet Dockerfile; then
        echo " --- === Result === ---"
        git diff Dockerfile
    fi
fi
