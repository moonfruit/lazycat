# HttpBin for LazyCat

在喵喵商店添加以下订阅地址，同步后选择 **HttpBin** 安装或升级。

```text
https://moonfruit.github.io/lazycat/source/v2/index.json
```

LPK 直接使用官方镜像 `ghcr.io/psf/httpbin:<上游版本>`。在 `0.10.4` 中，Gunicorn 26.0.0 的默认 gevent worker 要求 gevent ≥24.10.1，镜像内装的是 24.2.1。LPK 使用 Gunicorn 自带的 `gthread` worker，以 4 个线程监听 8080。`0.10.4.4` 起使用 LPK v2 格式，旧包可通过同一订阅地址升级。

GitHub Actions 每天台北时间 05:43 检查 PSF 的稳定版本标签。新版本发布前，CI 读取成品 LPK 中的镜像和启动命令，在 amd64、arm64 上分别检查 `/get` 和容器内的 HttpBin 版本。LPK 前三段跟随上游版本，同一上游版本的打包调整使用第四段修订号。每个修订有独立的 Release，软件源索引会校验已发布附件的 SHA-256 和大小。

手动运行工作流时，选择 `httpbin` 可以发布当前版本并重建索引，勾选 `check_upstream` 可以先检查上游版本。[发布工作流](../.github/workflows/publish-lpks.yml)
