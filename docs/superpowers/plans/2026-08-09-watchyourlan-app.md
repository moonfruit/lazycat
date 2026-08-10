# WatchYourLAN 应用实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在本仓库新增 `watchyourlan/` 目录，用上游官方镜像把 WatchYourLAN（局域网 ARP 扫描器）打包成懒猫微服 `.lpk` 应用，并接入统一的版本更新流程。

**Architecture:** 纯转发型 lpk——无 `contentdir`、无 `buildscript`、无自建镜像。单个 `services.wyl` 容器以 `network_mode: host` 直跑官方镜像（ARP 扫描的硬性要求），Web UI 经 `routes` 反代到 `http://host.lzcapp:8840/`。唯一的可变项 `IFACES` 通过 `lzc-deploy-params.yml` 在安装时注入。

**Tech Stack:** `lzc-cli`（构建/安装）、`yq`（Makefile 读包名）、`skopeo` + `jq`（`find-image-latest-version` 内部）、`sed`（就地版本替换）、GNU coreutils。

**设计依据：** `docs/superpowers/specs/2026-08-09-watchyourlan-app-design.md`（已确认）。

## Global Constraints

- 所有文件创建在仓库根下的新目录 **`watchyourlan/`**，工作目录即该目录。
- 包名固定 **`com.github.moonfruit.watchyourlan`**，子域名固定 **`wyl`**。
- 镜像固定 **`aceberg/watchyourlan`**（Docker Hub 官方），初始版本 **`2.1.4`**。
- **不得**写入以下字段：`background_task`（已从官方 manifest spec 移除）、`public_path`（会绕过懒猫鉴权公开内网设备清单）、`ingress`（host 模式下 8840 已开在盒子上）、`netadmin`、`compose_override` / `cap_add`（`arp-scan` 只需 NET_RAW，属 Docker 默认 capability）。
- `environment` **只写** `TZ` 与 `IFACES` 两项。`TIMEOUT` / `THEME` / `COLOR` 有意省略——上游默认值本就合理，且这些项可在 WatchYourLAN 的 Settings 页面修改并落盘到 `config.yaml`，写进 manifest 会在每次容器重启时覆盖用户的界面设置。
- 包元信息放 **`package.yml`**（新格式），`lzc-manifest.yml` 里**只有** `application` + `services` 两段。
- Makefile 的 `uninstall` 必须读 **`package.yml`**（`yq .package package.yml`）。**不要**照抄 `seq` / `caddy` / `macvtap-helper` 的 `yq .package lzc-manifest.yml`——那三处读的文件里没有 `package` 字段，返回 `null`，是既存缺陷。
- `update.sh` 遵循仓库统一规约：首行 `proxy` 自举、`source "$ENV/lib/bash/docker.sh"`、`sed -i` 就地替换、末尾保留 `$1 != "-N"` 分支（仓库根 `update.sh` 以 `-N` 调用子脚本）。
- 仓库根 `update.sh` 用 `fd -tf update.sh` 递归发现子脚本，**无需修改**任何既有文件。
- `.gitignore` 已含 `*.lpk`，**无需修改**。

---

### Task 1: 应用骨架——产出可构建的 `app.lpk`

**Files:**
- Create: `watchyourlan/lzc-build.yml`
- Create: `watchyourlan/package.yml`
- Create: `watchyourlan/lzc-manifest.yml`
- Create: `watchyourlan/lzc-deploy-params.yml`
- Create: `watchyourlan/lzc-icon.png`
- Create: `watchyourlan/Makefile`
- Test: 无单元测试框架——验证手段是 `lzc-cli project lint` 与 `make` 构建，见各步骤的 Expected

**Interfaces:**
- Consumes: 无（本任务是起点）
- Produces: 供 Task 2 消费的两个 `sed` 锚点——`package.yml` 中的行首 `version:`，`lzc-manifest.yml` 中的 `image: aceberg/watchyourlan:`。Task 2 的替换正则依赖这两处的确切写法，改动它们会破坏 `update.sh`。

