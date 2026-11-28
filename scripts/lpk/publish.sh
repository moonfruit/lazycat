#!/usr/bin/env bash
set -euo pipefail

die() { echo "$*" >&2; exit 1; }
[[ $# == 2 ]] || die "Usage: $0 SLUG TESTED_V2_LPK"
slug=$1
[[ $slug =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]] || die "Invalid LPK slug: $slug"
lpk=$(realpath "$2")
cd "$(git rev-parse --show-toplevel)"
[[ $RELEASE_SHA =~ ^[0-9a-f]{40}$ && $(git rev-parse HEAD) == "$RELEASE_SHA" ]] ||
    die "Check out the exact RELEASE_SHA before publishing"
package=$(git show "$RELEASE_SHA:.github/release-targets.json" |
    jq -er --arg slug "$slug" '.lpk[$slug].package | select(type == "string" and length > 0)')

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
python3 - "$lpk" > "$out/package.yml" <<'PY'
import sys
import tarfile

with tarfile.open(sys.argv[1]) as archive:
    for name in ("manifest.yml", "package.yml"):
        if not archive.getmember(name).isfile():
            sys.exit(f"Missing regular {name} in V2 LPK")
    sys.stdout.buffer.write(archive.extractfile("package.yml").read())
PY

metadata() {
    yq -o=json '.' - | jq -ce '{packageId: .package, name, summary: (.description // ""), version}'
}
expected=$(git show "$RELEASE_SHA:$slug/package.yml" | metadata)
packed=$(metadata < "$out/package.yml")
[[ $expected == "$packed" ]] || die "$slug packaged metadata differs from $RELEASE_SHA"
jq -e --arg pkg "$package" '.packageId == $pkg and (.name | type == "string" and length > 0)
    and (.summary | type == "string") and (.version | type == "string")' <<< "$packed" >/dev/null ||
    die "Invalid packaged metadata for $slug"
version=$(jq -r '.version' <<< "$packed")
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+(\.[1-9][0-9]*)?$ ]] || die "Invalid LPK version: $version"
tag="lpk-$slug-v$version"
asset="$slug-$version.lpk"
cp "$lpk" "$out/$asset"
digest=$(sha256sum "$out/$asset" | cut -d' ' -f1)
size=$(wc -c < "$out/$asset")
jq --arg digest "$digest" --argjson size "$size" '. + {sha256: $digest, size: $size}' \
    <<< "$packed" > "$out/release.json"

# A pre-existing tag takes precedence over --target, even when it has no Release yet.
same_inputs() {
    git diff --quiet "$1" "$RELEASE_SHA" -- "$slug/" ":(exclude)$slug/README.md" \
        ":(exclude)$slug/update.sh" ":(exclude)$slug/Makefile" ":(exclude)$slug/LICENSE" ||
        die "$tag already refers to different package inputs. Increase the LPK version."
}
if git show-ref --verify --quiet "refs/tags/$tag"; then
    same_inputs "refs/tags/$tag"
fi

# Include drafts; a failed API request must never be interpreted as a missing Release.
releases=$(gh api --paginate --slurp "repos/$GITHUB_REPOSITORY/releases?per_page=100")
existing=$(jq -c --arg tag "$tag" 'add[] | select(.tag_name == $tag)' <<< "$releases")
if [[ -z $existing ]]; then
    name=$(jq -r '.name' <<< "$packed")
    gh release create "$tag" -R "$GITHUB_REPOSITORY" --draft --target "$RELEASE_SHA" \
        --title "$name $version" --notes "LazyCat LPK · $package · $version"
else
    [[ $(jq -r '.prerelease' <<< "$existing") == false ]] || die "$tag is marked as a prerelease"
    if [[ $(jq -r '.draft' <<< "$existing") == false ]]; then
        git show-ref --verify --quiet "refs/tags/$tag" || die "Fetch the published tag $tag to verify its inputs"
        gh release download "$tag" -R "$GITHUB_REPOSITORY" -p release.json -O "$out/published.json"
        jq -e --argjson metadata "$packed" --argjson release "$existing" --arg asset "$asset" '
            . as $record | ($record | del(.size, .sha256)) == $metadata
            and any($release.assets[]; .name == $asset and .state == "uploaded"
                and .size > 0 and .size == $record.size and .digest == ("sha256:" + $record.sha256))
        ' "$out/published.json" >/dev/null || die "$tag assets do not match its release metadata"
        echo "$tag already published from matching package inputs"
        exit 0
    fi
    target=$(jq -r '.target_commitish' <<< "$existing")
    [[ $target =~ ^[0-9a-f]{40}$ ]] || die "$tag draft must target an exact commit"
    same_inputs "$target"
fi

# Rebuilds of the same inputs can differ byte-for-byte. Replace a conflicting draft asset
# by its original ID, checking that it still belongs to this unpublished draft first.
for filename in "$asset" release.json; do
    saved=$(jq -c --arg file "$filename" '.assets[]? | select(.name == $file)' <<< "$existing")
    if [[ -n $saved ]]; then
        hash=$(sha256sum "$out/$filename" | cut -d' ' -f1)
        bytes=$(wc -c < "$out/$filename")
        if jq -e --arg digest "sha256:$hash" --argjson size "$bytes" \
            '.state == "uploaded" and .digest == $digest and .size == $size' <<< "$saved" >/dev/null; then
            continue
        fi
        release_id=$(jq -er '.id' <<< "$existing")
        asset_id=$(jq -er '.id' <<< "$saved")
        gh api "repos/$GITHUB_REPOSITORY/releases/$release_id" |
            jq -e --arg tag "$tag" --arg target "$target" --argjson asset "$asset_id" \
                '.draft and .tag_name == $tag and .target_commitish == $target and any(.assets[]; .id == $asset)' \
                >/dev/null || die "$tag draft changed during publication"
        gh api --method DELETE "repos/$GITHUB_REPOSITORY/releases/assets/$asset_id"
    fi
    gh release upload "$tag" -R "$GITHUB_REPOSITORY" "$out/$filename"
done
gh release edit "$tag" -R "$GITHUB_REPOSITORY" --draft=false --latest=false
