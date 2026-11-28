#!/usr/bin/env bash
# Called by the app's update.sh from its own directory.
set -euo pipefail

export LPK_SERVICE=$1
latest=$2
export LPK_IMAGE=$3
version=$(yq -er '.version' package.yml)
image=$(yq -er '.services[strenv(LPK_SERVICE)].image' lzc-manifest.yml)
stable='(0|[1-9][0-9]*)[.](0|[1-9][0-9]*)[.](0|[1-9][0-9]*)'
if ! [[ $latest =~ ^$stable$ && $version =~ ^$stable([.][1-9][0-9]*)?$ ]]; then
    echo "Invalid upstream version '$latest' or LPK version '$version'" >&2
    exit 1
fi
IFS=. read -r major minor patch revision <<< "$version"
base="$major.$minor.$patch"
tracked=("$base")
image_tag=${image##*:}
image_version=${image_tag#v}
if [[ ${image%:*} == "${LPK_IMAGE%:*}" && $image_version =~ ^$stable$ ]]; then
    tracked+=("$image_version")
fi
highest=$(printf '%s\n' "${tracked[@]}" "$latest" | sort -V | tail -1)
if [[ $highest != "$latest" ]]; then
    echo "Keeping $LPK_SERVICE $version ($image is tracked, latest upstream is $latest)"
    exit 0
fi

next=$latest
if [[ $base == "$latest" ]]; then
    if [[ $image == "$LPK_IMAGE" ]]; then
        echo "$LPK_SERVICE $version already uses $image"
        exit 0
    fi
    next="$base.$((${revision:-0} + 1))"
fi
yq -i '.services[strenv(LPK_SERVICE)].image = strenv(LPK_IMAGE)' lzc-manifest.yml
LPK_VERSION="$next" yq -i '.version = strenv(LPK_VERSION)' package.yml
echo "$LPK_SERVICE LPK $version -> $next ($LPK_IMAGE)"
if [[ ${4:-} != -N ]]; then
    git diff -- lzc-manifest.yml package.yml
fi
