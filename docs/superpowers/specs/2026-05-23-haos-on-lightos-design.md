# HAOS on LightOS Debian 设计文档

- **创建日期**：2026-05-23
- **状态**：已落地（2026-05-23 验证：HAOS 17.3 在 192.168.50.216 跑通，LAN 可达）
- **作用范围**：本仓库新增 `haos/` 子目录，部署到运行中的 LightOS debian 实例（192.168.50.13）

## Amendments (2026-05-23 实施阶段)

**持久化数据位置从 LazyCat 的 document 映射目录改到 `/var/lib/haos/`**：

设计时计划把 qcow2/UEFI vars 放在 `/lzcapp/document/VM/haos/`（通过 LightOS bind_mount
注入），优点是重建 LightOS 实例不丢数据。实施时发现这个 mount 是 LazyCat 的 idmapped
mount（uid 1000 ↔ 容器 root），LightOS 内 root 写入会触发 EOVERFLOW。由于 `haos.service`
必须以 root 跑（apt、systemd、macvtap 创建），数据只能落在 root 可写的位置。

改用 LightOS rootfs 内 `/var/lib/haos/`（btrfs subvol），权限干净；代价是重建 LightOS
实例时数据会丢，但宿主侧 `btrfs subvolume snapshot` 仍可备份（snapshot 路径在
`/lzcsys/data/appvar/cloud.lazycat.lightos.entry/var/lib/haos/`）。

下文正文与代码均已对齐到 `/var/lib/haos/`。下面 §已知 trade-off 表里"qcow2 在 document
目录"那行也已经不再适用，但保留以记录原设计意图。

## 目标

在懒猫微服 192.168.50.11 上现有的 LightOS debian 实例（macvlan、IP 192.168.50.13）内，
通过嵌套 KVM 跑一台完整的 Home Assistant OS（HAOS），让 HAOS 在 LAN 上以独立 MAC + 独立 IP
出现，能完整支持 mDNS / SSDP / DHCP / HomeKit 等所有依赖二层身份的家庭自动化协议。

## 非目标

- 不打包成 LazyCat `.lpk` 应用（HAOS 是完整 OS，无 lzc 集成可言）。
- 不引入 libvirt / Ansible / Nix 等额外管理框架，纯 systemd unit + 几个 bash 脚本。
- 不做 USB 设备（Zigbee / Z-Wave）透传，留待后续按需扩展。
- 不接入 LazyCat ingress 反代，HAOS 通过自己的 LAN IP:8123 暴露 Web UI。
- 不做监控告警（Prometheus / Grafana 等），journald + 一个可选状态脚本足够。

## 现状盘点（spec 写作时刻）

- 宿主：lzcbox-052d6a70，Debian 12 bookworm + 6.5.0 内核，CPU 支持 VMX，
  `/sys/module/kvm_intel/parameters/nested = Y`。
- LightOS debian 实例：Debian 13 trixie，runc 启动，security_level=system，
  容器内可见 `/dev/kvm`（10:232）和 `/dev/net/tun`（10:200）。
- 网络模式已切换至 macvlan，DHCP 拿到 192.168.50.13。
- 内存 31 GiB、`/lzcsys/data` 4.8 TiB 可用。
- 已验证：在 LightOS 内 `ip link add link lzc-debian name X type macvtap mode bridge` 可成功创建，
  `/dev/tap<ifindex>`（major 238）字符设备自动出现。**主路径可行**。

## 架构

