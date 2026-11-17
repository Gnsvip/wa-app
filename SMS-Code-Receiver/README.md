# SMS Code Receiver

独立接码工具，只负责：

- 从短信平台购买/提取号码
- 显示号码给你去别的网站注册
- 轮询短信验证码
- 成功后完成订单，失败时手动取消订单

不包含 ChatGPT/OpenAI 注册、浏览器自动化、邮箱绑定、OAuth。

## 直接使用

双击：

```bat
run-sms-receiver.bat
```

如果要对接别的项目，优先启动本地 API：

```bat
run-sms-gateway.bat
```

默认地址：

```text
http://127.0.0.1:9010
```

服务器部署时，如果要让别的机器访问，把 `config.json` 里的 `gateway.host` 改成：

```json
{
  "gateway": {
    "host": "0.0.0.0",
    "port": 9010,
    "apiToken": "你的长随机token"
  }
}
```

公网/服务器对接一定要保留 `apiToken`，否则别人知道地址就能消耗你的接码余额。

当前本地配置文件：

```text
C:\Users\GN\Desktop\SMS-Code-Receiver\config.json
```

你的原项目 key 已经复制到这个本地配置里。`config.example.json` 只保留占位符，不放真实 key。

## WhatsApp 服务码

- SMSBower / HeroSMS：`wa`
- 5sim：`whatsapp`

当前默认已经改成 WhatsApp：

```json
{
  "service": "wa",
  "fiveSim": {
    "product": "whatsapp"
  }
}
```

## 常用命令

```bat
run-sms-receiver.bat
run-sms-receiver.bat --provider smsbower --service wa --countries 33,151,16 --max-price 0.35
run-sms-receiver.bat --provider herosms --service wa --countries 31,4,6 --max-price 0.35
run-sms-receiver.bat --provider 5sim --five-product whatsapp --five-country england
run-sms-receiver.bat --balance
run-sms-receiver.bat --prices --service wa
```

说明：

- `--service` 是 SMSBower/HeroSMS 的项目代码。
- `--countries` 是国家 ID，多个用逗号分隔。
- `--max-price` 是最高价格，`0` 表示不限制。
- `--provider` 可选 `smsbower`、`herosms`、`5sim`。
- 5sim 不用 `--service`，用 `--five-product whatsapp`。

## 对接别的项目

### HTTP API

启动：

```bat
run-sms-gateway.bat
```

检查：

```text
GET http://127.0.0.1:9010/health
GET http://127.0.0.1:9010/docs
```

如果配置了 `apiToken`，请求要带：

```http
Authorization: Bearer 你的token
```

或者：

```http
X-Api-Key: 你的token
```

拿号：

```http
POST http://127.0.0.1:9010/allocate
Content-Type: application/json
Authorization: Bearer 你的token

{
  "provider": "smsbower",
  "service": "wa",
  "countries": [33, 151, 16],
  "maxPrice": 0.35
}
```

返回示例：

```json
{
  "ok": true,
  "order": {
    "provider": "smsbower",
    "id": "123456789",
    "phone": "+12025550123",
    "country": "33",
    "price": "0.335",
    "service": "wa"
  }
}
```

查码一次：

```http
POST http://127.0.0.1:9010/code
Content-Type: application/json
Authorization: Bearer 你的token

{
  "provider": "smsbower",
  "id": "123456789"
}
```

等待验证码：

```http
POST http://127.0.0.1:9010/wait-code
Content-Type: application/json
Authorization: Bearer 你的token

{
  "provider": "smsbower",
  "id": "123456789",
  "markReady": true,
  "timeout": 180,
  "interval": 5
}
```

成功后完成订单：

```http
POST http://127.0.0.1:9010/finish
Content-Type: application/json
Authorization: Bearer 你的token

{
  "provider": "smsbower",
  "id": "123456789"
}
```

失败/不用了取消订单：

```http
POST http://127.0.0.1:9010/cancel
Content-Type: application/json
Authorization: Bearer 你的token

{
  "provider": "smsbower",
  "id": "123456789"
}
```

### JSON CLI

不想开 HTTP 服务，也可以直接让别的项目调用命令行：

```bat
sms-cli.bat health
sms-cli.bat allocate --provider smsbower --service wa --countries 33,151,16 --max-price 0.35
sms-cli.bat wait-code --provider smsbower --id 123456789 --mark-ready --timeout 180
sms-cli.bat finish --provider smsbower --id 123456789
sms-cli.bat cancel --provider smsbower --id 123456789
```

命令行输出也是 JSON，方便别的项目解析。

### 放到 Linux 服务器

把整个 `SMS-Code-Receiver` 文件夹复制到服务器，安装 Python 3，然后：

```bash
chmod +x run-sms-gateway.sh
./run-sms-gateway.sh
```

也可以直接运行：

```bash
python3 sms_gateway.py serve --host 0.0.0.0 --port 9010
```

服务器安全建议：

- `config.json` 里必须设置 `gateway.apiToken`。
- 服务器防火墙只放行你自己项目所在 IP。
- 如果平台接口需要代理，把 `proxy` 改成服务器可用的代理；没有代理就设成空字符串 `""`。

如果随 `wa-app-main` 一起部署，根目录 `docker-compose.yml` 已内置 `sms-code-receiver` 服务。保持本目录的 `config.json` 在服务器本地存在，并让根目录 `.env` 的 `WA_APP_SMS_GATEWAY_TOKEN` 与本文件里的 `gateway.apiToken` 一致即可。

## 使用流程

1. 工具拿到号码后，会显示 phone / provider / order id / price。
2. 你把号码填到目标网站。
3. 目标网站发送短信后，回到工具按回车开始等码。
4. 收到验证码后工具显示 code。
5. 注册成功按回车完成订单；失败输入 `c` 取消订单。

## 数据

运行记录保存在：

```text
data\orders.jsonl
```
