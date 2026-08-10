#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/docker.sh"

echo " --- === Updating WatchYourLAN === ---"
# 查镜像 registry 的最新 tag。find-image-latest-version 只取形如 v?N(.N)+ 的 tag，
# 因此 latest / dev / v2 这三个非语义 tag 会被自动滤掉。
VERSION=$(find-image-latest-version aceberg/watchyourlan)
# 版本号分散在两个文件：package.yml 存元信息，lzc-manifest.yml 存镜像 tag。
sed -e 's/^version:.*/version: '"$VERSION"'/' -i package.yml
sed -e 's|\(image: aceberg/watchyourlan:\).*|\1'"$VERSION"'|' -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet package.yml lzc-manifest.yml; then
        echo " --- === Result === ---"
        git diff package.yml lzc-manifest.yml
    fi
fi