```
┌─────────────────────────────────────────────────────────────────┐
│  LAN 192.168.50.0/24   (家庭路由器 192.168.50.1 = gateway/DHCP)  │
│                                                                 │
│  ┌──────────────┐    ┌─────────────────────┐    ┌────────────┐  │
│  │ 智能家居设备  │    │ 宿主 LazyCat        │    │ 其他客户端  │  │
│  │  HomeKit /   │    │  192.168.50.11      │    │  手机/PC    │  │
│  │  Zigbee Hub  │    │  (enp2s0 物理网卡)  │    │             │  │
│  └──────┬───────┘    └──────────┬──────────┘    └─────┬──────┘  │
│         │ mDNS/SSDP             │                     │         │
│         │                       │ macvlan child       │         │
│         │              ┌────────▼──────────────┐      │         │
│         │              │  LightOS debian       │      │         │
│         │              │  192.168.50.13        │◀─────┘         │
│         │              │  iface lzc-debian     │  ssh/web       │
│         │              │  (独立 MAC #A)        │                │
│         │              │                       │                │
│         │              │  ┌─────────────────┐  │                │
│         │              │  │ qemu (HAOS)     │  │                │
│         └──────────────┼──│ 192.168.50.X    │  │                │
│            L2 直连     │  │ macvtap on      │  │                │
│                        │  │ lzc-debian      │  │                │
│                        │  │ (独立 MAC #B)   │  │                │
│                        │  └─────────────────┘  │                │
│                        └───────────────────────┘                │
└─────────────────────────────────────────────────────────────────┘
```

### 核心约束

1. HAOS 在 L2 上是独立设备 —— macvtap 让它带独立 MAC，路由器看到三台机器各占一个 DHCP 租约。
2. systemd 是唯一的进程主管 —— `haos.service` 拉起 qemu；qemu 内部 HAOS 自治。
3. 持久化数据在 LightOS rootfs 内 —— qcow2 + UEFI vars 落在 `/var/lib/haos/`
   （= 宿主 btrfs subvol `/lzcsys/data/appvar/cloud.lazycat.lightos.entry/var/lib/haos`）。
   service 重启不丢；LightOS 实例重建会丢，备份依赖宿主侧 `btrfs subvolume snapshot`。
4. 没有 LazyCat 反代 —— HAOS 直接通过 `http://192.168.50.X:8123` 访问；不在 LazyCat 主屏出现。

### 已知 trade-off

| 限制 | 影响 | 处理 |
|---|---|---|
| LightOS ↔ HAOS L2 隔离 | LightOS 内 ping 不通 HAOS | 接受；管理走宿主或外部 LAN |
| 嵌套 KVM 性能 5–8% loss | HAOS 数据库写入略慢 | 可接受 |
| qcow2 在 document 目录 | 用户已确认 document 非云同步目录 | 不处理 |
| HAOS 跑独立 MAC | 首次需在路由器侧确认新设备 | README 文档化 |

## 文件布局

### 仓库内 `haos/`

```
haos/
├── README.md           # 部署说明、依赖、FAQ
├── VERSION             # HAOS 上游版本号（如 15.2）
├── update.sh           # 跟上游 release，更新 VERSION
├── install.sh          # 部署入口（在 LightOS 内跑）
├── uninstall.sh        # 卸载
└── lib/                # 部署到 LightOS 的资源
    ├── haos.conf.example
    ├── haos.service
    ├── haos-network.sh
    ├── haos-launch.sh
    ├── haos-stop.sh
    └── haos-status.sh   # 可选诊断脚本（非 systemd 调用路径，给人用）
```

顶层 = 管理动作（人执行的入口），`lib/` = 被部署的产物。与仓库现有 `router/`、`aipod/` 等风格一致。

### LightOS debian 内 —— 集中在 `/opt/haos/`

```
/opt/haos/
├── haos.conf           # 用户旋钮（systemd EnvironmentFile）
├── bin/                # 可执行脚本
│   ├── haos-network.sh
│   ├── haos-launch.sh
│   ├── haos-stop.sh
│   └── haos-status.sh
└── data → /var/lib/haos/   # symlink 到持久化数据

/etc/systemd/system/haos.service       # 唯一不在 /opt/haos/ 下的文件
                                       # systemd 只从这条路径加载 unit，避不开
```

`/opt/haos/` 子项职责：

