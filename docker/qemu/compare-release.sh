#!/usr/bin/env bash
# Called by CI after a fresh build has exported candidate.oci and candidate.tar.
set -euo pipefail
image=$1
version=$2
dir=$3
here=$(cd "$(dirname "$0")" && pwd)
tags=$(skopeo list-tags --retry-times 3 "docker://$image")
revision=$(jq -er --arg prefix "$version-r" '
    [.Tags[] | select(startswith($prefix)) | ltrimstr($prefix) |
     select(test("^[1-9][0-9]*$")) | tonumber] | max // 0
' <<< "$tags")

skopeo inspect --config "oci-archive:$dir/candidate.oci" > "$dir/candidate.json"
candidate=$(python3 "$here/fingerprint.py" "$dir/candidate.tar" "$dir/candidate.json")
echo "Candidate content $candidate"
changed=true
if (( revision > 0 )); then
    previous="$image:$version-r$revision"
    digest=$(skopeo inspect --no-tags --format '{{.Digest}}' "docker://$previous")
    # Export both filesystems with BuildKit, including files normally hidden by runtime mounts.
    printf 'FROM %s@%s\n' "$image" "$digest" | docker buildx build --platform linux/amd64 \
        --output "type=tar,dest=$dir/previous.tar" -
    skopeo inspect --config "docker://$image@$digest" > "$dir/previous.json"
    published=$(python3 "$here/fingerprint.py" "$dir/previous.tar" "$dir/previous.json")
    echo "$previous content $published"
    if [[ $candidate == "$published" ]]; then
        changed=false
    fi
fi

if [[ $changed == true ]]; then
    revision=$((revision + 1))
    digest=$(skopeo inspect --format '{{.Digest}}' "oci-archive:$dir/candidate.oci")
fi
{
    echo "changed=$changed"
    echo "digest=$digest"
    echo "revision=$revision"
} >> "$GITHUB_OUTPUT"
echo "QEMU $version-r$revision, changed=$changed, digest=$digest"
