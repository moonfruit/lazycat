#!/bin/busybox sh
CONFIG=Caddyfile
CHECKSUM=$CONFIG.md5
cd /etc/caddy || exit 1
if [[ ! -f $CONFIG ]] || md5sum -c $CHECKSUM; then
    BASE=$(dirname "$0")
    cp "$BASE/Caddyfile" $CONFIG
    md5sum $CONFIG >$CHECKSUM
fi
