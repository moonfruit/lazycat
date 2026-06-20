# HAOS macvtap 冷启动自愈 Helper 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现一个 LazyCat `.lpk` 应用 `haos-helper`，开机自动加载 macvtap 内核模块并在需要时重启 lightos 实例，使 HAOS（macvtap/LAN 独立 IP 部署）在物理机冷重启后自动恢复；并提供状态页与两个手动按钮。

**Architecture:** 单个 Go 静态二进制、双模式：`-web`（router 式 `backend_launch_command`，普通 content 运行时，出状态页+按钮）与 `-agent`（显式 `services`，`network_mode: host`+`netadmin`，用 netlink 建 macvtap、net/http 调本地 pkgm API、监听 unix socket、跑开机逻辑）。前端与 agent 经 `/lzcapp/var/ipc/agent.sock` 通信，全程零宿主 TCP 端口。

**Tech Stack:** Go 1.22+（`CGO_ENABLED=0` 静态编译）、`github.com/vishvananda/netlink`（建 macvtap）、Go 标准库（net/http、unix socket、html/template）、LazyCat lpk（`lzc-cli`）、busybox 基础镜像。

**设计依据**：见 `docs/superpowers/specs/2026-06-20-haos-macvtap-helper-design.md`。所有平台机制均已实测（macvtap 内核自动加载、pkgm 7733 无鉴权 resume 重生成白名单含 238、backend_launch_command 与 services 共存且共享 /lzcapp/var）。

## Global Constraints

逐字取自 spec，所有任务隐含遵守：

- 包名：`com.github.moonfruit.haos-helper`；subdomain：`haos-helper`。
- macvtap 父接口：`enp2s0`。
- lightos 实例 id：`cloud.lazycat.lightos.entry`；实例属主 uid：`moon`。
- pkgm 本地 API 基址：`http://127.0.0.1:7733/api/app`（无鉴权）；启动=`POST /instance/resume?id=&uid=`，停止=`POST /instance/pause?id=&uid=`，状态=`GET /instance/status?id=`。
- resume 可能返回 HTTP 400（残留实例 NAT reconcile 报错，与 debian 无关）→ **必须忽略 400**。
- unix socket 路径：`/lzcapp/var/ipc/agent.sock`。
- Go 构建：`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`。
- agent 镜像：`busybox`（二进制从 `/lzcapp/pkg/content/` 取，**不自建镜像**）。
- 零宿主 TCP 端口：前端监听容器内回环端口，agent 只监听 unix socket。
- 开机逻辑（修正后、部署安全）：`/proc/devices` 无 macvtap → 加载 → 重启 lightos；已有 → no-op。
- 仓库 Go 伴侣构建惯例见 `router/`（`lzc-build.yml` buildscript + `contentdir: ./dist`）。

---

### Task 1: Go 工程脚手架 + macvtap 检测

**Files:**
- Create: `haos-helper/src/go.mod`
- Create: `haos-helper/src/macvtap.go`
- Test: `haos-helper/src/macvtap_test.go`

**Interfaces:**
- Produces: `func MacvtapLoaded(procDevices string) bool`（解析 `/proc/devices` 文本，判断是否存在 `macvtap` 字符设备）；`func MacvtapLoadedFromProc() bool`（读 `/proc/devices` 文件后调用前者）。

- [ ] **Step 1: 写失败测试**

`haos-helper/src/macvtap_test.go`:
```go
package main

import "testing"

func TestMacvtapLoaded(t *testing.T) {
	withMacvtap := "Character devices:\n  1 mem\n 10 misc\n238 macvtap\n239 aux\n"
	withoutMacvtap := "Character devices:\n  1 mem\n 10 misc\n239 aux\n"
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"present", withMacvtap, true},
		{"absent", withoutMacvtap, false},
		{"empty", "", false},
		// 不能被 "macvtap" 子串误伤其它名字
		{"substring-safe", "Character devices:\n 60 macvtapx\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MacvtapLoaded(c.in); got != c.want {
				t.Fatalf("MacvtapLoaded(%q)=%v want %v", c.name, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd haos-helper/src && go test ./... -run TestMacvtapLoaded -v`
Expected: 编译失败（`MacvtapLoaded` 未定义）/ FAIL。

- [ ] **Step 3: 写 go.mod 与最小实现**

`haos-helper/src/go.mod`:
```
module haos-helper

go 1.22
```

