# Tailscale（懒猫微服）

用官方 `tailscale/tailscale` 镜像把懒猫接入你的 tailnet。两个容器：

- **tailscaled**：直接运行守护进程（`content/tailscaled.sh`，不经官方 `containerboot`——避免无 `TS_AUTHKEY` 时 containerboot 启动阶段 60s 死线杀掉 tailscaled）。host 网络 + 内核 TUN 模式 + 强制 nftables 后端。
- **web**：运行 `tailscale web` 登录/管理页（`content/web.sh`），并把 hostname / 子网路由等部署参数应用到共享的 tailscaled。经懒猫服务入口（SSO 保护）访问。

**始终交互式 OAuth 登录，不使用 `TS_AUTHKEY`。**

## 安装与登录

1. `make install`（或在懒猫应用商店安装本 lpk）。
2. 打开懒猫里的 **Tailscale** 应用入口 → web 管理页 → 点击登录 → 跳转 `login.tailscale.com` 完成 **OAuth2 登录**（浏览器携带懒猫会话回跳，无需将应用设为公开）。
3. 登录从容完成（无时间限制），节点加入 tailnet。登录状态持久化到 `/lzcapp/var/data`，重启即恢复。

## 部署参数

| 参数 | 默认 | 说明 |
|---|---|---|
| Tailscale 主机名 | 盒子名 | tailnet 内主机名（`web.sh` 经 `tailscale set` 应用） |
| 广播子网路由 | （空） | 如 `192.168.50.0/24`，登录后经 `tailscale set --advertise-routes` 广播；需在管理后台批准 |
| tailscale up 额外参数 | `--accept-routes` | 经 `tailscale set` 应用（如 `--accept-routes` 接收对端子网） |
| tailscaled 额外参数 | `--port=0` | 传给 tailscaled；`--port=0` 为随机 WireGuard 端口 |

## 默认能力（已验证）

- **tailnet 节点访问懒猫本身**：登录后即可。
- **tailnet 节点访问懒猫所在子网的其它主机**：把「广播子网路由」设为懒猫子网 CIDR（如 `192.168.50.0/24`），在 Tailscale 管理后台 **Machines → 该节点 → Edit route settings** 批准。**无需任何宿主机改动**——容器以 host 网络运行，`TS_DEBUG_FIREWALL_MODE=nftables` 让 tailscaled 把转发/SNAT 规则写进与宿主一致的 nftables 后端（默认的 iptables-legacy 在此宿主上不生效，会导致转发被丢弃，故必须强制 nftables）。

## 高级场景（手动配置）

### 3a：局域网机器经华硕路由器 → 懒猫 → tailnet / 其它 subnet

让不装 tailscale 的局域网机器也能访问 tailnet（及对端 subnet）：

1. **懒猫节点接受对端路由**：部署参数「tailscale up 额外参数」含 `--accept-routes`（默认已含）。
2. **华硕路由器加静态路由**：目的网段 = tailnet `100.64.0.0/10`（以及要访问的对端 subnet CIDR），下一跳 = 懒猫的局域网 IP。
3. 转发本身由 tailscale 的 nftables 规则放行、宿主 `ip_forward` 已开，通常无需额外操作。若某些对端不通（需要源地址伪装），再在宿主机补 SNAT（随宿主重启失效，需自行持久化）：
   ```bash
   sudo nft add rule inet ts-... 2>/dev/null # 或 legacy:
   sudo iptables -t nat -A POSTROUTING -o tailscale0 -j MASQUERADE
   ```

### 3b：路由器把 tailnet 域名转发到懒猫解析

`tailscaled` 在 `100.100.100.100`（MagicDNS）提供 tailnet 内名称解析。要让局域网经路由器解析 `*.ts.net`，需把这些域名的 DNS 查询指向懒猫，并在懒猫侧再转发给 `100.100.100.100`。本应用未内置该 DNS 转发；如需，可另行部署 dnsmasq 之类转发器并在路由器 DNS 配置中指向懒猫。属可探讨方案，存在 MagicDNS 仅对 tailnet 生效等局限。

## 本机 CLI 诊断（从盒子 tailscaled 视角看直连/中继）

盒子的 `web` 容器内跑了一个哑字节转发器 `tsproxy`，把 tailscaled 的 LocalAPI（unix socket）暴露成 **裸 TCP `:5253`**，经 `ingress` 发布。本机把它桥回成本地 unix socket，再喂给 `tailscale --socket` —— 于是 `tailscale status`/`ping` 直接以**盒子的视角**运行（CLI 二进制配 `--socket` 时不需要本机跑 tailscaled）。

> ⚠️ **安全**：`:5253` 是**裸 TCP、无 SSO**，且**常开**。能连到 `盒子:5253` 的人即可经 LocalAPI **完全控制**盒子 tailscaled（改路由、登出等）。请用 **Tailscale ACL** 限制可达该端口的节点；若不想暴露在 LAN，再用路由器/盒子防火墙限制 `5253` 的源地址。

一次性准备：`brew install tailscale socat`。

手动用法：
```bash
# 盒子地址：LAN IP（如 192.168.50.11）或经 sing-box 路由到的 tailnet IP 任一
socat UNIX-LISTEN:/tmp/box.sock,fork,reuseaddr TCP:192.168.50.11:5253 &
tailscale --socket=/tmp/box.sock status
tailscale --socket=/tmp/box.sock ping <对端节点>
```

包装脚本 `ts-box`（放进 `$PATH`，把起桥 + 调 CLI 合一）：
```bash
#!/usr/bin/env bash
set -euo pipefail
BOX="${TS_BOX_ADDR:-192.168.50.11}:5253"
SOCK="/tmp/ts-box.sock"
# 起桥（若未在跑）
if ! socat -T0 /dev/null UNIX-CONNECT:"$SOCK" 2>/dev/null; then
	rm -f "$SOCK"
	socat UNIX-LISTEN:"$SOCK",fork,reuseaddr TCP:"$BOX" &
	for _ in $(seq 1 50); do [ -S "$SOCK" ] && break; sleep 0.1; done
fi
exec tailscale --socket="$SOCK" "$@"
```
用法：`ts-box status` / `ts-box ping <对端节点>`（盒子地址可用 `TS_BOX_ADDR` 覆盖）。

## 更新

`./update.sh` 从 GitHub 取 `tailscale/tailscale` 最新发布版，就地替换 `lzc-manifest.yml` 的 `version:` 与镜像 tag。
