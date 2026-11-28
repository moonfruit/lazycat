#!/usr/bin/env bash
set -euo pipefail

target=$1
key=$(jq -er --arg name "$target" '.images[$name].versionEnv' .github/release-targets.json)
[[ $key =~ ^[A-Z][A-Z0-9_]*$ ]]
version=$(sed -nE "s/^(ENV |[[:blank:]]+)$key=([^[:space:]]+).*/\2/p" "docker/$target/Dockerfile")
[[ $version =~ ^[[:alnum:]][[:alnum:]_.-]{0,127}$ ]]
echo "$version"
