#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

upstream_version() {
    local version=${1#*:}
    version=${version%-*}
    # Debian sometimes republishes the source tarball as +ds, +ds1 or +dfsg.
    if [[ $version =~ ^([0-9]+\.[0-9]+\.[0-9]+)(\+(ds|dfsg)[0-9.]*)?$ ]]; then
        echo "${BASH_REMATCH[1]}"
    else
        return 1
    fi
}

if [[ $# == 0 ]]; then
    candidate=$(apt-cache policy qemu-system-x86 | awk '$1 == "Candidate:" {print $2}')
    upstream_version "$candidate"
    exit
fi

versions=$(apt-cache madison qemu-system-x86 | awk '{print $3}')
best=
for version in $versions; do
    if [[ $(upstream_version "$version") == "$1" ]] && dpkg --compare-versions "$version" gt "$best"; then
        best=$version
    fi
done
if [[ -z $best ]]; then
    echo "Debian has no qemu-system-x86 package for upstream $1; run update.sh to check available versions." >&2
    exit 1
fi
echo "$best"
