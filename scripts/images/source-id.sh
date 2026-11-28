#!/usr/bin/env bash
set -euo pipefail

target=$1
ref=${2:-HEAD}
workflows=(.github/workflows/build-images.yml)
if [[ $target == qemu ]]; then workflows+=(.github/workflows/build-qemu.yml); fi
# Include the target's registration and the files that build and test it.
{
    git ls-tree "$ref" -- "docker/$target" scripts/images scripts/ci "${workflows[@]}"
    git show "$ref:.github/release-targets.json" | jq -ceS --arg name "$target" '.images[$name]'
} | git hash-object --stdin
