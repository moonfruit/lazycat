# Tailscale 应用（LazyCat `.lpk`）设计

日期：2026-06-21
状态：设计已确认，待写实现计划

## 1. 目标

在懒猫微服上新增一个 `tailscale` 应用，参考 `~/Downloads/Tailscale-v1.98.4.zip`（第三方 czyt 打包）但做如下改动：

1. **使用官方镜像 `tailscale/tailscale`，不覆盖 `run.sh`**——主服务完全靠官方 `containerboot` 入口 + 环境变量驱动。
2. 通过官方镜像支持的环境变量实现功能（`TS_HOSTNAME` / `TS_ROUTES` / `TS_EXTRA_ARGS` / `TS_TAILSCALED_EXTRA_ARGS` 等）。
3. tailscaled 自身（WireGuard）监听**随机端口**。
4. **自动启动 webclient**；其监听端口在反代要求下取固定内网端口（见 §5）。
5. 部署参数暴露 `TS_HOSTNAME`、`TS_ROUTES`、`TS_EXTRA_ARGS`、`TS_TAILSCALED_EXTRA_ARGS`，**不暴露、不使用 `TS_AUTHKEY`**。

### 默认要达到的目标
- **goal 1**：通过懒猫服务入口打开 tailscale 的 web 管理界面，并在该页面完成 **OAuth2 登录**。
- **goal 2**：tailnet 内节点既能访问懒猫本身，也能访问懒猫所在子网的其它主机。

### 可探讨场景（本次仅写文档，不自动化——已与用户确认「核心 + 文档说明」）
- **3a**：局域网机器经默认网关（华硕路由器）路由到懒猫，再转发进 tailnet，甚至到其它 subnet。
- **3b**：路由器把 tailnet 相关域名转发到懒猫做解析。

## 2. 关键技术事实（已用官方文档核实）

`containerboot`（官方镜像入口）环境变量：
- `TS_HOSTNAME` → 节点主机名。
- `TS_ROUTES` → 等价 `tailscale set --advertise-routes=`（子网路由广播）。
- `TS_EXTRA_ARGS` → 透传给 `tailscale up`（如 `--accept-routes`、`--ssh`）。
- `TS_TAILSCALED_EXTRA_ARGS` → 透传给 `tailscaled`（如 `--port=0` 取随机 WireGuard 端口、`--verbose=2`）。
- `TS_SOCKET` → LocalAPI unix socket，默认 `/var/run/tailscale/tailscaled.sock`。
- `TS_USERSPACE=0` → 内核网络模式（子网路由/访问 LAN 必需），需 `/dev/net/tun` + `NET_ADMIN`/`NET_RAW`。
- `TS_STATE_DIR` → 状态持久化目录。
- `TS_ACCEPT_DNS` → 是否接管容器 DNS（默认置 `false`，避免改容器 resolv.conf）。

**webclient 的两种形态（决定架构的核心事实）：**

| | `tailscale web`（独立进程） | `tailscale set --webclient`（内置） |
|---|---|---|
| 监听 | `--listen` 指定的普通 TCP 地址 | 节点 **tailnet IP : 5252**，需 ACL 放行 |
| 登出态 | **可用**（显示登录按钮，可走 OAuth） | **不可用**（要先登录到 tailnet） |
| 反代可指向 | 是（固定地址） | 否（部署时不知 100.x 地址） |

> 结论：`containerboot` **不会**自动起 `tailscale web`，且没有任何环境变量能让它起；内置 `--webclient` 因「登出态不可用 + 反代无法稳定指向」无法满足 goal 1 的首次 OAuth 登录。**必须独立运行 `tailscale web`。**

## 3. 架构：双服务（同一官方镜像）

```
┌─────────────────────────────────────────────────────────────┐
│ service: tailscaled  (tailscale/tailscale, 纯 containerboot)  │
│   network_mode: host  +  /dev/net/tun + NET_ADMIN/NET_RAW    │
│   env: TS_USERSPACE=0, TS_ACCEPT_DNS=false, TS_*=部署参数      │
│   bind: /lzcapp/var/data:/var/lib            (state 持久化)    │
│   bind: /lzcapp/var/run:/var/run/tailscale   (共享 socket)    │
└───────────────────────────┬─────────────────────────────────┘
                            │ 经共享 socket(文件系统)通信
┌───────────────────────────┴─────────────────────────────────┐
│ service: web  (tailscale/tailscale, entrypoint:"")           │
│   bridge 网络（默认）；不需要 host net、不需要 netadmin       │
│   command: sh -c '等待 socket; exec tailscale web ...'        │
│   bind: /lzcapp/var/run:/var/run/tailscale   (共享 socket)    │
│   depends_on: tailscaled                                     │
└───────────────────────────┬─────────────────────────────────┘
                            │ HTTP 127-internal :5252
┌───────────────────────────┴─────────────────────────────────┐
│ lazycat 反代  routes: /=http://web:5252/   (走 SSO，非公开)   │
└─────────────────────────────────────────────────────────────┘
```

