# wa-app

当前项目版本：**v1.1.0**（2026-11-17）。[版本说明](./CHANGELOG.md) · [GitHub Release](https://github.com/Gnsvip/wa-app/releases/tag/v1.1.0)

项目版本与 WA 协议兼容版本独立：本版适配 **2.26.40.76**。注册流程已完成离线自动测试，真实号码收码注册尚未验证。

`wa-app` 是 WA 应用链路服务，提供账号与资料管理、号码探测、注册、登录态检查、长连接会话、消息与联系人处理、账号在线态与转出检测，并内置管理 dashboard。

> [!CAUTION]
> 使用本项目即表示你同意 [NOTICE](./NOTICE) 的全部条款。本项目仅限协议建模、教学演示、授权安全研究和内部非商业验证；禁止用于商业用途、未授权目标或违反第三方服务条款的场景。

## 功能

- 账号与资料：维护 WAAccount、客户端 profile、注册记录和登录态投影；支持资料编辑、头像、换绑号码，注册时从多机型画像池分配设备指纹。
- 号码与注册：支持号码探测、SMS 探测、注册请求、OTP 提交和登录态检查。
- 账号安全：两步验证（2FA PIN）状态管理与登录适配。
- 接码集成：dashboard 添加账号时可通过 `SMS-Code-Receiver` 自动拿号、等待验证码并提交注册。
- 连接与消息：支持长连接会话、消息接收与 ack、1:1 文本消息发送、会话查看、联系人解析（usync）和头像拉取。
- 在线态与转出检测：账号在线态由长连接实况派生；转出/远程登出按 device_removed / device_logout 判定并周期复核，避免状态漂移。
- 数据提取：从消息中提取 OTP/Flag 候选值，并按敏感数据规则保存引用或脱敏投影。
- 管理界面：提供 dashboard，用于账号、联系人、消息、连接状态、安全和账号资料操作。

## 部署方式

推荐使用本仓库提供的 Docker Compose 启动服务：

```sh
cp .env.example .env
cp SMS-Code-Receiver/config.example.json SMS-Code-Receiver/config.json
# 按需编辑 .env 与接码网关配置
docker build -t wa-app-service:v1.1.0 .
docker compose up -d
```

本仓库不包含账号数据库、登录密钥、接码平台凭据或线上部署配置；启动后是独立空实例。构建镜像无需访问其他私有仓库。

默认端口（固定）：

- Dashboard：`http://127.0.0.1:8080`（`docker-compose.yml` 映射）
- gRPC：`127.0.0.1:50091`

若你确实要改主机映射端口，请直接改 `docker-compose.yml` 的 `ports` 行（非配置项）。

### 配置

`.env` 中保留少量运行必需配置：

- `WA_APP_IMAGE_TAG`：镜像标签，生产建议使用固定版本。
- `WA_APP_AUTH_PASSWORD`：可选 dashboard 单密码登录；为空则关闭鉴权。
- `WA_APP_DATA_DIR`：容器内持久化目录；默认 `/var/lib/wa-app`。
- `WA_APP_PG_DSN`：可选 PostgreSQL DSN；为空时使用内置 SQLite 持久化。
- `WA_APP_REDIS_URL`：可选 Redis URL；为空时使用内置 SQLite 运行态存储。
- `WA_COMMON_PROXY`：可选 WA 出站代理；配置后所有出站走该共享代理，为空则直连。
- `WA_APP_DEVICE_PROFILES_FILE`：可选设备画像池文件路径；为空时使用内置多机型画像。
- `WA_APP_SMS_GATEWAY_URL`：可选 `SMS-Code-Receiver` HTTP 网关地址；compose 默认使用 `http://sms-code-receiver:9010`。
- `WA_APP_SMS_GATEWAY_TOKEN`：可选接码网关鉴权 token；服务器部署建议设置长随机值，并与 `SMS-Code-Receiver/config.json` 的 `gateway.apiToken` 保持一致。

PostgreSQL 和 Redis 都是可选组件。需要启用时，在 `docker-compose.yml` 中取消对应服务注释，并在 `.env` 中填写 `WA_APP_PG_DSN` / `WA_APP_REDIS_URL`。

### SMS-Code-Receiver 集成

`docker-compose.yml` 已包含 `sms-code-receiver` 服务，wa-app 通过内部地址调用它：

```sh
WA_APP_SMS_GATEWAY_URL=http://sms-code-receiver:9010
WA_APP_SMS_GATEWAY_TOKEN=替换为长随机token
```

服务器上需要保留整个 `SMS-Code-Receiver` 文件夹，并在 `SMS-Code-Receiver/config.json` 中配置接码平台 key、默认 WhatsApp 服务码和相同的 `gateway.apiToken`。真实 key 不要写进镜像或公开仓库；`.dockerignore` 已排除 `SMS-Code-Receiver/config.json`。

### 源码构建镜像

`Dockerfile` 支持在 `wa-app` 仓库内直接构建，不依赖 `common-lib` 构建上下文：

```sh
docker build -t wa-app-service:local .
```

在 `byte-v-forge` 聚合目录构建也可用：

```sh
docker build -f wa-app/Dockerfile -t wa-app-service:local .
```

### 历史记录保留与连接失败

SQLite 部署启动时及每小时自动分批清理历史数据，保留最近 10 天的消息、验证码记录和会话条目；有近期消息的联系人会保留。无消息引用且已结束超过 24 小时的连接会话、已过期的临时记录也会清理。每批最多选取 250 条主记录，不在线执行全库 VACUUM。账号、注册资料、登录密钥和未过期的取码链接不在清理范围内；历史数据删除后不能恢复。

WA 明确返回登录拒绝（401/403）时停止自动重连，并持久化“连接失败”及拒绝原因。重新启动服务不会继续重试这些登录态；显式检查登录状态成功后可恢复连接。网络超时等暂时故障仍按退避策略重连。

原生消息缓存每次保存时也会清除超过 10 天的内容，保留登录和 Signal 密钥。旧版缓存缺少时间戳时，从首次保存起给予 10 天过渡期。

## 逆向工程

WA APK 逆向工程、字段形状对齐记录、patch 模板和本地分析脚本统一维护在私有仓库：

- https://github.com/byte-v-forge/wa-reverse

`wa-app` 只保留运行时代码和服务说明，不提交 APK、反编译产物、抓包、WAMSYS blob 或逆向记录。

## 友情链接

- [LINUX DO - 新的理想型社区](https://linux.do/)
