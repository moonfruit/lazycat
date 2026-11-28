#!/usr/bin/env bash
set -euo pipefail

target=$1
ref=${2:-HEAD}
# Include the target's registration and the files that build and test it.
{
    git ls-tree "$ref" -- "docker/$target" scripts/images scripts/ci .github/workflows/build-images.yml
    git show "$ref:.github/release-targets.json" | jq -ceS --arg name "$target" '.images[$name]'
} | git hash-object --stdin
