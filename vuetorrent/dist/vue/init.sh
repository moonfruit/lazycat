#!/usr/bin/env bash
cat <<EOF >>/config/qBittorrent/qBittorrent.conf
WebUI\AuthSubnetWhitelist=0.0.0.0/0, ::/0
WebUI\AuthSubnetWhitelistEnabled=true
WebUI\LocalHostAuth=false
WebUI\Password_PBKDF2="@ByteArray(NxKJkBjp982vKvo8z8hiTw==:P6wUC7xhHrhvsCvPmDnVoBeeg0eKeUX5HokyIbBXI4tsJLuOh3roY5UlBgdTlvv0Nj3ubJIDA/uvtn/W/ZnH9A==)"
EOF
