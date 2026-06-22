# Tailscale 远程 LocalAPI 暴露 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让本机的 `tailscale` CLI 经一个常开的 TCP↔unix 哑字节隧道直接驱动懒猫盒子上的 tailscaled，做 `status`/`ping` 诊断（直连 vs 中继）。

**Architecture:** 在 `web` 容器内跑一个极简 Go 转发器 `tsproxy`，把 tailscaled 的 LocalAPI unix socket 暴露成 TCP `:5253`，经 `application.ingress`（`service: web`）发布；本机用 `socat` 把该 TCP 桥回成本地 unix socket，喂给 `tailscale --socket`。转发器是哑字节管道，`Host`/`Sec-Tailscale` 头由本机真 CLI 自带、原样穿过，转发器以 root 连 socket 满足 peercred。

**Tech Stack:** Go（stdlib only，交叉编译 linux/amd64）、lzc-cli project build、tailscale/tailscale:v1.98.4（Alpine+busybox）、socat（本机）。

## Global Constraints

- 端口固定 `5253`（容器内 listen 与 ingress publish 一致）。
- 常开、**无部署参数开关**。
- Go **仅用 stdlib**，无第三方依赖（无 go.sum）。
- 交叉编译固定 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`。
- 转发器必须是**哑字节管道**，绝不解析/改写 HTTP（否则破坏 `Host`/`Sec-Tailscale` 三道闸的透传）。
- 镜像 `tailscale/tailscale:v1.98.4` 无 `socat`、`nc` 缺 `-U`，故必须自带 Go 二进制。
- Go 构建产物 `content/tsproxy` 为构建产物，不入库。
- 本机 shell 脚本以 `#!/usr/bin/env bash` 开头（用户全局约定）。

---

### Task 1: Go 转发器 `tsproxy`

**Files:**
- Create: `tailscale/src/go.mod`
- Create: `tailscale/src/main.go`
- Test: `tailscale/src/main_test.go`

**Interfaces:**
- Consumes: 无（本任务起点）。
- Produces:
  - `func serve(ln net.Listener, socketPath string)` —— 接受循环，每连接起 `handle`。
  - `func handle(tcpConn net.Conn, socketPath string)` —— 把一个 TCP 连接与 `socketPath` 的 unix 连接双向 `io.Copy`。
  - 编译出的二进制 `tsproxy`，标志 `-listen`（默认 `:5253`）、`-socket`（默认 `/var/run/tailscale/tailscaled.sock`）。

- [ ] **Step 1: 写 go.mod**

`tailscale/src/go.mod`:

```
module tsproxy

go 1.25
```

- [ ] **Step 2: 写失败的测试**

`tailscale/src/main_test.go`:

```go
package main

import (
	"bufio"
	"net"
	"path/filepath"
	"testing"
)

// 起一个 unix echo 服务（模拟 tailscaled.sock），经 tsproxy 的 TCP 监听往返一行，
// 验证哑字节双向转发正确。
func TestForwardRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "echo.sock")

	ul, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ul.Close()
	go func() {
		for {
			c, err := ul.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				line, _ := bufio.NewReader(c).ReadString('\n')
				c.Write([]byte("echo:" + line))
			}(c)
		}
	}()

	tl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tl.Close()
	go serve(tl, sockPath)

	c, err := net.Dial("tcp", tl.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	resp, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if resp != "echo:hello\n" {
		t.Fatalf("got %q, want %q", resp, "echo:hello\n")
	}
}
```

- [ ] **Step 3: 运行测试，确认编译失败**

Run: `cd tailscale/src && go test ./...`
Expected: FAIL —— `undefined: serve`（实现尚未写）。

- [ ] **Step 4: 写最小实现**

`tailscale/src/main.go`:

```go
// tsproxy：把 tailscaled 的 LocalAPI unix socket 暴露成 TCP 端口的哑字节转发器。
// 仅做双向 io.Copy，绝不解析/改写 HTTP —— Host/Sec-Tailscale 头由本机 tailscale CLI
// 自带并原样穿过；本进程以 root 连 socket，满足 LocalAPI 的 SO_PEERCRED 校验。
package main

import (
	"flag"
	"io"
	"log"
	"net"
)

func main() {
	listenAddr := flag.String("listen", ":5253", "TCP listen address")
	socketPath := flag.String("socket", "/var/run/tailscale/tailscaled.sock", "tailscaled LocalAPI unix socket")
	flag.Parse()

	ln, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("tsproxy: listen %s: %v", *listenAddr, err)
	}
	log.Printf("tsproxy: forwarding %s -> %s", *listenAddr, *socketPath)
	serve(ln, *socketPath)
}

func serve(ln net.Listener, socketPath string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			log.Printf("tsproxy: accept: %v", err)
			return
		}
		go handle(c, socketPath)
	}
}

func handle(tcpConn net.Conn, socketPath string) {
	defer tcpConn.Close()
	unixConn, err := net.Dial("unix", socketPath)
	if err != nil {
		log.Printf("tsproxy: dial %s: %v", socketPath, err)
		return
	}
	defer unixConn.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(unixConn, tcpConn); done <- struct{}{} }()
	go func() { io.Copy(tcpConn, unixConn); done <- struct{}{} }()
	<-done
}
```

