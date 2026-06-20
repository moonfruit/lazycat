# HAOS macvtap 冷启动自愈 · 设计文档

- 日期：2026-06-20
- 状态：待用户审阅
- 方案代号：B2 方案①（保留 macvtap / LAN 独立 IP）

## 1. 背景与问题

HAOS 作为 KVM 虚拟机跑在 LazyCat/LightOS 的 `debian` 开发实例（`cloud.lazycat.lightos.entry` 的子实例）里，通过 **macvtap** 挂在物理网卡 `enp2s0` 上，让 HAOS 拿到局域网**独立 MAC + DHCP IP**，使依赖广播/mDNS/SSDP 的智能家居设备自动发现能原生工作。部署源在仓库 `haos/`（systemd + `haos.service` + `haos-network.sh`/`haos-launch.sh`，经 `install.sh` 手动装到 debian 实例）。

**故障**：物理机**冷重启**后 HAOS 永久崩溃重启，`haos-launch.sh` 报 `/dev/tapN: 不允许的操作 (EPERM)`。

### 根因（已用 bpftool 实证）

- root 打开字符设备得 EPERM、且无 LSM/seccomp/userns（init userns、真 root）→ 只可能是 **device cgroup 拒绝**。
- LightOS 创建 debian 实例时，在其 cgroup（`…/lightos--debian`）挂一个 `cgroup_device` BPF（实测 prog id 1152/1268/1480/1642…），规则是**对当时宿主 `/proc/devices` 的一次快照**——major 放行列表含 `0xe2(226)…0xee(238=macvtap)…0xfe(254)`，与宿主动态主设备号一一对应。
- macvtap 是**按需加载的内核模块**，宿主**无 modules-load 预载**。冷启动时它未加载→`/proc/devices` 无 238→实例创建时生成的白名单**不含 238**→之后再加载也不改这份已生成的 BPF→HAOS 打开 `/dev/tapN` 永久 EPERM。
- 时间线吻合：冷重启后**连 ifindex=2 也 EPERM**（与 ifindex/minor 无关，封的是 major）；手动重启 debian 实例（此时崩溃循环已把模块载入）→ 白名单重新快照含 238 → 恢复。

### 已排除的其它路线

- **改普通 .lpk 应用（SLIRP/NAT）**：HAOS 失去 LAN 二层 → 丢设备自动发现（实测普通 .lpk 设备白名单为最小集、**永不含 238**）。与目标冲突，放弃。
- **manifest 声明设备/模块/特权**：实证 manifest **无** `devices`/`modules`/`privileged`/`capabilities` 字段；能力是封闭清单（`gpu_accel`/`usb_accel`/`kvm_accel`/`netadmin`/`network_mode`/`runtime`），无放行 macvtap 的开关。
- **宿主持久预载模块**：`/etc/modules-load.d`、`/lzcsys/etc/kernel/*` 都在 overlay（易失），无持久 modprobe 入口。
- **应用间 HTTP 触发（`app.<pkg>.lzcx`）**：需真实用户 `X-HC-USER-TICKET`，开机无用户→401；重放捕获的 ticket 也 401。
- **`hc api_auth_token` + `/sys` 系统 API**：能无登录调系统 API（实测 `/sys/whoami` 通），但无文档化的实例重启端点；本地 7733 API 更直接，故不用。

## 2. 目标与非目标

**目标**：保留 macvtap / LAN 独立 IP / 设备自动发现；HAOS 在物理机冷重启后**自动恢复**，无需人工；尽量**不占用宿主端口**；提供**状态页**便于观测与手动干预。

**非目标**：不改 HAOS 网络模型为 NAT；不修改易失的宿主系统文件；不依赖应用间 HTTP/ticket；不自建 Docker 镜像（agent 用 stock 最小镜像）。

## 3. 已验证的关键机制（实证）

