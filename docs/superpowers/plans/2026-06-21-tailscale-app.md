# Tailscale 应用 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增懒猫微服应用 `tailscale/`，用官方 `tailscale/tailscale` 镜像 + containerboot 环境变量运行 tailscaled，并以同镜像 sidecar 跑 `tailscale web` 提供可 OAuth 登录的 web 管理页（经 lazycat SSO 反代）。

**Architecture:** 双服务同镜像：`tailscaled`（纯 containerboot，host 网络，内核 TUN 模式）通过共享 unix socket（bind `/lzcapp/var/run`）暴露 LocalAPI；`web`（`entrypoint:""` 直接跑 `tailscale web`）连该 socket 并在 bridge 内网 `:5252` 提供页面，lazycat `routes` 反代到它。无 `contentdir`、无自定义 `run.sh`。

**Tech Stack:** LazyCat `.lpk`（`lzc-cli project build`）、`lzc-manifest.yml` 源格式、`compose_override`（`lzc-build.yml`）、`yq`、官方 `tailscale/tailscale` Docker 镜像。

## Global Constraints

- 主服务 `tailscaled` **必须**纯 containerboot + 环境变量，**不得**覆盖入口或引入 `run.sh`。
- **不暴露、不使用** `TS_AUTHKEY`。
- 镜像固定 `tailscale/tailscale:v1.98.4`（两处 service 同版本）；包 `version: 1.98.4`。
- 包名 `com.github.moonfruit.tailscale`，`subdomain: tailscale`。
- tailscaled WireGuard 取随机端口（`--port=0`）；webclient 取固定内网端口 `5252`（反代需稳定目标）。
- 脚本头 `#!/usr/bin/env bash`；`update.sh` 保留 `exec proxy` 与 `-N` 分支（仓库规约）。
- web 走 lazycat SSO，**不设** `public_path`。
- 设计依据：`docs/superpowers/specs/2026-06-21-tailscale-app-design.md`。
- 在默认分支 `main` 上：**先开 `feat/tailscale` 分支再提交**（用户 git 规约）。

---

## 文件结构

```
tailscale/
├── .gitignore             # app.lpk + *.env
├── lzc-manifest.yml       # 包元数据 + application.routes + 两个 service
├── lzc-build.yml          # manifest/icon/pkgout + compose_override(devices/caps)
├── lzc-deploy-params.yml  # 4 个部署参数（无 TS_AUTHKEY）+ zh locales
├── lzc-icon.png           # 复用 czyt lpk 官方 Tailscale 图标
├── README.md              # 使用说明 + 3a/3b 高级场景手动指引
├── Makefile               # all/clean/install/uninstall/update
└── update.sh              # GitHub 取最新版，sed 替换 version + image tag
```

---

### Task 1: 脚手架（目录 / 分支 / 图标 / .gitignore / Makefile）

**Files:**
- Create: `tailscale/.gitignore`
- Create: `tailscale/lzc-icon.png`（从 `/tmp/ts-inspect/icon.png` 复制）
- Create: `tailscale/Makefile`

**Interfaces:**
- Produces: `make` 目标 `all/clean/install/uninstall/update`；`make uninstall` 用 `yq .package lzc-manifest.yml` 寻址。

- [ ] **Step 1: 开分支并建目录**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat
git switch -c feat/tailscale
mkdir -p tailscale
```

- [ ] **Step 2: 复用官方图标**

```bash
# /tmp/ts-inspect/icon.png 来自解包 ~/Downloads/Tailscale-v1.98.4.zip（POSIX tar）
# 若 /tmp 已清理，重新解包：
#   mkdir -p /tmp/ts-inspect && tar -xf ~/Downloads/Tailscale-v1.98.4.zip -C /tmp/ts-inspect
cp /tmp/ts-inspect/icon.png tailscale/lzc-icon.png
file tailscale/lzc-icon.png   # 期望: PNG image data
```

- [ ] **Step 3: 写 `.gitignore`**

```
app.lpk
*.env
```

- [ ] **Step 4: 写 `Makefile`**

```makefile
all: app.lpk

app.lpk: lzc-*
	lzc-cli project build -o app.lpk

clean:
	rm -f app.lpk

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall `yq .package lzc-manifest.yml`

update:
	@./update.sh
