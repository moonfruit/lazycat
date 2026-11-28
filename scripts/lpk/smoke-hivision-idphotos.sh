#!/usr/bin/env bash
set -euo pipefail

manifest=$1
platform=$2
version=$3
lpk=$4
web_image=$(yq -er '.services.hivision.image' "$manifest")
api_image=$(yq -er '.services.hivision_api.image' "$manifest")
[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ && $web_image == "linzeyi/hivision_idphotos:v$version" && $api_image == "$web_image" ]]
yq -e '
    .services.hivision.command == null and
    (.application.routes | any_c(. == "/=http://hivision:7860/")) and
    (.application.routes | any_c(. == "/api/=http://hivision_api:8080/")) and
    (.application.routes | any_c(. == "/favicon.ico=file:///lzcapp/pkg/content/favicon.ico"))
' "$manifest" >/dev/null
favicon="$(dirname "$0")/../../hivision-idphotos/dist/favicon.ico"
tar -xOf "$lpk" content.tar | tar -xOf - favicon.ico | cmp "$favicon" -

command=$(yq -er '.services.hivision_api.command' "$manifest")
read -r -a args <<< "$command"
docker pull --platform "$platform" "$web_image"
web=$(docker run -d --platform "$platform" -p 127.0.0.1::7860 "$web_image")
api=
cleanup() {
    local container
    for container in "$web" "$api"; do
        if [[ -n $container ]]; then
            docker logs --tail 30 "$container"
            docker rm -f "$container" >/dev/null
        fi
    done
}
trap cleanup EXIT
api=$(docker run -d --platform "$platform" -p 127.0.0.1::8080 "$api_image" "${args[@]}")
web_address=$(docker port "$web" 7860/tcp)
api_address=$(docker port "$api" 8080/tcp)
request=(curl --noproxy '*' -fsS --max-time 10 --retry 60 --retry-all-errors --retry-delay 2)
homepage=$("${request[@]}" "http://$web_address/")
[[ $homepage == *'<gradio-app'* && $homepage == *HivisionIDPhotos* ]]
"${request[@]}" "http://$api_address/openapi.json" | jq -e '.paths["/idphoto"].post != null'
tar -xOf "$lpk" icon.png |
    curl --noproxy '*' -fsS --max-time 30 -F 'input_image=@-;filename=icon.png;type=image/png' \
        "http://$api_address/add_background" |
    jq -e '.status == true and (.image_base64 | length > 0)'
echo "HivisionIDPhotos $version on $platform passed the web, API and packaged favicon checks"
