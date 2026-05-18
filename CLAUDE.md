# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 仓库性质

此仓库是 **懒猫微服 (LazyCat) 应用包 (`.lpk`)** 的集合。每个顶层目录（`aria2`、`vuetorrent`、`caddy`、`hivision-idphotos`、`bambu-studio`、`myip`、`httpbin`、`netcat`、`router`、`switch1`、`switch2`、`postgres`、`aipod` 等）是一个独立的 LazyCat 应用。`docker/` 目录是不同的——它存放被 LazyCat 应用引用的自构建基础镜像（`ghcr.io/moonfruit/*`）。

不存在跨应用的源代码共享，**`aipod/Makefile` 复用 `../router/src/`** 这一个例外。

## 通用构建工作流

所有应用目录的 `Makefile` 遵循同一模式，命令应在该应用目录内执行：

```
make            # 构建 app.lpk
make install    # lzc-cli app install app.lpk（需先登录目标设备）
make uninstall  # 通过 yq 从 lzc-manifest.yml 读取 package 名再卸载
make clean      # 删除 app.lpk（Go 类的也会删 dist/）
make update     # 当目录下存在 update.sh 时运行它
```

构建本质上调用 `lzc-cli project build`，依据 `lzc-build.yml` 指定的 `manifest` / `contentdir` / `buildscript` / `icon`。`app.lpk` 与 `*.env` 在 `.gitignore` 中（注意：`vuetorrent/vue.env` 在本地保存真实 GitHub PAT，**不要将其内容写入跟踪文件或日志**）。

## 模块结构差异

不同应用属于以下几类，先识别类别再做修改：

- **纯转发型** （`switch1`、`switch2`、`bambu-studio`、`myip`、`httpbin`、`hivision-idphotos`、`vuetorrent`、`caddy`）：没有 `contentdir`，或 `contentdir` 仅为打包好的下游镜像配置；逻辑全在 `lzc-manifest.yml` 的 `services` + `upstreams`/`routes` 里。
- **打包静态资源型** （`aria2`）：`update.sh` 下载上游产物到 `dist/<sub>/`，构建时一并打包；含 `hack.py` 等小工具向 HTML 注入脚本以适配 LazyCat 域名/RPC。
- **Go 反向代理伴侣型** （`router`、`netcat`、`aipod`）：`lzc-build.yml` 的 `buildscript` 用 `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go -C src build -o ../dist/` 交叉编译，再由 `upstreams.backend_launch_command` 拉起。`aipod` 与 `router` 共享 `router/src/`。
- **基础镜像** （`docker/aria2`、`docker/crond`）：仅 `Dockerfile` + `update.sh`；由 `.github/workflows/build-*.yml` 在 push 到 `main` 且对应 Dockerfile 改动时构建多架构镜像并推送到 `ghcr.io/moonfruit/<name>:<version>` 和 `:latest`，含 build provenance attestation。

## 版本更新约定（重要）

每个含 `update.sh` 的目录采用统一脚本规约，理解后再改：

1. 脚本第一行若可执行 `proxy` 命令则自动 `exec proxy` 重启自身——这是为越过 GitHub API 限速。
2. 通过 `source "$ENV/lib/bash/{github,docker,fs}.sh"` 引入用户私有 shell 库；这些库不在仓库内，常用函数为 `find-latest-version`、`find-image-latest-version`、`find-alpine-package-version`、`download-latest-release`、`create-temp-file`。
3. 用 `sed -e '...' -i lzc-manifest.yml`（或 `Dockerfile`）**就地替换镜像 tag 或 `version:` 字段**——保留 `sed` 锚点（如 `image: lscr.io/linuxserver/qbittorrent:`）的写法，避免误匹配。
4. 末尾若 `$1` 不是 `-N` 则打印 `git diff`；顶层 `update.sh` 调用 `./update.sh -N`（non-interactive，跳过 diff），因此**不要去掉 `-N` 分支**。
5. 仓库根的 `update.sh` 通过 `fd -tf update.sh` 递归执行所有子模块脚本，然后打印总 diff。

引入新应用时若涉及自动版本跟踪，沿用同一模板。

## Manifest 速读

`lzc-manifest.yml` 是每个应用的核心声明，常见键值：

- `package`：应用唯一 id（命名空间 `com.github.moonfruit.<name>`），`make uninstall` 与 LazyCat 系统都用它寻址。
- `application.subdomain`：访问路径前缀，须全局唯一。
- `application.upstreams` / `routes`：HTTP 反向代理；`backend_launch_command` 可让伴侣进程随路由启动（见 `router`、`netcat`、`aipod`）。
- `application.ingress`：声明 TCP 暴露端口，例如 `router` / `aipod` 暴露 `0-65535` 给远程 socks。
- `services`：基于容器镜像启动的后端，与 `binds`/`environment`/`setup_script` 配套；`/lzcapp/pkg/content/` 即打包内容根。
- 模板语法 `{{- if .U.GithubToken }}`：使用 `lzc-deploy-params.yml` 中定义的部署参数（如 `vuetorrent`）。

## 环境工具

仓库假定本地具备：`lzc-cli`、`yq`、`fd`、`rg`、`go`（交叉编译 linux/amd64）、`make`、`python3`（`hack.py` 等小脚本）、可选的 `proxy` 命令。脚本头按用户全局约定使用 `#!/usr/bin/env bash` / `#!/usr/bin/env python3`。

## CI

`.github/workflows/build-*.yml` 只构建 `docker/` 下的基础镜像，**不对 `.lpk` 做 CI**——`.lpk` 的发布完全本地完成（`make` → `make install`）。新增镜像目录时同步添加对应 workflow，触发 paths 至少包含该 Dockerfile 与 workflow 自身。