```

- [ ] **Step 5: 提交**

```bash
git add tailscale/.gitignore tailscale/lzc-icon.png tailscale/Makefile
git commit -m "feat(tailscale): scaffold app dir (icon, gitignore, Makefile)"
```

---

### Task 2: `lzc-manifest.yml`（双服务 + 反代路由）

**Files:**
- Create: `tailscale/lzc-manifest.yml`

**Interfaces:**
- Consumes: 部署参数 `{{ .U.ts_hostname }}` / `{{ .U.ts_routes }}` / `{{ .U.ts_extra_args }}` / `{{ .U.ts_tailscaled_extra_args }}`（Task 4 定义）。
- Produces: service 名 `tailscaled`、`web`；共享 socket 路径 `/var/run/tailscale/tailscaled.sock`（容器内）↔ `/lzcapp/var/run/tailscale/tailscaled.sock`（宿主 bind）。`compose_override`（Task 3）按 service 名 `tailscaled` 注入 devices/caps。

- [ ] **Step 1: 写 `lzc-manifest.yml`**

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
      - 'while [ ! -S /var/run/tailscale/tailscaled.sock ]; do sleep 1; done; exec tailscale web --listen 0.0.0.0:5252 --origin https://$LAZYCAT_APP_DOMAIN'
    binds:
      - /lzcapp/var/run:/var/run/tailscale
    depends_on:
      - tailscaled
```

- [ ] **Step 2: 校验 YAML 与关键字段**

```bash
cd tailscale
yq '.package, .version, .application.subdomain' lzc-manifest.yml
# 期望: com.github.moonfruit.tailscale / 1.98.4 / tailscale
yq '.services | keys' lzc-manifest.yml          # 期望: [tailscaled, web]
yq '.services.web.entrypoint' lzc-manifest.yml  # 期望: 空字符串 ""
yq '.application.routes' lzc-manifest.yml        # 期望: ["/=http://web:5252/"]
```

Expected: 全部解析无错、值符合上面注释。

- [ ] **Step 3: 提交**

```bash
git add tailscale/lzc-manifest.yml
git commit -m "feat(tailscale): manifest with containerboot service + tailscale web sidecar"
```

---

### Task 3: `lzc-build.yml`（compose_override 注入 devices/caps）

**Files:**
- Create: `tailscale/lzc-build.yml`

**Interfaces:**
- Consumes: service 名 `tailscaled`（Task 2）。
- Produces: 构建时为 `tailscaled` 注入 `/dev/net/tun` 与 `NET_ADMIN`/`NET_RAW`（内核 TUN 模式必需）。无 `contentdir`。

- [ ] **Step 1: 写 `lzc-build.yml`**

```yaml
manifest: ./lzc-manifest.yml
pkgout: ./
icon: ./lzc-icon.png

compose_override:
  services:
    tailscaled:
      devices:
        - /dev/net/tun:/dev/net/tun
      cap_add:
        - NET_ADMIN
        - NET_RAW
```

- [ ] **Step 2: 校验**

```bash
cd tailscale
yq '.compose_override.services.tailscaled.devices' lzc-build.yml
# 期望: ["/dev/net/tun:/dev/net/tun"]
yq '.compose_override.services.tailscaled.cap_add' lzc-build.yml
# 期望: [NET_ADMIN, NET_RAW]
yq 'has("contentdir")' lzc-build.yml   # 期望: false
```

- [ ] **Step 3: 提交**

```bash
git add tailscale/lzc-build.yml
git commit -m "feat(tailscale): build config with compose_override for tun/caps"
```

---

### Task 4: `lzc-deploy-params.yml`（4 参数，无 TS_AUTHKEY）

**Files:**
- Create: `tailscale/lzc-deploy-params.yml`

**Interfaces:**
- Produces: 参数 id `ts_hostname` / `ts_routes` / `ts_extra_args` / `ts_tailscaled_extra_args`，被 Task 2 的 `{{ .U.<id> }}` 引用。**无 `ts_auth_key`**。

- [ ] **Step 1: 写 `lzc-deploy-params.yml`**

```yaml
params:
  - id: ts_hostname
    type: string
    name: Tailscale hostname
    default_value: "{{ .S.BoxName }}"
    description: "The hostname for this node on your tailnet."
    optional: true

  - id: ts_routes
    type: string
    name: Advertise subnet routes
    default_value: ""
    description: "Comma-separated CIDRs to advertise, e.g. 192.168.50.0/24. Must be approved in the Tailscale admin console."
    optional: true

  - id: ts_extra_args
    type: string
    name: tailscale up extra args
    default_value: "--accept-routes"
    description: "Extra flags passed to `tailscale up`. See https://tailscale.com/kb/1080/cli"
    optional: true

  - id: ts_tailscaled_extra_args
    type: string
    name: tailscaled extra args
    default_value: "--port=0"
    description: "Extra flags passed to `tailscaled`. --port=0 picks a random WireGuard port."
    optional: true

locales:
  zh:
    ts_hostname:
      name: "Tailscale 主机名"
      description: "本节点在 tailnet 上的主机名。"
    ts_routes:
      name: "广播子网路由"
      description: "以逗号分隔的 CIDR，如 192.168.50.0/24；需在 Tailscale 管理后台批准后生效。"
    ts_extra_args:
      name: "tailscale up 额外参数"
      description: "透传给 `tailscale up` 的参数，参考 https://tailscale.com/kb/1080/cli"
    ts_tailscaled_extra_args:
      name: "tailscaled 额外参数"
      description: "透传给 `tailscaled` 的参数。--port=0 表示随机 WireGuard 端口。"
```

