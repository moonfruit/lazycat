# HttpBin、MyIP 与基础镜像发布迁移

- 将 HttpBin 的静态元数据迁入 `httpbin/package.yml`，用 `0.10.4.4` 向现有用户交付 v2 tar。保留包 ID、官方镜像版本标签和 gthread 命令。MyIP 的包版本与上游镜像同步到 `8.2.0`。
- 在 `.github/release-targets.json` 登记公开目标。保留 HttpBin 的目录 ID 1，分配 MyIP 的 ID 2。定时检查登记项，push 从变更路径选择，手动支持单项或全部。
- LPK 发布前读取成品元数据与运行清单，在 amd64 和 arm64 验证实际服务。构建和发布交接同一个 LPK 文件。通过测试后记录更新并发布 Release，一次部署聚合的软件源。用既有六条 HttpBin 记录核对历史下载地址与 SHA-256。
- 基础镜像首批接入 `docker/aria2` 和 `docker/crond`，保留 linux/amd64、既有版本标签和 latest。验证候选容器功能和构建证明后更新标签；定时检查无变动时跳过。QEMU 与 Hivision 沿用各自的发布策略。
- 验收覆盖版本更新、重复运行、按路径筛选、公开 Release 附件及 Pages，以及从 GHCR 反查并运行发布镜像。