`haos-helper/src/macvtap.go`:
```go
package main

import (
	"bufio"
	"os"
	"strings"
)

// MacvtapLoaded 解析 /proc/devices 文本，判断字符设备表里是否存在名为 "macvtap" 的设备
// （即 macvtap 内核模块已加载、major 已注册）。
func MacvtapLoaded(procDevices string) bool {
	sc := bufio.NewScanner(strings.NewReader(procDevices))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		// 形如 "238 macvtap"：两列，第二列精确等于 macvtap
		if len(fields) == 2 && fields[1] == "macvtap" {
			return true
		}
	}
	return false
}

// MacvtapLoadedFromProc 读取宿主 /proc/devices 后判断。
func MacvtapLoadedFromProc() bool {
	b, err := os.ReadFile("/proc/devices")
	if err != nil {
		return false
	}
	return MacvtapLoaded(string(b))
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd haos-helper/src && go test ./... -run TestMacvtapLoaded -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add haos-helper/src/go.mod haos-helper/src/macvtap.go haos-helper/src/macvtap_test.go
git commit -m "feat(haos-helper): macvtap /proc/devices detection"
```

---

### Task 2: pkgm 本地 API 客户端

**Files:**
- Create: `haos-helper/src/pkgm.go`
- Test: `haos-helper/src/pkgm_test.go`

**Interfaces:**
- Consumes: 无。
- Produces:
  - `type Pkgm struct { Base string; HTTP *http.Client }`
  - `func NewPkgm() *Pkgm`（Base=`http://127.0.0.1:7733/api/app`，超时 30s）
  - `func (p *Pkgm) Status(id string) (int, error)`（GET /instance/status，返回 JSON 的 `status` 整数）
  - `func (p *Pkgm) Resume(id, uid string) error`（POST /instance/resume，**忽略 HTTP 400**，其它非 2xx 报错）
  - `func (p *Pkgm) Pause(id, uid string) error`（POST /instance/pause，忽略非 2xx，仅记录——pause 失败不致命）

- [ ] **Step 1: 写失败测试**

`haos-helper/src/pkgm_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPkgmStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/app/instance/status" || r.URL.Query().Get("id") != "x" {
			t.Errorf("unexpected req: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Write([]byte(`{"status":8,"deploy":{"deploy_id":"x"}}`))
	}))
	defer srv.Close()
	p := &Pkgm{Base: srv.URL + "/api/app", HTTP: srv.Client()}
	got, err := p.Status("x")
	if err != nil || got != 8 {
		t.Fatalf("Status=%d err=%v want 8,nil", got, err)
	}
}

func TestPkgmResumeIgnores400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/app/instance/resume" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, `{"error":"instance not found"}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	p := &Pkgm{Base: srv.URL + "/api/app", HTTP: srv.Client()}
	if err := p.Resume("cloud.lazycat.lightos.entry", "moon"); err != nil {
		t.Fatalf("Resume should ignore 400, got err=%v", err)
	}
}

