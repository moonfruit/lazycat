#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/docker.sh"

echo " --- === Updating Tailscale === ---"
# 查 Docker 镜像 registry 的最新 tag，而非 GitHub release——镜像 tag 常滞后于 GitHub 发版。
VERSION=$(find-image-latest-version tailscale/tailscale)
VERSION=${VERSION#v} # 镜像 tag 形如 v1.98.4；version: 字段用裸版本号，image 行用 :v 前缀
sed -e 's/^version:.*/version: '"$VERSION"'/;' \
    -e 's|\(image: tailscale/tailscale:v\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

echo " --- === Updating Go deps === ---"
go -C src get -u ./...
go -C src mod tidy
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet -- lzc-manifest.yml src; then
        echo " --- === Result === ---"
        git diff -- lzc-manifest.yml src
    fi
fi
