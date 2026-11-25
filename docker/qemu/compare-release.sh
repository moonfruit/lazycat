#!/usr/bin/env bash
# Called by CI after a fresh build has exported candidate.oci and candidate.tar.
set -euo pipefail
image=$1
version=$2
dir=$3
here=$(cd "$(dirname "$0")" && pwd)
tags=$(skopeo list-tags "docker://$image")
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
# Check aliases even on a no-op so a partially completed release can be repaired.
publish_tags=()
for tag in "$version-r$revision" "$version" latest; do
    if jq -e --arg t "$tag" '.Tags | index($t) != null' <<< "$tags" >/dev/null; then
        current=$(skopeo inspect --no-tags --format '{{.Digest}}' "docker://$image:$tag")
        if [[ $current == "$digest" ]]; then continue; fi
        if [[ $tag == "$version-r$revision" ]]; then
            echo "Revision $tag already points to another image" >&2
            exit 1
        fi
    fi
    publish_tags+=("$tag")
done
{
    echo "changed=$changed"
    echo "digest=$digest"
    echo "tags=${publish_tags[*]}"
} >> "$GITHUB_OUTPUT"
echo "QEMU $version-r$revision, changed=$changed, tags to publish: ${publish_tags[*]:-none}"
