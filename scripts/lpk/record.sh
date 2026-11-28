#!/usr/bin/env bash
set -euo pipefail

scope=("$@" .github/release-targets.json .github/workflows/{publish-lpks,verify-lpk}.yml scripts/{lpk,ci})
git fetch --quiet origin main
latest=$(git rev-parse FETCH_HEAD)
if [[ $HAS_UPDATES != true ]]; then
    git diff --exit-code HEAD "$latest" -- "${scope[@]}"
    exit
fi

# Compute the exact tree the build tested, without modifying the checkout.
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
patch="$RUNNER_TEMP/lpk-updates.patch"
tested=$(
    set -e
    export GIT_INDEX_FILE="$tmp/index"
    git read-tree HEAD
    git apply --cached "$patch"
    git write-tree
)
if git diff --quiet "$tested" "$latest" -- "${scope[@]}"; then
    # A prior attempt may have pushed the versions and then failed to publish.
    git switch --quiet --detach "$latest"
else
    git diff --exit-code HEAD "$latest" -- "${scope[@]}"
    git switch --quiet --detach "$latest"
    git apply --index "$patch"
    git -c user.name='github-actions[bot]' \
        -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
        commit -m 'chore(lpk): update tested application versions'
    git push origin HEAD:refs/heads/main
fi