**Sidecar 不违反「不覆盖 run.sh」**：主服务 `tailscaled` 严格纯 containerboot + env；`web` 只是用 `entrypoint:""` 直接调用**官方 `tailscale` 二进制**（无任何自带脚本文件）。这是已与用户确认的选择。

**跨容器共享 socket** 是仓库既有模式：`macvtap-helper` 的 host-net 服务在 `/lzcapp/var` 下建 unix socket，由另一侧连接（healthcheck `test -S /lzcapp/var/ipc/agent.sock`）。本设计同理：`tailscaled` 在 `/lzcapp/var/run/tailscale/tailscaled.sock` 建 socket，`web` 通过相同 bind 看到它。

**避免健康检查死锁**：`tailscaled` 的 healthcheck 用 `test -S /var/run/tailscale/tailscaled.sock`（守护进程就绪即可），**不**用「Online=true」——否则「登录才 Online、起 web 才能登录」互锁。`web` 侧也只在 command 里循环等 socket 出现，不依赖 `tailscaled` 的 Online 状态。

## 4. Manifest 草案（`lzc-manifest.yml`，源格式 + 内联包元数据）

```yaml
name: Tailscale
package: com.github.moonfruit.tailscale
version: 1.98.4
description: 将设备和用户接入你专属的安全虚拟专用网络（官方镜像 + OAuth 登录）。

application:
  subdomain: tailscale
  background_task: true
  routes:
    - /=http://web:5252/

services:
  tailscaled:
    image: tailscale/tailscale:v1.98.4
    network_mode: host
    netadmin: true
    environment:
      - TS_STATE_DIR=/var/lib/tailscale
      - TS_SOCKET=/var/run/tailscale/tailscaled.sock
      - TS_USERSPACE=0
      - TS_ACCEPT_DNS=false
      - TS_HOSTNAME={{ .U.ts_hostname }}
      - TS_ROUTES={{ .U.ts_routes }}
      - TS_EXTRA_ARGS={{ .U.ts_extra_args }}
      - TS_TAILSCALED_EXTRA_ARGS={{ .U.ts_tailscaled_extra_args }}
    binds:
      - /lzcapp/var/data:/var/lib
      - /lzcapp/var/run:/var/run/tailscale
    healthcheck:
      test: ["CMD-SHELL", "test -S /var/run/tailscale/tailscaled.sock"]
      start_period: 30s

  web:
    image: tailscale/tailscale:v1.98.4
    entrypoint: ""
    command:
      - sh
      - -c
      - 'while [ ! -S /var/run/tailscale/tailscaled.sock ]; do sleep 1; done;
         exec tailscale web --listen 0.0.0.0:5252 --origin https://$LAZYCAT_APP_DOMAIN'
    binds:
      - /lzcapp/var/run:/var/run/tailscale
    depends_on:
      - tailscaled
```

`devices` 与 `cap_add` 通过 `lzc-build.yml` 的 `compose_override` 注入（仓库习惯，参考 `bambu-studio`）：

```yaml
compose_override:
  services:
    tailscaled:
      devices:
        - /dev/net/tun:/dev/net/tun
      cap_add:
        - NET_ADMIN
        - NET_RAW
```

## 5. 端口策略

- **需求 3（tailscaled 随机端口）**：`ts_tailscaled_extra_args` 默认 `--port=0`，WireGuard UDP 取随机端口，避免 host 网络下与 41641 冲突。
- **需求 4（webclient 端口）**：`web` 在 **bridge 内网**监听固定 `5252`。因为它在桥接内网口、不映射到宿主机，**不存在主机端口冲突**；而 lazycat 反代必须有稳定目标地址，故固定端口是正确选择，「随机端口」不适用于 webclient。

## 6. 部署参数（`lzc-deploy-params.yml`，不含 `TS_AUTHKEY`）

