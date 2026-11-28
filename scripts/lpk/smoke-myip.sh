#!/usr/bin/env bash
set -euo pipefail

manifest=$1
platform=$2
version=$3
[[ $version =~ ^([0-9]+\.[0-9]+\.[0-9]+)(\.[1-9][0-9]*)?$ ]]
image_version=${BASH_REMATCH[1]}
image=$(yq -er '.services.myip.image' "$manifest")
[[ $image == "jason5ng32/myip:v$image_version" ]]
backend=$(yq -er '.application.upstreams[] | select(.location == "/" and .use_backend_host == false) | .backend' "$manifest")
[[ $backend == 'http://myip:18966/' ]]

box_domain=box.test
host="$(yq -er '.application.subdomain' "$manifest").$box_domain"
setting=$(yq -er '.services.myip.environment[] | select(test("^ALLOWED_DOMAINS="))' "$manifest")
placeholder="\${LAZYCAT_BOX_DOMAIN}"
[[ $setting == *"$placeholder"* ]]
setting=${setting//"$placeholder"/$box_domain}
[[ $setting == "ALLOWED_DOMAINS=$host" ]]
container=$(docker run -d --pull=always --platform "$platform" -p 127.0.0.1::18966 -e "$setting" "$image")
trap 'docker logs --tail 40 "$container"; docker rm -f "$container" >/dev/null' EXIT
address=$(docker port "$container" 18966/tcp)
request=(curl --noproxy '*' -fsS --max-time 5 --retry 30 --retry-all-errors --retry-delay 1 -H "Host: $host")
homepage=$("${request[@]}" "http://$address/")
[[ $homepage == *'IPCheck.ing'* ]]
"${request[@]}" --referer "https://$host/" "http://$address/api/configs" |
    jq -e '.originalSite == false'
installed=$(docker exec "$container" node -p "require('/app/package.json').version")
[[ $installed == "$image_version" ]]
echo "MyIP $installed on $platform passed / and /api/configs"
