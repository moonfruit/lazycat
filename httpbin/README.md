# HttpBin for LazyCat

在喵喵商店添加以下订阅地址，同步后选择 **HttpBin** 安装或升级。

```text
https://moonfruit.github.io/lazycat/source/v2/index.json
```

LPK 直接使用官方镜像 `ghcr.io/psf/httpbin:<上游版本>`。在 `0.10.4` 中，Gunicorn 26.0.0 的默认 gevent worker 要求 gevent ≥24.10.1，镜像内装的是 24.2.1。LPK 在 `lzc-manifest.yml` 中改用 Gunicorn 自带的 `gthread` worker，使用 4 个线程监听 8080。已使用自有镜像的用户可通过喵喵升级至 `0.10.4.3`。

GitHub Actions 每天台北时间 05:43 检查 PSF 的稳定版本标签。新版本发布前，CI 读取成品 LPK 中的镜像和启动命令，在 amd64、arm64 上分别检查 `/get` 和容器内的 HttpBin 版本。LPK 前三段跟随上游版本，同一上游版本的打包调整使用第四段修订号。每个修订有独立的 Release，软件源索引会校验已发布附件的 SHA-256 和大小。

手动运行 **Publish HttpBin LPK and software source** 可以从现有 Release 恢复索引。勾选 **Check for a new stable PSF image version** 可以立即检查上游。
