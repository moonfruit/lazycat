#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/docker.sh"

IMAGE=gaozih/pvzge
REPO=Gzh0821/pvzge_web

echo " --- === Updating PvZ2 Gardendless === ---"
# 上游从 0.12.1 之后就不再打版本 tag 了（git tag 和 Docker Hub tag 都停在 v0.12.1），
# 新版本只是重推 latest，所以 find-image-latest-version 只能取到过期的 v0.12.1。
# 真正的版本号写在上游 docs/index.html 的标题里：
#   <title>PvZ2 Gardendless Online | 0.14.0</title>
VERSION=$(gh api "repos/$REPO/contents/docs/index.html" \
    -H "Accept: application/vnd.github.raw" |
    rg -o -r '$1' '<title>[^<]*\|\s*([0-9][0-9.]*)\s*</title>')
if [[ -z $VERSION ]]; then
    echo "Cannot detect version from $REPO" >&2
    exit 1
fi

# 上游哪天恢复打 tag 就直接用 tag；否则把 latest 解析成 digest 钉死，保证可复现。
if skopeo list-tags "docker://$IMAGE" |
    jq -e --arg t "v${VERSION}" 'any(.Tags[]; . == $t)' >/dev/null; then
    REF=":v${VERSION}"
else
    # --override-* 是为了让 macOS 上的 skopeo 不因镜像没有 darwin/arm64 而报错；
    # 取到的仍然是 tag 指向的 manifest index digest，多架构通用。
    REF="@$(skopeo inspect --override-os linux --override-arch amd64 \
        --format '{{.Digest}}' "docker://${IMAGE}:latest")"
    echo "No tag v${VERSION} on ${IMAGE}, pinning latest by digest"
fi

# 版本号分散在两个文件：package.yml 存元信息，lzc-manifest.yml 存镜像引用。
# 镜像引用可能是 :tag 也可能是 @digest，所以 sed 锚点要同时吃掉 : 和 @。
sed -e 's/^version:.*/version: '"$VERSION"'/' -i package.yml
sed -e 's|\(image: '"$IMAGE"'\)[:@].*|\1'"$REF"'|' -i lzc-manifest.yml
echo "Using version: ${VERSION} (${IMAGE}${REF})"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet package.yml lzc-manifest.yml; then
        echo " --- === Result === ---"
        git diff package.yml lzc-manifest.yml
    fi
fi
