#!/usr/bin/env bash
set -euo pipefail

image=$1
version=$2
revision=$3
digest=$4
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && $revision =~ ^[1-9][0-9]*$ && $digest =~ ^sha256:[a-f0-9]{64}$ ]]
immutable="$version-r$revision"

# A rerun or a partially promoted revision may have changed GHCR since comparison.
tags=$(skopeo list-tags --retry-times 3 "docker://$image")
newest_revision=$(jq -er --arg prefix "$version-r" '
    [.Tags[] | select(startswith($prefix)) | ltrimstr($prefix) |
     select(test("^[1-9][0-9]*$")) | tonumber] | max // 0
' <<< "$tags")
if (( newest_revision > revision )); then
    echo "$version-r$newest_revision has already been published. Skipping $immutable."
    exit 0
fi

wanted=("$immutable" "$version")
# Both the moving version tag and an immutable revision can establish the newest upstream.
newest_version=$(jq -r '.Tags[] | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+(-r[1-9][0-9]*)?$")) |
    split("-r")[0]' <<< "$tags" | sort -V | tail -1)
if [[ $(printf '%s\n' "$version" "$newest_version" | sort -V | tail -1) == "$version" ]]; then
    wanted+=(latest)
fi

pending=()
for tag in "${wanted[@]}"; do
    if jq -e --arg tag "$tag" '.Tags | index($tag) != null' <<< "$tags" >/dev/null; then
        current=$(skopeo inspect --retry-times 3 --no-tags --format '{{.Digest}}' "docker://$image:$tag")
        if [[ $current == "$digest" ]]; then continue; fi
        if [[ $tag == "$immutable" ]]; then
            echo "Revision $immutable already points to $current" >&2
            exit 1
        fi
    fi
    pending+=("$tag")
done
if (( ${#pending[@]} )); then
    scripts/images/promote.sh qemu "$digest" "${pending[@]}"
else
    echo "All applicable tags already point to $digest."
fi
