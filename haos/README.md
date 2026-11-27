# Home Assistant OS 普通 LPK

在 x86-64 懒猫上用 KVM 启动官方 HAOS OVA。HAOS 从家庭路由器获得独立的局域网 IP，可以通过 mDNS 和 SSDP 发现其他物理设备。应用入口通过管理网卡转发 HTTP 和 WebSocket。旧版 LightOS 手工部署方式见 [legacy](legacy/README.md)。

## 构建和安装

构建机需要 `lzc-cli`、Go 1.26 或更新版本、Make、`curl`、`shasum`、`unzip` 和 `jq`。`make uninstall` 另需 `yq` 读取包名。LPK 携带一个静态 Go 程序 `haos-vm`、插件归档 `hass-oidc-auth.zip` 和许可证 `hass-oidc-auth.LICENSE.md`。懒猫从 GHCR 拉取 [manifest](lzc-manifest.yml) 指定的 x86-64 QEMU 镜像。目标设备需要支持 KVM 的 x86-64 CPU。默认分配 2 个 vCPU、4 GiB 内存，初始系统盘虚拟容量为 32 GiB，实际占用随使用增长。

| 执行位置 | 命令 | 用途 |
|---|---|---|
| qemu | `haos-vm` 或 `haos-vm vm` | 准备网络和磁盘，启动虚拟机并处理停机 |
| haos | `haos-vm oidc` | 初装 OIDC 插件并核对当前部署的凭据 |
| haos | `haos-vm oidc --watch` | 随 Docker 启动核对一次，随后监听恢复备份产生的凭据变化 |
| haos | `haos-vm oidc --recover` | 在 Docker 启动前回滚上次中断的配置写入 |

在仓库根运行 `./docker/qemu/update.sh` 可更新 QEMU 上游版本，根目录的 `./update.sh` 也会调用它。CI 每周一重新拉取 `debian:trixie-slim`，安装与上游版本匹配的最新 Debian 修订。CI 对比最终文件系统和运行配置，有实质变化时才发布 `<QEMU版本>-rN`，同时更新上游版本标签和 `latest`。文件时间、镜像构建时间和归档顺序不计入比较。[更新脚本](../docker/qemu/update.sh) · [GitHub Actions 工作流](../.github/workflows/build-qemu.yml)

在本目录执行 `make update` 可以更新初装镜像、QEMU 和新实例使用的 OIDC 插件版本。脚本读取 HAOS 稳定渠道中 OVA 的版本，核对对应 QCOW2 压缩包的 SHA-256，选择 GHCR 已发布的最新 QEMU 修订，并记录插件正式发布包的版本和 SHA-256。`make` 在构建时下载并校验插件，有有效缓存时直接复用。LPK 版本由 `package.yml` 单独管理。[LPK 更新脚本](update.sh)

构建并安装：

```bash
make
lzc-cli app install app.lpk
```

应用使用 host 的有线网口 `enp2s0` 接入局域网，接口名固定在 `lzc-build.yml` 中。

安装时选择文稿所属用户，按需调整 CPU 和内存。`HostIP` 留空即可。每次启动应用时，启动器根据 macvlan 的父接口索引和网络命名空间，通过 Linux rtnetlink 读取物理网口的主 IPv4。网口有多个主 IPv4 或查询失败时，应用记录原因，HAOS 继续启动。手填地址会覆盖自动识别结果。此参数仅用于访客回连宿主。应用包名为 `com.github.moonfruit.haos`，子域名为 `haos`。首次进入 Home Assistant 后，建立新的家庭或使用 HA 的备份恢复向导。

### 预先下载 HAOS 镜像