- [ ] **Step 5: 运行测试，确认通过**

Run: `cd tailscale/src && go test ./...`
Expected: PASS（`ok  	tsproxy`）。

- [ ] **Step 6: 提交**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat
git add tailscale/src/go.mod tailscale/src/main.go tailscale/src/main_test.go
git commit -m "feat(tailscale): add tsproxy TCP<->unix forwarder for LocalAPI"
```

---

### Task 2: 构建集成（buildscript / Makefile / gitignore）

**Files:**
- Modify: `tailscale/lzc-build.yml`
- Modify: `tailscale/Makefile`
- Modify: `tailscale/.gitignore`

**Interfaces:**
- Consumes: Task 1 的 `tailscale/src/`（Go 模块）。
- Produces: `make` 在 `tailscale/content/tsproxy` 产出 linux/amd64 二进制并打进 `app.lpk`。

- [ ] **Step 1: 给 lzc-build.yml 增加 buildscript**

`tailscale/lzc-build.yml` —— 在 `icon:` 行之后、`compose_override:` 之前插入 buildscript（`contentdir: ./content` 保持不变）：

```yaml
manifest: ./lzc-manifest.yml
contentdir: ./content
pkgout: ./
icon: ./lzc-icon.png

# 交叉编译 tsproxy（TCP<->unix 转发器）到 content/，随内容一并打包。
buildscript: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C src build -o ../content/tsproxy

compose_override:
  services:
    tailscaled:
      devices:
        - /dev/net/tun:/dev/net/tun
      cap_add:
        - NET_ADMIN
        - NET_RAW
