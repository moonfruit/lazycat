#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

upstream=ghcr.io/psf/httpbin
latest=$(skopeo list-tags "docker://$upstream" | jq -er '
    [.Tags[] | select(test("^(0|[1-9][0-9]*)([.](0|[1-9][0-9]*)){2}$"))]
    | max_by(split(".") | map(tonumber)) // error("No stable HttpBin image tag found")
')
version=$(yq -er '.version' lzc-manifest.yml)
[[ $version =~ ^([0-9]+\.[0-9]+\.[0-9]+)(\.([1-9][0-9]*))?$ ]]
base=${BASH_REMATCH[1]}
revision=${BASH_REMATCH[3]:-0}
if ! printf '%s\n' "$base" "$latest" | sort -V -C; then
    echo "Keeping HttpBin $version (newest available tag is $latest)"
    exit 0
fi

image="$upstream:$latest"
if [[ -f ../docker/httpbin/$latest/Dockerfile ]]; then
    image="ghcr.io/moonfruit/httpbin:$latest"
fi
next=$latest
if [[ $base == "$latest" ]]; then
    if [[ $(yq -er '.services.httpbin.image' lzc-manifest.yml) == "$image" ]]; then
        echo "HttpBin $version already uses $image"
        exit 0
    fi
    next="$base.$((revision + 1))"
fi
LPK_VERSION="$next" HTTPBIN_IMAGE="$image" yq -i \
    '.version = strenv(LPK_VERSION) | .services.httpbin.image = strenv(HTTPBIN_IMAGE)' lzc-manifest.yml
echo "HttpBin LPK $version -> $next ($image)"
if [[ ${1:-} != -N ]]; then
    git diff -- lzc-manifest.yml
fi