- [ ] **Step 1: 建目录并写 `lzc-build.yml`**

```bash
mkdir -p watchyourlan
```

`watchyourlan/lzc-build.yml`：

```yaml
# manifest: 指定 lpk 包的 manifest.yml 文件路径
manifest: ./lzc-manifest.yml

# pkgout: lpk 包的输出路径
pkgout: ./

# icon 指定 lpk 包 icon 的路径，仅允许 png 后缀的文件
icon: ./lzc-icon.png
```

不写 `contentdir`（没有要打包的内容）、不写 `buildscript`（没有要编译的东西）、不写 `compose_override`（不需要额外 capability）。

- [ ] **Step 2: 运行 lint 确认它失败**

Run:
```bash
cd watchyourlan && lzc-cli project lint
```
Expected: **FAIL**——报找不到 `lzc-manifest.yml`（或等价的读取错误）。这确认了 lint 确实在检查我们指定的文件，而不是空转通过。

- [ ] **Step 3: 写 `package.yml`**

`watchyourlan/package.yml`：

```yaml
package: com.github.moonfruit.watchyourlan
version: 2.1.4
name: WatchYourLAN
description: 轻量级局域网扫描器，持续记录设备上下线历史。
author: MoonFruit
license: https://choosealicense.com/licenses/mit/
homepage: https://github.com/aceberg/WatchYourLAN
```

`version` 行必须是行首 `version:` 且无前导空格——Task 2 的 `sed 's/^version:.*/.../'` 依赖这一点。

- [ ] **Step 4: 写 `lzc-manifest.yml`**

`watchyourlan/lzc-manifest.yml`：

```yaml
application:
  subdomain: wyl
  routes:
    - /=http://host.lzcapp:8840/

services:
  wyl:
    image: aceberg/watchyourlan:2.1.4
    network_mode: host
    environment:
      - TZ=Asia/Shanghai
      - IFACES={{ .U.ifaces }}
    binds:
      - /lzcapp/var/data:/data/WatchYourLAN
```

要点说明（供审阅者核对）：
- `host.lzcapp` 是访问 host 网络服务的规范写法，依据是 `vuetorrent/lzc-manifest.yml:28` 的既有实践（`QBIT_BASE=http://host.lzcapp:6880` 指向同包内 `network_mode: host` 的 `qbit` 服务）。**不要**改成短名 `http://wyl/`——host 模式容器不在应用私有网桥上，短名解析不到。
- `{{ .U.ifaces }}` 是懒猫的部署参数模板，构建时**不**渲染，由盒子在安装时替换。
- 无 `package` / `version` / `name` 等元信息——它们在 `package.yml` 里。

- [ ] **Step 5: 写 `lzc-deploy-params.yml`**

`watchyourlan/lzc-deploy-params.yml`：

```yaml
params:
  - id: ifaces
    type: string
    name: Network interfaces
    default_value: "enp2s0"
    description: "Space-separated physical interface names to scan, e.g. `enp2s0` or `enp2s0 wlp4s0`. Keep this pointed at a real NIC: when empty, WatchYourLAN auto-detects and scans every interface on the box, including the docker and LazyCat bridges (lzc-br-*, veth*, br-*), filling the database with meaningless 172.28.x entries. Run `ip -br addr` on the box to list yours."
    optional: false

locales:
  zh:
    ifaces:
      name: "扫描网口"
      description: "以空格分隔的物理网口名，如 `enp2s0` 或 `enp2s0 wlp4s0`。必须填真实网口：留空时 WatchYourLAN 会自动探测并扫描全部接口，把盒子上的 docker 与懒猫网桥（lzc-br-*、veth*、br-*）一起扫进去，数据库里会塞满无意义的 172.28.x 记录。在盒子上执行 `ip -br addr` 可查看本机网口。"
```

格式对照 `tailscale/lzc-deploy-params.yml`（`params` 列表 + `locales.zh` 映射）。

- [ ] **Step 6: 取上游官方图标**

