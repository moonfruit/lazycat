# Tailscale 远程 LocalAPI 暴露：本机 CLI 直接诊断盒子 tailscaled

日期：2026-06-22
应用：`tailscale/`（`com.github.moonfruit.tailscale`）

## 1. 背景与目标

希望从**本机**用 `tailscale` 命令行直接对懒猫盒子上的 tailscaled 做诊断：

- `tailscale status`：看盒子视角下与各 peer 的连接状态（直连 / DERP 中继）。
- `tailscale ping <peer>`：从**盒子的 tailscaled** 主动探测到某节点的路径（直连还是中继）。

关键点：`ping`/`status` 反映的是**执行它的那个守护进程**的视角，所以必须把命令作用到盒子的 tailscaled 上，而不是本机的。

## 2. 约束（决定了方案选型）

- 本机**没有 tailscaled**：日常经 **sing-box** 接入 tailnet，因此 `tailscale ssh` 这个子命令用不了（它需要本机有真实 tailscaled 节点身份才能发起）。
- 但 `tailscale` CLI **二进制**配 `--socket=<路径>` 时**不需要本机跑 tailscaled**——它只是个 LocalAPI 的 HTTP-over-unix 客户端。
- 本机经 sing-box 可路由到盒子的 tailnet IP（`100.x`）；也能走 LAN（`192.168.50.11`）。
- 懒猫三层架构：裸机 Debian → lzc-os 系统容器 → lpk 应用容器。**裸机宿主机的 `docker` 看不到应用容器**（跑在 lzc-os 嵌套运行时里），宿主机也**没有** `tailscale` 二进制、**没有** `tailscale0` 网卡（在应用容器 netns 内）。→ 经宿主机 SSH 去 exec/转发的路子又脏又脆，**否决**。
- tailscaled LocalAPI 是 HTTP 服务，但对**特权端点**有三道闸：① `Host` 头须为 `local-tailscaled.sock`；② 部分端点须带 `Sec-Tailscale: localapi` 头（防 CSRF/DNS-rebinding）；③ unix 连接的 `SO_PEERCRED` 须是 root/operator。

## 3. 现有架构（改动前）

- `tailscaled` service：`network_mode: host`，直跑 tailscaled，socket 在 `/var/run/tailscale/tailscaled.sock`（bind 到 `/lzcapp/var/run`）。
- `web` service：跑 `tailscale web` 登录/管理页（`/=http://web:5252/`，经懒猫 SSO），**共享同一个 socket bind**，容器内有 `tailscale` CLI，**以 root 运行**。

## 4. 方案（X'：本机原生 CLI / 裸 TCP 隧道）

在 `web` 容器里跑一个**极简 TCP→unix 裸字节转发器**，把 LocalAPI socket 暴露成一个 TCP 端口，再用 `application.ingress` 把该端口发布出去。本机把这个 TCP 桥回成一个本地 unix socket，喂给 `tailscale --socket`。

**为什么三道闸全过**：转发器是**哑字节管道**，HTTP 客户端是本机的 `tailscale` CLI——它自己带 `Host: local-tailscaled.sock` 和 `Sec-Tailscale: localapi`，原样穿过 socat 与转发器到达 tailscaled；转发器以 root 连 socket，`SO_PEERCRED` 也过。转发器不做任何 HTTP 解析/改写。

**为什么 ping 判定准确**：本机↔盒子这条隧道只是**控制通道**（LocalAPI）。`tailscale ping` 的数据面探测由盒子 tailscaled 自己发起，与控制通道无关——即使控制通道走 DERP，ping 出来的直连/中继结论仍是盒子的真实视角。

数据流：

```
本机 tailscale CLI
  └─(unix)→ /tmp/box.sock
        └─ socat → TCP box:5253
              └─ lzc ingress(publish 5253 → service web :5253)
                    └─ tsproxy(web 容器, 哑字节管道)
                          └─(unix root)→ tailscaled.sock → tailscaled LocalAPI
```

### 决策（已确认）

- **常开、无开关**：不加部署参数，转发器随 web 一直启动，ingress 一直声明（用户已确认从「默认关」改为「默认开」）。
- **固定端口 5253**：紧挨 web UI 的 5252，便于记忆与关联。

## 5. 盒子侧改动

1. **新增 Go 转发器 `tsproxy`**（`tailscale/src/`，沿用 `netcat`/`router` 的 Go-companion 惯例，stdlib only）：
   - 标志：`-listen :5253`（默认）、`-socket /var/run/tailscale/tailscaled.sock`（默认）。
   - 逻辑：`net.Listen("tcp", listen)`；每个连接 `net.Dial("unix", socket)` 后双向 `io.Copy`；dial 失败则关连接（容忍 socket 尚未就绪）。约 ~50 行。
