#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

image=kennethreitz/httpbin
manifest=$(mktemp)
trap 'rm -f "$manifest"' EXIT
skopeo inspect --retry-times 3 --raw "docker://docker.io/$image:latest" > "$manifest"
jq -e '.schemaVersion == 2' "$manifest" >/dev/null
digest=$(skopeo manifest-digest "$manifest")
[[ $digest =~ ^sha256:[0-9a-f]{64}$ ]]
current=$(yq -r '.services.httpbin.image' lzc-manifest.yml)
if [[ $current == "$image@$digest" ]]; then
    echo "HttpBin image unchanged ($digest)"
    exit 0
fi
version=$(yq -r '.version' lzc-manifest.yml)
[[ $version =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]
next="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((10#${BASH_REMATCH[3]} + 1))"
LPK_VERSION="$next" HTTPBIN_IMAGE="$image@$digest" yq -i \
    '.version = strenv(LPK_VERSION) | .services.httpbin.image = strenv(HTTPBIN_IMAGE)' lzc-manifest.yml
echo "HttpBin LPK $version -> $next ($digest)"
if [[ ${1:-} != -N ]]; then
    git diff -- lzc-manifest.yml
fi
