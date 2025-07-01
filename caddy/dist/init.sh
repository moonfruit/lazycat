#!/bin/busybox sh
DIR=/etc/caddy
FILE=Caddyfile

cd $DIR || exit 1
if [[ ! -f $FILE.md5 ]] || md5sum -c $FILE.md5; then
    BASE=$(dirname "$0")
    cp "$BASE/$FILE" $DIR
    md5sum $FILE >$FILE.md5
fi
