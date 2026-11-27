#!/usr/bin/env bash
set -euo pipefail

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
lzc-cli project build httpbin -o "$out/app.lpk"
info=$(lzc-cli lpk info "$out/app.lpk")
echo "$info"
version=$(sed -n 's/^version: //p' <<< "$info")
package=$(sed -n 's/^package: //p' <<< "$info")
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && $package == com.github.moonfruit.httpbin ]]
tag="lpk-httpbin-v$version"
asset="httpbin-$version.lpk"
mv "$out/app.lpk" "$out/$asset"
if unzip -tq "$out/$asset" >/dev/null 2>&1; then
    unzip -p "$out/$asset" manifest.yml > "$out/metadata.yml"
else
    tar -xOf "$out/$asset" package.yml > "$out/metadata.yml"
fi
digest=$(sha256sum "$out/$asset" | cut -d' ' -f1)
size=$(wc -c < "$out/$asset")
yq -o=json '.' "$out/metadata.yml" |
    jq -e --arg package "$package" --arg version "$version" --arg digest "$digest" --argjson size "$size" '
      select(.package == $package and .version == $version and (.name | length > 0))
      | {packageId: .package, name, summary: .description, version, sha256: $digest, size: $size}
    ' > "$out/release.json"

# The releases API includes drafts for the publishing token. Do not treat an API error as an absent tag.
releases=$(gh api --paginate --slurp "repos/$GITHUB_REPOSITORY/releases?per_page=100")
existing=$(jq -c --arg tag "$tag" 'add[] | select(.tag_name == $tag)' <<< "$releases")
if [[ -n $existing ]] && [[ $(jq -r '.draft' <<< "$existing") == false ]]; then
    git diff --exit-code "$tag" HEAD -- 'httpbin/lzc-*' httpbin/package.yml
    echo "$tag already published. Rebuilding the software source from its original assets."
    exit 0
fi
if [[ -z $existing ]]; then
    gh release create "$tag" --draft --target "$GITHUB_SHA" --title "HttpBin $version" \
        --notes "LazyCat LPK · $package · $version"
else
    # Only resume a draft whose source files match the current build.
    target=$(jq -r '.target_commitish' <<< "$existing")
    [[ $target =~ ^[0-9a-f]{40}$ ]]
    git diff --exit-code "$target" HEAD -- 'httpbin/lzc-*' httpbin/package.yml
fi
gh release upload "$tag" "$out/$asset" "$out/release.json" --clobber
gh release edit "$tag" --draft=false --latest=false
