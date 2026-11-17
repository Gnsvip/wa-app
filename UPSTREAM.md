# 上游来源

本项目基于 WA 应用服务源码维护独立版本；原有使用须知见 [NOTICE](./NOTICE)。保留源码中的原始包路径与作者声明。

注册兼容修复参考上游 [pood1e/wa-app](https://github.com/pood1e/wa-app) 的提交 [`73423e459fd3dabaa0b4bf4453e0e96edee98e3f`](https://github.com/pood1e/wa-app/tree/73423e459fd3dabaa0b4bf4453e0e96edee98e3f)，移植 2.26.40.76 配套构建材料与三阶段请求结构，未整体迁移上游 V2。

`internal/app/android-2.26.40.76.pb` 是上述公开上游的构建兼容材料，不包含部署者账号的登录密钥或会话。项目不包含 APK、生产数据库或抓包数据。
