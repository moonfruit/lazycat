# router UDP 端口段转发 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `netmap`（`router/src/`，被 `router` 与 `aipod` 共享）在保留 TCP 0-65535 透传的基础上，额外提供 UDP 0-65535 透传，外部目的端口端到端不变，源端口由会话表保证同源同向稳定。

**Architecture:** 单二进制并行监听 TCP+UDP，两条数据路径互不感知。UDP 路径含一个 `(clientIP, publishPort) → upstream UDP socket` 的会话表，带 idle TTL 与 LRU 上限。**探针实测确认 publish_port 通过 datagram 的 source port 字段传递**（不是 payload 前缀），且 LazyCat 同时通过 IPv4 + IPv6 双栈把外部 datagram 复制注入容器，因此 UDP 监听必须用 `udp4` 仅接收 IPv4 一份避免重复。

**Tech Stack:** Go 1.25（标准库 `net` / `sync`），LazyCat `lzc-cli project build` 打包 `.lpk`，`netcat`/`dig`/`iperf3` 做端到端验证。

**Spec:** `docs/superpowers/specs/2026-05-25-router-udp-forwarding-design.md`

---

## File Structure

新增 / 修改的文件：

| 路径 | 用途 | 状态 |
|---|---|---|
| `router/src/probe/main.go` | 一次性探针：UDP 监听并 dump 原始报文 + source addr | 新建（作为历史 artifact 提交） |
| `router/probe/Makefile` | 探针 lpk 的 Make 入口 | 新建 |
| `router/probe/lzc-build.yml` | 探针构建配置（指向 `../src/probe`） | 新建 |
| `router/probe/lzc-manifest.yml` | 探针 manifest，仅含 UDP ingress | 新建 |
| `router/probe/package.yml` | 探针 package 元数据（LPK v2） | 新建 |
| `router/src/udp.go` | UDP 路径全部实现：会话表、runUDP/reverseUDPLoop | 新建 |
| `router/src/main_test.go` | `udpSessionTable` 单测 | 新建 |
| `router/src/main.go` | 增加 `-udp-port` flag、启动 UDP goroutine | 修改 |
| `router/lzc-manifest.yml` | 追加 UDP ingress + 调整 `backend_launch_command` | 修改 |
| `aipod/lzc-manifest.yml` | 同上（target 改为 192.168.50.12） | 修改 |

文件职责分工：
- `main.go`：参数解析 + TCP loop（保留现状）+ UDP goroutine 启动；不含业务逻辑
- `udp.go`：所有 UDP 转发逻辑；持锁 map 易于测试
- `main_test.go`：仅覆盖 `udp.go` 中无 I/O 的会话表操作（网络 I/O 走端到端手工验证）

---

## Phase 0 — 前置探针（已完成 2026-05-25）

### Task 1: 编写并部署 probe，搜集原始报文样本

**Files:**
- Create: `router/src/probe/main.go`
- Create: `router/probe/Makefile`
- Create: `router/probe/lzc-build.yml`
- Create: `router/probe/lzc-manifest.yml`
- Create: `router/probe/package.yml`

- [x] **Step 1: 创建探针目录骨架**

```bash
mkdir -p router/src/probe router/probe
```

- [x] **Step 2: 写探针源码 `router/src/probe/main.go`**

```go
package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
)

func main() {
	// LazyCat 的 backend health check 默认 ping http://127.0.0.1:9/，
	// 否则容器会被周期性重启。这里起一个最简 HTTP server 凑过去。
	go func() {
		http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		})
		if err := http.ListenAndServe("127.0.0.1:9", nil); err != nil {
			fmt.Printf("health http exited: %v\n", err)
		}
	}()

	pc, err := net.ListenPacket("udp", "0.0.0.0:34")
	if err != nil {
		fmt.Printf("listen failed: %v\n", err)
		os.Exit(-1)
	}
	defer pc.Close()
	fmt.Println("UDP probe listening on :34")
	buf := make([]byte, 64*1024)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			fmt.Printf("readfrom error: %v\n", err)
			continue
		}
		dump := hex.EncodeToString(buf[:n])
		if len(dump) > 96 {
			dump = dump[:96] + "..."
		}
		fmt.Printf("from=%s len=%d hex=%s\n", addr, n, dump)
	}
}
```

