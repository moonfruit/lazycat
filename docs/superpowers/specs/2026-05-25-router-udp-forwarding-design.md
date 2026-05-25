# router UDP 端口段转发设计

- 日期：2026-05-25
- 范围：`router/` 与 `aipod/`（两者共享 `router/src/`）
- 目标：让 `netmap` 在现有 TCP 0-65535 透传基础上，额外支持 UDP 0-65535 透传，使外网可像在内网一样访问 `192.168.50.1`（路由器）或 `192.168.50.12`（aipod 主机）的任意 UDP 服务

## 1. 背景与现状

`router/src/main.go` 当前实现了一条 TCP 路径：

- `lzc-manifest.yml` 声明 ingress `protocol: tcp` `publish_port: 0-65535` `send_port_info: true`，所有公网 TCP 端口段流量被平台导入容器 33 端口
- `send_port_info: true` 让平台在 TCP 连接建立后、用户载荷之前先送 2 字节 little-endian uint16，告知原始 `publish_port`
- `netmap` 读完前 2 字节后 dial `<target>:<publish_port>` 并用 `remotesocks.ForwardConn` 做双向 pipe

UDP 是与 TCP 对齐的能力缺口：业务上希望外部访问 DNS / IoT / 其它 UDP 服务时，与访问 TCP 服务体验一致。

## 2. 目标 / 非目标

**目标**：

- TCP 路径行为完全保留，无回归
- 新增 UDP 0-65535 透传，**目的端口端到端不变**（外部 PUB → `<target>:PUB`）
- `router` 与 `aipod` 均获益（共享 `router/src/`）
- 提供回滚开关：仅靠 manifest 去除 UDP ingress 即可关闭 UDP 而保留 TCP

**非目标**：

- IPv6（现有 TCP 同样只 listen 0.0.0.0）
- 流量统计 / Prometheus 指标
- UDP payload 修改、防火墙规则、ACL
- 多 target（仍为单 `-target` flag）
- 解决 STUN/TURN/SIP/BT-DHT 等需要 NAT 穿透或 payload 含端口的协议（双 NAT 架构本就不支持，与本设计无关）

## 3. 平台能力依据

LazyCat manifest 规范（developer.lazycat.cloud/spec/manifest.html）：

- `application.ingress[].protocol`：支持 `tcp` / `udp`
- `application.ingress[].publish_port`：支持端口范围（文档示例 `1000~50000`，仓库现存 TCP ingress 已验证 `0-65535` 写法可用）
- `application.ingress[].send_port_info`：「以 little endian 发送 uint16 类型的实际入站端口给目标端口后再进行数据转发」——**文档未明说 UDP 下的精确语义**，详见 §6 探针

## 4. 关键设计决定与权衡

### 4.1 端口透传：目的端口全程不变

经典经验法则「UDP 转发必须同端口」的根因是部分协议在 payload 内嵌端口、或回包路径预期目的端口不变。本设计令：

```
外部客户端 -> LazyCat入口:PUB -> 容器:U -> netmap -> <target>:PUB
```

**目的端口端到端 = PUB**，规避该坑。

### 4.2 源端口必然变化，由会话表保证「同源同向稳定」

外部客户端真实源端口 `CSRC` 经过两次 NAT（平台 SNAT、netmap SNAT）后，`<target>` 看到的源端口 `Q` 必然不同——这是 NAT 固有行为，TCP 路径同样如此。

为了让 `<target>` 上的服务能稳定识别「同一客户端会话」，**会话表**必须保证：相同 `(clientAddr, publishPort)` 在 idle TTL 内复用同一条上行 UDP socket（即固定 `Q`）。

### 4.3 单二进制并行 TCP + UDP

不拆 sidecar、不拆二进制。理由：

- 与现有架构最一致，部署链路（`buildscript` 交叉编译到 `dist/`，由 `backend_launch_command` 拉起）零改动
- `aipod` 复用 `router/src/` 的约定可继续维持
- TCP/UDP 两条数据路径在代码层面互不感知，崩一个不影响另一个的运行时状态；进程级致命错误仍由 LazyCat 守护重启

## 5. 架构总览

```
                ┌── tcp ingress :33 (0-65535, send_port_info) ─> netmap TCP loop ─> <target>:PUB/tcp
ext client ──> ─┤
                └── udp ingress :34 (0-65535, send_port_info) ─> netmap UDP loop ─> <target>:PUB/udp
```

