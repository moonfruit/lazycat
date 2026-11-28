#!/usr/bin/env bash
set -euo pipefail
if [[ -z ${PROXY_ENABLED:-} ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

cd "$(dirname "$0")"

release_digest() {
    jq -er --arg name "$1" '
        first(.assets[] | select(.name == $name) | .digest | strings
              | select(test("^sha256:[0-9a-f]{64}$")) | ltrimstr("sha256:"))
        // error("missing SHA-256 for " + $name)
    '
}

# HAOS sometimes publishes releases for just a subset of boards.
version=$(curl -fsSL https://version.home-assistant.io/stable.json |
    jq -er '.hassos.ova | select(test("^[0-9]+(\\.[0-9]+)+$"))')
archive="haos_ova-$version.qcow2.xz"
digest=$(gh api "repos/home-assistant/operating-system/releases/tags/$version" |
    release_digest "$archive")

qemu=$(skopeo list-tags docker://ghcr.io/moonfruit/qemu |
    jq -er '.Tags[] | select(test("^[0-9]+\\.[0-9]+\\.[0-9]+-r[1-9][0-9]*$"))' |
    sort -V | tail -n 1)

oidc_repo=christiaangoossens/hass-oidc-auth
oidc_release=$(gh api "repos/$oidc_repo/releases/latest")
oidc_tag=$(jq -er '.tag_name | select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))' <<< "$oidc_release")
oidc_digest=$(release_digest hass-oidc-auth.zip <<< "$oidc_release")
oidc_license=$(mktemp)
trap 'rm -f "$oidc_license"' EXIT
gh api -H 'Accept: application/vnd.github.raw+json' \
    "repos/$oidc_repo/contents/LICENSE.md?ref=$oidc_tag" > "$oidc_license"
test -s "$oidc_license"

storage=src/internal/vm/storage_linux.go
current=$(yq -er .version package.yml)
committed=$(git show HEAD:./package.yml | yq -er .version)
if ! [[ $current =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    echo "Invalid HAOS LPK version: $current" >&2
    exit 1
fi
next="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))"
# BSD sed requires a separate argument for the empty backup suffix.
sed_flags=(-E -i)
if ! sed --version >/dev/null 2>&1; then
    sed_flags+=('')
fi
sed "${sed_flags[@]}" \
    -e "s|^const imageVersion = .*$|const imageVersion = \"$version\"|" \
    -e "s|^([[:space:]]+digest:[[:space:]]*)\"[0-9a-f]{64}\"|\\1\"$digest\"|" "$storage"
sed "${sed_flags[@]}" "s|^(    image: ghcr.io/moonfruit/qemu:).*|\\1$qemu|" lzc-manifest.yml
sed "${sed_flags[@]}" "s|haos_ova-[0-9.]+\\.qcow2\\.xz|$archive|g" lzc-deploy-params.yml
# The acceptance records later in the README describe specific builds.
sed "${sed_flags[@]}" "/^### 预先下载 HAOS 镜像/,/^## /{
    s|haos_ova-[0-9.]+\\.qcow2\\.xz|$archive|g
    s|(operating-system/releases/download/)[0-9.]+/|\\1$version/|g
    s|[0-9a-f]{64}|$digest|g
}" README.md

if ! cmp -s "$oidc_license" hass-oidc-auth.LICENSE.md; then
    install -m 644 "$oidc_license" hass-oidc-auth.LICENSE.md
fi
printf '%s %s\n' "${oidc_tag#v}" "$oidc_digest" > hass-oidc-auth.lock

# Compare the pins to the last commit so a retry after a partial local update
# still records one version, including when several dependencies changed.
if [[ $current == "$committed" ]] && {
    ! git diff --quiet HEAD -G '^const imageVersion = |^[[:space:]]+digest:[[:space:]]+"' -- "$storage" ||
    ! git diff --quiet HEAD -G '^    image: ghcr.io/moonfruit/qemu:' -- lzc-manifest.yml ||
    ! git diff --quiet HEAD -- hass-oidc-auth.lock hass-oidc-auth.LICENSE.md
}; then
    LPK_VERSION="$next" yq -i '.version = strenv(LPK_VERSION)' package.yml
    echo "HAOS LPK $current -> $next"
fi

echo "HAOS OVA $version ($digest), QEMU $qemu"
echo "hass-oidc-auth $oidc_tag ($oidc_digest)"
if [[ ${1:-} != -N ]]; then
    git diff -- package.yml "$storage" lzc-manifest.yml lzc-deploy-params.yml README.md hass-oidc-auth.lock hass-oidc-auth.LICENSE.md
fi