- [x] **Step 3: 写 `router/probe/lzc-build.yml`**

```yaml
buildscript: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C ../src build -o ../probe/dist/probe ./probe
manifest: ./lzc-manifest.yml
contentdir: ./dist
pkgout: ./
icon: ../lzc-icon.png
```

- [x] **Step 4: 写 `router/probe/lzc-manifest.yml`**

```yaml
application:
  subdomain: router-probe
  public_path:
    - /
  upstreams:
    - location: /
      backend_launch_command: /lzcapp/pkg/content/probe
      backend: http://127.0.0.1:9/
  ingress:
    - protocol: udp
      port: 34
      publish_port: 0-65535
      send_port_info: true
```

- [x] **Step 5: 写 `router/probe/package.yml`（LPK v2 要求）**

```yaml
package: com.github.moonfruit.router-probe
version: 0.0.1
name: router-probe
```

- [x] **Step 6: 写 `router/probe/Makefile`**

```makefile
all: probe.lpk

probe.lpk: lzc-* package.yml ../src/probe/main.go
	lzc-cli project build -o probe.lpk

install: probe.lpk
	lzc-cli app install probe.lpk

uninstall:
	lzc-cli app uninstall com.github.moonfruit.router-probe

clean:
	rm -rf probe.lpk dist
```

- [x] **Step 7: 构建并安装 probe lpk**

Run: `cd router/probe && make install`
Expected: `probe.lpk` 生成，`lzc-cli` 报告安装成功。

- [x] **Step 8: 从外部触发 probe**

把 `<lazycat>` 换成 LazyCat 实例可达地址（域名或 IP 均可，例如 `router-probe.<your-cloud-prefix>.heiyu.space`）：

```bash
echo -n "BBBB"     | nc -u -w1 <lazycat> 12345
echo -n "BBBB"     | nc -u -w1 <lazycat> 12345
echo -n "CCCCCCCC" | nc -u -w1 <lazycat> 60001
echo -n "CCCCCCCC" | nc -u -w1 <lazycat> 60001
```

可选对照实验（强制不同源端口）：

```bash
echo -n "X" | nc -u -p 11111 -w1 <lazycat> 22222
```

- [x] **Step 9: 抓 probe 日志**

```bash
lzc-cli app log com.github.moonfruit.router-probe
```

### Task 2: 分析探针数据并更新 spec

**Files:**
- Modify: `docs/superpowers/specs/2026-05-25-router-udp-forwarding-design.md`（已完成：§6.6 / §6.7 / §7 / §9.1 全部按实测改写）

- [x] **Step 1: 解读 hex / source port 样本**

实测结论（已写入 spec §6.6）：
- payload **无** 2 字节前缀（hex 长度与发送 payload 完全一致）
- source port **等于 publish_port**（对照实验：客户端用 `-p 11111` 发 publish_port=22222，容器看到 22222 而非 11111）
- source IP 是 LazyCat 平台 SNAT 后的代理地址，与外部客户端真实 IP 无关
- 同一外部 datagram 在容器内出现 2 份（IPv4 + IPv6 双栈复制）

- [x] **Step 2: 同步 spec §6.6 / §6.7 / §7 / §9.1**

按 §6.7 新版规则改写 §7：删除 decode/encode、监听 `udp4`、publishPort 从 clientAddr 取。

- [x] **Step 3: 卸载探针**

```bash
cd router/probe && make uninstall
```

- [x] **Step 4: 提交探针 + spec 更新**

探针源码与构建脚手架作为历史 artifact 保留（spec §9.2 要求），便于平台升级后复跑。

```bash
git add router/src/probe router/probe \
        router/src/go.mod router/src/go.sum \
        docs/superpowers/specs/2026-05-25-router-udp-forwarding-design.md \
        docs/superpowers/plans/2026-05-25-router-udp-forwarding.md
git commit -m "router: deploy UDP send_port_info probe + record actual semantics"
```

---

## Phase 1 — UDP 模块实现（TDD）

### Task 3: `udpSessionTable` —— get / put / touch / remove / reapExpired / LRU eviction

**Files:**
- Create: `router/src/udp.go`
- Create: `router/src/main_test.go`

- [ ] **Step 1: 写 `router/src/main_test.go`**