- 一个 `netmap` 进程；TCP loop 与 UDP loop 各自一个 goroutine
- 容器内 TCP 监听 `:33`（沿用），UDP 监听 `:34`（新加，与 TCP 不同号便于 `ss -lnt`/`ss -lnu` 排障）
- 新增 flag：`-udp-port`（默认 0 = 不启动 UDP loop，等价于关闭功能；显式传值才启用）

## 6. 前置探针（必须先做）

LazyCat 对 UDP 下 `send_port_info` 的精确编码方式文档未写死。在正式实现 §7 之前，先做一次性观测：

### 6.1 探针位置

`router/src/probe/main.go`，临时二进制。验证完后归档或删除，不进入长期 ingress 路径。

### 6.2 探针程序

`ListenPacket("udp", ":34")`，每收到 datagram 即把 `(clientAddr, len, hex.Dump(buf[:min(32,len)]))` 输出到 stdout。

### 6.3 临时 manifest

仅含 UDP ingress：

```yaml
ingress:
  - protocol: udp
    port: 34
    publish_port: 0-65535
    send_port_info: true
```

`backend_launch_command` 指向 probe 二进制。

### 6.4 外部触发

从客户端运行：

```
echo -n "A"    | nc -u -w1 <lazycat> 5000
echo -n "BBBB" | nc -u -w1 <lazycat> 12345
echo -n "...." | nc -u -w1 <lazycat> 60001
```

各端口重复 2-3 次（同源不同包）。

### 6.5 待回答的四个问题

| # | 问题 | 影响 |
|---|---|---|
| Q1 | 是否每个 datagram 都带 2 字节前缀？ | 决定解析路径是「逐包剥头」还是「会话首包剥头」 |
| Q2 | 前缀是否就是 little-endian uint16 publish_port？ | 决定解码函数实现 |
| Q3 | 同源不同 publish_port，前缀都正常吗？ | 排除「会话首包」假说 |
| Q4 | 我们写回的包，是否需要带 2 字节前缀才能让平台还原 publish_port？ | 决定 §7 写回路径是否套前缀 |

### 6.6 结果记录（2026-05-25 实测）

| 项 | 观察结果 |
|---|---|
| Q1 / Q2 / Q3（payload 是否含 2 字节前缀） | **否**。`hex` 字段长度与发送 payload 完全相等，无前缀字节 |
| publish_port 的实际传递方式 | **编码在 datagram 的 source port 字段**：`pc.ReadFrom` 返回的 `clientAddr.(*net.UDPAddr).Port` 即等于发送方使用的 publish_port |
| source IP | LazyCat 平台 SNAT 后的内部代理 IP：IPv4 段 `172.28.0.0/16`、IPv6 ULA 段 `fc03::/16`（与 LazyCat 实例相关，不是外部客户端真实 IP） |
| Q4（写回是否需要前缀） | **否**。`pc.WriteTo(payload, clientAddr)` 原样写回即可；clientAddr 的 port 字段会被平台解释为 publish_port 并反向 SNAT 回真实客户端 |
| 双栈复制 | **同一个外部 datagram 在容器内会被复制为 IPv4 + IPv6 两份到达**（LazyCat 平台双栈代理）。如果监听 `udp`（dual-stack）会收到两次，导致 netmap 向 target 上行发包两次。最简方案：监听 `udp4`（仅 IPv4），平台仍能反向回到客户端 |

### 6.7 设计调整（基于探针结果）

§7 整体重写：
- 删除 `decodePublishPort` / `encodePublishPort`（不再需要 payload 编解码）
- UDP 监听协议从 `"udp"` 改为 `"udp4"`，避免双栈复制
- publish_port 来源从 "payload 前 2 字节" 改为 "`clientAddr.(*net.UDPAddr).Port`"
- 会话表 key 仍为 `(clientIP string, publishPort uint16)`，但 publishPort 直接取自 clientAddr
- 写回路径 `pc.WriteTo(payload, clientAddr)` 原样回写，无任何前缀处理

## 7. UDP 子模块实现

> 基于 §6.6 探针实测：publish_port 通过 source port 字段传递，监听 `udp4` 避免双栈复制。

### 7.1 入口循环

```go
pc, err := net.ListenPacket("udp4", ":34")
buf := make([]byte, 64*1024)
for {
    n, clientAddr, err := pc.ReadFrom(buf)
    if err != nil { /* log + continue 或 fatal，视错误类型 */ }
    handleDatagram(buf[:n], clientAddr, pc)
}
```

