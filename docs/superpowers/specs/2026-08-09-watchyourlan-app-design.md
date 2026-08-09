# WatchYourLAN 应用（LazyCat `.lpk`）设计

日期：2026-08-09
状态：设计已确认，待写实现计划

## 1. 目标

新增一个 `watchyourlan` 应用，把 [WatchYourLAN](https://github.com/aceberg/WatchYourLAN)（轻量局域网 ARP 扫描器，持续记录设备上下线）部署到懒猫微服。

参考物：
- `~/Downloads/WatchYourLAN-v0.0.1.zip`——第三方 `dev.beiyu.wyl` 打包，仅作对照，**不照抄**。
- 上游仓库 `aceberg/WatchYourLAN`（README、`Dockerfile`、`docker-compose.yml`）。
- 本仓库既有应用：`seq`（最简纯转发型骨架）、`vuetorrent`（host 网络服务的反代写法）、`tailscale`（部署参数与 locale 格式）。

相对参考包要做的改动：

1. **用上游官方镜像 `aceberg/watchyourlan`**，而非参考包那个 `registry.lazycat.cloud/beiyu/...` 的 digest tag。
2. **版本跟上游**（2.1.4），参考包停在 0.0.1。
3. **`IFACES` 做成部署参数**，参考包完全没设该变量。
4. **不抄 `public_path: - /`**（安全降级，见 §4）。
5. **不写 `background_task`**（字段已从官方 manifest spec 移除）。
6. 路由改用 `host.lzcapp`，而非参考包的 `<service>.<package>.lzcapp` FQDN（见 §3）。

## 2. 关键技术事实（已核实）

**上游运行要求**（README / `docker-compose.yml` / `Dockerfile`）：

- 镜像 `aceberg/watchyourlan`（另有镜像 `ghcr.io/aceberg/watchyourlan`），Alpine 基底，装了 `arp-scan` + `tzdata`，入口 `./WatchYourLAN`，未声明 `EXPOSE`。
- **`network_mode: host` 是硬性要求**——README 原文 "must be `host`"。ARP 扫描必须直连物理网段，桥接网络里只能看到 docker 网桥。
- 数据目录 `/data/WatchYourLAN`（`USE_DB=sqlite`）。
- 环境变量与默认值：`HOST=0.0.0.0`、`PORT=8840`、`TZ`（无默认）、`IFACES`（无默认，留空则自动探测）、`TIMEOUT=120`、`THEME=sand`、`COLOR=dark`、`LOG_LEVEL=info`、`USE_DB=sqlite`、`TRIM_HIST=48`、`SHOUTRRR_URL`（通知）。
- v2 的 Settings 页面可在线修改上述大部分配置并落盘到 `config.yaml`。
- 上游最新发布：**2.1.4**（2025-09-10），Docker Hub 同名 tag 存在。

**懒猫侧事实**（`https://developer.lazycat.cloud/spec/manifest.html`）：

- `application` 段字段表中**没有 `background_task`**，该字段已弃用。`services:` 声明的容器随应用安装即常驻，不依赖前端会话——本仓库 `seq` 未写该字段即为佐证。
- `services.netadmin` 的官方说明是 "NET_ADMIN capability flag"，给的是 **NET_ADMIN**。
- `application.public_path` 的语义是「免鉴权 HTTP 路径」。

**权限结论**：`arp-scan` 发原始 ARP 帧只需 **`CAP_NET_RAW`**，而 NET_RAW 属于 Docker 默认 capability 集合，无需额外授予。因此本应用**不设 `netadmin`、不需要 `compose_override` 加 `cap_add`**。参考包同样一个权限字段都没有且能正常工作，佐证该结论。（对照：`tailscale` 需要 `netadmin: true` + `cap_add: [NET_ADMIN, NET_RAW]`，是因为它要改路由表、写 nftables、开 `/dev/net/tun`，那些依赖 NET_ADMIN。）

**盒子实测**（`ssh 192.168.50.11`，`ip -br addr`）：物理网口为 `enp2s0`（192.168.50.11/24），同命名空间内另有 20+ 个 `veth*` / `lzc-br-*` / `br-*` 接口。**若不指定 `IFACES`，WatchYourLAN 会自动探测并扫描全部接口**，把 docker 网桥网段一并扫进数据库，产生大量无意义记录。而接口名逐机不同，不能硬编码——这正是必须把 `IFACES` 做成部署参数的原因。

## 3. Web UI 如何接入懒猫路由

`network_mode: host` 的容器不在应用私有网桥上，短服务名（`http://wyl/`，即 `seq` / `caddy` 的写法）解析不到它。

采用 **`host.lzcapp`**——本仓库 `vuetorrent` 已验证的写法：其 `qbit` 服务是 `network_mode: host`，非 host 的 `vue` 服务通过 `QBIT_BASE=http://host.lzcapp:6880` 访问它（`vuetorrent/lzc-manifest.yml:28`）。

```yaml
routes:
  - /=http://host.lzcapp:8840/
```

被否决的备选：

| 方案 | 否决理由 |
|---|---|
| `/=http://wyl.<package>.lzcapp:8840/`（参考包写法） | 依赖 lzc 为 host 模式容器注册服务名，无文档保证 |
| `/=http://wyl/`（短名） | host 模式容器不在应用网桥上，解析不到 |
| 另起一个桥接反代容器转发到盒子 IP:8840 | 多一个容器且需取盒子 IP，YAGNI |

**兜底**：若实测 `host.lzcapp:8840` 不通，退回到上表第三方案。

## 4. 安全考量（有意为之的取舍）

- **不加 `public_path: - /`**。参考包写了这行，等于允许匿名访问，绕过懒猫账号鉴权把内网设备清单对外公开。本仓库其它应用均无此字段。
- **8840 在局域网内裸露**。这是 `network_mode: host` 的必然结果：WatchYourLAN 直接在盒子的 IP 上监听 8840，且自身无鉴权（上游需另配 ForAuth）。懒猫的路由鉴权只保护 `wyl.<box>.heiyu.space` 这条路径，保护不了 `192.168.50.11:8840`。上游 README 对此有同样警告。**已知并接受**——本应用面向内网自用；需要收紧时应在盒子防火墙层面限制，而非改本应用。
- **不加 `ingress`**。host 模式下 8840 已经开在盒子上，再声明 TCP ingress 是多余的。

## 5. 文件结构

```
watchyourlan/
├── package.yml              # 包元信息（新格式，同 seq）
├── lzc-manifest.yml         # application + services
├── lzc-deploy-params.yml    # 唯一参数 ifaces，带 zh locale
├── lzc-build.yml            # 最简形式：无 contentdir、无 buildscript、无 compose_override
├── lzc-icon.png             # 上游 assets/logo.png
├── Makefile                 # 与 seq 同款
└── update.sh                # 版本跟踪
```

### `package.yml`

```yaml
package: com.github.moonfruit.watchyourlan
version: 2.1.4
name: WatchYourLAN
description: 轻量级局域网扫描器，持续记录设备上下线历史。
author: MoonFruit
license: https://github.com/aceberg/WatchYourLAN/blob/main/LICENSE  # MIT
homepage: https://github.com/aceberg/WatchYourLAN
```

### `lzc-manifest.yml`

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

`environment` 只留两项。`TIMEOUT` / `THEME` / `COLOR` **有意不写**：上游默认值（120 / sand / dark）本就合理，且这些项可在 WatchYourLAN 的 Settings 页面修改并落盘到 `config.yaml`；写进 manifest 会在每次容器重启时覆盖用户的界面设置。

### `lzc-deploy-params.yml`

单参数 `ifaces`，`optional: false`，`default_value: "enp2s0"`，英文 + `zh` locale 双语，description 中说明：空格分隔多个网口；留空会连 `lzc-br-*` / `veth*` 一起扫；并给出查询本机网口的方法。

### `lzc-build.yml`

`seq` 的最简形式——只有 `manifest` / `pkgout` / `icon`。既无打包内容也无编译步骤。

### `Makefile`

抄 `seq`（`all` / `clean` / `install` / `uninstall` / `update`），依赖只写 `lzc-*`。

### `update.sh`

遵循仓库统一规约：

1. 首行 `proxy` 自举。
2. `source "$ENV/lib/bash/docker.sh"`。
3. `VERSION=$(find-image-latest-version aceberg/watchyourlan)`。
4. `sed` 分别就地替换 `package.yml` 的 `^version:` 与 `lzc-manifest.yml` 的 `image: aceberg/watchyourlan:` 锚点。
5. 末尾 `$1 != "-N"` 时打印 `git diff`。

**待实现时验证**：Docker Hub 上该镜像除语义版本外还有 `latest` / `v2` / `dev` 三个非语义 tag，需确认 `find-image-latest-version` 能正确过滤并返回 `2.1.4`。

## 6. 验证方式

1. `make` 构建出 `app.lpk`，`make install` 安装到盒子。
2. 浏览器打开 `wyl.<box>.heiyu.space`，确认 `host.lzcapp:8840` 路由通、界面加载正常。
3. `lzc-cli project log` 确认 `arp-scan` 无 `Operation not permitted` 一类权限错误——若有，说明 §2 的 NET_RAW 结论被证伪，需补 `compose_override` 的 `cap_add: [NET_RAW]`。
4. 界面上确认扫出的是 `192.168.50.0/24` 的真实设备，而非 `172.28.x` 网桥地址——验证 `IFACES=enp2s0` 生效。
5. 重启应用后确认 `/lzcapp/var/data` 下的 sqlite 数据与历史记录仍在。
6. `./update.sh` 干跑一次，确认版本探测与 `sed` 替换正确。
