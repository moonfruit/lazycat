#!/usr/bin/env bash
set -euo pipefail

select_images() {
    python3 scripts/ci/select-targets.py images
}

refresh_pins() {
    local attempt target workflow_sha registration newest
    local paths=(.github/workflows/build-images.yml scripts/images scripts/ci)
    workflow_sha=$(git rev-parse HEAD)
    registration=$(jq -ceS '.images' .github/release-targets.json)
    for target in $(jq -r '.[]' <<< "$targets"); do
        if [[ $target == qemu ]]; then paths+=(.github/workflows/build-qemu.yml); fi
        paths+=("docker/$target" ":(exclude)docker/$target/Dockerfile")
    done
    for attempt in 1 2 3; do
        git fetch --quiet origin main
        # The run still executes the workflow definition from its triggering commit.
        # A newer definition or helper must have its own run before it can label an image.
        newest=$(git show FETCH_HEAD:.github/release-targets.json | jq -ceS '.images')
        if [[ $registration != "$newest" ]] || ! git diff --quiet "$workflow_sha" FETCH_HEAD -- "${paths[@]}"; then
            echo "Image release rules changed on main. Deferring to a run using the new rules."
            targets='[]'
            return 0
        fi
        git switch --quiet --detach FETCH_HEAD
        for target in $(jq -r '.[]' <<< "$targets"); do
            "docker/$target/update.sh" -N
            if [[ $target == qemu ]] && ! git diff --quiet -- docker/qemu/Dockerfile; then
                # Only record a new upstream pin after its packages install and both binaries work.
                docker build --pull --no-cache --platform linux/amd64 -t qemu-ci:pin docker/qemu
                docker/qemu/smoke.sh qemu-ci:pin "$(scripts/images/version.sh qemu)"
            fi
            git add -- "docker/$target/Dockerfile"
        done
        if git diff --cached --quiet; then return 0; fi
        git -c user.name='github-actions[bot]' -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
            commit -m 'chore(images): update scheduled image pins'
        if git push origin HEAD:refs/heads/main; then return 0; fi
        echo "Push attempt $attempt failed; checking the latest main before retrying." >&2
    done
    return 1
}

inspect_tag() {
    local reference=$1
    local error_file="$RUNNER_TEMP/inspect-image.err"
    if skopeo inspect --override-os linux --override-arch amd64 --retry-times 2 --no-tags \
        "docker://$reference" 2>"$error_file"; then return 0; fi
    if grep -Eiq 'manifest unknown|name unknown' "$error_file"; then echo '{}'; return 0; fi
    cat "$error_file" >&2
    return 1
}

targets=$(select_images)
repairs='[]'
if [[ $EVENT == schedule ]]; then
    refresh_pins
fi
qemu=$(jq -c 'index("qemu") != null' <<< "$targets")
targets=$(jq -c 'map(select(. != "qemu"))' <<< "$targets")
if [[ $EVENT == schedule ]]; then
    pending=()
    for target in $(jq -r '.[]' <<< "$targets"); do
        expected=$(scripts/images/source-id.sh "$target")
        image="ghcr.io/${GITHUB_REPOSITORY_OWNER,,}/$target"
        latest=$(inspect_tag "$image:latest")
        published=$(jq -r '.Labels["io.github.moonfruit.image-source"] // ""' <<< "$latest")
        if [[ $expected != "$published" ]]; then
            pending+=("$target")
            continue
        fi
        version=$(scripts/images/version.sh "$target")
        alias=$(inspect_tag "$image:$version")
        digest=$(jq -er '.Digest | select(test("^sha256:[a-f0-9]{64}$"))' <<< "$latest")
        alias_digest=$(jq -r '.Digest // ""' <<< "$alias")
        if [[ $digest != "$alias_digest" ]]; then
            repairs=$(jq -c --arg name "$target" --arg digest "$digest" '. + [{name:$name,digest:$digest}]' <<< "$repairs")
        fi
    done
    targets=$(jq -cn --args '$ARGS.positional' "${pending[@]}")
fi
echo "Selected images for $EVENT: Alpine=$targets, QEMU=$qemu"
echo "Version tags to repair: $repairs"
{
    echo "targets=$targets"
    echo "repairs=$repairs"
    echo "qemu=$qemu"
    echo "sha=$(git rev-parse HEAD)"
} >> "$GITHUB_OUTPUT"
