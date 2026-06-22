#!/bin/sh
# 运行 tailscale 网页管理/登录界面，并把部署参数对应的 tailscale CLI 配置
# 应用到（与 tailscaled 容器共享的）守护进程。
#
# 登录是交互式 OAuth：用户经懒猫服务入口打开本页面 → 点登录 → tailscale OAuth。
# 不使用 TS_AUTHKEY。tailscaled 由 tailscaled.sh 常驻运行（无 60s 死线）。
set -u

sock=/var/run/tailscale/tailscaled.sock
origin="https://${LAZYCAT_APP_DOMAIN:-tailscale.${LAZYCAT_BOX_DOMAIN}}"

# 等待共享 socket 就绪（tailscaled 容器创建）
while [ ! -S "$sock" ]; do sleep 1; done

# 暴露 LocalAPI 给本机 tailscale CLI 诊断：哑字节 TCP->unix 转发器，经 ingress 发布 5253。
# 仅转发原始字节；Host/Sec-Tailscale 头由本机 CLI 自带，本进程以 root 连 socket 满足 peercred。
/lzcapp/pkg/content/tsproxy -listen :5253 -socket "$sock" &

hostname="${TS_HOSTNAME:-${LAZYCAT_BOX_NAME:-}}"
extra="${TS_EXTRA_ARGS:---accept-routes}"

# 后台：等节点登录成功(Running)后，再应用 env 驱动的配置。
# 放在登录之后，是因为 advertise-routes 等需要在已认证节点上才能真正生效。
{
	while ! tailscale status --json 2>/dev/null | grep -q '"BackendState": *"Running"'; do
		sleep 2
	done
	tailscale set --accept-dns=false 2>/dev/null || true
	[ -n "$hostname" ] && tailscale set --hostname="$hostname" 2>/dev/null || true
	[ -n "${TS_ROUTES:-}" ] && tailscale set --advertise-routes="$TS_ROUTES" 2>/dev/null || true
	[ -n "$extra" ] && tailscale set $extra 2>/dev/null || true
} &

# --origin 用于反代后的 CSRF/来源校验，取懒猫注入的应用域名。
exec tailscale web --listen 0.0.0.0:5252 --origin "$origin"
