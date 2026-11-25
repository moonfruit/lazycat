#!/usr/bin/env bash
set -euo pipefail
if [[ -z ${PROXY_ENABLED:-} ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

cd "$(dirname "$0")"
base=debian:trixie-slim
digest=$(curl -fsSL --retry 3 https://hub.docker.com/v2/repositories/library/debian/tags/trixie-slim |
    jq -er '.digest | select(test("^sha256:[0-9a-f]{64}$"))')

# Use the development machine's Docker, even when the repo selects the LazyCat context.
version=$(docker --context default run --rm --platform linux/amd64 "$base@$digest" sh -ec '
    apt-get -qq -o APT::Update::Error-Mode=any update >&2
    version=$(LC_ALL=C apt-cache policy qemu-system-x86 | sed -n "s/^  Candidate: //p")
    apt-get -s install "qemu-system-x86=$version" "qemu-utils=$version" >/dev/null
    echo "$version"
')
[[ $version =~ ^[0-9][0-9A-Za-z.+:~-]*$ ]]
sed -i -e "s|^FROM debian:trixie-slim@.*|FROM $base@$digest|" \
    -e "s|^ARG QEMU_VERSION=.*|ARG QEMU_VERSION=$version|" Dockerfile
echo "Debian $digest, QEMU $version"

if [[ ${1:-} != -N ]]; then
    git diff -- Dockerfile
fi
