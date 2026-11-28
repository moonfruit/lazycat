#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

upstream=ghcr.io/psf/httpbin
latest=$(skopeo list-tags "docker://$upstream" | jq -er '
    [.Tags[] | select(test("^(0|[1-9][0-9]*)([.](0|[1-9][0-9]*)){2}$"))]
    | max_by(split(".") | map(tonumber)) // error("No stable HttpBin image tag found")
')
exec bash ../scripts/lpk/update-app-version.sh httpbin "$latest" "$upstream:$latest" "$@"
