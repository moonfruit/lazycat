#!/usr/bin/env bash
# update.sh — bump VERSION to upstream HAOS latest stable release.
# Per the repo convention, sources user shell helpers from $ENV/lib/bash.
set -euo pipefail

if [[ -z "${PROXY_ENABLED:-}" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

cd "$(dirname "$0")"

# Use repo's shared helpers if present
if [[ -n "${ENV:-}" && -d "$ENV/lib/bash" ]]; then
  # shellcheck disable=SC1091
  . "$ENV/lib/bash/github.sh"
  NEW=$(find-latest-version home-assistant operating-system)
else
  # Fallback: GitHub redirect of /releases/latest points at the latest tag.
  NEW=$(curl -sLI "https://github.com/home-assistant/operating-system/releases/latest" \
        | awk -F'/' 'tolower($1) ~ /^location:/ { print $NF }' | tr -d '\r')
fi

[[ -n "$NEW" ]] || { echo "could not determine latest version" >&2; exit 1; }

sed -i.bak "s|^[0-9.]\\+$|$NEW|" VERSION && rm -f VERSION.bak
echo "VERSION = $(cat VERSION)"

if [[ "${1:-}" != "-N" ]]; then
  git --no-pager diff VERSION || true
fi
