# HttpBin for LazyCat

在喵喵商店添加以下订阅地址，同步后选择 **HttpBin** 安装或升级。

```text
https://moonfruit.github.io/lazycat/source/v2/index.json
```

GitHub Actions 每天台北时间 05:43 检查 `ghcr.io/psf/httpbin` 的稳定版本标签。LPK 直接引用版本标签。镜像仓库可以为同一标签重新构建镜像；具体拉取时间取决于设备。新上游版本先用镜像的默认命令启动并测试 `/get`，通过后才发布 LPK。

- 默认直接使用 `ghcr.io/psf/httpbin:<上游版本>`。需要修复某一版本时，在 `docker/httpbin/<上游版本>/Dockerfile` 中基于同版本 PSF 镜像修改。工作流测试 amd64 和 arm64，通过后发布 `ghcr.io/moonfruit/httpbin:<上游版本>`。下一版单独判断是否需要修复。
- LPK 前三段跟随上游版本。同一版本切换镜像来源时，更新脚本递增第四段；仅修改打包内容时，维护者手动递增第四段。每个 LPK 版本有独立的 Release、SHA-256 和字节数记录，软件源会重新下载附件校验。
- `0.10.4.2` 使用自有镜像 `ghcr.io/moonfruit/httpbin:0.10.4`。PSF 原镜像的 Gunicorn 26.0.0 要求 gevent ≥24.10.1，实际打包的是 24.2.1。升级 gevent 需要 greenlet 3，而 HttpBin 0.10.4 在 Python 3.10 上声明了 `greenlet<3`。自有镜像用 Python 3.13 重建虚拟环境，安装 gevent 26.9.0 和 greenlet 3.5.6，保留 PSF 的默认 gevent 启动脚本。工作流验证依赖一致性、实际 worker 和 `/get` 响应。

手动运行 **Publish HttpBin LPK and software source** 可从现有 Release 恢复索引。勾选 **Check for a new stable PSF image version** 会立即检查上游；勾选 **Rebuild the current patched image** 会重新构建当前版本的自有镜像。