1. **加载 macvtap**：`netadmin: true` + `network_mode: host` 的容器里建 macvtap（`ip link add link enp2s0 ... type macvtap`，或等价的 netlink 调用），**内核自动加载** macvtap 模块（无需特权/CAP_SYS_MODULE/模块文件）。冷态实测：rmmod 后一次创建即让 238 复现。
2. **触发 debian 启动 / 重启 lightos**：host-network 进程 curl 宿主本地 pkgm API
   `POST http://127.0.0.1:7733/api/app/instance/resume?id=cloud.lazycat.lightos.entry&uid=moon`
   （**无鉴权**、本地信任、阻塞至启动完成）；停止用 `…/instance/pause`，查状态 `GET …/instance/status?id=`。来源：`/lzcsys/bin/lpk-manager`（Python，`--api http://127.0.0.1:7733/api/app`）。
3. **白名单重生成**：resume 重新快照 `/proc/devices` 生成新 BPF（实测 **1480→1642**），macvtap 已载则**含 238**（实证 1642 含 `0xee`）。注意 resume 可能返回 **400**（残留实例 `hermes` 的 NAT reconcile 报错，与 debian 无关）但 debian 仍会起来——忽略 400、以轮询 status 为准。
4. **HAOS 打开 macvtap**：白名单含 238 后，`haos.service` 启动，qemu `fd3 -> /dev/tap3` 成功。

## 4. 架构

一个 .lpk 应用，**两个 component、一个 Go 二进制（双模式 `-web` / `-agent`）**，全程**零宿主 TCP 端口**：

### 4.1 前端（router 式，`backend_launch_command`，普通 content 运行时）

不用显式 `services`、不用自建镜像——沿用 router 模式：`upstreams.backend_launch_command` 在默认 content 运行时（**普通网络**）拉起 Go 二进制的 `-web` 模式。

- 监听**容器内**地址 `127.0.0.1:<port>`（普通网络 → 该端口在容器 netns，**不占宿主端口**），`upstreams.backend` 指向它，routes/ingress 经容器网络代理到此。
- 职责：渲染**状态页**（展示 macvtap 是否加载、debian 实例状态、最近动作/日志），提供**两个按钮**：①「强制加载 macvtap」②「重启 lightos」。
- 自身不做特权动作；状态查询与按钮动作**经 unix socket 转发给 agent**。

### 4.2 agent（显式 `services`，host 网络 + netadmin）

- `services.agent`：`image: <极小 stock 镜像>`、`network_mode: host`、`netadmin: true`、`command: /lzcapp/pkg/content/<bin> -agent -socket /lzcapp/var/ipc/agent.sock`。
  - **镜像极小化（已定：`busybox`）**：Go 静态编译（`CGO_ENABLED=0`，buildscript 已是）→ 二进制无 libc 依赖；agent 仅用 netlink/net-http/unix-socket、**不需要 ip/curl/sh**。`image: busybox`（~4MB，docker.io 可代理拉取，带 shell 便于排障），二进制仍从 content 取（前端/agent 共用一次构建），**无需自建镜像/CI**。
- 监听 `/lzcapp/var/ipc/agent.sock`（**unix socket，无 TCP 端口**）。
- 职责：
  - **开机自动逻辑**（启动时跑一次）：见 §4.4。
  - 响应前端经 socket 的请求：`status`（macvtap/实例状态）、`load-macvtap`、`restart-lightos`。
  - 特权动作：用 **netlink** 建/删 macvtap（无需 `ip`）、用 **net/http** 调 `127.0.0.1:7733` pkgm（无需 `curl`）。

### 4.3 通信与端口

- 前端 ↔ agent：`/lzcapp/var/ipc/agent.sock`（两 component 都能访问 `/lzcapp/var`，socket 走文件系统、与网络模式无关）。
- 对外：routes/ingress → 前端的容器内端口（普通网络，无宿主端口）。
- **结论：不占用任何宿主 TCP 端口。**

### 4.4 agent 开机自动逻辑 `BootHeal`（前提：debian/lightos 实例自启 OFF）