| 子项 | 性质 | install.sh 行为 |
|---|---|---|
| `haos.conf` | 用户可改的配置 | 首次安装从 `lib/haos.conf.example` 拷贝；后续保留不覆盖 |
| `bin/` | 受版本管理的逻辑脚本 | 每次 install 都覆盖 |
| `data` | 持久化数据（软链） | install 时建 symlink，目标目录不存在则创建 |

### 持久化数据物理位置

```
/var/lib/haos/
├── haos_ova.qcow2          # 主镜像（首次下载，之后 HAOS OTA 升级）
├── OVMF_VARS.fd            # UEFI 变量
└── haos_ova-<ver>.qcow2.bak  # update.sh 拉的参考镜像（灾备）
```

## haos.conf 字段

```sh
# /opt/haos/haos.conf  — 由 systemd EnvironmentFile 读取
HAOS_IMAGE_PATH=/opt/haos/data/haos_ova.qcow2
HAOS_OVMF_VARS=/opt/haos/data/OVMF_VARS.fd

HAOS_RAM_MB=4096
HAOS_VCPUS=2

# 网络
HAOS_PARENT_IF=lzc-debian       # LightOS 内的物理父网卡名
HAOS_TAP_IF=haos-mvtap0         # macvtap 接口名
HAOS_MAC=52:54:00:0A:05:13      # 固定 MAC；52:54:00 是 KVM 保留前缀，
                                # 后三字节由 install.sh 在首次安装时基于 hostname 哈希生成（保证同网段唯一）

# 管理 socket
HAOS_QMP_SOCK=/run/haos/qmp.sock
```

`HAOS_MAC` 采用 KVM 保留前缀 `52:54:00`；后三字节由 `install.sh` 在首次安装时基于 LightOS hostname
计算 MD5 取前 6 位生成，保证同 LAN 段唯一且每次安装稳定。用户也可在 `haos.conf` 手工覆盖。

## 核心脚本契约

### `haos-network.sh`

**契约**：参数 `up` / `down`，环境变量 `HAOS_PARENT_IF`、`HAOS_TAP_IF`、`HAOS_MAC`。幂等。

**`up` 行为**：

1. 检查父接口存在且 UP。
2. macvtap 不存在 → 创建 `ip link add link $HAOS_PARENT_IF name $HAOS_TAP_IF type macvtap mode bridge`；
   已存在 → 跳过创建，对齐状态。
3. 设置 MAC：`ip link set $HAOS_TAP_IF address $HAOS_MAC`。
4. `ip link set $HAOS_TAP_IF up`。
5. `udevadm settle`（最多 5 秒）确保 `/dev/tap<ifindex>` 字符设备出现。
6. 读 `/sys/class/net/$HAOS_TAP_IF/ifindex` 写到 `/run/haos/tap.ifindex`。
7. `chgrp kvm /dev/tap<N> && chmod 660 /dev/tap<N>`（为未来非 root qemu 预留）。

**`down` 行为**：删除接口（不存在则忽略），清理 `/run/haos/tap.ifindex`。

**失败语义**：set -euo pipefail；任何步失败立即退出 1，systemd 标记 service failed。

### `haos-launch.sh`

**契约**：从环境读 `HAOS_*` 变量，从 `/run/haos/tap.ifindex` 读 tap 索引，
打开 `/dev/tap<N>` 为 fd 3 后 `exec qemu-system-x86_64`，不返回。

**关键 qemu 参数**：

| 角色 | 参数（要点） |
|---|---|
| 加速 | `-enable-kvm -cpu host -machine q35,accel=kvm` |
| CPU/内存 | `-smp $HAOS_VCPUS -m $HAOS_RAM_MB` |
| UEFI | `-drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd` + `-drive if=pflash,format=raw,file=$HAOS_OVMF_VARS` |
| 主盘 | `-drive file=$HAOS_IMAGE_PATH,if=virtio,cache=none,aio=native,discard=unmap` |
| 网卡 | `-netdev tap,id=n0,fd=3,vhost=on` + `-device virtio-net-pci,netdev=n0,mac=$HAOS_MAC` |
| 显示 | `-display none -serial unix:/run/haos/serial.sock,server,nowait` |
| 管理 | `-qmp unix:$HAOS_QMP_SOCK,server,nowait` + `-monitor unix:/run/haos/monitor.sock,server,nowait` |
| 进程 | `-name haos,process=haos` |
| 看门狗 | `-device i6300esb -action watchdog=pause`（qemu ≥ 7.0 语法；Debian 13 自带的 qemu 9.x 适用） |