| id | env | 默认值 | 说明 |
|---|---|---|---|
| `ts_hostname` | `TS_HOSTNAME` | `{{ .S.BoxName }}` | tailnet 内主机名 |
| `ts_routes` | `TS_ROUTES` | （空） | 广播子网（如 `192.168.50.0/24`），让 tailnet 节点访问该子网其它主机；**需在 admin console 批准路由** |
| `ts_extra_args` | `TS_EXTRA_ARGS` | `--accept-routes` | 透传 `tailscale up`；`--accept-routes` 使懒猫节点接受其它路由器广播的子网（支撑 3a） |
| `ts_tailscaled_extra_args` | `TS_TAILSCALED_EXTRA_ARGS` | `--port=0` | 透传 `tailscaled`，随机 WireGuard 端口 |

含 `locales.zh` 中文名/描述。全部 `optional: true`。

## 7. 登录与连通流程

1. 无 `TS_AUTHKEY` → containerboot 起 `tailscaled`，节点处于 NeedsLogin（`tailscale up` 阻塞等待登录，`tailscaled` 持续运行、socket 已建）。
2. `web` 等到 socket → 起 `tailscale web` → 用户从懒猫服务入口（经 SSO）打开页面 → 点登录 → 跳 `login.tailscale.com` OAuth → 浏览器携 lazycat 会话回跳 → 完成 → `tailscaled` 转 Running、阻塞的 `tailscale up` 返回。
3. **goal 2**：host net + 内核模式 + `TS_ROUTES=子网` + admin 批准 → tailnet 节点访问懒猫本身及同子网其它主机（tailscale 默认 `--snat-subnet-routes` 开启，懒猫侧无需额外配置）。

**安全**：不设 `public_path`，web 管理页走 lazycat SSO（比 czyt 的全公开更安全）。OAuth 是纯浏览器重定向，无需对外开放即可完成。

## 8. 高级场景文档（README，仅说明不自动化）

- **3a**：华硕路由器加静态路由（tailnet `100.64.0.0/10` 及目标 subnet 的下一跳指向懒猫 IP）；懒猫侧开 `net.ipv4.ip_forward=1` 并对 `tailscale0` 出口做 `MASQUERADE`；`TS_EXTRA_ARGS` 含 `--accept-routes` 以接收对端 subnet。给出命令与注意事项，标注为宿主机级手动操作。
- **3b**：说明 tailscaled 本地 DNS（`100.100.100.100` / MagicDNS）与「把 `*.ts.net` 指向懒猫解析」的方案及局限（需额外 DNS 转发，未自动化）。

## 9. 打包与版本跟踪

目录结构（源格式，**无 `contentdir`、无 `run.sh`**）：

```
tailscale/
├── lzc-manifest.yml       # §4
├── lzc-build.yml          # manifest/icon/pkgout + compose_override（无 contentdir）
├── lzc-deploy-params.yml  # §6
├── lzc-icon.png           # 复用 czyt lpk 内官方 Tailscale 图标（/tmp/ts-inspect/icon.png）
├── README.md              # §8 高级场景文档 + 使用说明
├── Makefile               # 仓库标准模板（all/install/uninstall/clean/update）
└── update.sh              # 见下
```

`update.sh`（沿用仓库规约：可 `exec proxy`、`source $ENV/lib/bash/*.sh`、`sed -i` 就地替换、`-N` 跳过 diff）：
- 用 GitHub 取 `tailscale/tailscale` 最新版（`find-latest-version tailscale tailscale` → 如 `1.98.4`）。
- `sed` 替换 `lzc-manifest.yml` 顶部 `version:` 与两处 `image: tailscale/tailscale:v…`。
- 末尾 `$1 != -N` 时打印 `git diff`。

## 10. 风险与待验证（有宿主机 SSH：`ssh 192.168.50.11`）

1. **无 authkey 时 containerboot 行为**：预期起 tailscaled 后 `tailscale up` 阻塞等登录、socket 已建、web 可完成登录并解阻塞。需在设备上确认容器不会因此判为失败而被反复重启。
2. **跨容器共享 socket 权限**：两容器默认 root，socket 默认权限应可互通；以 `macvtap-helper` 为先例，但需实测 `tailscale web` 能连上。
3. **`$LAZYCAT_APP_DOMAIN` 注入**：确认 lazycat 把该 env 注入到 `web` 服务容器（czyt 的 run.sh 依赖它，应已注入）；`--origin` 取值正确，避免登录 POST 被 CSRF 拦截。
4. **SSO 与 OAuth 回跳**：确认走 lazycat SSO 时回跳不被拦；若有问题再评估是否需要对特定回调路径放行。
5. **`lzc-cli` 对 `entrypoint: ""` / `command` 数组 / `depends_on` 的支持**：构建后核对生成的 `compose.override`/`manifest`。
```