```bash
cd watchyourlan
gh api repos/aceberg/WatchYourLAN/contents/assets/logo.png --jq .download_url \
  | xargs curl -sL -o lzc-icon.png
file lzc-icon.png
```
Expected: `PNG image data, 512 x 512, 8-bit/color RGBA, non-interlaced`，文件约 21KB。若 `file` 输出不是 PNG（例如拿到 HTML 错误页），停下排查，不要继续。

- [ ] **Step 7: 运行 lint 确认通过**

Run:
```bash
cd watchyourlan && lzc-cli project lint
```
Expected: **PASS**——无 error 级输出（App Store 相关的 hint/warn 可接受）。

若报 `optional: false` 不受支持，改成 `optional: true` 并重跑；`default_value` 已保证参数有值，语义不受影响。这是本任务唯一预期的可能返工点。

- [ ] **Step 8: 写 `Makefile`**

`watchyourlan/Makefile`：

```make
all: app.lpk

app.lpk: lzc-* package.yml
	lzc-cli project build -o app.lpk

clean:
	rm -f app.lpk

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall `yq .package package.yml`

update:
	@./update.sh
```

与 `seq/Makefile` 的两处差异，都是有意的：依赖里加了 `package.yml`（版本号在那里，改了要重新打包），`uninstall` 读 `package.yml` 而非 `lzc-manifest.yml`（后者没有 `package` 字段）。

- [ ] **Step 9: 构建并验证产物**

Run:
```bash
cd watchyourlan && make
ls -l app.lpk
```
Expected: 生成 `app.lpk`，非空。

再确认打进包里的 manifest 保留了模板占位符（**没有**在构建期被误渲染成空串）：

lpk v2 的产物是 **POSIX tar**（不是 zip，`unzip` 读不了），内含 `manifest.yml` / `package.yml` / `deploy_params.yml` / `icon.png` 四个文件：

```bash
cd watchyourlan && tar -tvf app.lpk
tar -xOf app.lpk manifest.yml | grep -E 'IFACES|host\.lzcapp|image:'
```
Expected: `tar -tvf` 列出上述四个文件；`grep` 输出中含 `- IFACES={{ .U.ifaces }}`、`- /=http://host.lzcapp:8840/`、`image: aceberg/watchyourlan:2.1.4`。

关键是确认 `{{ .U.ifaces }}` **原样保留**——它必须由盒子在安装时渲染，构建期渲染成空串就说明配置错了。`deploy_params.yml` 由 `lzc-cli` 从 `lzc-deploy-params.yml` 自动发现并打包，无需在 `lzc-build.yml` 里声明。

- [ ] **Step 10: 校验 uninstall 目标能取到包名**

Run:
```bash
cd watchyourlan && yq .package package.yml
```
Expected: `com.github.moonfruit.watchyourlan`（**不是** `null`）。这一步是专门防回归的——照抄 seq 的写法会在这里返回 `null`。

- [ ] **Step 11: 提交**

```bash
git add watchyourlan/lzc-build.yml watchyourlan/package.yml watchyourlan/lzc-manifest.yml \
        watchyourlan/lzc-deploy-params.yml watchyourlan/lzc-icon.png watchyourlan/Makefile
git status --short   # 确认 app.lpk 未被加入（.gitignore 的 *.lpk 应已排除）
git commit -m "Add WatchYourLAN app"
```

---

### Task 2: `update.sh`——接入统一版本更新流程

**Files:**
- Create: `watchyourlan/update.sh`
- Modify: `watchyourlan/package.yml`（仅在验证过程中临时改动并由脚本改回）
- Modify: `watchyourlan/lzc-manifest.yml`（同上）
- Test: 手工验证循环，见步骤

**Interfaces:**
- Consumes: Task 1 产出的两个 `sed` 锚点——`package.yml` 的行首 `version:`、`lzc-manifest.yml` 的 `image: aceberg/watchyourlan:`
- Produces: `watchyourlan/update.sh`，可执行位已置。仓库根 `update.sh` 会通过 `fd -tf update.sh` 自动发现并以 `-N` 调用它，无需注册

- [ ] **Step 1: 造出待修复状态（failing test）**

