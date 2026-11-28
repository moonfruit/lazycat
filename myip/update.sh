#!/usr/bin/env bash
set -euo pipefail
if [[ -z ${PROXY_ENABLED:-} ]] && command -v proxy >/dev/null; then
    exec proxy "$0" "$@"
fi
cd "$(dirname "$0")"

tag=$(gh api repos/jason5ng32/MyIP/releases/latest --jq '.tag_name')
[[ $tag == v* ]]
exec bash ../scripts/lpk/update-app-version.sh myip "${tag#v}" "jason5ng32/myip:$tag" "$@"
