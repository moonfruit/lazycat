#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

image=ghcr.io/psf/httpbin
latest=$(skopeo list-tags "docker://$image" | jq -er '
    [.Tags[] | select(test("^(0|[1-9][0-9]*)([.](0|[1-9][0-9]*)){2}$"))]
    | max_by(split(".") | map(tonumber)) // error("No stable HttpBin image tag found")
')
version=$(yq -er '.version' lzc-manifest.yml)
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
if ! printf '%s\n' "$version" "$latest" | sort -V -C; then
    echo "Keeping HttpBin $version (newest available tag is $latest)"
    exit 0
fi
if [[ $version == "$latest" ]]; then
    current=$(yq -er '.services.httpbin.image' lzc-manifest.yml)
    prefix="$image:$version@sha256:"
    if [[ $current != "$prefix"* || ! ${current#"$prefix"} =~ ^[0-9a-f]{64}$ ]]; then
        echo "LPK $version must pin $image:$version by digest" >&2
        exit 1
    fi
    echo "HttpBin $version is already pinned"
    exit 0
fi

manifest=$(mktemp)
trap 'rm -f "$manifest"' EXIT
skopeo inspect --retry-times 3 --raw "docker://$image:$latest" > "$manifest"
jq -e '.schemaVersion == 2' "$manifest" >/dev/null
digest=$(skopeo manifest-digest "$manifest")
[[ $digest =~ ^sha256:[0-9a-f]{64}$ ]]
LPK_VERSION="$latest" HTTPBIN_IMAGE="$image:$latest@$digest" yq -i \
    '.version = strenv(LPK_VERSION) | .services.httpbin.image = strenv(HTTPBIN_IMAGE)' lzc-manifest.yml
echo "HttpBin LPK $version -> $latest ($digest)"
if [[ ${1:-} != -N ]]; then
    git diff -- lzc-manifest.yml
fi
