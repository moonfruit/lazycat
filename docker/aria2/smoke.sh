#!/usr/bin/env bash
set -euo pipefail

image=$1
docker run --rm --platform linux/amd64 "$image" --version
docker run --rm --platform linux/amd64 --network none --entrypoint sh "$image" -ec '
    echo "aria2 smoke" >/tmp/sample
    { printf "HTTP/1.0 200 OK\r\nContent-Length: 12\r\n\r\n"; cat /tmp/sample; } |
        nc -l -p 8080 >/dev/null &
    aria2c --no-conf --quiet --max-tries=3 --retry-wait=1 --timeout=5 \
        --dir=/tmp --out=download http://127.0.0.1:8080/sample
    cmp /tmp/sample /tmp/download
'
