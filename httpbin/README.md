# HttpBin for LazyCat

在喵喵商店添加以下订阅地址，同步后选择 **HttpBin** 安装或升级。

```text
https://moonfruit.github.io/lazycat/source/v2/index.json
```

GitHub Actions 每天台北时间 05:43 检查 `ghcr.io/psf/httpbin` 的稳定版本标签。LPK 直接引用版本标签。镜像仓库可以为同一标签重新构建镜像；具体拉取时间取决于设备。新上游版本先用镜像的默认命令启动并测试 `/get`，通过后才发布 LPK。

- 默认直接使用 `ghcr.io/psf/httpbin:<上游版本>`。需要修复某一版本时，在 `docker/httpbin/<上游版本>/Dockerfile` 中基于同版本 PSF 镜像修改。工作流测试 amd64 和 arm64，通过后发布 `ghcr.io/moonfruit/httpbin:<上游版本>`。下一版单独判断是否需要修复。
- LPK 前三段跟随上游版本。同一版本切换镜像来源时，更新脚本递增第四段；仅修改打包内容时，维护者手动递增第四段。每个 LPK 版本有独立的 Release、SHA-256 和字节数记录，软件源会重新下载附件校验。
- `0.10.4.1` 使用自有镜像 `0.10.4`，在上游启动脚本中选用 Gunicorn 的 gthread worker，保留原有 Python 依赖以及监听地址和端口配置。已安装 `0.10.4` 的设备可以通过软件源升级。

手动运行 **Publish HttpBin LPK and software source** 可从现有 Release 恢复索引。勾选 **Check for a new stable PSF image version** 会立即检查上游；勾选 **Rebuild the current patched image** 会重新构建当前版本的自有镜像。