**macvtap fd 传递**：在 exec 前 bash 用 `3<>/dev/tap$TAPIDX` 打开为 fd 3；qemu 用 `fd=3` 接管。
这是 macvtap+qemu 的标准接法（macvtap 字符设备走 fd，不是文件路径）。

### `haos-stop.sh`

**契约**：无参数，从环境读 `HAOS_QMP_SOCK`，通过 QMP 向 guest 发优雅关机命令。

**行为**：

1. 检查 `$HAOS_QMP_SOCK` socket 存在；不存在则退出 0（qemu 没起也算"已停"）。
2. 通过 socat 向 QMP 发送两条命令（先握手再 powerdown）：

   ```
   {"execute":"qmp_capabilities"}
   {"execute":"system_powerdown"}
   ```

3. 退出 0；不等待 guest 实际关机，由 systemd 的 `TimeoutStopSec=90s` 控制等待时间，
   超时则 systemd 用 `SIGTERM` 兜底（这正是 `KillMode=mixed` 的目的）。

**为何独立脚本而非内联 ExecStop**：systemd unit 的 `ExecStop=` 行不支持 bash herestring (`<<<`)
和管道，会被字面量传给执行程序。独立脚本让逻辑可读、可单独测试。

### `haos.service`

```ini
[Unit]
Description=Home Assistant OS (KVM in LightOS)
Documentation=https://www.home-assistant.io/installation/linux
Wants=network-online.target
After=network-online.target

[Service]
Type=exec
EnvironmentFile=/opt/haos/haos.conf
RuntimeDirectory=haos
RuntimeDirectoryMode=0750
ExecStartPre=/opt/haos/bin/haos-network.sh up
ExecStart=/opt/haos/bin/haos-launch.sh
ExecStop=/opt/haos/bin/haos-stop.sh
ExecStopPost=/opt/haos/bin/haos-network.sh down

KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=90s

Restart=on-failure
RestartSec=10s

ProtectSystem=strict
# /opt/haos 整个写入（symlink 自身用）+ symlink 的真实 target + 运行时与日志
ReadWritePaths=/opt/haos /var/lib/haos /run/haos /var/log/haos

[Install]
WantedBy=multi-user.target
```

**关键决定**：

- `Type=exec`：等到 execve 真正返回再认为启动成功。
- `ExecStop` 走 QMP `system_powerdown`：让 HAOS 优雅关机；90 秒 timeout 内不退则 SIGTERM 兜底。
  避免 SQLite 损坏。
- `Restart=on-failure` + `RestartSec=10s`：异常自动拉起，但 systemd 内置重启限频防死循环。

### `install.sh`

**前置**：root；在 LightOS 内（自检 `/etc/os-release` + `/dev/kvm`）。

**步骤**（每步幂等）：

1. 环境自检：`/dev/kvm`、KVM 模块、父网卡、Debian 13。
2. `apt update && apt install -y qemu-system-x86 ovmf qemu-utils socat`（已装跳过）。
3. 建目录：`/opt/haos/bin`、`/var/log/haos`、`/var/lib/haos/`；建 `/opt/haos/data → /var/lib/haos/` symlink。
4. 拷贝 `lib/haos-{network,launch,stop,status}.sh` → `/opt/haos/bin/`，`chmod +x`。
5. `/opt/haos/haos.conf` 不存在 → 从 `lib/haos.conf.example` 拷贝，并替换 `HAOS_MAC` 占位为
   基于 `hostname` 计算的稳定 MAC（`52:54:00` + md5(hostname) 前 6 hex）；
   存在 → 不动，diff 出新增字段提示用户。
