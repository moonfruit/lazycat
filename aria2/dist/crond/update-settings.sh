#!/bin/busybox sh
fetch() {
    local key="$1"
    local url="$2"
    local value
    if value=$(curl -fsSL "$url") && [[ -n "$value" ]]; then
        if [[ -z "$SETTINGS" ]]; then
            SETTINGS="\"$key\":\"$value\""
        else
            SETTINGS="$SETTINGS,\"$key\":\"$value\""
        fi
        echo "$key=$value"
    fi
}

fetch bt-external-ip ipv4.ip.sb
fetch bt-tracker https://fastly.jsdelivr.net/gh/XIU2/TrackersListCollection/best_aria2.txt

if [[ -n "$SETTINGS" ]]; then
    ID=$(date +%s)
    JSON='{"id":"'$ID'","jsonrpc":"2.0","method":"aria2.changeGlobalOption","params":["token:aria2",{'"$SETTINGS}]}"

    curl -fsSL -X POST -H 'Content-Type: application/json' \
        --data "$JSON" http://aria2:6880/jsonrpc
    echo
fi