func TestPkgmResumeFailsOn500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := &Pkgm{Base: srv.URL + "/api/app", HTTP: srv.Client()}
	if err := p.Resume("x", "moon"); err == nil {
		t.Fatalf("Resume should fail on 500")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd haos-helper/src && go test ./... -run TestPkgm -v`
Expected: 编译失败（`Pkgm` 未定义）。

- [ ] **Step 3: 写实现**

`haos-helper/src/pkgm.go`:
```go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type Pkgm struct {
	Base string
	HTTP *http.Client
}

func NewPkgm() *Pkgm {
	return &Pkgm{
		Base: "http://127.0.0.1:7733/api/app",
		HTTP: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Pkgm) Status(id string) (int, error) {
	u := fmt.Sprintf("%s/instance/status?id=%s", p.Base, url.QueryEscape(id))
	resp, err := p.HTTP.Get(u)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("status http %d: %s", resp.StatusCode, body)
	}
	var out struct {
		Status int `json:"status"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, fmt.Errorf("decode status: %w", err)
	}
	return out.Status, nil
}

func (p *Pkgm) post(action, id, uid string) (int, []byte, error) {
	u := fmt.Sprintf("%s/instance/%s?id=%s&uid=%s",
		p.Base, action, url.QueryEscape(id), url.QueryEscape(uid))
	resp, err := p.HTTP.Post(u, "", nil)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

// Resume 启动/恢复实例。忽略 HTTP 400（已知残留实例 NAT reconcile 报错），其它非 2xx 报错。
func (p *Pkgm) Resume(id, uid string) error {
	code, body, err := p.post("resume", id, uid)
	if err != nil {
		return err
	}
	if code == http.StatusBadRequest {
		return nil // 忽略 400
	}
	if code/100 != 2 {
		return fmt.Errorf("resume http %d: %s", code, body)
	}
	return nil
}

// Pause 停止实例。pause 失败不致命（实例本就可能未运行），仅返回错误供调用方记录。
func (p *Pkgm) Pause(id, uid string) error {
	code, body, err := p.post("pause", id, uid)
	if err != nil {
		return err
	}
	if code/100 != 2 && code != http.StatusBadRequest {
		return fmt.Errorf("pause http %d: %s", code, body)
	}
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd haos-helper/src && go test ./... -run TestPkgm -v`
Expected: PASS（3 个子测试）。

- [ ] **Step 5: 提交**

```bash
git add haos-helper/src/pkgm.go haos-helper/src/pkgm_test.go
git commit -m "feat(haos-helper): pkgm 7733 local API client"
```

---

### Task 3: IPC 协议（前端 ↔ agent，unix socket 上的 JSON 行协议）

**Files:**
- Create: `haos-helper/src/ipc.go`
- Test: `haos-helper/src/ipc_test.go`

**Interfaces:**
- Consumes: 无。
- Produces:
  - `type Request struct { Action string \`json:"action"\` }`（Action ∈ `status` / `load-macvtap` / `restart-lightos`）
  - `type Response struct { OK bool \`json:"ok"\`; MacvtapLoaded bool \`json:"macvtap_loaded"\`; InstanceStatus int \`json:"instance_status"\`; Message string \`json:"message"\` }`
  - `func ServeIPC(socketPath string, handle func(Request) Response) (io.Closer, error)`（在 unix socket 上起服务，每连接一行 JSON 请求→一行 JSON 响应）
  - `func CallIPC(socketPath string, req Request) (Response, error)`（客户端）

- [ ] **Step 1: 写失败测试**

`haos-helper/src/ipc_test.go`:
```go
package main

import (
	"path/filepath"
	"testing"
)

func TestIPCRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "agent.sock")
	srv, err := ServeIPC(sock, func(req Request) Response {
		if req.Action == "status" {
			return Response{OK: true, MacvtapLoaded: true, InstanceStatus: 8, Message: "ok"}
		}
		return Response{OK: false, Message: "unknown"}
	})
	if err != nil {
		t.Fatalf("ServeIPC: %v", err)
	}
	defer srv.Close()

	resp, err := CallIPC(sock, Request{Action: "status"})
	if err != nil {
		t.Fatalf("CallIPC: %v", err)
	}
	if !resp.OK || !resp.MacvtapLoaded || resp.InstanceStatus != 8 {
		t.Fatalf("unexpected resp: %+v", resp)
	}

	resp2, err := CallIPC(sock, Request{Action: "bogus"})
	if err != nil {
		t.Fatalf("CallIPC2: %v", err)
	}
	if resp2.OK {
		t.Fatalf("expected not-ok for bogus action")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd haos-helper/src && go test ./... -run TestIPC -v`
Expected: 编译失败（未定义）。

- [ ] **Step 3: 写实现**

`haos-helper/src/ipc.go`:
```go
package main

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
)

type Request struct {
	Action string `json:"action"`
}

type Response struct {
	OK             bool   `json:"ok"`
	MacvtapLoaded  bool   `json:"macvtap_loaded"`
	InstanceStatus int    `json:"instance_status"`
	Message        string `json:"message"`
}

// ServeIPC 在 unix socket 上提供服务：每个连接读一行 JSON Request、回一行 JSON Response。
func ServeIPC(socketPath string, handle func(Request) Response) (io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return nil, err
	}
	_ = os.Remove(socketPath) // 清理残留
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // 监听已关闭
			}
			go func(c net.Conn) {
				defer c.Close()
				dec := json.NewDecoder(bufio.NewReader(c))
				var req Request
				if err := dec.Decode(&req); err != nil {
					return
				}
				resp := handle(req)
				_ = json.NewEncoder(c).Encode(resp)
			}(conn)
		}
	}()
	return ln, nil
}

// CallIPC 连接 unix socket，发一条 Request、读一条 Response。
func CallIPC(socketPath string, req Request) (Response, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd haos-helper/src && go test ./... -run TestIPC -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add haos-helper/src/ipc.go haos-helper/src/ipc_test.go
git commit -m "feat(haos-helper): unix-socket JSON IPC between web and agent"
```

---

### Task 4: 开机逻辑（可注入依赖、纯逻辑、可单测）

**Files:**
- Create: `haos-helper/src/bootlogic.go`
- Test: `haos-helper/src/bootlogic_test.go`

**Interfaces:**
- Consumes: 无（用接口注入，便于测试）。
- Produces:
  - `type Actions interface { MacvtapLoaded() bool; LoadMacvtap() error; RestartLightos() error }`
  - `func EnsureMacvtap(a Actions) (restarted bool, err error)`：若已加载→返回 `(false, nil)`（no-op）；否则 LoadMacvtap→再查→仍未加载则报错；加载成功→RestartLightos→返回 `(true, nil)`。

- [ ] **Step 1: 写失败测试**

`haos-helper/src/bootlogic_test.go`:
```go
package main

import (
	"errors"
	"testing"
)

type fakeActions struct {
	loaded       bool
	loadCalled   int
	restartCalls int
	loadErr      error
	loadMakesIt  bool // LoadMacvtap 后是否让 MacvtapLoaded 变 true
}

func (f *fakeActions) MacvtapLoaded() bool { return f.loaded }
func (f *fakeActions) LoadMacvtap() error {
	f.loadCalled++
	if f.loadErr != nil {
		return f.loadErr
	}
	if f.loadMakesIt {
		f.loaded = true
	}
	return nil
}
func (f *fakeActions) RestartLightos() error { f.restartCalls++; return nil }

func TestEnsureMacvtap_AlreadyLoaded_NoOp(t *testing.T) {
	f := &fakeActions{loaded: true}
	restarted, err := EnsureMacvtap(f)
	if err != nil || restarted {
		t.Fatalf("got restarted=%v err=%v want false,nil", restarted, err)
	}
	if f.loadCalled != 0 || f.restartCalls != 0 {
		t.Fatalf("should be no-op: load=%d restart=%d", f.loadCalled, f.restartCalls)
	}
}

func TestEnsureMacvtap_Absent_LoadsAndRestarts(t *testing.T) {
	f := &fakeActions{loaded: false, loadMakesIt: true}
	restarted, err := EnsureMacvtap(f)
	if err != nil || !restarted {
		t.Fatalf("got restarted=%v err=%v want true,nil", restarted, err)
	}
	if f.loadCalled != 1 || f.restartCalls != 1 {
		t.Fatalf("load=%d restart=%d want 1,1", f.loadCalled, f.restartCalls)
	}
}

func TestEnsureMacvtap_LoadFails_NoRestart(t *testing.T) {
	f := &fakeActions{loaded: false, loadErr: errors.New("nope")}
	restarted, err := EnsureMacvtap(f)
	if err == nil || restarted {
		t.Fatalf("got restarted=%v err=%v want false,err", restarted, err)
	}
	if f.restartCalls != 0 {
		t.Fatalf("must not restart on load failure")
	}
}

func TestEnsureMacvtap_LoadButStillAbsent_NoRestart(t *testing.T) {
	f := &fakeActions{loaded: false, loadMakesIt: false}
	restarted, err := EnsureMacvtap(f)
	if err == nil || restarted {
		t.Fatalf("want error when still absent after load")
	}
	if f.restartCalls != 0 {
		t.Fatalf("must not restart when macvtap still absent")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd haos-helper/src && go test ./... -run TestEnsureMacvtap -v`
Expected: 编译失败（未定义）。

- [ ] **Step 3: 写实现**

`haos-helper/src/bootlogic.go`:
```go
package main

import "fmt"

// Actions 抽象出可被测试替换的副作用。
type Actions interface {
	MacvtapLoaded() bool
	LoadMacvtap() error
	RestartLightos() error
}

// EnsureMacvtap 实现修正后的开机逻辑：
//   - macvtap 已加载 → no-op（部署安全：不打断正在跑的 HAOS）
//   - 未加载 → 加载 → 复查 → 仍无则报错（不重启）→ 有则重启 lightos
// 返回 restarted 表示是否触发了 lightos 重启。
func EnsureMacvtap(a Actions) (restarted bool, err error) {
	if a.MacvtapLoaded() {
		return false, nil
	}
	if err := a.LoadMacvtap(); err != nil {
		return false, fmt.Errorf("load macvtap: %w", err)
	}
	if !a.MacvtapLoaded() {
		return false, fmt.Errorf("macvtap still not present after load attempt")
	}
	if err := a.RestartLightos(); err != nil {
		return false, fmt.Errorf("restart lightos: %w", err)
	}
	return true, nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd haos-helper/src && go test ./... -run TestEnsureMacvtap -v`
Expected: PASS（4 个子测试）。

- [ ] **Step 5: 提交**

```bash
git add haos-helper/src/bootlogic.go haos-helper/src/bootlogic_test.go
git commit -m "feat(haos-helper): boot logic (load-if-absent then restart lightos)"
```

---

### Task 5: agent 模式（netlink macvtap + 真实 Actions + lightos 重启编排 + socket 服务）

**Files:**
- Create: `haos-helper/src/agent.go`
- Modify: `haos-helper/src/go.mod`（加 netlink 依赖）
- Create: `haos-helper/src/go.sum`（`go mod tidy` 生成）

**Interfaces:**
- Consumes: `MacvtapLoadedFromProc`（Task1）、`Pkgm`（Task2）、`ServeIPC`/`Request`/`Response`（Task3）、`Actions`/`EnsureMacvtap`（Task4）。
- Produces:
  - 常量：`parentIf="enp2s0"`、`instanceID="cloud.lazycat.lightos.entry"`、`instanceUID="moon"`、`socketPath="/lzcapp/var/ipc/agent.sock"`、`probeIf="lzc-mvprobe"`、`statusRunning=8`。
  - `type RealActions struct { Pkgm *Pkgm }` 实现 `Actions`：
    - `MacvtapLoaded()` → `MacvtapLoadedFromProc()`
    - `LoadMacvtap()` → 用 netlink 在 `enp2s0` 上加一个名为 `lzc-mvprobe` 的 macvtap（bridge 模式）再删除（触发内核加载）；重试有限次直到 `/proc/devices` 出现 macvtap。
    - `RestartLightos()` → `Pkgm.Pause`（忽略错误）+ `Pkgm.Resume`（忽略 400）+ 轮询 `Pkgm.Status==statusRunning`（超时 90s）。
  - `func RunAgent()`：建 `RealActions`、启动时跑 `EnsureMacvtap`（记日志）、`ServeIPC` 处理 `status`/`load-macvtap`/`restart-lightos`，阻塞常驻。

- [ ] **Step 1: 加 netlink 依赖**

Run:
```bash
cd haos-helper/src
go get github.com/vishvananda/netlink@v1.3.0
go mod tidy
```
Expected: `go.mod` 出现 `github.com/vishvananda/netlink`，生成 `go.sum`。

- [ ] **Step 2: 写 agent 实现**

`haos-helper/src/agent.go`:
```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/vishvananda/netlink"
)

const (
	parentIf    = "enp2s0"
	instanceID  = "cloud.lazycat.lightos.entry"
	instanceUID = "moon"
	socketPath  = "/lzcapp/var/ipc/agent.sock"
	probeIf     = "lzc-mvprobe"
	statusRunning = 8 // 实测观测值：pkgm instance/status 运行态=8（未公开，随版本或变）
)

type RealActions struct {
	Pkgm *Pkgm
}

func (r *RealActions) MacvtapLoaded() bool { return MacvtapLoadedFromProc() }

// LoadMacvtap 在 enp2s0 上建一个临时 macvtap 接口再删除，触发内核加载 macvtap 模块。
// 不需要 CAP_SYS_MODULE：内核在 RTM_NEWLINK(kind=macvtap) 时自动 request_module。
func (r *RealActions) LoadMacvtap() error {
	parent, err := netlink.LinkByName(parentIf)
	if err != nil {
		return fmt.Errorf("find parent %s: %w", parentIf, err)
	}
	mvt := &netlink.Macvtap{
		Macvlan: netlink.Macvlan{
			LinkAttrs: netlink.LinkAttrs{Name: probeIf, ParentIndex: parent.Attrs().Index},
			Mode:      netlink.MACVLAN_MODE_BRIDGE,
		},
	}
	// 先清理可能的残留
	if old, e := netlink.LinkByName(probeIf); e == nil {
		_ = netlink.LinkDel(old)
	}
	if err := netlink.LinkAdd(mvt); err != nil {
		return fmt.Errorf("add macvtap probe: %w", err)
	}
	// 创建即已触发加载；删除临时接口（HAOS 会另建自己的）
	if l, e := netlink.LinkByName(probeIf); e == nil {
		_ = netlink.LinkDel(l)
	}
	// 复查 /proc/devices，最多等 5s
	for i := 0; i < 50; i++ {
		if MacvtapLoadedFromProc() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("macvtap not registered after probe")
}

// RestartLightos 重启 lightos 实例：pause(忽略错误)+resume(忽略400)+轮询至运行态。
func (r *RealActions) RestartLightos() error {
	if err := r.Pkgm.Pause(instanceID, instanceUID); err != nil {
		log.Printf("pause (ignored): %v", err)
	}
	if err := r.Pkgm.Resume(instanceID, instanceUID); err != nil {
		return fmt.Errorf("resume: %w", err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := r.Pkgm.Status(instanceID); err == nil && st == statusRunning {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("lightos did not reach running state within timeout")
}

// RunAgent 启动 agent：开机自动逻辑 + unix socket 服务（常驻）。
func RunAgent() {
	acts := &RealActions{Pkgm: NewPkgm()}

	// 开机自动逻辑（失败仅记录，不退出——保留 socket 服务以便手动干预）
	if restarted, err := EnsureMacvtap(acts); err != nil {
		log.Printf("boot EnsureMacvtap error: %v", err)
	} else {
		log.Printf("boot EnsureMacvtap ok: restarted=%v", restarted)
	}

	closer, err := ServeIPC(socketPath, func(req Request) Response {
		switch req.Action {
		case "status":
			st, _ := acts.Pkgm.Status(instanceID)
			return Response{OK: true, MacvtapLoaded: acts.MacvtapLoaded(), InstanceStatus: st, Message: "ok"}
		case "load-macvtap":
			if err := acts.LoadMacvtap(); err != nil {
				return Response{OK: false, MacvtapLoaded: acts.MacvtapLoaded(), Message: err.Error()}
			}
			return Response{OK: true, MacvtapLoaded: true, Message: "macvtap loaded"}
		case "restart-lightos":
			if err := acts.RestartLightos(); err != nil {
				return Response{OK: false, Message: err.Error()}
			}
			return Response{OK: true, Message: "lightos restarted"}
		default:
			return Response{OK: false, Message: "unknown action: " + req.Action}
		}
	})
	if err != nil {
		log.Fatalf("ServeIPC: %v", err)
	}
	defer closer.Close()
	log.Printf("agent listening on %s", socketPath)
	select {} // 常驻
}
```

- [ ] **Step 3: 编译确认（agent 副作用无法在 CI 单测，编译通过即可；逻辑已由 Task4 覆盖）**

Run: `cd haos-helper/src && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...`
Expected: 编译成功，无输出。

- [ ] **Step 4: 跑既有测试确保未回归**

Run: `cd haos-helper/src && go test ./... -v`
Expected: 全部 PASS（Task1-4 的测试）。

- [ ] **Step 5: 提交**

```bash
git add haos-helper/src/agent.go haos-helper/src/go.mod haos-helper/src/go.sum
git commit -m "feat(haos-helper): agent mode (netlink macvtap + lightos restart + ipc server)"
```

---

### Task 6: web 模式（状态页 + 两个按钮，经 socket 调 agent）

**Files:**
- Create: `haos-helper/src/web.go`
- Test: `haos-helper/src/web_test.go`

**Interfaces:**
- Consumes: `CallIPC`/`Request`/`Response`（Task3）、`socketPath`（Task5）。
- Produces:
  - `func webHandler(call func(Request) (Response, error)) http.Handler`：`GET /` 渲染状态页（含两个按钮的表单）；`POST /load-macvtap` 与 `POST /restart-lightos` 调对应 IPC、重定向回 `/`；可注入 `call` 便于测试。
  - `func RunWeb(listen string)`：用真实 `CallIPC(socketPath, …)` 起 HTTP 服务于 `listen`（容器内回环地址）。

- [ ] **Step 1: 写失败测试**

`haos-helper/src/web_test.go`:
```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebStatusPageRenders(t *testing.T) {
	h := webHandler(func(req Request) (Response, error) {
		return Response{OK: true, MacvtapLoaded: true, InstanceStatus: 8, Message: "ok"}, nil
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"macvtap", "load-macvtap", "restart-lightos"} {
		if !strings.Contains(body, want) {
			t.Fatalf("status page missing %q", want)
		}
	}
}

func TestWebButtonForwardsToAgent(t *testing.T) {
	var gotAction string
	h := webHandler(func(req Request) (Response, error) {
		gotAction = req.Action
		return Response{OK: true, Message: "done"}, nil
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/restart-lightos", nil))
	if gotAction != "restart-lightos" {
		t.Fatalf("forwarded action=%q want restart-lightos", gotAction)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected redirect, code=%d", rec.Code)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd haos-helper/src && go test ./... -run TestWeb -v`
Expected: 编译失败（未定义）。

- [ ] **Step 3: 写实现**

`haos-helper/src/web.go`:
```go
package main

import (
	"html/template"
	"log"
	"net/http"
)

var statusTmpl = template.Must(template.New("status").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>HAOS Helper</title>
<style>body{font-family:sans-serif;max-width:640px;margin:40px auto;padding:0 16px}
.k{color:#666}.v{font-weight:600}form{display:inline}button{padding:8px 14px;margin:6px 6px 0 0;cursor:pointer}</style>
</head><body>
<h1>HAOS macvtap Helper</h1>
<p><span class="k">macvtap 模块:</span> <span class="v">{{if .MacvtapLoaded}}已加载{{else}}未加载{{end}}</span></p>
<p><span class="k">lightos 实例状态码:</span> <span class="v">{{.InstanceStatus}}</span></p>
<p><span class="k">最近消息:</span> <span class="v">{{.Message}}</span></p>
<form method="post" action="load-macvtap"><button type="submit">强制加载 macvtap</button></form>
<form method="post" action="restart-lightos"><button type="submit">重启 lightos</button></form>
</body></html>`)

func webHandler(call func(Request) (Response, error)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/load-macvtap", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_, _ = call(Request{Action: "load-macvtap"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("/restart-lightos", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		_, _ = call(Request{Action: "restart-lightos"})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		resp, err := call(Request{Action: "status"})
		if err != nil {
			resp = Response{OK: false, Message: "agent 未就绪: " + err.Error()}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = statusTmpl.Execute(w, resp)
	})
	return mux
}

func RunWeb(listen string) {
	h := webHandler(func(req Request) (Response, error) {
		return CallIPC(socketPath, req)
	})
	log.Printf("web listening on %s", listen)
	log.Fatal(http.ListenAndServe(listen, h))
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd haos-helper/src && go test ./... -run TestWeb -v`
Expected: PASS（2 个子测试）。

- [ ] **Step 5: 提交**

```bash
git add haos-helper/src/web.go haos-helper/src/web_test.go
git commit -m "feat(haos-helper): web status page with manual buttons"
```

---

### Task 7: main 入口（模式分发）+ 构建配置

**Files:**
- Create: `haos-helper/src/main.go`
- Create: `haos-helper/lzc-build.yml`
- Create: `haos-helper/Makefile`
- Create: `haos-helper/.gitignore`

**Interfaces:**
- Consumes: `RunAgent`（Task5）、`RunWeb`（Task6）。
- Produces: 二进制 `haos-helper`，支持 `-agent` 与 `-web -listen <addr>`。

- [ ] **Step 1: 写 main.go**

`haos-helper/src/main.go`:
```go
package main

import (
	"flag"
	"log"
)

func main() {
	agent := flag.Bool("agent", false, "run agent mode (host network + netadmin)")
	web := flag.Bool("web", false, "run web frontend mode")
	listen := flag.String("listen", "127.0.0.1:8080", "web listen address (container-local)")
	flag.Parse()

	switch {
	case *agent:
		RunAgent()
	case *web:
		RunWeb(*listen)
	default:
		log.Fatal("specify -agent or -web")
	}
}
```

- [ ] **Step 2: 跑全部测试 + 交叉编译确认**

Run:
```bash
cd haos-helper/src && go test ./... && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/haos-helper-bin .
```
Expected: 测试全 PASS；`/tmp/haos-helper-bin` 生成。

- [ ] **Step 3: 写 lzc-build.yml**

`haos-helper/lzc-build.yml`:
```yaml
buildscript: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C src build -o ../dist/haos-helper
manifest: ./lzc-manifest.yml
contentdir: ./dist
pkgout: ./
icon: ./lzc-icon.png
```

- [ ] **Step 4: 写 Makefile 和 .gitignore**

`haos-helper/Makefile`:
```makefile
all: app.lpk

app.lpk: lzc-* src/*
	lzc-cli project build -o app.lpk

clean:
	rm -rf app.lpk dist

install: app.lpk
	lzc-cli app install app.lpk

uninstall:
	lzc-cli app uninstall `yq .package lzc-manifest.yml`
```

`haos-helper/.gitignore`:
```
app.lpk
dist/
```

- [ ] **Step 5: 提交**

```bash
git add haos-helper/src/main.go haos-helper/lzc-build.yml haos-helper/Makefile haos-helper/.gitignore
git commit -m "feat(haos-helper): main dispatch + lpk build config"
```

---

### Task 8: lpk 清单（前端 backend_launch_command + agent service）+ 元数据

**Files:**
- Create: `haos-helper/lzc-manifest.yml`
- Create: `haos-helper/package.yml`
- Create: `haos-helper/lzc-icon.png`（从仓库现有图标复制占位）

**Interfaces:**
- Consumes: 二进制路径 `/lzcapp/pkg/content/haos-helper`、socket `/lzcapp/var/ipc/agent.sock`。

- [ ] **Step 1: 写 lzc-manifest.yml**

`haos-helper/lzc-manifest.yml`:
```yaml
application:
  subdomain: haos-helper
  background_task: true
  upstreams:
    - location: /
      backend_launch_command: /lzcapp/pkg/content/haos-helper -web -listen 127.0.0.1:8080
      backend: http://127.0.0.1:8080/
services:
  agent:
    image: busybox
    network_mode: host
    netadmin: true
    command: /lzcapp/pkg/content/haos-helper -agent
```

- [ ] **Step 2: 写 package.yml**

`haos-helper/package.yml`:
```yaml
package: com.github.moonfruit.haos-helper
version: 0.0.1
name: HAOS Helper
description: 开机加载 macvtap 并在需要时重启 lightos，使 macvtap 部署的 HAOS 冷重启后自动恢复；附状态页与手动按钮。
```

- [ ] **Step 3: 复制占位图标**

Run: `cp router/lzc-icon.png haos-helper/lzc-icon.png`
Expected: 文件存在。

- [ ] **Step 4: lint 校验清单**

Run: `cd haos-helper && lzc-cli project lint`
Expected: 无 error（`netadmin`/`network_mode`/`background_task` 等可能有 warning，但不应是 unknown-at-wrong-level；确认 `services.agent.netadmin` 不再报"unknown"）。

- [ ] **Step 5: 提交**

```bash
git add haos-helper/lzc-manifest.yml haos-helper/package.yml haos-helper/lzc-icon.png
git commit -m "feat(haos-helper): lpk manifest (web frontend + host-net agent) + metadata"
```

---

### Task 9: 现有 haos/ 部署微调（haos.service enable + 限流硬化）

**Files:**
- Modify: `haos/lib/haos.service`
- Modify: `haos/install.sh`
- Modify: `haos/README.md`

**Interfaces:** 无（独立于 helper）。

- [ ] **Step 1: 看现状**

Run: `sed -n '1,80p' haos/lib/haos.service; echo '--- install ---'; cat haos/install.sh`
Expected: 看到 `[Service]` 段的 `Restart=on-failure`、`RestartSec=10s`、`StartLimitIntervalSec=10s`、`StartLimitBurst=5`（或类似），以及 install.sh 是否 enable 服务。

- [ ] **Step 2: 硬化限流（编辑 haos.service）**

把 `[Unit]` 段（systemd 中 StartLimit* 属于 `[Unit]`）的限流改为窗口远大于 `RestartSec`：将 `StartLimitIntervalSec` 设为 `600s`、`StartLimitBurst` 设为 `5`（若分散在 `[Service]` 则移到 `[Unit]`）。例如确保存在：
```ini
[Unit]
StartLimitIntervalSec=600
StartLimitBurst=5
```
（具体行按现文件位置就地修改，保持其余不变。）

- [ ] **Step 3: install.sh 确保 enable**

在 `install.sh` 安装 unit 后、`systemctl daemon-reload` 之后，确保有：
```bash
systemctl enable haos.service
```
（若已存在则不动。）

- [ ] **Step 4: README 增补部署说明**

在 `haos/README.md` 增补一节，说明：
- 安装 `haos-helper` 应用（`cd haos-helper && make install`），它负责开机加载 macvtap 并在缺失时重启 lightos；
- 实例自启策略：默认保持 debian/lightos 实例自启 ON（冷启动会先坏启动一次、helper 重启后恢复）；
- `haos.service` 已 enable + 限流硬化的目的（避免崩溃风暴）。

- [ ] **Step 5: 提交**

```bash
git add haos/lib/haos.service haos/install.sh haos/README.md
git commit -m "fix(haos): enable haos.service + harden StartLimit; document helper"
```

---

### Task 10: 集成验证（在 box 上，非 CI；逐项手测）

**Files:** 无（验证任务）。

**前置**：已登录目标 box（`lzc-cli box default` = dkmooncat）；HAOS 当前正常运行。

- [ ] **Step 1: 构建并安装 helper**

Run: `cd haos-helper && make && make install`
Expected: 生成 `app.lpk` 并安装成功。

- [ ] **Step 2: 验证两 component 都起来 + 部署安全（macvtap 已载 → no-op）**

Run（在 box 宿主 192.168.50.11）:
```bash
ssh 192.168.50.11 'find /sys/fs/cgroup -path "*haos-helper*" -name cgroup.procs | head; \
  for f in $(find /sys/fs/cgroup -path "*haos-helper*" -name cgroup.procs); do for p in $(cat $f); do tr "\0" " " </proc/$p/cmdline; echo; done; done'
```
Expected: 看到前端 `-web` 进程与 agent `-agent` 进程；因部署时 macvtap 已加载 → agent 日志应为 no-op（未重启 lightos，HAOS 未被打断——确认 HAOS qemu pid 未变）。

- [ ] **Step 3: 访问状态页**

打开 `https://haos-helper.dkmooncat.heiyu.space/`，确认显示 "macvtap 模块: 已加载"、实例状态码、两个按钮。

- [ ] **Step 4: 冷态自愈验证（核心）**

Run（在宿主，模拟冷启动；会短暂中断 HAOS，做完自动恢复）:
```bash
# 1) 停 HAOS 并卸载 macvtap 进入冷态
ssh 192.168.50.11 'INIT=$(...找 debian init...); nsenter -t $INIT -a systemctl stop haos.service; sleep 2; modprobe -r macvtap; grep macvtap /proc/devices || echo COLD_OK'
# 2) 记录当前 whitelist prog id
# 3) 重启 helper 的 agent（触发开机逻辑），或点状态页"重启 lightos"前先点"强制加载 macvtap"
#    —— 实际冷启动时 agent 启动会自动执行 EnsureMacvtap
# 4) 观察：/proc/devices 出现 macvtap → whitelist prog id 变化且含 0xee(238) → HAOS 自启打开 /dev/tapN
```
Expected: agent 自动加载 macvtap → 重启 lightos → 新 whitelist 含 238 → HAOS 恢复（qemu fd3 -> /dev/tapN）。

- [ ] **Step 5: 手动按钮验证 + 真冷重启（择机）**

- 点状态页「重启 lightos」→ 确认 lightos 重启、HAOS 恢复。
- 择机对**整机**做一次真实冷重启，开机后**不人工干预**，确认 HAOS 自动上线并拿到 LAN 独立 IP。
- 记录结果；若 §6 优化项（关实例自启）要采用，另测"关 debian 子实例自启后 resume 仍拉起 debian"。

- [ ] **Step 6: 提交验证记录（可选）**

将集成验证结果补记到 `haos-helper/README.md` 或 spec 末尾。
```bash
git add -A && git commit -m "docs(haos-helper): record integration verification results"
```

---

## 备注

- agent 的 netlink macvtap 创建、真实 pkgm 调用、unix socket 跨容器互通，均无法在 CI 单测，依赖 Task10 的 box 集成验证；纯逻辑（检测、决策、客户端解析、IPC、web 渲染）已由 Task1-4/6 的单测覆盖。
- `statusRunning=8` 为实测观测值（pkgm 未公开），若 LightOS 版本变更需在 Task10 复核。
- 若 Task10 Step2 发现前端 `backend` 指向 `127.0.0.1:8080` 无法被网关代理到（content 运行时回环可达性问题），回退方案：将前端也改为 host-net service + `routes` 经 `_outbound:<port>`（占 1 宿主端口），见 spec §10。