6. 拷贝 `lib/haos.service` → `/etc/systemd/system/`，`systemctl daemon-reload`。
7. `data/haos_ova.qcow2` 不存在 → 读仓库 `VERSION` → 从 GitHub release 下载 `haos_ova-<ver>.qcow2.xz` → 解压到 `data/`。
8. `data/OVMF_VARS.fd` 不存在 → 从 `/usr/share/OVMF/OVMF_VARS_4M.fd` 拷贝。
9. `systemctl enable haos.service`；询问"是否立即启动？"。

所有副作用通过文件/包状态体现，重跑 install.sh 接着干即可。

### `uninstall.sh`

`systemctl disable --now haos.service` → 删 unit + `/opt/haos/bin/` + symlink。
`/opt/haos/haos.conf` 和 `data/` 默认保留。可选标志：

- `--purge`：连同 `haos.conf` 和整个 `data/`（qcow2 + UEFI vars）一起删，回到全新状态。
- `--purge-uefi-only`：仅删 `data/OVMF_VARS.fd`，下次启动让 HAOS 自动重建 UEFI 变量。
  用于"UEFI 变量损坏但 qcow2 数据要保留"的场景。

### `update.sh`（仓库侧）

跟 CLAUDE.md 的"版本更新约定"对齐：

1. 第一行 `command -v proxy >/dev/null && exec proxy`（越过 GitHub 限速）。
2. 调用 `find-latest-version home-assistant operating-system`。
3. `sed -i 's/^[0-9.]*$/<新版本>/' VERSION`。
4. `[[ "$1" != "-N" ]] && git diff VERSION`。

**update.sh 不下载镜像**，镜像下载是 install.sh 在目标机上做的。仓库永远很小。

## 故障矩阵

| 故障 | 表现 | 检测 | 自愈 / 恢复路径 |
|---|---|---|---|
| 父网卡不存在 | service 启不来 | ExecStartPre 第一步 | 退出；提示 LightOS 网络可能不是 macvlan |
| macvtap 创建失败 | ExecStartPre 退出 1 | journalctl | `Restart=on-failure` 10s 后再试；3 次失败 → systemd `failed` 停手 |
| `/dev/tap<N>` 缺失 | qemu 报 `Could not open` | qemu stderr | udevadm settle 等节点，5s 超时退出 |
| qcow2 损坏 | guest panic | journalctl | 从 `data/haos_ova-<ver>.qcow2.bak` 恢复，或重跑 install.sh |
| OVMF_VARS 损坏 | UEFI 启动卡死 | qemu 串口 | `uninstall.sh --purge-uefi-only` 后重建 |
| HAOS guest panic | qemu 活、guest 不响应 | QMP `query-status = paused` | watchdog 触发；管理员介入 |
| 磁盘满 | qemu I/O error | journalctl | 扩容；qemu-img info 查 qcow2 占用 |
| 嵌套 KVM 失效 | `KVM: not available` | install.sh 自检 | 查宿主 `nested` 参数 |
| LightOS 重启 | qemu 死亡 + tap 消失 | service enabled | 重启后 systemd 自动起 |
| LightOS 重建 | 实例换新 | 数据通过 bind mount 保留 | 重跑 install.sh，data/ 不动 |

## 日志去哪儿

| 日志类型 | 位置 | 看法 |
|---|---|---|
| systemd service 状态 | journald | `journalctl -u haos.service -f` |
| qemu stdout/stderr | journald | 同上 |
| HAOS guest 控制台 | `/run/haos/serial.sock` | `socat - UNIX-CONNECT:/run/haos/serial.sock` |
| HAOS guest 内部 log | guest 内 journalctl | SSH 进 HAOS 或 Web 终端 |
| install.sh | stdout + `/var/log/haos/install-<ts>.log` | 同时写 |