```
1. 确保 macvtap 模块已加载：
     若 /proc/devices 无 macvtap → netlink 建临时 macvtap(parent=enp2s0)再删 → 触发内核加载 → 复查 238
2. 若 lightos 实例【未运行】(7733 instance/status != 运行码6) → 启动它：
     POST .../instance/pause?id=cloud.lazycat.lightos.entry&uid=moon (忽略错误)
     POST .../instance/resume?id=...&uid=moon (忽略 400；resume 阻塞至启动完成)
   若 lightos【已运行】→ no-op（部署安全，不打断 HAOS）
```

**为何破解竞态**：决策依据是"**lightos 是否运行**"而非"模块是否已加载"。因 **debian 自启 OFF**，冷启动时 debian 停止 → 其内 `haos.service` 也不运行 → 无人抢先加载 macvtap，只有 agent 加载、再启动 lightos（此时实例创建对 /proc/devices 快照，含 238）。**部署/重装 helper 时 lightos 已运行 → no-op，不打断。**（早先"模块在场则 no-op"的逻辑在自启 ON 时有竞态：haos 自己 `ip link add type macvtap` 会抢先加载模块、骗过 agent；改为按运行态判定 + 自启 OFF 后消除。）

> 运行态码 6 为实测值（7733 内部枚举，非公开 SDK 的 InstanceStatus 0-4 / AppStatus 0-5）；仅用于"已运行则跳过"的部署安全判定，判错最坏只是 redeploy 多启一次，不影响冷启动关键路径。resume 不再轮询特定 status 码判定成败（已阻塞至启动完成）。

### 4.5 手动按钮

- 「强制加载 macvtap」：无条件 netlink 建临时 macvtap 再删（确保模块在册）。
- 「重启 lightos」：无条件 pause+resume `cloud.lazycat.lightos.entry`。
- 两者用于排障/手动恢复，不参与开机自动逻辑的判定。

## 5. 现有 `haos/` 部署微调（组件外）

- `haos/lib/haos.service`：设为 **enabled**（debian 启动即拉起 haos）；**限流硬化**：`StartLimitIntervalSec` 调大（如 600s）配合 `StartLimitBurst`，避免失败时 10s 一次的崩溃风暴（现配置 `StartLimitIntervalSec=10s`=`RestartSec=10s` 致熔断永不触发）。
- `install.sh`：`systemctl enable haos.service`。
- `haos-launch.sh`/`haos-network.sh` 逻辑不变。

## 6. 实例自启策略（必需：OFF）

**debian/lightos 实例必须设为"非开机自启"（OFF）**，这是 §4.4 `BootHeal` 逻辑成立的前提：

- 自启 OFF → 冷启动时 debian 停止 → 其内 haos.service 不运行 → 无人抢先加载 macvtap → 无竞态。agent 加载 macvtap 后启动 lightos，白名单必含 238。
- 若自启 ON → debian 抢先启动并以坏白名单跑 haos（haos 的 `ip link add` 会加载 macvtap 模块），agent 按"是否运行"判定会看到 lightos 已运行 → no-op → HAOS 保持崩溃。**故必须 OFF。**
- 配置方式：LightOS 面板把该实例的开机自启关闭。
- haos.service 在 debian 内须 **enabled**（debian 被 agent 启动后，haos 随之自启并打开 macvtap）。

> 待实测（test ②/③ 确认）：关自启后 `resume id=cloud.lazycat.lightos.entry` 确能拉起 debian 子实例；以及 7733 对"停止态"返回的 status 码（应 != 6）。

## 7. 冷启动闭环流程（默认 ON）

1. 物理机硬重启 → macvtap 未载；lightos+debian 自启（坏白名单，HAOS 暂 EPERM）。
2. `macvtap-helper` 启动：前端常驻出页；agent 开机逻辑发现 `/proc/devices` 无 macvtap → netlink 加载 → 重启 lightos（pause+resume，忽略 400，轮询至运行）。
3. lightos 重启 debian → 白名单重新快照**含 238** → `haos.service`(enabled) 打开 `/dev/tapN` → HAOS 带 LAN 独立 IP 上线、设备发现正常。

