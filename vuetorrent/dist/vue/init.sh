#!/usr/bin/env bash
CONFIG=/config/qBittorrent/qBittorrent.conf

if grep -F 'WebUI\Password_PBKDF2' $CONFIG >/dev/null; then
    echo "qBittorrent already initialized."
    exit 0
fi

echo "Initializing qBittorrent configuration..."

echo "Set download directory to '/home/Downloads'"
mkdir -p /home/Downloads
sed -i "s#/downloads/#/home/Downloads/#" $CONFIG

echo "Setting WEBUI username/password to 'admin/admin'"
cat <<EOF >>$CONFIG
WebUI\AuthSubnetWhitelist=0.0.0.0/0, ::/0
WebUI\AuthSubnetWhitelistEnabled=true
WebUI\LocalHostAuth=false
WebUI\Password_PBKDF2="@ByteArray(NxKJkBjp982vKvo8z8hiTw==:P6wUC7xhHrhvsCvPmDnVoBeeg0eKeUX5HokyIbBXI4tsJLuOh3roY5UlBgdTlvv0Nj3ubJIDA/uvtn/W/ZnH9A==)"
EOF
