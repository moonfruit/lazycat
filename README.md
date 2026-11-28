# lazycat

Lazycat Apps

## 公开发布

在喵喵商店订阅 [MoonFruit 软件源](https://moonfruit.github.io/lazycat/source/v2/index.json)。公开应用有 HttpBin、MyIP、Home Assistant OS 和 HivisionIDPhotos。软件源与应用的图标通过 GitHub Pages 提供，每个应用的图标取自它最新发布的 LPK。

[发布清单](.github/release-targets.json) 的 `lpk` 项登记目录 ID、包 ID 和测试架构，`images` 项登记发布到 GHCR 的基础镜像。HAOS 使用 QEMU 的固定修订，需要 x86-64、KVM 和名为 `enp2s0` 的有线网口。HivisionIDPhotos 的 LPK 版本跟随 Docker Hub 上 `linzeyi/hivision_idphotos` 的稳定版本标签。

推送到 `main` 时按变更路径和登记项选择目标。公共构建流程变更会检查对应类别的全部目标。也可以在 Actions 中按目录名选择单个目标或输入 `all`。

- `Publish LPKs and software source` 每天台北时间 05:43 检查上游。HAOS 在 amd64 上验证，其余应用在 amd64 和 arm64 上验证。测试通过的同一份 LPK 会上传到 Release，并进入订阅索引。
- `Build registered Docker images` 每天台北时间 06:29 检查 Alpine 及软件包版本。镜像通过 linux/amd64 运行测试并生成构建证明后，才更新版本标签和 `latest`。
- QEMU 在同一工作流中每周一台北时间 11:23 检查 Debian，重新构建并比较文件系统和运行配置。内容变化时发布新的 `<版本>-rN`，内容相同时复用现有修订并检查别名。Hivision 自建镜像保留独立的手动和工作流文件推送触发。

## TODO:

- Gitlab
- Home Assistant with mi-home extension
- Jellyfin
- OpenList