`handleDatagram` 同步分发（命中表则直接 send 到上行 conn；未命中则建立新会话）。

### 7.2 publish_port 提取

```go
ua, ok := clientAddr.(*net.UDPAddr)
if !ok || ua.Port == 0 { /* 丢弃 */ }
publishPort := uint16(ua.Port)
```

无 payload 编解码，datagram 原始内容即上行 payload。

### 7.3 会话表

```go
type sessionKey struct {
    client      string // ua.IP.String()
    publishPort uint16
}

type udpSession struct {
    upstream     *net.UDPConn
    lastActivity time.Time
}
```

- 存储：`map[sessionKey]*udpSession` + `sync.Mutex`
- **命中**：写 payload 到 `upstream`；刷新 `lastActivity`
- **未命中**：`net.DialUDP("udp", nil, &net.UDPAddr{IP: targetIP, Port: int(publishPort)})`；OS 选本地源端口 Q；存表；启动反向 goroutine：
  ```go
  for {
      n, _, err := upstream.ReadFrom(rbuf)
      if err != nil { /* upstream 已被 reaper/eviction 关闭 */ return }
      pc.WriteTo(rbuf[:n], clientAddr)   // 原样回写，无前缀
      tbl.touch(k)
  }
  ```

### 7.4 TTL 与上限

- **idle TTL**：60 秒。后台 ticker（每 10 秒扫一次）关闭并移除 `now - lastActivity > 60s` 的会话
- **硬上限**：4096 个并发会话。新建会话时若超限，按 `lastActivity` 最旧者驱逐（LRU 近似）
- **可调**：以上数字作为常量直接定义在源码顶部，必要时再做 flag

### 7.5 错误处理

| 情形 | 行为 |
|---|---|
| `clientAddr` 非 `*net.UDPAddr` 或 port=0 | 丢弃 |
| 上行 Dial 失败 | 丢弃当包，不建会话；不向客户端报错（UDP 语义） |
| 上行 Write 失败 | 丢弃当包，关闭并移除会话（下次同 key 再触发会重建） |
| 反向 Read 错误 | 静默 return（upstream 已被关闭，意味会话被 reaper/LRU 清理） |
| `ListenPacket` 失败 | 进程退出（与 TCP 一致由 LazyCat 守护重启） |

## 8. Manifest 变更

### 8.1 `router/lzc-manifest.yml`

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

### 8.2 `aipod/lzc-manifest.yml`

`backend_launch_command` 改为 `/lzcapp/pkg/content/netmap -target=192.168.50.12 -port=33 -udp-port=34`；ingress 同样追加 UDP 一条。

## 9. 测试与验证

### 9.1 单元测试（`router/src/main_test.go`，新增）

- `udpSessionTable`：put / get / touch / TTL 驱逐 / LRU 上限驱逐——用 fake clock 推进时间，不做真实网络 I/O

（探针确认 publish_port 走 source port 字段，不再需要 payload 编解码函数，因此无对应单测）

### 9.2 端到端手工验证

1. **探针构建 + 观察脚本**：作为附件同 commit 提交，便于平台升级后复跑
2. **TCP 回归**：`curl http://<lazycat>:80/` 或访问路由器管理页常用端口，确认 TCP 路径未坏
3. **UDP DNS**：`dig @<lazycat> -p 53 example.com`（依赖 `<target>` 跑 DNS）
4. **UDP 会话稳定性**：`iperf3 -u -c <lazycat> -p 5201 -t 30`（若 `<target>` 跑 iperf3 server），观察 30s 内丢包率不应飙升
5. **端口段覆盖**：选 3 个非典型端口（如 1234、40000、60001）各打几个包，确认都能到达 `<target>` 对应端口
6. **资源回收**：发包完成静置 ~60s 后 `ss -unp | grep netmap | wc -l` 应回落至 0-1

### 9.3 回滚

- UDP 路径异常但 TCP 正常：从 manifest 删除 UDP ingress 与 `-udp-port` 参数即可关闭 UDP
- 二进制层面：未传 `-udp-port` 时不启动 UDP loop，向后兼容

## 10. 实施顺序

1. 实现 §6 探针并部署，记录 §6.6 结果
2. 按 §6.7 决策更新本文档 §7 假设（若需要）
3. 实现 §7（含单元测试 §9.1）
4. 改 §8 两份 manifest
5. 本地 `make` 构建 `router/` 与 `aipod/`
6. `make install` 后跑完 §9.2 全部 6 步
7. commit & 收尾
