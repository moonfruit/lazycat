#!/usr/bin/env bash
set -euo pipefail
if [[ -z ${PROXY_ENABLED:-} ]] && command -v proxy >/dev/null; then
    exec proxy "$0" "$@"
fi
cd "$(dirname "$0")"

upstream=linzeyi/hivision_idphotos
tag=$(skopeo list-tags "docker://docker.io/$upstream" | jq -er '
    [.Tags[] | select(test("^v(0|[1-9][0-9]*)([.](0|[1-9][0-9]*)){2}$"))]
    | max_by(ltrimstr("v") | split(".") | map(tonumber)) // error("No stable Hivision image tag found")
')
export LPK_VERSION=${tag#v}
export LPK_IMAGE="$upstream:$tag"

web_image=$(yq -er '.services.hivision.image' lzc-manifest.yml)
api_image=$(yq -er '.services.hivision_api.image' lzc-manifest.yml)
version=$(yq -er '.version' package.yml)
if [[ $(printf '%s\n' "$version" "$LPK_VERSION" | sort -V | tail -1) != "$LPK_VERSION" ]]; then
    echo "Latest stable image $tag is older than the current LPK $version" >&2
    exit 1
fi
if [[ $web_image != "$LPK_IMAGE" || $api_image != "$LPK_IMAGE" ]]; then
    yq -i '.services.hivision.image = strenv(LPK_IMAGE) | .services.hivision_api.image = strenv(LPK_IMAGE)' lzc-manifest.yml
fi
if [[ $version != "$LPK_VERSION" ]]; then
    yq -i '.version = strenv(LPK_VERSION)' package.yml
fi
echo "HivisionIDPhotos $LPK_VERSION ($LPK_IMAGE)"
if [[ ${1:-} != -N ]]; then
    git diff -- lzc-manifest.yml package.yml
fi