```

- [ ] **Step 2: 更新 Makefile 的依赖与 clean**

`tailscale/Makefile` —— 把 `app.lpk` 依赖加上 `src/*`，并让 `clean` 删除构建出的二进制：

```makefile
all: app.lpk

app.lpk: lzc-* src/*
	lzc-cli project build -o app.lpk

clean:
	rm -f app.lpk content/tsproxy

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall `yq .package lzc-manifest.yml`

update:
	@./update.sh
```

- [ ] **Step 3: gitignore 忽略二进制产物**

`tailscale/.gitignore`:

```
app.lpk
*.env
content/tsproxy
```

- [ ] **Step 4: 构建并验证产物架构**

Run:
```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat/tailscale
make clean && make
file content/tsproxy
ls -la app.lpk
```
Expected:
- `make` 成功无报错；
- `file content/tsproxy` 含 `ELF 64-bit LSB executable, x86-64`；
- `app.lpk` 存在。

- [ ] **Step 5: 提交**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat
git add tailscale/lzc-build.yml tailscale/Makefile tailscale/.gitignore
git commit -m "build(tailscale): cross-compile tsproxy into content/"
```

---

### Task 3: 接线 web.sh + manifest ingress

**Files:**
- Modify: `tailscale/content/web.sh`
- Modify: `tailscale/lzc-manifest.yml`

**Interfaces:**
- Consumes: Task 2 产出的 `/lzcapp/pkg/content/tsproxy`；现有 `sock=/var/run/tailscale/tailscaled.sock`。
- Produces: `web` 容器内 `:5253` 上的 tsproxy；经 ingress 发布的盒子 `:5253` 裸 TCP 口。

- [ ] **Step 1: 在 web.sh 的 socket 等待之后后台启动 tsproxy**

`tailscale/content/web.sh` —— 在 `while [ ! -S "$sock" ]; do sleep 1; done` 这一行之后、`hostname=...` 之前插入：

```sh
# 暴露 LocalAPI 给本机 tailscale CLI 诊断：哑字节 TCP->unix 转发器，经 ingress 发布 5253。
# 仅转发原始字节；Host/Sec-Tailscale 头由本机 CLI 自带，本进程以 root 连 socket 满足 peercred。
/lzcapp/pkg/content/tsproxy -listen :5253 -socket "$sock" &
```

修改后 web.sh 相关片段应为：

```sh
# 等待共享 socket 就绪（tailscaled 容器创建）
while [ ! -S "$sock" ]; do sleep 1; done

# 暴露 LocalAPI 给本机 tailscale CLI 诊断：哑字节 TCP->unix 转发器，经 ingress 发布 5253。
# 仅转发原始字节；Host/Sec-Tailscale 头由本机 CLI 自带，本进程以 root 连 socket 满足 peercred。
/lzcapp/pkg/content/tsproxy -listen :5253 -socket "$sock" &

hostname="${TS_HOSTNAME:-${LAZYCAT_BOX_NAME:-}}"
```

- [ ] **Step 2: 在 manifest 增加 ingress**

`tailscale/lzc-manifest.yml` —— 在 `application:` 块的 `routes:` 之后增加 `ingress`（参照 `seq/` 的 `service:` 写法）：

```yaml
application:
  subdomain: tailscale
  background_task: true
  routes:
    - /=http://web:5252/
  # 把 web 容器内 tsproxy 的 :5253 发布为盒子裸 TCP 口，供本机 tailscale CLI 诊断。
  ingress:
    - protocol: tcp
      port: 5253
      publish_port: 5253
      service: web
```

- [ ] **Step 3: 重新构建 lpk**

Run:
```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat/tailscale
make clean && make && ls -la app.lpk
```
Expected: `make` 成功，`app.lpk` 存在。

- [ ] **Step 4: 提交**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat
git add tailscale/content/web.sh tailscale/lzc-manifest.yml
git commit -m "feat(tailscale): expose tsproxy :5253 via ingress for remote CLI"
```

- [ ] **Step 5: 部署并人工验证（需盒子已登录 tailnet）**

> 此步需真实盒子，无法自动化；逐条执行并核对输出。

```bash
# 1) 安装（需先 lzc-cli 登录目标盒子）
cd /Users/moon/Workspace.localized/lazycat/lazycat/tailscale && make install

# 2) 本机起桥（盒子 LAN IP 或经 sing-box 路由到的 tailnet IP 任一）
brew list socat >/dev/null 2>&1 || brew install socat
brew list tailscale >/dev/null 2>&1 || brew install tailscale
socat UNIX-LISTEN:/tmp/box.sock,fork,reuseaddr TCP:192.168.50.11:5253 &

# 3) 诊断
tailscale --socket=/tmp/box.sock status
tailscale --socket=/tmp/box.sock ping <某个对端节点>
```
Expected:
- `status` 列出盒子视角的 peer，含 `direct`/`relay "<derp>"`；
- `ping` 回 `pong from <peer> ... via DERP(<region>)`（中继）或 `via <ip>:<port>`（直连）。

否定验证（确认三道闸仍在、隧道仅对带正确头的 CLI 透明）：
```bash
curl -s -o /dev/null -w '%{http_code}\n' http://192.168.50.11:5253/localapi/v0/status
```
Expected: `403`（缺 `Host: local-tailscaled.sock` / `Sec-Tailscale` 头被拒）。

---

### Task 4: README 文档 + `ts-box` 包装脚本

**Files:**
- Modify: `tailscale/README.md`

**Interfaces:**
- Consumes: Task 3 发布的盒子 `:5253`。
- Produces: 用户向文档（用法 + 安全告警 + `ts-box` 脚本）。

- [ ] **Step 1: 在 README 增加「本机 CLI 诊断」小节**

`tailscale/README.md` —— 在「## 更新」小节**之前**插入以下整节：

````markdown
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
````

- [ ] **Step 2: 提交**

```bash
cd /Users/moon/Workspace.localized/lazycat/lazycat
git add tailscale/README.md
git commit -m "docs(tailscale): document remote CLI diagnostics via tsproxy"
```

---

## Self-Review

**1. Spec coverage：**
- §4/§5 盒子侧（tsproxy + web.sh + ingress + 构建）→ Task 1/2/3 ✅
- §6 本机用法 + ts-box → Task 4 ✅
- §7 安全告警 → Task 4 README ⚠️ 节 ✅
- §9 验证（status/ping + 否定 curl 403）→ Task 3 Step 5 ✅
- §10 文件清单全部被任务覆盖（src/main.go、go.mod、web.sh、manifest、lzc-build.yml、Makefile、.gitignore、README）✅

**2. Placeholder 扫描：** 无 TBD/TODO/「类似上文」。`<对端节点>`/`<derp>`/`<region>` 是运行期由用户填的真实参数占位，非计划占位。✅

**3. 类型一致性：** `serve(ln net.Listener, socketPath string)`、`handle(tcpConn net.Conn, socketPath string)` 在实现、测试、Interfaces 三处签名一致；端口 `5253`、socket 路径、`-listen`/`-socket` 标志全文一致。✅
