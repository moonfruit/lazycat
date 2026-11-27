# lazycat-httpbin
HttpBin for Lazycat

在喵喵商店添加以下订阅地址，同步后选择 **HttpBin** 安装或升级。

```text
https://moonfruit.github.io/lazycat/source/v2/index.json
```

GitHub Actions 每天台北时间 05:43 检查上游镜像。digest 变化时，工作流递增 LPK 补丁版本，打包并测试新版镜像的 `/get`，通过后将这次变更提交到 `main`。每个版本的 Release 包含 LPK、SHA-256 和字节数记录。生成软件源时会重新下载公开附件核对记录。

手动运行 **Publish HttpBin LPK and software source** 可以从现有 Release 恢复索引。勾选 **Check for a new upstream image digest** 可立即执行日常更新检查。