```go
package main

import (
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func TestSessionTable_PutGet(t *testing.T) {
	tbl := newUDPSessionTable()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	tbl.nowFunc = clock.Now

	k := sessionKey{client: "1.2.3.4", publishPort: 80}
	s := &udpSession{}
	tbl.put(k, s)

	got, ok := tbl.get(k)
	if !ok || got != s {
		t.Fatalf("expected to find session, got %v %v", got, ok)
	}
}

func TestSessionTable_Touch(t *testing.T) {
	tbl := newUDPSessionTable()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	tbl.nowFunc = clock.Now

	k := sessionKey{client: "a", publishPort: 1}
	s := &udpSession{}
	tbl.put(k, s)
	t0 := s.lastActivity

	clock.now = clock.now.Add(5 * time.Second)
	tbl.touch(k)
	if !s.lastActivity.After(t0) {
		t.Errorf("touch did not refresh lastActivity")
	}
}

func TestSessionTable_ReapExpired(t *testing.T) {
	tbl := newUDPSessionTable()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	tbl.nowFunc = clock.Now

	tbl.put(sessionKey{client: "a", publishPort: 1}, &udpSession{})
	clock.now = clock.now.Add(30 * time.Second)
	tbl.put(sessionKey{client: "b", publishPort: 2}, &udpSession{})

	// a is now 30+45=75s old, b is 45s old; ttl=60s → only a expires.
	clock.now = clock.now.Add(45 * time.Second)
	removed := tbl.reapExpired(60 * time.Second)
	if len(removed) != 1 {
		t.Errorf("expected 1 expired, got %d", len(removed))
	}
	if tbl.size() != 1 {
		t.Errorf("expected 1 remaining, got %d", tbl.size())
	}
}

func TestSessionTable_LRUEviction(t *testing.T) {
	tbl := newUDPSessionTable()
	clock := &fakeClock{now: time.Unix(1000, 0)}
	tbl.nowFunc = clock.Now
	tbl.maxSize = 3

	for i := 0; i < 3; i++ {
		tbl.put(sessionKey{client: "x", publishPort: uint16(i)}, &udpSession{})
		clock.now = clock.now.Add(time.Second)
	}
	_, evicted := tbl.put(sessionKey{client: "x", publishPort: 99}, &udpSession{})
	if evicted == nil {
		t.Fatal("expected eviction")
	}
	if _, ok := tbl.get(sessionKey{client: "x", publishPort: 0}); ok {
		t.Error("expected port=0 to be evicted")
	}
	if tbl.size() != 3 {
		t.Errorf("size = %d, want 3", tbl.size())
	}
}
```

- [ ] **Step 2: 跑测试，预期失败（类型未定义）**

Run: `cd router/src && go test ./... -run TestSessionTable -v`
Expected: 编译失败，`undefined: newUDPSessionTable / sessionKey / udpSession`。

- [ ] **Step 3: 写 `router/src/udp.go` 的会话表部分**

```go
package main

import (
	"net"
	"sync"
	"time"
)

const (
	udpIdleTTL      = 60 * time.Second
	udpMaxSessions  = 4096
	udpScanInterval = 10 * time.Second
	udpReadBufBytes = 64 * 1024
)

type sessionKey struct {
	client      string
	publishPort uint16
}

type udpSession struct {
	upstream     *net.UDPConn
	lastActivity time.Time
}

type udpSessionTable struct {
	mu       sync.Mutex
	sessions map[sessionKey]*udpSession
	nowFunc  func() time.Time
	maxSize  int
}

func newUDPSessionTable() *udpSessionTable {
	return &udpSessionTable{
		sessions: make(map[sessionKey]*udpSession),
		nowFunc:  time.Now,
		maxSize:  udpMaxSessions,
	}
}

func (t *udpSessionTable) get(k sessionKey) (*udpSession, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[k]
	if ok {
		s.lastActivity = t.nowFunc()
	}
	return s, ok
}

func (t *udpSessionTable) touch(k sessionKey) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[k]; ok {
		s.lastActivity = t.nowFunc()
	}
}

func (t *udpSessionTable) put(k sessionKey, s *udpSession) (evictedKey sessionKey, evicted *udpSession) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s.lastActivity = t.nowFunc()
	if len(t.sessions) >= t.maxSize {
		for kk, ss := range t.sessions {
			if evicted == nil || ss.lastActivity.Before(evicted.lastActivity) {
				evicted = ss
				evictedKey = kk
			}
		}
		if evicted != nil {
			delete(t.sessions, evictedKey)
		}
	}
	t.sessions[k] = s
	return
}

func (t *udpSessionTable) remove(k sessionKey) *udpSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.sessions[k]
	if !ok {
		return nil
	}
	delete(t.sessions, k)
	return s
}

func (t *udpSessionTable) reapExpired(ttl time.Duration) []*udpSession {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.nowFunc()
	var removed []*udpSession
	for k, s := range t.sessions {
		if now.Sub(s.lastActivity) > ttl {
			removed = append(removed, s)
			delete(t.sessions, k)
		}
	}
	return removed
}

func (t *udpSessionTable) size() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sessions)
}
```

