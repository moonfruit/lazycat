#!/usr/bin/env bash
set -euo pipefail

image=$1
version=$2
platform=$3
container=$(docker run -d --platform "$platform" -p 127.0.0.1::8080 "$image")
trap 'docker logs "$container"; docker rm -f "$container" >/dev/null' EXIT
address=$(docker port "$container" 8080/tcp)
curl -fsS --max-time 5 --retry 12 --retry-all-errors --retry-delay 1 \
    "http://$address/get?source=lazycat" | jq -e '.args.source == "lazycat"'
installed=$(docker exec "$container" /opt/httpbin/bin/python -c 'from importlib.metadata import version; print(version("httpbin"))')
[[ $installed == "$version" ]]
if [[ $version == 0.10.4 ]]; then
    docker logs "$container" 2>&1 | grep -F 'Using worker: gevent'
fi
