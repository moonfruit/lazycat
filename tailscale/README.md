# Tailscale（懒猫微服）

用官方 `tailscale/tailscale` 镜像把懒猫接入你的 tailnet。主服务为纯 `containerboot`（环境变量驱动），并以同镜像 sidecar 运行 `tailscale web` 提供可登录的 web 管理页，经懒猫服务入口（SSO 保护）访问。

## 安装与登录

1. `make install`（或在懒猫应用商店安装本 lpk）。
2. 打开懒猫里的 **Tailscale** 应用入口 → 进入 web 管理页 → 点击登录 → 跳转 `login.tailscale.com` 完成 **OAuth2 登录**（浏览器携带懒猫会话回跳，无需将应用设为公开）。
3. 登录后节点即加入 tailnet。**不使用 `TS_AUTHKEY`**——始终交互式 OAuth 登录。

## 部署参数

| 参数 | 默认 | 说明 |
|---|---|---|
| Tailscale 主机名 | `{{ BoxName }}` | tailnet 内主机名 |
| 广播子网路由 | （空） | 如 `192.168.50.0/24`，需在管理后台批准 |
| tailscale up 额外参数 | `--accept-routes` | 透传 `tailscale up` |
| tailscaled 额外参数 | `--port=0` | 随机 WireGuard 端口 |

## 默认能力

- **tailnet 节点访问懒猫本身**：登录后即可（懒猫就是 tailnet 节点）。
- **tailnet 节点访问懒猫所在子网的其它主机**：把「广播子网路由」设为懒猫所在子网 CIDR（如 `192.168.50.0/24`），并在 Tailscale 管理后台 **Machines → 该节点 → Edit route settings** 批准该子网路由。懒猫侧为 host 网络 + 内核模式，tailscale 默认开启 `--snat-subnet-routes`，无需额外配置。

## 高级场景（手动配置，未自动化）

### 3a：局域网机器经华硕路由器 → 懒猫 → tailnet / 其它 subnet

让不装 tailscale 的局域网机器也能访问 tailnet（及对端 subnet）：

1. **懒猫节点接受对端路由**：部署参数「tailscale up 额外参数」保留/包含 `--accept-routes`。
2. **华硕路由器加静态路由**：目的网段 = tailnet `100.64.0.0/10`（以及你要访问的对端 subnet CIDR），下一跳 = 懒猫的局域网 IP。
3. **懒猫宿主机开启转发与 NAT**（宿主机级操作，需自行评估）：
   ```bash
   sudo sysctl -w net.ipv4.ip_forward=1
   sudo iptables -t nat -A POSTROUTING -o tailscale0 -j MASQUERADE
   ```
   说明：因 `tailscaled` 为 host 网络，`tailscale0` 接口在宿主机上；上述规则让来自局域网、转发进 tailnet 的流量做源地址伪装。该改动随宿主机重启失效，需自行持久化。

### 3b：路由器把 tailnet 域名转发到懒猫解析

`tailscaled` 在 `100.100.100.100`（MagicDNS）提供 tailnet 内名称解析。要让局域网经路由器解析 `*.ts.net`，需把这些域名的 DNS 查询指向懒猫，并在懒猫侧再转发给 `100.100.100.100`。本应用未内置该 DNS 转发；如需，可另行部署 dnsmasq 之类转发器并在路由器 DNS 配置中指向懒猫。属可探讨方案，存在 MagicDNS 仅对 tailnet 生效等局限。

## 更新

`./update.sh` 从 GitHub 取 `tailscale/tailscale` 最新发布版，就地替换 `lzc-manifest.yml` 的 `version:` 与镜像 tag。