- [ ] **Step 4: 跑测试，预期通过**

Run: `cd router/src && go test ./... -v`
Expected: 全部 4 个 test `PASS`。

- [ ] **Step 5: 提交**

```bash
git add router/src/udp.go router/src/main_test.go
git commit -m "router: add UDP session table with TTL + LRU eviction"
```

### Task 4: UDP loop wiring（`runUDP` / `udpReaper` / `handleUDPDatagram` / `reverseUDPLoop`）

**Files:**
- Modify: `router/src/udp.go`

> 实测 publish_port 由 LazyCat 平台编码在 datagram 的 source port 字段，且容器会同时通过 IPv4 + IPv6 收到两份。因此监听 `udp4` 仅取一份，publishPort 直接从 `clientAddr.(*net.UDPAddr).Port` 取，无 payload 编解码。

- [ ] **Step 1: 在 `router/src/udp.go` 追加四个网络 I/O 函数**

把以下代码贴到文件末尾，并把 `errors`/`fmt`/`strconv` 合并进文件顶部的 import 块：

```go
import (
	"errors"
	"fmt"
	"strconv"
)

func runUDP(listenPort int, targetIP string) error {
	pc, err := net.ListenPacket("udp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(listenPort)))
	if err != nil {
		return fmt.Errorf("udp listen :%d failed: %w", listenPort, err)
	}
	defer pc.Close()
	fmt.Printf("UDP TARGET IP IS %v LISTEN PORT IS:%d\n", targetIP, listenPort)

	tbl := newUDPSessionTable()
	go udpReaper(tbl)

	buf := make([]byte, udpReadBufBytes)
	for {
		n, clientAddr, err := pc.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			fmt.Printf("udp readfrom error: %v\n", err)
			continue
		}
		ua, ok := clientAddr.(*net.UDPAddr)
		if !ok || ua.Port == 0 {
			continue
		}
		// 拷贝 payload，避免 buf 在下一轮 ReadFrom 被覆盖时影响异步 upstream.Write。
		payload := make([]byte, n)
		copy(payload, buf[:n])
		handleUDPDatagram(pc, tbl, ua, payload, targetIP)
	}
}

func udpReaper(tbl *udpSessionTable) {
	ticker := time.NewTicker(udpScanInterval)
	defer ticker.Stop()
	for range ticker.C {
		for _, s := range tbl.reapExpired(udpIdleTTL) {
			s.upstream.Close()
		}
	}
}

func handleUDPDatagram(pc net.PacketConn, tbl *udpSessionTable, clientAddr *net.UDPAddr, payload []byte, targetIP string) {
	publishPort := uint16(clientAddr.Port)
	k := sessionKey{client: clientAddr.IP.String(), publishPort: publishPort}
	s, ok := tbl.get(k)
	if !ok {
		conn, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(targetIP), Port: int(publishPort)})
		if err != nil {
			fmt.Printf("udp dial %s:%d failed: %v\n", targetIP, publishPort, err)
			return
		}
		s = &udpSession{upstream: conn}
		if _, evicted := tbl.put(k, s); evicted != nil {
			evicted.upstream.Close()
		}
		go reverseUDPLoop(pc, tbl, conn, clientAddr, k)
		fmt.Printf("BEGIN UDP FORWARD %v -> %s:%d\n", clientAddr, targetIP, publishPort)
	}
	if _, err := s.upstream.Write(payload); err != nil {
		fmt.Printf("udp upstream write %v failed: %v\n", k, err)
		if removed := tbl.remove(k); removed != nil {
			removed.upstream.Close()
		}
	}
}

func reverseUDPLoop(pc net.PacketConn, tbl *udpSessionTable, upstream *net.UDPConn, clientAddr *net.UDPAddr, k sessionKey) {
	rbuf := make([]byte, udpReadBufBytes)
	for {
		n, _, err := upstream.ReadFromUDP(rbuf)
		if err != nil {
			// upstream 已被 reaper / LRU eviction / explicit remove 关闭，静默退出。
			return
		}
		if _, err := pc.WriteTo(rbuf[:n], clientAddr); err != nil {
			fmt.Printf("udp writeback %v failed: %v\n", k, err)
			if removed := tbl.remove(k); removed != nil {
				removed.upstream.Close()
			}
			return
		}
		tbl.touch(k)
	}
}
```

