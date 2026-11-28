#!/usr/bin/env bash
set -euo pipefail

target=$1
digest=$2
tags=("${@:3}")
image="ghcr.io/${GITHUB_REPOSITORY_OWNER,,}/$target"
git fetch --quiet origin main
expected=$(scripts/images/source-id.sh "$target")
current=$(scripts/images/source-id.sh "$target" FETCH_HEAD)
if [[ $expected != "$current" ]]; then
    echo "Image inputs changed on main. Skipping superseded release for $target."
    exit 0
fi
for tag in "${tags[@]}"; do
    docker buildx imagetools create --prefer-index=false --tag "$image:$tag" "$image@$digest"
done
