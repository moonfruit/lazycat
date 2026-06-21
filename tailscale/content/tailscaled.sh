#!/bin/sh
# 直接运行 tailscaled，绕开官方 containerboot 入口。
#
# 为什么不用 containerboot：无 TS_AUTHKEY 时 containerboot 在启动阶段会跑一个被
# 硬编码 60s 超时（cmd/containerboot/main.go）包住的阻塞式 `tailscale up`，等不到
# 交互式网页 OAuth 登录就会 SIGTERM 杀掉 tailscaled。直接跑 tailscaled 则常驻、
# 无死线，登录从容完成后状态持久化、永久稳定。
#
# 本脚本只负责 tailscaled 自身的启动参数；所有 tailscale CLI 配置
# （hostname / advertise-routes / accept-routes 等）由 web 容器的 web.sh 负责。
set -eu

mkdir -p /var/lib/tailscale /var/run/tailscale

# 默认 --port=0（随机 WireGuard 端口）；用户在 TS_TAILSCALED_EXTRA_ARGS 覆盖时以其为准。
# 内核 TUN 模式为 tailscaled 默认（容器已挂 /dev/net/tun + NET_ADMIN/NET_RAW）。
exec tailscaled \
	--statedir=/var/lib/tailscale \
	--socket=/var/run/tailscale/tailscaled.sock \
	${TS_TAILSCALED_EXTRA_ARGS:---port=0}