- [ ] **Step 2: 本地编译，确认无语法错误**

Run: `cd router/src && go build ./...`
Expected: 无输出（成功）。

- [ ] **Step 3: 跑全部单测，确认未破坏 Task 3 的测试**

Run: `cd router/src && go test ./... -v`
Expected: 全部 4 个 test `PASS`。

- [ ] **Step 4: 提交**

```bash
git add router/src/udp.go
git commit -m "router: implement UDP forwarding loop driven by source-port publish_port"
```

### Task 5: `main.go` 增加 `-udp-port` flag 并 dispatch UDP goroutine

**Files:**
- Modify: `router/src/main.go`

- [ ] **Step 1: 修改 `router/src/main.go`**

完整新版本：

```go
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"

	"gitee.com/linakesi/remotesocks"
)

func main() {
	port := flag.String("port", "10", "the local listen port")
	udpPort := flag.Int("udp-port", 0, "the local UDP listen port (0 = disabled)")
	target := flag.String("target", "", "the target ip to forward")
	flag.Parse()

	if *udpPort > 0 {
		go func() {
			if err := runUDP(*udpPort, *target); err != nil {
				fmt.Printf("UDP loop fatal: %v\n", err)
				os.Exit(-1)
			}
		}()
	}

	l, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", *port))
	if err != nil {
		fmt.Printf("Listen on %q failed: %v", *port, err)
		os.Exit(-1)
	}

	targetip := *target

	fmt.Printf("TARGET IP IS %v LISTEN PORT IS:%v\n", targetip, *port)

	for {
		conn, err := l.Accept()
		if err != nil {
			panic(err)
		}
		var port uint16
		binary.Read(conn, binary.LittleEndian, &port)

		target := net.JoinHostPort(targetip, strconv.Itoa(int(port)))
		fmt.Printf("BEGIN FORWARD %v -> %v\n", conn.LocalAddr(), target)
		go func() {
			t, err := net.Dial("tcp", target)
			if err != nil {
				fmt.Printf("dial target %v failed, err:%v\n", target, err)
				return
			}
			remotesocks.ForwardConn(context.TODO(), remotesocks.TCPBufSize, t, conn)
		}()
	}
}
```

唯一新增：`udpPort := flag.Int(...)` 与紧随其后的 `if *udpPort > 0 { go ... }` 块。其余 TCP 路径字符完全保留。

- [ ] **Step 2: 本地编译**

Run: `cd router/src && go build ./...`
Expected: 无输出。

- [ ] **Step 3: 不传 `-udp-port` 运行，验证 TCP 行为不变**

Run（开发机本地烟雾测试）：

```bash
cd router/src && go run . -port 19999 -target 127.0.0.1 &
sleep 1
echo -ne '\x50\x00' | nc -q1 127.0.0.1 19999   # 0x0050 = port 80
kill %1
```

Expected: stdout 含 `BEGIN FORWARD ... -> 127.0.0.1:80`。不需要 80 真的开着——只要看到那行日志即说明 TCP loop 解码逻辑正常。

- [ ] **Step 4: 提交**

```bash
git add router/src/main.go
git commit -m "router: add -udp-port flag dispatching UDP goroutine alongside TCP loop"
```

---

## Phase 2 — Manifest 变更

### Task 6: 更新 `router/lzc-manifest.yml` 与 `aipod/lzc-manifest.yml`

**Files:**
- Modify: `router/lzc-manifest.yml`
- Modify: `aipod/lzc-manifest.yml`

- [ ] **Step 1: `router/lzc-manifest.yml` 替换为如下内容**