手动预下载和应用自动下载共用所选用户应用文稿下的 `Images/haos_ova-18.4.qcow2.xz`。要跳过下载，在首次启动前创建 `Images/`，将官方压缩包原样放入。曾按旧版说明将文件放在文稿根目录或 `/lzcapp/cache/` 的用户，可将其移入 `Images/` 供下次初始化使用。[下载 haos_ova-18.4.qcow2.xz](https://github.com/home-assistant/operating-system/releases/download/18.4/haos_ova-18.4.qcow2.xz)

以 `moon` 为例，LZCOS 1.6.3 的宿主文件是 `/lzcsys/data/document/moon/.apps/com.github.moonfruit.haos/Images/haos_ova-18.4.qcow2.xz`，在 LPK 内对应 `/lzcapp/documents/moon/Images/haos_ova-18.4.qcow2.xz`。已记录为旧布局的镜像位置是用户文稿下的 `HAOS/Images/`。与 `Images/` 并列的 `Backups/` 单独挂载到 HAOS，用于存放备份。

1. 重新部署时，只要原系统盘 `/lzcapp/var/haos/haos.qcow2` 还在，启动器就直接使用它。
2. 首次创建系统盘时，启动器读取 `Images/` 里的压缩包。若文件缺失，则从 GitHub 下载，校验成功后以正式文件名保存。以后用同一份文稿重新创建系统盘，可以直接复用该文件。
3. 导入前核对固定 SHA-256 `756ec485b1b153099ca53e88a5eebd63a801d56b791afe5c6d63ef94a35dc02d`，解压后检查 QCOW2。文稿里已有的文件若校验失败，启动器保留原文件并记录路径，换上正确文件即可重试。

首次开机后，HAOS 自行获取 Supervisor 更新和 Home Assistant Core 镜像，这部分仍需要网络。

## 访问入口

| 从哪里访问 | 地址 |
|---|---|
| 懒猫客户端 | 打开 Home Assistant OS 应用，在 HA 登录页选择「懒猫微服」使用当前懒猫账号 |
| 局域网其他物理设备 | 路由器分配的 HAOS 地址，例如 `http://<HAOS-LAN-IP>/` |
| 家庭 Wi-Fi 或 Tailscale 子网路由 | 设置物理网卡转发后，两种网络都使用 `http://<盒子局域网IP>:39128/` |
| HAOS 访问宿主 SMB/WebDAV | 使用自动识别或手动指定的宿主局域网地址，已有共享按原有凭据认证 |

在懒猫的「局域网端口转发工具」添加 TCP 规则，入口选物理网卡，目标选「微服应用 → Home Assistant OS → qemu」，目标端口填 `8123`。例如在 dkmooncat 监听 `enp2s0 · 192.168.50.11:39128`。HAOS 包只提供内部服务端口，宿主端口由转发工具分配。手机经 Tailscale 访问时需要盒子发布对应的 LAN 子网路由，并允许手机访问该端口。Companion App 对 HTTP 外部地址的支持取决于连接安全设置。[端口转发工具](https://lazycat.cloud/appstore/detail/cloud.lazycat.app.forward) · [Companion App 连接设置](https://companion.home-assistant.io/docs/getting_started/connection-security-level/)

HAOS 的 LAN 地址监听 80，局域网设备可以直接访问。LAN 和 TCP 转发入口使用 HA 自身认证，首次安装时请完成 HA 用户注册。

### 懒猫账号登录

首次启动时，先在 HA 完成家庭设置并创建本地 Owner，或通过向导恢复备份。LPK 随后从包内安装 OpenID Connect/SSO Authentication，写入默认登录配置，通过 `ha core check` 后重启 Core。如果检查失败，配置程序会撤回本次修改并稍后重试。若已自行安装或配置同名集成，则沿用已有设置。[OIDC 集成](https://github.com/christiaangoossens/hass-oidc-auth)

启用 OIDC 时，qemu 在每次 haos 开机时提供固定的 `haos-lpk-oidc.service`。Docker 启动前先恢复中断的配置写入，启动后立即核对部署凭据。等待家庭设置完成期间约每 30 秒检查一次。初装成功后，服务阻塞等待 Linux 文件通知。覆盖受管的 `secrets.yaml`、替换其父目录或替换整个 HA 配置目录都会触发核对。恢复备份时会等 Supervisor 回到运行状态再同步。配置校验或 Core 重启失败后，等待 5 分钟再试。

初装成功后，插件的升级和卸载都由 HA 管理。要在 HA 页面收到更新，先配置 HACS，在 HACS 中搜索「OpenID Connect」并下载一次。HACS 会将插件下载到初装使用的同一目录 `/config/custom_components/auth_oidc/`，随后通过 HA 的更新页面提供新版本。保留初装版本且未在 HACS 下载时，插件会一直使用 HA 配置目录中现有的文件，管理员也可以手工更新。重启或升级 LPK 会保留 HA 中的插件版本，卸载插件后也会保留卸载状态。彻底移除懒猫登录时，一并从 `configuration.yaml` 删除对应的 `auth_oidc: !include` 行。[插件安装指南](https://github.com/christiaangoossens/hass-oidc-auth/blob/v1.3.0/README.md#installation-guide) · [HACS 接管已有插件](https://www.hacs.xyz/docs/faq/existing_elements/) · [HACS 更新](https://www.hacs.xyz/docs/use/entities/update/)

登录名称、角色映射等设置可在 `/config/.haos-lpk-oidc/provider.yaml` 中修改。该文件通过 HA 的 `!secret` 读取同目录的 `secrets.yaml`。只要保留插件和这份配置的引用，LPK 就会在懒猫轮换凭据时更新 `secrets.yaml`（权限 `0600`），检查配置并重启 Core。恢复备份后同样使用当前部署的凭据。[HA 的 YAML 密钥](https://www.home-assistant.io/docs/configuration/secrets/)

从懒猫 HTTPS 应用域名选择「懒猫微服」登录。懒猫返回的 `groups` 包含 `ADMIN` 时，新建的 HA 用户具有管理员权限，其余新用户为普通用户。已有用户的角色在 HA 中管理。OIDC 用户只持有 OIDC 登录凭据，普通用户无法通过 HA 原生 Web UI 自行添加第一组本地用户名和密码。需要局域网密码登录时，HA 管理员可通过 Core 的 `config/auth_provider/homeassistant/create` 接口把本地凭据加到同一个用户。现有本地账号仍可从登录选择页进入。[角色映射](https://github.com/christiaangoossens/hass-oidc-auth/blob/v1.3.0/docs/configuration.md) · [HA 本地凭据接口](https://github.com/home-assistant/core/blob/2026.11.3/homeassistant/components/config/auth_provider_homeassistant.py#L29)

懒猫注册的回调地址是应用 HTTPS 域名下的 `/auth/oidc/callback`。HA Core 使用自己的 DNS 解析盒子域名，并校验真实域名的 TLS 证书。启用 OIDC 时，QEMU 管理容器为管理网卡提供双栈用户网络，HAOS 虚拟机通过 NetworkManager 将懒猫内部 IPv6 网段 `fc03:1136:3800::/40` 路由到管理网关。家庭 LAN 仍提供默认路由和 DNS。网页入口继续使用原有的 `8123 → 80` 转发，从 LAN IP 或端口转发地址进入时可选择 HA 本地登录。[懒猫网络机制](https://developer.lazycat.cloud/network.html) · [懒猫 OIDC](https://developer.lazycat.cloud/advanced-oidc.html)

每个 qemu 实例的管理链路都使用固定的 IPv6 私有前缀 `fd97:42b6:7a61:2::/64`。虚拟网关使用 `::2`，haos 的 `enp0s2` 静态使用 `::15`。多个实例的管理链路彼此隔离，可以复用地址。如果同一台 haos 还需经 LAN 或 VPN 访问这个前缀内的地址，其管理网卡的直连路由可能与之冲突。启动器目前不会自动另选前缀。

应用域名的路径为 `app（懒猫 HTTPS 反代）→ qemu 的桥接 IPv4:8123 → haos 管理网卡:80`。QEMU 直接监听 8123，通过内置的 `hostfwd` 把连接交给 haos。haos 看到的 TCP 对端是 app 的桥接地址。

每次启动时，qemu 从自己的桥接接口读取实际分配的应用网段。例如，`172.28.11.130/26` 对应 `172.28.11.128/26`。当前版本要求桥接前缀长度为 26，发现其他长度时会记录实际地址并停止启动。haos 在首次启动内部 Docker 前启用 `use_x_forwarded_for`，将这个网络地址写入 `trusted_proxies`。同一网段内即使 app 换了地址也可以继续访问。同一次安装后来分到另一个 `/26` 时，脚本只替换 Core 配置中仍保留的那一项。网桥网关和同一网段内的其他容器同样可以提供转发头，访问 HA 仍需通过 HA 自身认证。[HA HTTP 配置](https://www.home-assistant.io/integrations/http/#reverse-proxies) · [两次分配不同网段的实测](legacy/lpk-network-test.md#2026-11-26--qemu-直接转发与应用网段信任)

LAN 网卡与管理网卡各有一个持久 MAC 和固定 PCI 插槽。可从应用日志取得 LAN MAC，按需在家庭路由器上设置 DHCP 地址保留。在当前拓扑，宿主和与宿主共享网络的 Tailscale 子网路由器直接访问 HAOS 的 LAN 地址会遇到 macvlan 隔离；上表的管理入口可用于这两个方向。[本机网络实测](legacy/lpk-network-test.md)

`lzc-build.yml` 的 `haoslan` 使用有线物理口作为 macvlan 的 `parent`。Docker 从 `192.0.2.0/29` 为容器端点分配地址。启动器在打开 VDE 前清空该接口的地址并设为访客的 LAN MAC，Docker IPAM 继续保留分配记录。大写 `IFNAME` 由 Docker 替换为实际容器接口名，关闭容器端点的 IPv6 自动寻址。访客的 LAN 网卡自行向真实路由器申请地址。[Docker 端点设置](https://docs.docker.com/reference/cli/docker/network/connect/#set-sysctls-for-a-containers-interface---driver-opt)

haos 将应用 `/26` 的回程流量通过第二块网卡 `enp0s2` 送往管理网关，识别或指定了 `HostIP` 时也为该地址添加一条 `/32` 路由。管理网关通常是 `10.0.2.2`，当 `HostIP` 属于 `10.0.0.0/8` 时使用 `172.31.249.2`。这两条路由都位于 haos 内，局域网发现和默认出网使用 LAN 网卡。

## 应用文稿共享

以文稿用户 `moon` 为例，host 上的应用文稿目录为 `/lzcsys/data/document/moon/.apps/com.github.moonfruit.haos/`，qemu 通过 `/lzcapp/documents/moon/` 访问。启动器自动创建并复用 `Media/` 和 `Share/`，通过 virtio-9p 分别读写挂载。文稿访问只需 `document.private` 权限。

| 应用文稿目录 | haos 中的挂载点 | 用途 |
|---|---|---|
| `Media/` | `/mnt/data/supervisor/media/Host` | Core 通过 `/media/Host` 读写媒体文件 |
| `Share/` | `/mnt/data/supervisor/share/Host` | Core 和获准访问共享目录的 HA 附加应用通过 `/share/Host` 交换文件 |
| `Backups/` | `/mnt/data/supervisor/backup` | Supervisor 本地备份归档 |

在 HA 备份界面勾选“媒体”或“共享”，对应目录里的文件也会收入归档，恢复这些文件时会写回 host。三个目录彼此独立，`Backups/` 位于媒体和共享的备份范围之外。只需要备份 HA 配置时，可取消“媒体”和“共享”。

## 备份和升级

在 HA 的“设置 → 系统 → 备份”中选择本地备份位置。QEMU 把所选用户的应用文稿子目录 `Backups` 通过 virtio-9p 挂到 HAOS 的 `/mnt/data/supervisor/backup`。生成的每个备份都是独立的 `.tar`，可以在懒猫文件管理中取走。实测 LZCOS 1.6.3 的文稿挂载对应宿主的 `/lzcsys/data/document/<uid>/.apps/com.github.moonfruit.haos/`。曾记录为旧布局的备份仍使用该用户文稿下的 `HAOS/Backups`，启动应用还需要平台提供应用文稿挂载。

应用会记住所选用户的备份目录位置。后续启动时若该用户的 `Backups/` 丢失，启动器会报错并退出。恢复原目录后可重新启动应用。切换文稿用户后，已有文件保存在原用户的应用文稿目录。

应用把系统盘、UEFI 变量和两张网卡的 MAC 保存在 `/lzcapp/var/haos`。更新 LPK 时继续使用已有磁盘，HAOS、Supervisor 和 Core 的日常更新使用 Home Assistant 自身的更新入口。停止 LPK 时，启动器通过 ACPI 和 QEMU Guest Agent 请求关机，最多等访客 460 秒，再给 QEMU 5 秒退出。Compose 的 `stop_grace_period: 8m` 把容器强制终止的期限放在两段等待之后，访客提早关机会立即结束等待。

需要共享其他目录时，可在 HAOS 的“设置 → 系统 → 存储”添加宿主 SMB，共享用途可以选媒体、共享文件或备份。HA 自带的 WebDAV 集成专用于增加备份位置。本地 `Backups/` 挂载独立于这两项服务。[网络存储](https://www.home-assistant.io/common-tasks/os/#network-storage) · [WebDAV 集成](https://www.home-assistant.io/integrations/webdav/)

## 0.5.0 懒猫登录验收

在开发机的隔离 HAOS 18.4、Core 2026.11.3 中完成首次设置后，连续观察 70 秒，受管的 `secrets.yaml` 没有被打开，Core 也没有重启。通过 Supervisor 创建并恢复一份含旧测试凭据的备份，备份中的标记文件恢复成功，同一监听进程自动将凭据同步为当前值。直接覆盖 `secrets.yaml`、将含旧凭据的整个 HA 配置目录移入原路径，也分别触发了同步。

开发期在盒子上的独立 HAOS 18.4 实例使用 Core 2026.11.3 完成首次设置。LPK 自动装入 v1.3.0 集成，登录选择页同时显示懒猫和本地登录。Core 经管理网关读取真实 issuer 的 discovery 和 JWKS 均收到 HTTP 200。集成生成的授权请求使用应用 HTTPS 域名的 `/auth/oidc/callback`、`openid profile groups` 和 PKCE S256。Supervisor 生成的宿主备份包含 OIDC 配置和插件。用 Supervisor 恢复一份带有旧测试密钥的备份后，配置程序自动换回当前凭据。重新部署和两次停止再启动隔离 LPK，懒猫都保留客户端 ID 并轮换密钥。通过 HA 页面重启 Core 或重启 HAOS 时，客户端凭据保持一致。

另以 Core 2026.11.3、同一插件和模拟身份服务验证两种角色。无论 `groups` 来自 ID Token 还是 UserInfo，`ADMIN` 和 `NORMAL` 均在首次登录获得对应权限，第二次登录时改组仍保留原 HA 权限。Core 拒绝了普通用户自行添加首组本地凭据的请求。管理员为同一用户添加凭据后，密码验证成功，原 OIDC 凭据和普通用户权限均保留。

改为一次性安装后，在本地用新 LPK 中的插件和访客程序生成的配置分别验证初装和轮换密钥后的登录。测试模拟 HACS 替换插件目录，并修改登录名称和请求范围。Core 2026.11.3 在两种情况下均能加载配置，与模拟身份服务完成登录。访客程序的 Go 测试还覆盖了升级 LPK 后保留插件版本、卸载后保持卸载，以及首次检查失败时回滚。

2026-11-27，在 dkmooncat 的独立实例上对照测试了 OIDC 网络。Core 2026.11.3 通过 HA DNS `172.30.32.3` 解析到的 issuer IPv6 地址与 QEMU 管理容器相同。经家庭 LAN 默认网关请求 discovery 超时；在同一个 Core 中将目标改经管理网卡、选用该网卡的 IPv6 源地址后，discovery 和 JWKS 均返回 200。使用最终 LPK 的程序冷启动后，NetworkManager 自动建立懒猫 `/40` 路由，插件完成了初装。暂停 OIDC 定时器并执行 `ha core rebuild` 后，新 Core 容器仍直接访问两个端点并通过 TLS 校验。Core 的 hosts 中没有 issuer 记录，QEMU 管理容器中也没有 OIDC 443 监听。

## 0.4 文件共享与端口转发实测

固定应用文稿目录后，在开发机的隔离 HAOS 18.4 虚拟机中验证了 Core 2026.11.3。Core 从 `/media/Host` 和 `/share/Host` 读取了开发机写入的文件，两处新建的文件也能从虚拟机外立即读回。Supervisor 将媒体和共享一起备份到 `Backups/`。改写两个测试文件并恢复后，Core 和开发机均读到了原内容，恢复任务报告成功。

2026-11-26，在 dkmooncat 的隔离实例上，当时的版本挂载了测试文稿根、媒体、共享和备份四个目录。文稿根和媒体拒绝写入，Core 能读取媒体文件。在 Core 的 `/share/LazyCat` 写入文件后，宿主立即读到了相同内容。Supervisor 创建的共享目录备份在宿主和访客的 SHA-256 一致。修改一份测试文件后，从该归档恢复成功，宿主文件也恢复了原内容。

用「局域网端口转发工具」临时将物理网卡的 `192.168.50.11:39128/TCP` 转到隔离实例的 `qemu:8123`。独立局域网设备和经过盒子 `tailscale0` 的远端客户端均收到 WebSocket 101 和 HA 的 `auth_required`。完成测试后移除了该规则和隔离应用。

正式实例使用用户预下载的官方压缩包初始化，升级 0.4.1 时沿用原系统盘。在正式 QEMU 容器中以空 `HostIP` 运行相同的识别代码，获得父网卡的 `192.168.50.11`，生成经 `10.0.2.2` 的宿主 `/32` 路由。实际启动保留了部署时手填的相同地址。访客中的文稿根和媒体目录均为只读，共享和备份目录均为读写。HAOS 向宿主 WebDAV 发起 PROPFIND 收到 HTTP 401 认证挑战。

同一台盒子上切换隔离包和正式包时，先卸载隔离包以释放 Docker macvlan 网络。两套包同时使用 `192.0.2.0/29` 会导致第二套在创建网络时收到 `Pool overlaps`。

正式虚拟机通过 DHCP 获得 `192.168.50.39`。Supervisor 在 GHCR 超时后重试并完成 Core 2026.11.3 的安装。局域网物理设备访问首次设置接口返回 HTTP 200，WebSocket 返回 101 和 `auth_required`。从正式应用反代所在网络向 `qemu:8123` 发送带转发头的 WebSocket 请求也收到相同握手。正式 HTTPS 域名对未登录请求返回懒猫登录跳转。

## 0.3.0 实机验收

在 dkmooncat 上用独立包名和空白 HAOS 系统盘验证首次配置。验收包使用交付包的启动器和反代配置，将系统盘和测试文稿放在宿主 `/tmp`，仅对 `/api/websocket` 放行懒猫登录，以便直接检验 HA 握手。原生 HTTPS 域名收到 HTTP 101 和 Core 2026.11.3 的 `auth_required`。宿主 TCP 入口带 `X-Forwarded-For` 请求 `/api/onboarding` 返回 200。在访客内访问其 LAN 地址，普通请求返回 200，添加同一个转发头后返回 400。

首次成功握手五分钟后以及 HAOS 虚拟机冷启动后，懒猫域名均再次收到同版本的 `auth_required`，三次检查的 HTTP 配置文件 SHA-256 一致。容器实际 `StopTimeout` 为 480 秒，本次从懒猫发起正常关机耗时 14.2 秒，QEMU 退出码为 0。

Docker 的 IPAM 记录为 `192.0.2.2`，启动后的容器 macvlan 端点 `eth1` 没有 IP，且 `net.ipv6.conf.eth1.disable_ipv6=1`。访客从真实路由器获得 `192.168.50.247`，默认路由走 LAN，访问宿主 `192.168.50.11` 的路由走管理网关 `10.0.2.2`。

为评估后续 OIDC 登录，还测试了 Core 到懒猫身份服务的反向连接。Core 直连盒子的 issuer 域名超时，QEMU 服务容器可以通过懒猫网络访问该域名。临时在容器回环上设置只转向这个 issuer 的 TCP 转发后，Core 经管理网关访问 discovery 和 JWKS 均返回 200，TLS 使用真实域名并通过证书校验。这次验证覆盖端点连通性，账号登录和令牌交换留待 OIDC 接入时验证。[懒猫 OIDC](https://developer.lazycat.cloud/advanced-oidc.html)

## 0.2.3 实机验收

2026-11-25，正式应用首次初始化时从 GitHub 下载 HAOS 18.4 到 `moon` 的应用文稿 `Images/haos_ova-18.4.qcow2.xz`。宿主和 LPK 看到同一 inode，文件为 508,539,728 字节，宿主计算的 SHA-256 与官方发布记录一致。随后升级到只引用 `ghcr.io/moonfruit/qemu:10.0.13-r1` 的 0.2.3，复用了原系统盘和压缩包。运行容器中的 QEMU 报告版本 10.0.13，Guest Agent 报告 HAOS 18.4，访客备份目录挂载为 9p。[HAOS 18.4 发布记录](https://github.com/home-assistant/operating-system/releases/tag/18.4)

Supervisor 下载并启动了 Home Assistant Core 2026.11.3。宿主管理入口的 `/api/onboarding` 返回 HTTP 200，显示可以创建首个用户；经 Tailscale 访问 `/api/websocket` 收到 HTTP 101 和同版本的 `auth_required` 消息。

## 0.2.0 实机验收

2026-11-25，懒猫安装盒子专用交付包后进入部署参数页面。运行验收另外使用独立的包名、子域名和端口，将用户文稿、系统盘和缓存映射到宿主 `/tmp`。交付包与验收包内的 `haos-vm` 二进制相同。

LZCOS 1.6.3 在镜像未缓存时拉取了 CI 发布的 QEMU，镜像索引 digest 为 `sha256:8843f874fbcff48947701b9b3eb0cb34e38591e99188e13a0e599bfedca70bdd`。运行容器中的两个 QEMU 包均为 `1:10.0.13+ds-0+deb13u1`。升级后从原系统盘启动，两张网卡保留各自的 MAC，物理观察机通过 mDNS 看到了 `192.168.50.140:80` 上的 HA 服务，打开页面返回 200。[本次镜像构建](https://github.com/moonfruit/lazycat/actions/runs/45540355694)

在同一镜像上另以空系统盘和空下载缓存启动，日志显示启动器从用户文稿读取并校验官方压缩包。访客启动为 HAOS 18.4，QEMU Guest Agent 正常运行，原备份通过 9p 重新挂载且 SHA-256 一致。

HAOS 18.4 和宿主读到的完整备份 `v2-startup.tar` 的 SHA-256 均为 `77250d416ec540567c233a63c9893e8bf3afa370b292102e1035ed9ae3ac4d06`。改写配置和 share 标记、升级并冷启动后，通过 Supervisor 从该备份恢复 Home Assistant 和 share。配置的 SHA-256 恢复为 `708527ad44b9981a63078e78d2610ce77f6785de578b99809e570778c6b5037d`，与宿主独立解出的文件相同。懒猫 HTTPS 域名与宿主 Tailscale 端口均恢复 WebSocket 101，并收到 Core 2026.11.3 的 `auth_required`。

## 0.1.0 实机验收

2026-11-24，在 x86-64 懒猫的 LZCOS 1.6.3 上以独立应用运行官方 HAOS 18.4。验收包使用交付包相同的 QEMU 镜像、启动器和反代程序，以宿主 `/tmp` 下的专用目录代替实际用户文稿、应用状态和下载缓存，部署参数预填测试值。为检查 HTTPS 反代，测试包只对 `/api/websocket` 启用了独立鉴权；交付包整个应用域名使用懒猫登录。

访客通过 DHCP 获得 `192.168.50.158`，独立物理观察机发现它发布的 `_home-assistant._tcp`。HAOS 在 LAN 网卡上通过 mDNS 发现真实 HomeKit 灯，SSDP 查询收到五台局域网设备的 HTTP 200 回答。HAOS 通过管理网卡访问宿主时，WebDAV 的 PROPFIND 收到 401，SMB 3.1.1 的只读匿名挂载收到权限拒绝。本轮只验证这两个生产服务的网络和认证入口。

Core 2026.11.3 就绪后，从开发机经懒猫 HTTPS 应用域名和 Tailscale 宿主端口分别完成 WebSocket 101 握手，均收到 HA 的 `auth_required` 帧。应用域名的根路径对未登录请求返回懒猫登录地址。局域网物理机也能直接打开访客的 HA 页面。

Supervisor 创建了完整备份 `0f4c25af`。访客和宿主上归档的 SHA-256 均为 `e2652225b55c33604619a9af340e90847afbd1f8dc1b122e6d4179ea7085db08`。改写访客配置和 share 测试文件、更新验收包并冷启动后，访客仍使用原先的两张网卡 MAC 和同一份 9p 备份。通过 Supervisor 从中恢复 Home Assistant 和 share，配置的 SHA-256 从 `461e908f…` 恢复为 `5397fd16…`，与宿主独立解出的原件相同。share 的标记也已还原，Core 重启后两个远程入口都再次通过 WebSocket 检查。

最后通过懒猫停止应用。HAOS Guest Agent 收到关机请求，QEMU 以状态码 0 自行退出，从发出停机指令到平台返回耗时约 13 秒。

应用图标来自 [Home Assistant Brands](https://brands.home-assistant.io/homeassistant/icon.png)。