把两处版本号改成假值，模拟"上游发新版、本地还没跟"的状态：

```bash
cd watchyourlan
sed -e 's/^version:.*/version: 0.0.0/' -i package.yml
sed -e 's|\(image: aceberg/watchyourlan:\).*|\10.0.0|' -i lzc-manifest.yml
grep -H '^version:' package.yml
grep -H 'image:' lzc-manifest.yml
```
Expected: 分别输出 `package.yml:version: 0.0.0` 和 `lzc-manifest.yml:    image: aceberg/watchyourlan:0.0.0`。

- [ ] **Step 2: 写 `update.sh`**

`watchyourlan/update.sh`：

```bash
#!/usr/bin/env bash
if [[ -z "$PROXY_ENABLED" ]] && hash proxy 2>/dev/null; then
    exec proxy "$0" "$@"
fi

source "$ENV/lib/bash/docker.sh"

echo " --- === Updating WatchYourLAN === ---"
# 查镜像 registry 的最新 tag。find-image-latest-version 只取形如 v?N(.N)+ 的 tag，
# 因此 latest / dev / v2 这三个非语义 tag 会被自动滤掉。
VERSION=$(find-image-latest-version aceberg/watchyourlan)
sed -e 's/^version:.*/version: '"$VERSION"'/' -i package.yml
sed -e 's|\(image: aceberg/watchyourlan:\).*|\1'"$VERSION"'|' -i lzc-manifest.yml
echo "Using version: $VERSION"
echo

if [[ $1 != "-N" ]]; then
    if ! git diff --quiet package.yml lzc-manifest.yml; then
        echo " --- === Result === ---"
        git diff package.yml lzc-manifest.yml
    fi
fi
```

```bash
chmod +x watchyourlan/update.sh
```

版本号分散在两个文件里，所以是两条 `sed`（`caddy/update.sh` 只用一条，是因为它的 `version:` 和 `image:` 都在 `lzc-manifest.yml` 内）。

- [ ] **Step 3: 运行脚本，确认它把 0.0.0 修回真实版本**

Run:
```bash
cd watchyourlan && ./update.sh
grep -H '^version:' package.yml
grep -H 'image:' lzc-manifest.yml
```
Expected: 脚本打印 `Using version: 2.1.4`；两处 `grep` 分别显示 `version: 2.1.4` 和 `image: aceberg/watchyourlan:2.1.4`。

脚本**不会**打印 `git diff` 段——这是对的，不是 bug：脚本把版本改回的正是 Task 1 已提交的 `2.1.4`，此时工作区与 HEAD 一致，`git diff --quiet` 返回 0。diff 只在上游真的发了新版时才会打印。

- [ ] **Step 4: 确认工作区已回到 Task 1 的干净状态**

Run:
```bash
cd watchyourlan && git diff --stat package.yml lzc-manifest.yml
```
Expected: **空输出**——两个文件已被脚本改回与 Task 1 提交内容完全一致。若非空，说明 `sed` 替换出的字符串与原文有出入（例如缩进或空格差异），修正 `sed` 表达式后重跑 Step 1–4。

- [ ] **Step 5: 验证 diff 打印分支与 `-N` 分支**

Step 3/4 之后工作区无差异，走不到 diff 打印分支。用一处无关改动把两个分支都逼出来（可逆）：

```bash
cd watchyourlan
sed -e 's/^description:.*/description: TEMP-验证diff分支/' -i package.yml
./update.sh        # 不带 -N
./update.sh -N     # 带 -N
git checkout -- package.yml
git diff --stat    # 确认已恢复
```
Expected:
- 第一次（不带 `-N`）打印 ` --- === Result === ---` 及 `description` 那一行的 diff。
- 第二次（带 `-N`）只打印 `Using version: 2.1.4`，**不打印** diff 段。
- `git checkout` 后 `git diff --stat` 空输出，`description` 恢复原文。

这一步验证的是脚本的输出分支，与版本号无关——`description` 只是个便于观察的可逆改动。

