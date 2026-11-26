#!/usr/bin/env bash
# HAOS runs this POSIX script with /bin/sh before Docker starts.
set -eu
cd "$1"
seed=$2
store=homeassistant/.storage/http
marker=.haos-lpk-proxy-subnet
stage=homeassistant/.storage/.haos-lpk-http.tmp
umask 077

next=$(jq -er '.data.stable.trusted_proxies[0]' "$seed")
remember_subnet() {
    printf '%s\n' "$next" > "$marker.tmp"
    mv "$marker.tmp" "$marker"
}

if [ ! -e homeassistant/configuration.yaml ] && [ ! -e "$store" ]; then
    mkdir -p homeassistant/.storage
    cp "$seed" "$stage"
    remember_subnet
    mv "$stage" "$store"
    exit 0
fi

[ -f "$store" ] && [ -f "$marker" ] || exit 0
previous=$(cat "$marker")
[ "$previous" != "$next" ] || exit 0

jq --arg old "$previous" --arg new "$next" '
    def managed: (.data.stable, .data.pending) | objects | .trusted_proxies | arrays | .[] | select(. == $old);
    select(any(managed; true)) | (managed) = $new
' "$store" > "$stage"
if [ -s "$stage" ]; then
    mv "$stage" "$store"
else
    rm -f "$stage"
fi
remember_subnet
