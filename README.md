# lazycat

Lazycat Apps

## 公开发布

在喵喵商店订阅 [MoonFruit 软件源](https://moonfruit.github.io/lazycat/source/v2/index.json)。[发布清单](.github/release-targets.json) 的 `lpk` 项登记应用目录、固定目录 ID 和包 ID；`images` 项登记 `docker/` 下的镜像目录和版本字段。HttpBin、MyIP 共用软件源；aria2、crond 基础镜像发布到 GHCR。

推送到 `main` 时按变更路径和登记项选择目标。公共构建流程变更会检查对应类别的全部目标。也可以在 Actions 中按目录名选择单个目标或输入 `all`。

- `Publish LPKs and software source` 每天台北时间 05:43 检查上游，在 amd64 和 arm64 上测试成品清单后，将同一份 LPK 上传到 Release 并更新订阅索引。旧版本保留各自的下载地址和校验值。
- `Build registered Docker images` 每天台北时间 06:29 检查 Alpine 及软件包版本。镜像通过 linux/amd64 运行测试并生成构建证明后，才更新版本标签和 `latest`。

## TODO:

- Gitlab
- Home Assistant with mi-home extension
- Jellyfin
- OpenList