- [ ] **Step 6: 确认仓库根脚本能发现它**

Run:
```bash
fd -tf update.sh | grep watchyourlan
```
Expected: 输出 `watchyourlan/update.sh`。这确认了根 `update.sh` 的递归发现能覆盖到新应用，无需注册。

- [ ] **Step 7: 提交**

```bash
git add watchyourlan/update.sh
git commit -m "Add WatchYourLAN version update script"
```

---

### Task 3: 部署到盒子实测

**Files:**
- Modify（仅在验证失败时）: `watchyourlan/lzc-build.yml`、`watchyourlan/lzc-manifest.yml`
- Test: 盒子上的端到端验证，见步骤

**Interfaces:**
- Consumes: Task 1 产出的 `app.lpk` 与 `make install` 目标
- Produces: 无后续任务依赖。本任务是验收关

**前置条件：** 已通过 `lzc-cli` 登录目标盒子。可用 `lzc-cli box` 相关子命令确认当前目标。盒子 SSH 地址 `192.168.50.11`（或 `dkmooncat.heiyu.space`），物理网口 `enp2s0`。

> **`lzc-cli project *` 子命令在这里都用不了。** `project log` / `project exec` / `project start` 面向的是「开发模式项目」，对已安装应用会直接报 `Project app is not running`。`lzc-cli app log` 则返回 `not yet realized`（未实现）。可用的只有 `lzc-cli app status <pkgId>`（返回 `Installed`）。
>
> 因此本任务的验证主要在**浏览器**里做，辅以 `ssh 192.168.50.11`。注意 SSH 登录的是**裸机 Debian**，lpk 应用容器不在这一层的 `docker ps` 里（那里只有 13 个 `lzc-*` 系统容器），但 host 网络的监听端口在裸机 `ss -lnt` 中可见。

- [ ] **Step 1: 安装**

Run:
```bash
cd watchyourlan && make install
```
Expected: 打印「安装成功！」及访问地址 `https://wyl.<盒子名>.heiyu.space`。

**此时应用还没有真正启动**——容器不存在，8840 也没有监听。这是正常的，见 Step 2。

- [ ] **Step 2: 在浏览器中完成部署向导**

带部署参数的应用装完后处于「待部署」状态，必须在浏览器里填完参数才会拉起容器。用**已登录懒猫账号的浏览器**打开 `https://wyl.<盒子名>.heiyu.space/`（未认证的 `curl` 只会被重定向到 `/sys/login`，触发不了部署）。

Expected: 出现「懒猫部署向导」页，内容本身就是对 `lzc-deploy-params.yml` 的验证：
- 字段标题为中文「扫描网口」且带红色 `*` → `locales.zh` 与 `optional: false` 均生效
- 说明文字是中文版，英文原文没有漏出
- 输入框已预填 `enp2s0` → `default_value` 生效

保持默认值，点「开始部署」。页面标题变为「WatchYourLAN 启动中」，随后开始拉取镜像。

确认服务已起（**在裸机上查，不是在容器里**）：
```bash
ssh 192.168.50.11 "ss -lnt | grep 8840"
```
Expected: 约 1–3 分钟内出现 `LISTEN 0 4096 *:8840 *:*`。

- [ ] **Step 3: 验证 arp-scan 权限正常**

打开 `https://wyl.<盒子名>.heiyu.space/`，看设备表格。

Expected: 表格中有若干设备，**MAC 列有值**，且 Hardware 列能解析出厂商（如 `Apple, Inc...`、`Beijing Xiao..`）。

这比翻日志更直接：能收发原始 ARP 帧才可能拿到 MAC 和厂商，所以有数据就等于 `CAP_NET_RAW` 到位，设计文档 §2 的结论成立。（日志路径在这里不可用，见本任务开头的说明。）

若表格**空白或只有 IP 没有 MAC**，说明 §2 的结论被证伪。修复方式是在 `lzc-build.yml` 末尾追加：

```yaml
compose_override:
  services:
    wyl:
      cap_add:
        - NET_RAW
```

然后 `make clean && make install` 重来，并在设计文档 §2 记下这一结论已被推翻。

