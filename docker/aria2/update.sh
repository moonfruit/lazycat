#!/usr/bin/env bash
set -euo pipefail
if [[ -z ${PROXY_ENABLED:-} ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

cd "$(dirname "$0")"
../../scripts/images/update-alpine.py ARIA2_VERSION=community/aria2
if [[ ${1:-} != "-N" ]]; then
    git diff -- Dockerfile
fi
