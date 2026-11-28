#!/usr/bin/env bash
set -euo pipefail

# On an unchanged daily check, an absent catalog entry must still trigger a repair.
catalog="$RUNNER_TEMP/current-catalog.json"
if [[ $CHECK_UPSTREAM == true ]]; then
    url="https://${GITHUB_REPOSITORY_OWNER,,}.github.io/${GITHUB_REPOSITORY#*/}/source/v2/index.json"
    if ! curl -fsSL --max-time 30 "$url" > "$catalog"; then
        echo '{}' > "$catalog"
    fi
fi

targets=()
while IFS= read -r slug; do
    if [[ $EVENT != workflow_dispatch || $CHECK_UPSTREAM == true ]]; then
        "./$slug/update.sh" -N
    fi
    if [[ $CHECK_UPSTREAM == true ]] && git diff --quiet -- "$slug"; then
        version=$(yq -er '.version' "$slug/package.yml")
        package=$(jq -er --arg slug "$slug" '.lpk[$slug].package' .github/release-targets.json)
        if jq -es --arg v "$version" --arg pkg "$package" \
            'any(.[].apps[]?; .packageId == $pkg and .latestVersion.version == $v)' "$catalog" >/dev/null; then
            continue
        fi
    fi
    targets+=("$slug")
done < <(jq -r '.[]' <<< "$SELECTED")

git diff --binary --output="$RUNNER_TEMP/lpk-updates.patch"
patch=false
[[ ! -s "$RUNNER_TEMP/lpk-updates.patch" ]] || patch=true
{
    echo "targets=$(jq -cn --args '$ARGS.positional' "${targets[@]}")"
    echo "patch=$patch"
    echo "revision=$(git rev-parse HEAD)"
} >> "$GITHUB_OUTPUT"