- [ ] **Step 2: 校验（确认无 authkey、id 与 manifest 引用一致）**

```bash
cd tailscale
yq '.params[].id' lzc-deploy-params.yml
# 期望: ts_hostname / ts_routes / ts_extra_args / ts_tailscaled_extra_args
yq '.params[] | select(.id == "ts_auth_key")' lzc-deploy-params.yml   # 期望: 空（无输出）
# 交叉核对 manifest 引用的每个 .U.<id> 都在此定义：
rg -o '\.U\.([a-z_]+)' lzc-manifest.yml -r '$1' | sort -u
```

Expected: 三个校验分别符合注释；`rg` 列出的 4 个 id 与 params 完全一致。

- [ ] **Step 3: 提交**

```bash
git add tailscale/lzc-deploy-params.yml
git commit -m "feat(tailscale): deploy params (hostname/routes/extra-args, no authkey)"
```

---

### Task 5: `README.md`（使用说明 + 3a/3b 高级场景手动指引）

**Files:**
- Create: `tailscale/README.md`

**Interfaces:** 纯文档，无代码依赖。

- [ ] **Step 1: 写 `README.md`**

````markdown
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
````

- [ ] **Step 2: 提交**

```bash
git add tailscale/README.md
git commit -m "docs(tailscale): usage + advanced 3a/3b manual setup guide"
```

---

### Task 6: `update.sh`（版本跟踪，仓库规约）

**Files:**
- Create: `tailscale/update.sh`（可执行）

**Interfaces:**
- Consumes: `lzc-manifest.yml` 的 `version:` 行与两处 `image: tailscale/tailscale:v…`。
- Produces: 就地把二者更新到 GitHub 最新版；`-N` 跳过 `git diff`。

- [ ] **Step 1: 写 `update.sh`**

```bash
#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/github.sh"

echo " --- === Updating Tailscale === ---"
VERSION=$(find-latest-version tailscale tailscale)
sed -e 's/^version:.*/version: '"$VERSION"'/;' \
    -e 's|\(image: tailscale/tailscale:v\).*|\1'"$VERSION"'|' \
    -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet lzc-manifest.yml; then
        echo " --- === Result === ---"
        git diff lzc-manifest.yml
    fi
fi
```

- [ ] **Step 2: 置可执行 + 静态检查**

```bash
cd tailscale
chmod +x update.sh
bash -n update.sh && echo "syntax ok"
# 验证 sed 两处锚点都能命中（dry-run，不写文件）：
sed -e 's/^version:.*/version: 9.9.9/;' \
    -e 's|\(image: tailscale/tailscale:v\).*|\19.9.9|' \
    lzc-manifest.yml | rg 'version: 9.9.9|tailscale/tailscale:v9.9.9'
```

Expected: `syntax ok`；`rg` 输出 1 行 `version:` + 2 行 image（两个 service 都被替换）。

- [ ] **Step 3: 提交**

```bash
git add tailscale/update.sh
git commit -m "feat(tailscale): update.sh tracking tailscale/tailscale releases"
```

---

### Task 7: 构建 lpk 并核对生成产物

**Files:** 无新增（产出 `tailscale/app.lpk`，已被 `.gitignore` 忽略）。

**Interfaces:** 验证 `lzc-cli project build` 接受全部源文件，且生成的 `manifest.yml` / `compose.override.yml` 与设计一致。

- [ ] **Step 1: 构建**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat/tailscale
make
ls -la app.lpk   # 期望: 文件存在
```

Expected: 构建成功无报错。若 `lzc-cli` 报某字段（如顶层 `permissions`、`entrypoint: ""`、`command` 数组、`depends_on`）schema 不识别，按报错调整并记录到设计文档 §10。

- [ ] **Step 2: 解包核对生成 manifest（lpk 为 tar）**

```bash
cd /tmp && rm -rf ts-built && mkdir ts-built && tar -xf \
  /Users/moon/Workspace.localized/lazycat/lazycat/tailscale/app.lpk -C ts-built
