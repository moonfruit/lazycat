# HAOS 独立 LPK 的网络与备份实测

2026-11-24，在 dkmooncat 上安装了独立测试包 `com.github.moonfruit.haosnetprobe`。访客使用 Alpine 3.23.4 的 NoCloud x86-64 UEFI QCOW2 镜像，磁盘与现有 HAOS 完全分开。测试覆盖网络传输、懒猫实际入口和备份文件恢复。网络部分使用 Alpine；后续备份部分使用官方 HAOS 18.4。HomeKit 验证覆盖设备发现。

## 实际拓扑

| 端点 | 配置 |
|---|---|
| 懒猫宿主 | `192.168.50.11/24`，物理口 `enp2s0`。宿主地址、路由、防火墙、账号和存储服务保持原配置 |
| LPK 的 VM 服务容器 | 私有网络命名空间，保留应用默认网络。通过包内 Compose Override 声明第二个 Docker macvlan 网络，parent 为 `enp2s0`。启用 `kvm_accel` 和 `netadmin` |
| 访客 LAN 网卡 | virtio-net → QEMU VDE → `pcap://eth1`（LPK 的专用 macvlan 子接口）。子接口和访客使用相同 MAC `52:54:00:b7:2c:91`，容器移除该接口自己的 IPv4 地址，由访客 DHCP 取得 `192.168.50.18` |
| 访客管理网卡 | 第二个 virtio-net → QEMU user 网络。访客 `10.0.2.15`，网关 `10.0.2.2`。默认路由走 LAN，访问宿主的例外路由单独配置在访客内 |
| 独立观察机 | 局域网内另一台物理设备，`eno1 = 192.168.50.12` |