无用户、无 token、无 ticket、无宿主端口。

## 8. 错误处理与边界

- **部署/重装 helper（HAOS 已健康、macvtap 已载）**：agent 逻辑判定"已存在"→ **no-op，不重启、不打断**。
- **resume 返回 400**：忽略（残留实例 `hermes` NAT reconcile 报错），以轮询 `instance/status` 为准。
- **macvtap 已加载**：开机逻辑直接 no-op；幂等。
- **helper 与 HAOS 的 macvtap 不冲突**：agent 只建临时 probe 接口并删除（仅为加载模块）；HAOS 的 `haos-mvtap0` 由 `haos-network.sh` 另建。
- **pkgm 调用失败/超时**：有超时与有限重试；最终失败写明确日志（前端状态页可见），不静默。
- **熔断**：`haos.service` 限流硬化，避免崩溃风暴。
- **前端无 agent（socket 未就绪）**：状态页显示"agent 未就绪"，按钮返回明确错误，不崩。

## 9. 仓库结构（新增/改动）

```
macvtap-helper/                 # 新增 helper .lpk 源（Go 单二进制双模式）
  lzc-manifest.yml           # upstreams.backend_launch_command(前端) + services.agent(host+netadmin)
  package.yml                # package/version/name/description
  lzc-build.yml              # buildscript: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C src build -o ../dist/ ; contentdir: ./dist
  lzc-icon.png
  Makefile                   # all/install/uninstall/clean（仓库风格）
  src/                       # Go：main.go(模式分发) web.go(状态页+按钮) agent.go(netlink macvtap + pkgm + socket server + 开机逻辑)
  dist/                      # 构建产物（.gitignore）
haos/                        # 改动
  lib/haos.service           # enable + StartLimit 硬化
  install.sh                 # systemctl enable haos.service
  README.md                  # 增补：装 macvtap-helper、实例自启策略、限流说明
```
不新增 `docker/` 自建镜像（agent 用 stock 镜像）。

## 10. 实现阶段需先验证的点

1. ✅ **已验证（2026-06-20）**：`upstreams.backend_launch_command`（前端，content 运行时）与 `services.agent`（host 网络）**可在同一应用共存**——实测两个容器都正常运行；且二者**共享 `/lzcapp/var`**——agent 写 `/lzcapp/var/ipc/marker`，前端能读到，故 unix socket 互通可行。承重假设成立。
2. **routes/upstreams → content 运行时容器内端口**正常代理（router 已证 backend_launch_command 模式可行，此为常规用法；实现时确认 backend 指向前端监听地址即可）。
3. **netlink 建 macvtap** 在 netadmin 容器内可行（等价于已实测的 `ip link add type macvtap`；实现时确认所用 netlink 库行为）。
4. 组件 ③ 优化项（关实例自启后 resume 仍拉起 debian）。

## 11. 测试策略

- **集成（box 上）**：
  1. 部署 `macvtap-helper`，确认前端状态页可访问、agent socket 就绪、按钮可用。
  2. 模拟冷态：停 HAOS → 宿主 `rmmod macvtap` → 触发 agent 开机逻辑 → 观察 netlink 载模块 + 重启 lightos → 白名单 prog id 变化且含 238 → HAOS 自启打开 macvtap。
  3. 部署安全：HAOS 健康时重装 helper → 确认 agent no-op、不打断。
  4. 真·冷重启：择机整机重启，开机后无人工干预 HAOS 应自动上线并拿到 LAN IP。
- **回归**：helper 重复运行幂等（macvtap 已载不重复建、no-op）；前端按钮多次点击安全。

## 12. 运维备注

- `enp2s0`、`uid=moon`、`cloud.lazycat.lightos.entry`、`/lzcapp/var/ipc/agent.sock` 等做成常量/配置。
- 7733 本地 pkgm API 未公开，行为可能随 LightOS 版本变化；agent 需容错并明确日志。
- debian 实例重启可能改变其自身 MAC/IP（DHCP）；HAOS 自身 MAC 固定、IP 独立，不受影响。
