#!/usr/bin/env bash
set -euo pipefail

manifest=$1
platform=$2
version=$3
[[ $version =~ ^([0-9]+\.[0-9]+\.[0-9]+)(\.[1-9][0-9]*)?$ ]]
image_version=${BASH_REMATCH[1]}
image=$(yq -er '.services.httpbin.image' "$manifest")
[[ $image == "ghcr.io/psf/httpbin:$image_version" ]]
backend=$(yq -er '.application.upstreams[] | select(.location == "/" and .use_backend_host == false) | .backend' "$manifest")
[[ $backend == 'http://httpbin:8080/' ]]
command=$(yq -er '.services.httpbin.command' "$manifest")
read -r -a args <<< "$command"

container=$(docker run -d --pull=always --platform "$platform" -p 127.0.0.1::8080 "$image" "${args[@]}")
trap 'docker logs "$container"; docker rm -f "$container" >/dev/null' EXIT
address=$(docker port "$container" 8080/tcp)
host="$(yq -er '.application.subdomain' "$manifest").box.test"
curl --noproxy '*' -fsS --max-time 5 --retry 12 --retry-all-errors --retry-delay 1 \
    -H "Host: $host" "http://$address/get?source=lazycat" |
    jq -e --arg host "$host" '.args.source == "lazycat" and .headers.Host == $host'
installed=$(docker exec "$container" /opt/httpbin/bin/python -c 'from importlib.metadata import version; print(version("httpbin"))')
[[ $installed == "$image_version" ]]
echo "HttpBin $installed on $platform passed /get"
