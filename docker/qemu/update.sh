#!/usr/bin/env bash
set -euo pipefail
if [[ -z ${PROXY_ENABLED:-} ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

cd "$(dirname "$0")"
# Use the development machine even if the repo selects the LazyCat Docker context.
version=$(docker --context default run --pull=always --rm --platform linux/amd64 \
    --mount "type=bind,source=$PWD/package-version.sh,target=/tmp/package-version.sh,readonly" \
    debian:trixie-slim sh -ec 'apt-get -qq -o APT::Update::Error-Mode=any update >&2; bash /tmp/package-version.sh')
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
sed -i -e "s|^ARG QEMU_VERSION=.*|ARG QEMU_VERSION=$version|" Dockerfile
echo "QEMU $version"

if [[ ${1:-} != -N ]]; then
    git diff -- Dockerfile
fi
