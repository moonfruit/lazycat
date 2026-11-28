#!/usr/bin/env bash
set -euo pipefail

target=$1
if [[ $target == qemu ]]; then
    version=$(sed -n 's/^ARG QEMU_VERSION=//p' docker/qemu/Dockerfile)
    [[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
    echo "$version"
    exit
fi
key=$(jq -er --arg name "$target" '.images[$name].versionEnv' .github/release-targets.json)
[[ $key =~ ^[A-Z][A-Z0-9_]*$ ]]
version=$(sed -nE "s/^(ENV |[[:blank:]]+)$key=([^[:space:]]+).*/\2/p" "docker/$target/Dockerfile")
[[ $version =~ ^[[:alnum:]][[:alnum:]_.-]{0,127}$ ]]
echo "$version"