- [ ] **Step 4: 验证 Web UI 路由通**

Step 2/3 里页面能正常打开，这一步其实已经过了。这里只记录失败时的分诊路径。

Expected: WatchYourLAN 界面正常加载（顶部 Home / Config / History 导航 + 设备表格），不是 502 / 白屏。

若是 502，先在盒子上确认服务确实在监听：

```bash
ssh 192.168.50.11 "ss -lnt | grep 8840"
```
- 若 8840 **有**监听 → 问题在 `host.lzcapp` 解析，改用设计文档 §3 的兜底方案（加一个桥接网络的反代容器转发到盒子 IP:8840）。
- 若 8840 **无**监听 → 容器没起来，回 Step 2 确认部署向导是否真的提交成功。

- [ ] **Step 5: 验证 `IFACES` 生效**

在 Web UI 的设备列表中查看 Iface 列与 IP 列。

Expected: Iface 列全部是 `enp2s0`；IP 全部落在 `192.168.50.0/24`，**没有** `172.28.x.x`、`172.18.x.x` 这类懒猫/docker 网桥地址。网关 `192.168.50.1` 会被标注为 `_gateway`。

若出现网桥地址，说明 `IFACES` 没被正确注入。检查 `lzc-deploy-params.yml` 的 `id` 是否与 manifest 里的 `.U.ifaces` 完全一致，以及部署向导里填的值。（`lzc-cli project exec` 在已安装应用上不可用，查不了容器内的环境变量。）

- [ ] **Step 6: 验证数据持久化**

用**同版本覆盖安装**来重建容器——`lzc-cli` 没有 restart 类命令，而覆盖安装正是日常升级会发生的事，比单纯重启更有说服力：

```bash
# 先记录基线：设备条数，以及最早那几条记录的时间戳
cd watchyourlan && make install
```

等约 10 秒后刷新 Web UI，对比表格。

Expected:
- 设备条数持平或略增（扫描期间可能新发现设备），**顺序不变**。
- 关键判据：**有若干条记录的时间戳仍早于本次覆盖安装的时刻**。这些是重装前扫描留下的，能活过容器重建就证明 `/lzcapp/var/data:/data/WatchYourLAN` 绑定生效、sqlite 落盘正常。
- 若所有时间戳都晚于覆盖安装时刻、且顺序重排，说明数据被清空了，绑定有问题。

> **不要用 `make uninstall` 再装来做这一步。** 卸载会清空 `/lzcapp/var/data`，那样验证的就不是持久化而是初始化，且已积累的历史会永久丢失。

- [ ] **Step 7: 记录验证结果**

若前面任何一步触发了兜底方案，把实际结论回写进 `docs/superpowers/specs/2026-08-09-watchyourlan-app-design.md` 的对应章节（§2 权限结论、§3 路由方案），并连同代码改动一起提交：

```bash
git add -A watchyourlan docs/superpowers/specs/2026-08-09-watchyourlan-app-design.md
git commit -m "Fix WatchYourLAN deployment issues found in box testing"
```

若各步一次通过，无需额外提交——Task 1、2 的提交即为最终产物。

---

## 验收清单

- [ ] `watchyourlan/` 下有 7 个文件：`package.yml`、`lzc-manifest.yml`、`lzc-deploy-params.yml`、`lzc-build.yml`、`lzc-icon.png`、`Makefile`、`update.sh`
- [ ] `lzc-manifest.yml` 中**不含** `background_task`、`public_path`、`ingress`、`netadmin`
- [ ] `lzc-build.yml` 中**不含** `contentdir`、`buildscript`（除非 Task 3 Step 3 触发了 `compose_override`）
- [ ] `make` 能产出 `app.lpk`，`git status` 中不出现 `app.lpk`
- [ ] `yq .package package.yml` 返回 `com.github.moonfruit.watchyourlan`
- [ ] `./update.sh` 运行后工作区无差异（已是最新版）
- [ ] 盒子上 Web UI 可访问，设备列表为真实局域网地址