不引入第三方监控。

## 可选状态探针

`/opt/haos/bin/haos-status.sh`（人执行，非 systemd 路径）：

- service active 状态
- QMP `query-status` → guest 状态
- tap 接口存在且 UP
- HAOS LAN IP（从宿主 arp 表查 `HAOS_MAC`）
- HAOS `:8123` 端口响应性

## 验证清单

### 安装时自检（install.sh 内置）

| 检查项 | 失败行为 |
|---|---|
| `[ -e /dev/kvm ]` | 退出，提示 OCI device cgroup 没透传 |
| `grep -qE '^(vmx\|svm)' /proc/cpuinfo`（容器内可见） | 提示宿主 CPU 不支持 |
| `ip link show $HAOS_PARENT_IF` | 提示 LightOS 不是 macvlan |
| `command -v qemu-system-x86_64` | 自动 apt install |
| `[ -d /lzcapp/document/VM ]` | 自动创建；`/lzcapp/document` 不存在则报错 |
| 测试 macvtap 创建（临时名）并删除 | 失败回退提示 |

### 安装后人工验证（README 给步骤）

```
1. systemctl status haos.service        # active (running)
2. ls /dev/tap*                          # 有对应字符设备
3. ip -br addr show haos-mvtap0          # UP
4. 在路由器管理界面或另一台 LAN 设备 arp -a   # 看到 HAOS_MAC 拿到 IP
                                              # 注意：不能在宿主或 LightOS 内查 — macvlan 同物理网卡互不见 ARP
5. ping <HAOS_DHCP_IP>                   # 从外部 LAN 连通（不要在 LightOS 内 ping）
6. curl -kI http://<HAOS_IP>:8123        # Web 起来后 HTTP/200
7. avahi-browse -art | grep home         # mDNS 上发现 HAOS — 关键交付
8. systemctl restart haos.service        # 90 秒内重新可访问
9. 重启 LightOS                          # HAOS 自动起，data/ 不变
```

第 7 步是整个设计的关键交付。

### 灾备演练

| 演练 | 期望结果 |
|---|---|
| 删除 OVMF_VARS.fd 后重启 | 自动重建，HAOS 数据保留 |
| 删除 qcow2、重跑 install.sh | 重下并启动；HAOS 是全新实例（数据丢失 — 用户应用 HA Backup 单独备份） |
| 重启 LightOS | HAOS 自动起、所有数据保留 |
| `kill -9` qemu | systemd 10s 内重启；HAOS 视为硬复位（已知接受的代价） |

## 关键设计决定与依据

| 决定 | 原因 |
|---|---|
| qemu 用 fd 形式接 `/dev/tap<N>` | macvtap 字符设备走 fd，不是文件路径 — 标准接法 |
| `cache=none,aio=native` | bypass guest 双层缓冲，aio native 减少线程切换；HAOS 数据库 fsync 友好 |
| `discard=unmap` | HAOS 内 fstrim 能回收 qcow2 空洞 |
| ExecStop 走 QMP 而非 SIGTERM | 避免 SQLite 损坏 |
| install.sh 下镜像、update.sh 不下 | 镜像 ~400 MB，不该进 git |
| `/run/haos/` 由 `RuntimeDirectory=haos` 创建 | systemd 自动管理生命周期 |
| 不加 LazyCat ingress 反代 | HAOS 自带登录 + Web，加反代等于加复杂度 |
| 不打包 `.lpk` | HAOS 是完整 OS，无 lzc 集成可言 |

## 实施前提（spec 通过后再执行）

1. LightOS debian 实例已切到 macvlan 网络模式（已完成）。
2. 父接口名 `lzc-debian` 在 LightOS 内可见（已验证）。
3. macvtap-on-macvlan 可创建（已验证）。
4. 宿主开启嵌套 KVM（已验证）。
5. 用户确认 `/lzcapp/document/` 非云同步目录（已确认）。
