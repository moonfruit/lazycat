#!/bin/busybox sh
DIR=/root/.aria2
FILE=aria2.conf

cd $DIR || exit 1
if [[ ! -f $FILE.md5 ]] || md5sum -c $FILE.md5; then
    BASE=$(dirname "$0")
    cp "$BASE/$FILE" $DIR
    md5sum $FILE >$FILE.md5
    if [[ ! -f session ]]; then
        touch session
    fi
    if [[ ! -f server-stat ]]; then
        touch server-stat
    fi
fi
