#!/usr/bin/env bash
# setup-proxy.sh — expose HAOS Web UI through this LightOS instance.
# Run as root inside LightOS. Idempotent.
#
# After this, hitting http://<lightos-instance>.heiyu.space/ inside the
# Lazycat network reverse-proxies to http://<HAOS_IP>:8123/, including
# WebSocket upgrade for Home Assistant's live UI.
#
# Reads the upstream HAOS IP from /opt/haos/haos.conf if HAOS_PROXY_UPSTREAM
# is not set in the environment; falls back to an arp-scan lookup via the
# configured HAOS_MAC.
set -euo pipefail

[[ $EUID -eq 0 ]] || { echo "must be root"; exit 1; }

CONF_OUT=/etc/nginx/conf.d/haos-proxy.conf
HAOS_CONF=/opt/haos/haos.conf

# --- 1. apt -----------------------------------------------------------------
echo "=== installing nginx-light ==="
apt-get install -y --no-install-recommends nginx-light

# --- 2. resolve upstream HAOS IP --------------------------------------------
upstream="${HAOS_PROXY_UPSTREAM:-}"
if [[ -z "$upstream" && -r "$HAOS_CONF" ]]; then
  # shellcheck disable=SC1090
  . "$HAOS_CONF"
  parent="${HAOS_PARENT_IF:-lzc-debian}"
  mac_lc=$(echo "${HAOS_MAC:-}" | tr '[:upper:]' '[:lower:]')
  if [[ -n "$mac_lc" ]]; then
    upstream=$(ip neigh 2>/dev/null \
      | awk -v m="$mac_lc" 'tolower($5)==m {print $1; exit}')
    if [[ -z "$upstream" ]] && command -v arp-scan >/dev/null; then
      upstream=$(arp-scan -q -I "$parent" -l 2>/dev/null \
        | awk -v m="$mac_lc" 'tolower($2)==m {print $1; exit}')
    fi
  fi
fi
[[ -n "$upstream" ]] \
  || { echo "could not determine HAOS IP. Set HAOS_PROXY_UPSTREAM=<ip> and re-run."; exit 1; }
echo "  upstream: $upstream:8123"

# --- 3. write nginx config --------------------------------------------------
echo "=== writing $CONF_OUT ==="
cat > "$CONF_OUT" <<EOF
# Managed by setup-proxy.sh. Re-running the script overwrites this file.
#
# Note on X-Forwarded-*: Home Assistant rejects requests carrying
# X-Forwarded-For unless the proxy's IP is listed under http.trusted_proxies
# in /config/configuration.yaml. So we DO NOT send those headers by default —
# HA will see the proxy IP (this LightOS instance) as the client. To get the
# real client IP visible inside HA, configure trusted_proxies first, then
# uncomment the X-Forwarded-* lines below and reload nginx.

upstream haos_upstream {
    server $upstream:8123;
}

server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    # Generous client body size for HA Backup uploads etc.
    client_max_body_size 256m;

    # Long timeouts because Home Assistant uses persistent WebSocket / SSE.
    proxy_read_timeout 86400s;
    proxy_send_timeout 86400s;

    location / {
        proxy_pass http://haos_upstream;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;

        # WebSocket upgrade — Home Assistant's frontend needs this.
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        # Disable buffering so SSE streams don't stall.
        proxy_buffering off;
        proxy_request_buffering off;

        # Opt-in real-client-IP forwarding (requires HA trusted_proxies):
        # proxy_set_header X-Real-IP \$remote_addr;
        # proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        # proxy_set_header X-Forwarded-Proto \$scheme;
    }
}
EOF

# Disable the stock default site so its `default_server` on :80 doesn't clash
# with ours. nginx-light symlinks /etc/nginx/sites-enabled/default → ../sites-
# available/default by default; unlinking is enough.
rm -f /etc/nginx/sites-enabled/default

# Sanity-check + start nginx (or reload if it's already running).
nginx -t
systemctl enable nginx
if systemctl is-active --quiet nginx; then
  systemctl reload nginx
else
  systemctl start nginx
fi

echo
echo "=== proxy ready ==="
echo "  upstream: $upstream:8123"
echo "  listen:   :80 on this LightOS instance"
echo
echo "In the Lazycat network, try:"
echo "  curl -I http://\$(hostname).<owner>.heiyu.space/"
echo "  (should return HTTP 200 with HA's headers)"
echo
echo "Optional — let HAOS see real client IP (instead of this proxy's IP)."
echo "In HAOS, edit /config/configuration.yaml to add:"
echo
echo "    http:"
echo "      use_x_forwarded_for: true"
echo "      trusted_proxies:"
echo "        - $(ip -4 -o addr show "${HAOS_PARENT_IF:-lzc-debian}" 2>/dev/null | awk '{print $4}' | cut -d/ -f1)"
echo
echo "Restart HA Core, then uncomment the X-Forwarded-* lines in"
echo "  $CONF_OUT and 'systemctl reload nginx'."