yq '.services.web.entrypoint' ts-built/manifest.yml          # 期望: ""
yq '.services.tailscaled.network_mode' ts-built/manifest.yml  # 期望: host
cat ts-built/compose.override.yml                            # 期望: tun + NET_ADMIN/NET_RAW
yq '.application.upstreams // .application.routes' ts-built/manifest.yml  # 反代指向 web:5252
```

Expected: 生成的 manifest/compose 字段与设计 §4 一致；devices/caps 出现在 compose.override。

- [ ] **Step 3: 提交（仅源文件变更，若 Step 1 有调整）**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat
git add tailscale/
git commit -m "fix(tailscale): adjust manifest per lzc-cli build schema" || echo "无需调整"
```

---

### Task 8: 设备安装与行为验证（goal 1 / goal 2）

**Files:** 无。设备验证，对应设计 §10 风险项。可用 `ssh 192.168.50.11` 实地排查。

**Interfaces:** 确认运行时行为满足 goal 1（web OAuth 登录）与 goal 2（tailnet 访问懒猫及其子网）。

- [ ] **Step 1: 安装到设备**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat/tailscale
make install   # 需先 lzc-cli 登录目标设备
```

Expected: 安装成功，应用出现在懒猫。

- [ ] **Step 2: 验证 §10 风险项（设备上）**

```bash
# 经 SSH 上盒子排查容器（容器名/工具以实际为准）
ssh 192.168.50.11
#  a) 无 authkey 时 tailscaled 起来且 socket 存在、容器未被反复重启：
#     docker ps | rg tailscale ；docker logs <tailscaled> | rg -i 'NeedsLogin|listening'
#     test -S /lzcapp/.../var/run/tailscale/tailscaled.sock
#  b) web 容器日志显示 tailscale web 已 serving 5252：docker logs <web>
#  c) 确认 LAZYCAT_APP_DOMAIN 已注入 web 容器：docker exec <web> printenv LAZYCAT_APP_DOMAIN
```

Expected：a) tailscaled 处于 NeedsLogin 且持续运行、socket 已建；b) web 在 5252 serving；c) 域名 env 非空（否则 `--origin` 无效，需改用模板变量或调整命令——记录到设计 §10 并修复）。

- [ ] **Step 3: 验证 goal 1（web OAuth 登录）**

打开懒猫 Tailscale 入口 → 看到 web 管理页 → 登录 → OAuth 回跳成功 → 节点变 Running。
Expected：登录成功，无 CSRF/回跳错误。若回跳被 SSO 拦截，记录现象并评估是否需对回调路径放行（设计 §10 第 4 项）。

- [ ] **Step 4: 验证 goal 2（子网访问）**

设「广播子网路由」为懒猫子网（如 `192.168.50.0/24`），在 Tailscale 后台批准路由；从另一台 tailnet 设备 `ping` 懒猫的 tailnet IP，并 `ping` 懒猫同子网另一主机（如 `192.168.50.1`）。
Expected：两者均通。

- [ ] **Step 5: 收尾**

按 `superpowers:finishing-a-development-branch` 处理 `feat/tailscale`（合并 / PR / 清理），由用户选择。

---

## Self-Review

**Spec coverage：**
- §1 改动 1（官方镜像/不覆盖 run.sh）→ Task 2（纯 containerboot）；改动 2（env）→ Task 2/4；改动 3（随机端口）→ Task 4 `--port=0`；改动 4（自动起 webclient）→ Task 2 `web` sidecar；改动 5（暴露 4 参数、无 authkey）→ Task 4。
- goal 1 → Task 8 Step 3；goal 2 → Task 8 Step 4。
- §3 架构（共享 socket / healthcheck 防死锁）→ Task 2。
- §4/§5 端口策略 → Task 2/4。
- §8 文档 3a/3b → Task 5。
- §9 打包/update.sh → Task 1/3/6；§10 风险 → Task 7/8。
- 覆盖完整，无遗漏需求。

**Placeholder 扫描：** 无 TBD/TODO；所有文件内容完整给出。

**类型/命名一致性：** service 名 `tailscaled`/`web` 在 Task 2/3/7 一致；参数 id 在 Task 2 引用、Task 4 定义、Task 4 Step 2 交叉校验一致；socket 路径 `/var/run/tailscale/tailscaled.sock` 与 bind `/lzcapp/var/run` 在 Task 2/8 一致；镜像 tag `v1.98.4` 与 `update.sh` sed 锚点 `image: tailscale/tailscale:v` 一致。