2. **`content/web.sh`**：在现有「等 socket 就绪」循环之后、`exec tailscale web` 之前，后台启动 `/lzcapp/pkg/content/tsproxy -listen :5253 -socket "$sock" &`。
3. **`lzc-manifest.yml`**：`application:` 下新增 ingress（参照 `seq/` 的 `service:` 写法）：
   ```yaml
   ingress:
     - protocol: tcp
       port: 5253
       publish_port: 5253
       service: web
   ```
   `web` service 本身无需改（已挂 socket bind、已 root）。
4. **构建集成**（最小扰动，binary 与 .sh 同放 `content/`，`contentdir` 不变）：
   - `lzc-build.yml` 增加 `buildscript: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C src build -o ../content/tsproxy`，`contentdir: ./content` 保持不变，`compose_override` 保持。
   - `Makefile`：`app.lpk` 依赖加 `src/*`；`clean` 增删 `content/tsproxy`。
   - `.gitignore`：新增 `content/tsproxy`（构建产物不入库）。

## 6. 本机侧用法（无需改 app）

一次性：`brew install tailscale socat`。

```bash
# 桥：本地 unix socket ← TCP → 盒子（LAN 或 sing-box 路由到的 tailnet IP）
socat UNIX-LISTEN:/tmp/box.sock,fork,reuseaddr TCP:192.168.50.11:5253 &
tailscale --socket=/tmp/box.sock status
tailscale --socket=/tmp/box.sock ping <对端节点>
```

附 `ts-box` 包装脚本（README 给出），把起桥 + 调 CLI 合一，例如 `ts-box status` / `ts-box ping <peer>`。

## 7. 安全模型

- ingress 发布的是**裸 TCP，无 SSO**；能到 `box:5253` 的人即可经 LocalAPI **完全控制**盒子 tailscaled（改路由、登出等）——LocalAPI 本身特权且无应用层鉴权。
- 常开 + 无开关意味着该端口持续暴露在盒子的 LAN 与 tailnet 可达面上。
- **缓解**：① 用 **Tailscale ACL** 限制可达 `100.x:5253` 的节点；② 如不希望 LAN 暴露，靠路由器/盒子防火墙限制 5253 源地址。README 必须显著标注此风险与加固方式。
- 注意用户「LAN 经路由器静态路由把 `100.64/10` 指向盒子」的高级场景会让 LAN 主机也能摸到 `100.x`，同样靠 ACL 兜底。

## 8. 否决的方案（记录理由）

- **A：Tailscale SSH 进容器**——`tailscale ssh` 需本机 tailscaled，用户用 sing-box，出局。
- **Z：把 LocalAPI 直接走 HTTP `route` 暴露**——两头不讨好：浏览器/curl 直打会被 `Host`/`Sec-Tailscale` 闸 403（除非反代逐请求注入头）；且 CLI 不能指向 URL，给它 HTTP route 也用不上。
- **宿主机 SSH + exec/socket 转发**——懒猫嵌套运行时下宿主机看不到应用容器、无 tailscale 二进制、路径随重装变化，脆。
- **v2「两全」（暂不做）**：本机起个注入 SSO cookie + 那俩头的 HTTP→unix 桥，既原生 CLI 又走 SSO route；卡在 LazyCat 浏览器态 SSO 的取 token，留待后续。

## 9. 验证

- 构建：`make`（应交叉编译出 `content/tsproxy` 并打包）。
- 安装：`make install`，盒子登录 tailnet 后：
  - 本机起桥 → `tailscale --socket=/tmp/box.sock status` 应输出盒子 peer 列表，含 `direct`/`relay "xxx"` 字样。
  - `tailscale --socket=/tmp/box.sock ping <peer>` 应回 `pong ... via DERP(xxx)` 或 `via <ip>:<port>`（直连）。
- 否定验证：不带 `Sec-Tailscale` 头直接 `curl http://box:5253/localapi/v0/status` 应被 tailscaled 拒（确认闸仍在、隧道仅对带正确头的 CLI 透明）。

## 10. 文件清单

- 新增：`tailscale/src/main.go`、`tailscale/src/go.mod`。
- 修改：`tailscale/content/web.sh`、`tailscale/lzc-manifest.yml`、`tailscale/lzc-build.yml`、`tailscale/Makefile`、`tailscale/.gitignore`、`tailscale/README.md`。

## 11. 非目标

- 不做部署参数开关（常开）。
- 不做 Y（SSO HTTP 小 API）。
- 不做 DNS 转发等无关增强。
