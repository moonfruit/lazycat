#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

read -r version digest < hass-oidc-auth.lock
if [[ ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || ! $digest =~ ^[0-9a-f]{64}$ ]]; then
    echo 'Invalid version or SHA-256 in hass-oidc-auth.lock' >&2
    exit 1
fi

check_digest() {
    printf '%s  %s\n' "$digest" "$1" | shasum -a 256 --check --quiet
}

mkdir -p .cache
archive=".cache/hass-oidc-auth-v$version.zip"
if [[ ! -f $archive ]] || ! check_digest "$archive"; then
    tmp=$(mktemp .cache/hass-oidc-auth.XXXXXXXX)
    trap 'rm -f "$tmp"' EXIT
    curl -fLsS --retry 2 -o "$tmp" \
        "https://github.com/christiaangoossens/hass-oidc-auth/releases/download/v$version/hass-oidc-auth.zip"
    check_digest "$tmp"
    mv "$tmp" "$archive"
fi
if ! unzip -p "$archive" manifest.json |
    jq -e --arg version "$version" '.domain == "auth_oidc" and .version == $version' >/dev/null; then
    echo "Invalid auth_oidc manifest in $archive (expected v$version)" >&2
    exit 1
fi

rm -rf dist
mkdir dist
install -m 644 "$archive" dist/hass-oidc-auth.zip
cp hass-oidc-auth.LICENSE.md src/internal/vm/guest/prepare.sh dist/
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C src build -trimpath -o ../dist/haos-vm .