```yaml
application:
  subdomain: router
  public_path:
    - /
  upstreams:
    - location: /
      backend_launch_command: /lzcapp/pkg/content/netmap -target=192.168.50.1 -port=33 -udp-port=34
      backend: http://192.168.50.1/
  ingress:
    - protocol: tcp
      port: 33
      publish_port: 0-65535
      send_port_info: true
    - protocol: udp
      port: 34
      publish_port: 0-65535
      send_port_info: true
```

- [ ] **Step 2: `aipod/lzc-manifest.yml` 替换为如下内容**

```yaml
name: 算力舱
package: com.github.moonfruit.aipod
version: 1.0.0
application:
  subdomain: aipod
  upstreams:
    - location: /
      backend_launch_command: /lzcapp/pkg/content/netmap -target=192.168.50.12 -port=33 -udp-port=34
      # backend: file:///lzcapp/pkg/content/web
      backend: http://192.168.50.12:8080/
    - location: /
      domain_prefix: dockge
      backend: http://192.168.50.12:5001/
  ingress:
    - protocol: tcp
      port: 33
      publish_port: 0-65535
      send_port_info: true
    - protocol: udp
      port: 34
      publish_port: 0-65535
      send_port_info: true
```

- [ ] **Step 3: 提交**

```bash
git add router/lzc-manifest.yml aipod/lzc-manifest.yml
git commit -m "router/aipod: add UDP ingress 0-65535 + wire -udp-port=34"
```

---

## Phase 3 — 端到端验证

> 前置：先把 `router/probe` 那个 lpk 卸载干净（Phase 0 Task 2 Step 3 已做）。若未卸载，先 `cd router/probe && make uninstall`，否则 router lpk 与 probe lpk 会在 UDP ingress 上冲突。

### Task 7: 构建并安装 router lpk + TCP 回归

**Files:** （无；只触发构建与安装）

- [ ] **Step 1: 构建 router lpk**

Run: `cd router && make clean && make`
Expected: `app.lpk` 生成；`dist/netmap` 是新的二进制。

- [ ] **Step 2: 安装**

Run: `cd router && make install`
Expected: `lzc-cli app install` 报告成功。

- [ ] **Step 3: TCP 回归——访问路由器管理页**

在浏览器或外部主机：

```bash
curl -sI http://<lazycat-host>:80/    # 192.168.50.1:80 的 HTTP
```

Expected: 收到 HTTP 响应头（具体取决于路由器型号），**而不是连接被拒绝/超时**。

- [ ] **Step 4: 看 netmap 日志确认两条 loop 都启动**

Run: `lzc-cli app log com.github.moonfruit.router | head -20`
Expected: 同时含 `TARGET IP IS 192.168.50.1 LISTEN PORT IS:33` 与 `UDP TARGET IP IS 192.168.50.1 LISTEN PORT IS:34`。

### Task 8: UDP DNS 端到端

- [ ] **Step 1: 用 dig 经 LazyCat 公网入口查 DNS**

前置：192.168.50.1 路由器需要开着 53/UDP DNS（绝大多数家用路由器默认开启）。

Run: `dig @<lazycat-host> -p 53 example.com +time=3 +tries=2`
Expected: `ANSWER SECTION` 含 example.com 的 A 记录；不是 timeout。

- [ ] **Step 2: 看 netmap 日志确认 UDP 转发触发**

Run: `lzc-cli app log com.github.moonfruit.router | grep "BEGIN UDP FORWARD"`
Expected: 至少 1 行 `BEGIN UDP FORWARD <client> -> 192.168.50.1:53`。

故障排查：
- 若 Step 1 timeout 且 Step 2 无日志：上行报文未到 netmap，可能是 `udp4` 监听漏接 IPv6 入口路径。回看 §6.6 双栈现象记录，若实测 IPv4 那份不到达只剩 IPv6，需要改成 `udp` 全栈并加去重。
- 若 Step 2 有日志但 Step 1 仍 timeout：上行通了但回包没回到客户端，可能是写回路径 SNAT 假设不对。试一次 `dig +tcp` 排查是否纯 UDP 回路问题。

### Task 9: UDP 会话稳定性（iperf3 30s）

> 前置：192.168.50.1 上跑 iperf3 server。如果路由器跑不了 iperf3，可以借 192.168.50.0/24 网段中任意机器临时跑一台，把 router lpk 的 `-target` 临时改向那台机器后重装。

