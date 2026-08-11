#!/bin/bash
# Seq init script（服务启动前由 seqentry 执行）：
# 生成自签证书并启用 HTTPS 监听。lzc-ingress 以 https 回源时，
# Seq 的 OIDC 中间件才会生成 https 且不带端口的 redirect_uri，
# 与懒猫在 dex 中注册的回调地址严格一致。
set -e

CERTS=/data/Certificates
PASSWORD=lzcapp-selfsigned

if [[ ! -f $CERTS/localhost.pfx ]]; then
    mkdir -p "$CERTS"
    KEY=$(mktemp) CERT=$(mktemp)
    # lzcinit 的 Go 代理校验证书主机名（要求 SAN），域名为服务名 seq
    openssl req -x509 -newkey rsa:2048 -days 3650 -nodes \
        -subj "/CN=seq" -addext "subjectAltName=DNS:seq,DNS:localhost" \
        -keyout "$KEY" -out "$CERT" 2>/dev/null
    openssl pkcs12 -export -inkey "$KEY" -in "$CERT" \
        -out "$CERTS/localhost.pfx" -password "pass:$PASSWORD"
    rm -f "$KEY" "$CERT"
fi

seqsvr config set -k certificates.certificatesPath -v "$CERTS"
seqsvr config set -k certificates.defaultPassword -v "$PASSWORD"