QEMU 10.0.13 使用 q35、OVMF、virtio 磁盘、2 vCPU 和 768 MiB。QMP 的 `query-kvm` 返回 `present=true, enabled=true`。原始镜像已与 [Alpine 官方发行文件](https://dl-cdn.alpinelinux.org/alpine/v3.23/releases/cloud/)的 SHA-512 对照。

## 网络结果

| 测试 | 观察 |
|---|---|
| 默认 LPK 打开 macvtap | 容器内在专用接口上成功创建 macvtap，取得 ifindex 2、字符设备号 `238:2`。打开 `/dev/tap2` 得到 `ENOENT`。在同一测试容器中创建相同设备号的临时节点后，打开得到 `EPERM`。没有为容器额外放行 macvtap 设备 |
| DHCP 与局域网单播 | 访客两次启动都取得 `192.168.50.18/24`。观察机通过该地址访问访客 HTTP 服务成功，访客记录对端为 `192.168.50.12` |
| IPv4 广播 | 访客和观察机轮流向 `192.168.50.255` 发送有独立标记的 UDP 广播。两个方向均在另一端收到，发送方也收到对方的单播确认 |
| SSDP | 从访客 LAN 网卡和观察机各发一次 `M-SEARCH ssdp:all`。两边都收到 `.1、.3、.81、.82、.88` 五台设备的 HTTP 200 SSDP 回答，包括网关、MediaRenderer 和 DIAL 服务 |
| mDNS 入站发现 | 在同一轮 12 秒浏览中，访客和观察机都解析出四个真实的 HomeKit 服务端点，位于 `.62、.64、.179、.224`。访客还能连到 `.224:80` |
| mDNS 对外公告 | 访客用 Avahi 发布独立的 `_hqlab._tcp` 服务和 TXT 路径。观察机在 `eno1` 上解析到 `haos-lpk-lab.local → 192.168.50.18:8123`，并成功请求该服务 |

同宿主旧 HAOS 是一个已测到的例外。观察机能解析 `192.168.50.15:8123` 的 `_home-assistant._tcp` 和该地址上的四个 HomeKit 端口，访客的 Avahi 同轮没有解析到它们。访客直接连旧 HAOS 的 8123 超时。仅在访客内为 `.15/32` 临时指定家庭路由器 `.1` 为下一跳后，HTTP 返回 200，直接发往 `.15:5353` 的 PTR 查询也收到了旧 HAOS 的回答，但 Avahi 仍未自动发现它。该临时路由已撤销。

## 懒猫入口和宿主存储

反代请求实际经过以下路径，所有回环监听都在 LPK 服务容器的网络命名空间内。

```text
懒猫 HTTPS 应用域名 → application.upstreams → 应用容器内的 HTTP 反代
→ /lzcapp/var 中与 VM 服务共享的 Unix socket
→ VM 服务容器的 127.0.0.1:29125 → QEMU hostfwd → 访客 10.0.2.15:8123
```

通过真实应用域名请求一次性测试路径，收到访客生成的 HTTP 内容。WebSocket 客户端校验了 101 握手、Sec-WebSocket-Accept 和访客发出的带标记文本帧。通过反代访问时访客看到的来源为 `10.0.2.2`。将 QEMU 改为测试包的服务启动命令并重新安装启动后，QEMU 是服务容器 PID 1，HTTP、WebSocket 和 LAN 直连复测通过。

备份样本是访客生成的 tar.gz，含一份合成 `configuration.yaml` 和 32 KiB 随机数据。原始样本、宿主落盘文件和访客读回文件的 SHA-256 均为 `d9f2c80e13009e5114841d5abdc6bedc273303839995282da101c5fb4f7fd669`。

| 路径 | 验证程度 |
|---|---|
| 访客 → 宿主现有 SMB | 走 LAN 网卡直接连 `.11:445` 超时。访客单独把 `192.168.50.11/32` 指向管理网关 `10.0.2.2` 后，用 `smbclient` 匿名协商并列举共享成功。生产共享没有执行写入测试 |
| 访客 → 宿主现有 SFTP | 同样的访客路由使 `.11:22` 可达。访客 OpenSSH SFTP 客户端完成 put/get，文件写在宿主获准的 `/tmp` 测试目录，双方校验一致。SSH 使用开发机现有身份完成认证，TCP 连接经过访客。仅 SFTP 子系统流交给访客，没有在访客中配置生产私钥，也没有验证 HAOS 自主认证 |
| 访客 → LPK 的 SMB → 宿主磁盘 | LPK 的 Samba 仅在其自身回环端口监听。QEMU 的 `guestfwd` 将访客所见的 `10.0.2.5:445` 接到该服务。访客用专用账号使用 Linux cifs 客户端以 SMB 3.1.1 挂载，完成写入、卸载、重新挂载和恢复。两个还原文件与原件逐字节一致 |
| 宿主指定目录 → LPK bind → SMB → 访客 | 在后一版测试 LPK 中，将获准的宿主 `/tmp` 专用子目录绑定到 LPK，单独导出 SMB 共享。访客把上一次运行留在 `/lzcapp/var` 的备份复制进去，卸载再挂载，读出原始 YAML。宿主从绑定源独立算出相同 SHA-256。正式使用时应选持久化的备份目录 |
| 宿主应用数据盘 → LPK → virtio-9p → 访客 | QEMU 导出 `/lzcapp/var/probe/backups`。Alpine 访客挂载 9p2000.L 后写入、卸载、重新挂载和读回，宿主和访客校验一致。该轮使用 Alpine；下文使用官方 HAOS 补测 9p 挂载和 Supervisor 接入 |

正式 LPK 的主备份目标是用户在懒猫文件管理中可以单独取出的备份文件。按当前懒猫开发文档，新包可声明 `document.private`，以 `/lzcapp/documents/<uid>/备份` 为源，只把该子目录通过 QEMU 文件系统设备导出给 HAOS。虚拟机系统盘和 UEFI 变量属于应用运行数据，放在 `/lzcapp/var`。如果要用已有的用户目录，按实际授权和挂载路径选目录。这台设备上现有的 qBittorrent 容器将用户文稿以读写方式挂载到 `/lzcapp/document`。[懒猫文件访问文档](https://developer.lazycat.cloud/advanced-file)

宿主 SMB 和 WebDAV 是独立的可选目标。HAOS 的“设置 → 系统 → 存储”支持将 SMB 共享设为备份位置；“设置 → 设备与服务”中的 WebDAV 集成也支持备份。懒猫应用容器对 `http://192.168.50.11:5006/dav/` 的 OPTIONS 和 PROPFIND（Depth 0）请求收到了 HTTP 401 和 Basic 认证挑战，HTTPS 文件应用入口 `https://file.dkmooncat.heiyu.space/dav/` 也返回 401。该检查只验证到认证入口。测试没有向生产 SMB 或 WebDAV 共享写文件。[HAOS 网络存储](https://www.home-assistant.io/common-tasks/os/#network-storage) · [HA WebDAV 集成](https://www.home-assistant.io/integrations/webdav/)

商店现有 HAOS 包只作资料对照。它的说明给出了从宿主 8123 访问的方式，本次网络发现与反代结果全部来自独立测试包。[商店 HAOS 包](https://lazycat.cloud/appstore/detail/ink.akawa.ety001.haos)

## 官方 HAOS 的目录直通验证

独立测试 LPK 通过 KVM 启动官方 `haos_ova-18.4.qcow2.xz`。发布资产的 SHA-256 为 `756ec485b1b153099ca53e88a5eebd63a801d56b791afe5c6d63ef94a35dc02d`。在开发机和懒猫上分别校验了下载文件。可写导出目录位于宿主本轮专用的 `/tmp` 子目录，经 LPK 的 bind 进入 QEMU。该包的实际默认挂载是 `/lzcsys/data/document → /lzcapp/document`，用户目录在 `/lzcapp/document/moon`。[HAOS 18.4 发行资产](https://github.com/home-assistant/operating-system/releases/tag/18.4)

QEMU 使用 `-virtfs local,path=/run/hq/backups,mount_tag=hqbackup,security_model=mapped-xattr,id=hostdir`。以下挂载项经 QEMU SMBIOS Type 11 的 `io.systemd.credential.binary:fstab.extra` 传入，每次启动由 systemd 生成挂载单元，排在 Docker 服务之前。这样 Supervisor 的本地备份目录直接落在导出的文件系统上。[systemd 启动凭据](https://github.com/systemd/systemd/blob/main/docs/CREDENTIALS.md) · [fstab.extra](https://www.freedesktop.org/software/systemd/man/256/systemd-fstab-generator.html)

```fstab
hqbackup /mnt/data/supervisor/backup 9p rw,trans=virtio,version=9p2000.L,msize=262144,x-systemd.before=docker.service 0 0
```

- 懒猫上的 HAOS 报告挂载类型为 `9p`，来源为 `hqbackup`。Supervisor 通过 `ha backups new --folders share` 创建本地备份，宿主目录直接出现 `lpk-direct.tar`。双方算出的 SHA-256 均为 `c80a8d5a226c70726a5e2f97b839ed8c374fab810a13cb08e0cd329256c78cde`。改写分享目录内的测试文件后，用 `ha backups restore` 恢复，读回原来的标记。
- 实机上的 Home Assistant Core 2026.11.3 就绪后，给测试配置加了一行标记，用 `ha backups new` 创建完整备份 `166747ca`（`lpk-config.tar`）。宿主文件和 HAOS 文件的 SHA-256 都是 `8d6991b1720c457e7f6626fb24f1d6b0e302d17ee9ee0a216f2a78d3cd8417cb`。宿主从中解出 `homeassistant.tar.gz → data/configuration.yaml`，其 SHA-256 为 `46cd5dbbf2b21ed632eced710eadbf042bd4d55bd02dd10748360456f56ba5d2`，与创建备份前的访客原件一致。
- 改写访客测试配置并关机，退出 QEMU 再启动。HAOS 的 boot ID 已改变，9p 在原位置自动挂载，备份 SHA-256 相同，Supervisor 通过 `ha backups info 166747ca` 找到完整的本地备份。复测新增只读设备后，OVMF 曾先尝试 PXE；给系统盘显式指定 `bootindex=1` 后正常冷启动。增加设备也曾使管理网卡从 `enp0s1` 变为 `enp0s2`，旧的 NetworkManager 配置无法匹配。将管理网卡固定在原 PCI 插槽 `0x1`、磁盘固定在 `0x2` 后再次冷启动，访客的管理路由、Supervisor 的联网检测和备份挂载都正常。此时用完整备份恢复 Home Assistant 和 share，`configuration.yaml` 从修改后的 `cfdad80a…` 恢复至原 SHA-256 `46cd5dbb…`，分享目录的测试标记也正确。
- 另将此测试 LPK 已有的 `/lzcapp/document/moon/Downloads` 作为只读 QEMU 导出。HAOS 用第二个挂载标签成功挂载，`findmnt` 显示 `9p ro`。只读取挂载点元数据，未列举或写入用户文件。真实文稿目录位于 Btrfs，可写实测源在获准的宿主 `/tmp`。

- HAOS 通过管理网卡向 `http://192.168.50.11:5006/dav/` 发出的 PROPFIND 收到 401 和 Basic 认证挑战。HTTPS 文件域名只有懒猫覆盖网络的 IPv6 地址，访客直连超时。把 TLS 连接经管理网关转到 LPK 内的回环 TCP 转发器，保留原始域名、SNI 和证书校验，访客收到了 HTTPS 401。该测试用 curl 指定连接地址；正式使用原域名需要给访客提供对应的解析或路由。访客用 `mount.cifs` 以 SMB 3.1.1 尝试匿名只读挂载宿主 `SharedFiles`，服务拒绝了匿名访问。本轮没有使用生产凭据。
- 开发机上同一官方 HAOS 的对照虚拟机使用同样的 QEMU 和 systemd 机制，Home Assistant Core 2026.11.3 启动后生成了完整备份。开发机直接从 TAR 内的 `homeassistant.tar.gz` 取出 `data/configuration.yaml`，与访客的 SHA-256 一致。修改配置后用 Supervisor 从该备份恢复 Home Assistant，配置文件恢复为原来的 SHA-256。

## 宿主、Tailscale 和固定 MAC

这轮使用 Alpine UEFI QCOW2 访客，沿用前述双网卡布局。Mac 的在线 Tailscale 节点是 sing-box 内的 `100.76.178.3`。懒猫的 Tailscale LPK 与宿主使用同一网络命名空间，节点 `100.98.10.37` 是 `192.168.50.0/24` 的主子网路由器，已启用 SNAT。实测以 `curl --noproxy '*'` 发起；在懒猫 `tailscale0` 仅记录本轮端点的 TCP 握手元数据，确认流量确实来自这台 Mac。[Tailscale 子网路由器](https://tailscale.com/docs/features/subnet-routers) · [Docker macvlan 的宿主互通限制](https://docs.docker.com/engine/network/drivers/macvlan/)

| 发起方 | 目标 | 实测 |
|---|---|---|
| Mac，经 Tailscale | 家庭路由器 `192.168.50.1:80` | HTTP 200 |
| Mac，经 Tailscale | 现有 HAOS `192.168.50.15:8123`、测试 VM `192.168.50.18:8123` | 两者都超时，各自的 TCP SYN 均已到达懒猫 `tailscale0` |
| 独立物理观察机 `.12` | 同样的 `.15:8123`、`.18:8123` | 两者均返回 HTTP 200。测试 VM 记录对端是 `.12`，本地地址是 `.18` |
| 懒猫宿主 | 测试 VM `192.168.50.18:8123` | 连接失败；内核经 `enp2s0` 发出，IPv4 邻居项进入 `FAILED` |
| Mac，经 Tailscale | 懒猫的 Tailnet 地址 `100.98.10.37:39128` | HTTP 200，读到同一个访客生成的测试标记 |
| 懒猫宿主 | `127.0.0.1:39128`，或测试服务容器的 `172.28.11.66:29125` | 两者均得到同一标记。访客记录对端为管理网关 `10.0.2.2` |
| 测试 VM | 宿主 `192.168.50.11:5006/dav/` | 访客内 `/32` 指向管理网关，PROPFIND 返回 401 和 Basic 认证挑战 |

测试包只在宿主回环和 Tailnet IP 发布 TCP 39128。请求进入 VM 服务容器的桥接接口后，由容器内的 TCP 转发器连到回环上的 QEMU `hostfwd`，再到访客管理网卡 `10.0.2.15:8123`。仅把 QEMU 的 `hostfwd` 绑在桥接 IP 上时，连接后收不到 HTTP 响应；加入上述回环转发后，实测两端均通过。测试端口随包卸载。

网络测试 VM 保留磁盘和 UEFI 变量后关机，再启动新的 QEMU 进程。`query-rx-filter` 在两次启动都读到 LAN `52:54:00:b7:2c:91`、管理网卡 `52:54:00:b7:2c:92`。外部观察机确认两次的 LAN 邻居 MAC 相同，冷启动后仍可 ping 通 `192.168.50.18`。现有 HAOS 的 `/opt/haos/haos.conf` 和运行中 QEMU 的 LAN MAC 都是 `52:54:00:c7:22:46`，观察机对 `.15` 的邻居项亦相符。首次安装生成 MAC，后续启动复用配置。正式 LPK 可将每个实例各网卡的 MAC 持久化到 `/lzcapp/var`；固定 DHCP 地址可在路由器中按 LAN MAC 保留。

## 收尾核对

已卸载测试 LPK，回收其容器、网络、宿主上的专用镜像、访客磁盘、测试账号凭据及宿主 `/tmp` 暂存目录。平台卸载时会保留应用数据目录，因此另外通过测试 LPK 清除了它创建的 `probe/` 子目录。宿主 `enp2s0` 的 IPv4 和默认路由与测试前一致。原 LightOS 内 `haos.service` 仍为 active，QEMU PID 仍为 1587，独立观察机访问原 HAOS 的 8123 返回 HTTP 200。后续两轮测试包和宿主专用 `/tmp` 目录均已回收，两个测试地址上的 39128 监听也已消失。Tailscale LPK 仍在运行。

## 2026-11-26 · QEMU 直接转发与应用网段信任

在 dkmooncat 上用两个独立包名验证了同一块测试磁盘。镜像为官方 HAOS 18.4，Home Assistant Core 为 2026.11.3，服务镜像为 `ghcr.io/moonfruit/qemu:10.0.13-r1`。下文的 host 是懒猫宿主机，app 是懒猫应用主容器，qemu 是运行 QEMU 的服务容器，haos 是虚拟机。

懒猫域名的路径为 `app → qemu 的桥接 IPv4:8123 → haos 管理网卡 10.0.2.15:80`。QEMU 自己在桥接地址监听并执行 `hostfwd`。测试包移除了 Go 的 TCP 中转和回环端口 18123。qemu 启动时从自身桥接接口读取 IPv4 地址与前缀长度，计算应用网段。haos 将到这个网段的流量经第二块网卡送至 `10.0.2.2`，默认路由仍经 LAN 网关 `192.168.50.1`。

| 启动场景 | 应用网段 | app / qemu / 桥网关 | haos 中的 `trusted_proxies` |
|---|---|---|---|
| 首次启动 | `172.28.11.128/26` | `.131 / .130 / .129` | 仅 `172.28.11.128/26` |
| 整套 LPK 停止后启动 | `172.28.11.128/26` | `.131 / .130 / .129` | 同上，HTTP 配置文件 SHA-256 完全一致 |
| 同一磁盘由另一个测试包启动 | `172.28.11.192/26` | `.195 / .194 / .193` | 仅 `172.28.11.192/26`，其余 HTTP 配置数据逐项相同 |

三次启动的 haos boot ID 均不同，数据分区 UUID、LAN MAC 和 LAN 地址 `192.168.50.146` 均相同。首次启动在 haos 内部 Docker 启动前初始化 Core 的 HTTP 配置并启用 `use_x_forwarded_for`。再次启动时按本次网段生成管理网卡路由，仅在网段变化且旧信任项仍存在时替换该项。换网段后，haos 的路由表和信任列表中都已移除旧网段。

| 实际请求 | 实测结果 |
|---|---|
| 客户端经懒猫 HTTPS 域名进入 app | 三次启动均能读取 Home Assistant 的 `/manifest.json`（HTTP 200）。`/api/websocket` 均完成 101 握手并收到 Core 2026.11.3 的 `auth_required`。WebSocket 保持连接时，haos 上观测到的域名入口 TCP 对端为 app 的 `.131` 或 `.195` |
| app 直接请求 `qemu:8123` | 含转发头的 HTTP 请求返回 200，WebSocket 通过 |
| host 直接请求 qemu 桥接地址的 8123 | HTTP 200，WebSocket 通过。haos 看到的 TCP 对端为桥网关 `.129` 或 `.193` |
| 独立物理机 `192.168.50.12` 直接请求 haos 的 LAN 地址 | 普通 HTTP 返回 200，WebSocket 通过。携带转发头的对照请求返回 400，验证了代理信任范围 |

在第一个网段，临时只撤掉 haos 内的 `172.28.11.128/26 via 10.0.2.2`，同时在两块虚拟网卡抓取握手包头。来自 app 的 SYN 从管理网卡到达，haos 发出的 SYN-ACK 却从 LAN 网卡离开，app 等待 HTTP 响应超时。恢复原路由后，新连接的 SYN 与 SYN-ACK 都经过管理网卡，同一请求返回 200。整个对照过程中代理信任配置保持一致。

此轮未重启 host。用户启用的 `192.168.50.11:8123` 转发规则指向正式包的 `qemu.com.github.moonfruit.haos.lzcapp:8123`，正式包处于已卸载状态，本轮跳过经该规则的 LAN、Tailscale 和手机端验收。两套测试 LPK 都只使用平台创建的应用网络和 host 的专用 `/tmp` 目录。HA 支持将可设置转发头的代理指定为 CIDR 网络地址。[Home Assistant HTTP 文档](https://www.home-assistant.io/integrations/http/#reverse-proxies)

收尾时已核对两个测试包与正式包均为 `NotInstalled`，测试容器、测试网络及 host 上的专用临时目录均已清理。原有 8123 规则仍为启用状态。