- [ ] **Step 1: 跑 30s UDP 测试**

```bash
iperf3 -u -c <lazycat-host> -p 5201 -b 10M -t 30
```

Expected: 30s 内连续吞吐，丢包率 < 1%（家用宽带正常水平），无中断。

- [ ] **Step 2: 测试中观察 netmap 是否复用同一会话**

期间另一终端持续：
```bash
lzc-cli app log com.github.moonfruit.router | grep "BEGIN UDP FORWARD"
```
Expected: 仅 1 行 `BEGIN UDP FORWARD ...:5201`（说明会话表命中，没在 30s 内重建）。

### Task 10: 端口段覆盖（3 个非典型端口）

- [ ] **Step 1: 用 nc 各打几个包**

```bash
echo -n "test1234"  | nc -u -w1 <lazycat-host> 1234
echo -n "test40000" | nc -u -w1 <lazycat-host> 40000
echo -n "test60001" | nc -u -w1 <lazycat-host> 60001
```

- [ ] **Step 2: 在 192.168.50.1 上用 `tcpdump -i any -n 'udp and (port 1234 or port 40000 or port 60001)'` 抓包确认到达**

Expected: 抓到 3 个端口各至少 1 个 datagram，payload 含对应 `test1234`/`test40000`/`test60001` 字符串。

如果路由器无 tcpdump 权限：用日志 `lzc-cli app log com.github.moonfruit.router | grep "BEGIN UDP FORWARD"`，应该看到 3 行不同 publish_port 的 BEGIN。

### Task 11: 资源回收

- [ ] **Step 1: 触发若干 UDP 会话后静置 70 秒**

```bash
for p in 5000 5001 5002 5003 5004; do
  echo -n "x" | nc -u -w1 <lazycat-host> $p
done
sleep 70
```

- [ ] **Step 2: 检查 netmap 容器内 UDP socket 数**

通过 LazyCat 控制台或：

```bash
lzc-cli app exec com.github.moonfruit.router -- ss -unp
```

（如果 `lzc-cli app exec` 不可用，从 LazyCat 控制台「容器 → router → terminal」执行 `ss -unp`。）

Expected: 输出中 netmap 持有的 UDP socket 数应回落到 ~1（仅监听 :34），不应残留 5 个上行 socket。

### Task 12: aipod 烟雾测试

- [ ] **Step 1: 构建并安装 aipod lpk**

Run: `cd aipod && make clean && make && make install`
Expected: `app.lpk` 生成并安装成功。

- [ ] **Step 2: 查看 aipod 日志确认 UDP loop 启动**

```bash
lzc-cli app log com.github.moonfruit.aipod | head -20
```
Expected: 含 `UDP TARGET IP IS 192.168.50.12 LISTEN PORT IS:34`。

如果 192.168.50.12 暴露了任何 UDP 服务（如 mDNS 5353、自跑 iperf3 等）也可以做一次端到端转发验证。

### Task 13: 收尾

- [ ] **Step 1: 若实测期间发现需要调整代码或 spec，提交修正**

```bash
git status
# 视情况 git add ... && git commit -m "router: ..."
```

- [ ] **Step 2: （可选）若日后不再需要探针，删除 `router/probe/` 与 `router/src/probe/` 并提交。spec §9.2 建议保留作为升级时复跑工具，因此默认保留。**

---

## Self-Review

- [x] **Spec 覆盖**：spec §1-10 的每个要点均映射到 plan 任务：
  - §3/§4/§5 架构 → Task 4/5
  - §6 探针 → Task 1/2（已完成）
  - §7 UDP 子模块 → Task 3/4
  - §8 manifest → Task 6
  - §9 测试 → Task 3 单测；Task 7-12 端到端
  - §10 实施顺序 → 整个 plan 顺序
- [x] **占位符**：每一步都给出具体命令或完整代码，无 TBD/「类似 Task N」
- [x] **类型一致**：`sessionKey`、`udpSession`、`udpSessionTable`、`newUDPSessionTable`、`runUDP`、`udpReaper`、`handleUDPDatagram`、`reverseUDPLoop`、`udpIdleTTL`、`udpMaxSessions`、`udpScanInterval`、`udpReadBufBytes` 在测试与实现中名字完全一致
