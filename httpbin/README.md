# lazycat-httpbin
HttpBin for Lazycat

在喵喵商店添加以下订阅地址，同步后选择 **HttpBin** 安装或升级。

```text
https://moonfruit.github.io/lazycat/source/v2/index.json
```

GitHub Actions 每天台北时间 05:43 检查 `ghcr.io/psf/httpbin`。出现更高的稳定版本标签（如 `0.10.4`）时，工作流将 LPK 版本设为该标签，锁定对应的镜像 digest，打包并测试 `/get`，通过后提交到 `main`。同一标签被重新推送时，已发布的 LPK 继续使用原 digest。每个版本的 Release 包含 LPK、SHA-256 和字节数记录。生成软件源时会重新下载公开附件核对记录。

手动运行 **Publish HttpBin LPK and software source** 可以从现有 Release 恢复索引。勾选 **Check for a new stable PSF image version** 可立即执行日常更新检查。
