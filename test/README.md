# GeoProxy 测试脚本

本目录包含用于验证 GeoProxy 代理服务的脚本。两个协议探针默认持续运行，
也可以指定有限的测试轮数，便于在自动化验收中取得明确退出码。

## 脚本列表

| 脚本 | 语言 | 依赖 | 运行模式 |
|------|------|------|----------|
| `test_proxy.sh` | Bash | curl + Python3 | 持续运行 |
| `test_socks5.sh` | Bash | curl + Python3 | 持续运行 / 可限轮数 |
| `test_http_https.sh` | Bash | curl + Python3 | 持续运行 / 可限轮数 |
| `test_proxy.go` | Go | 标准库 | 持续运行 |
| `test_proxy.py` | Python | `requests` | 持续运行 |

## 快速使用

先在当前终端设置真实认证信息；密码只放在环境变量中，不要写入脚本或仓库：

```bash
export GEOPROXY_AUTH_USERNAME=username
export GEOPROXY_AUTH_PASSWORD='replace-with-your-proxy-password'
```

HTTP 代理的 HTTPS CONNECT 探针（默认端口 `7802`）：

```bash
# 持续观察
./test/test_http_https.sh 7802

# 自动化验收：运行 2 轮后退出
./test/test_http_https.sh 7802 2
```

SOCKS5 探针（默认端口 `7801`）：

```bash
# 持续观察
./test/test_socks5.sh 7801

# 自动化验收：运行 2 轮后退出
./test/test_socks5.sh 7801 2
```

可选路由参数仍通过环境变量提供：

```bash
export GEOPROXY_AUTH_REGION=us
export GEOPROXY_AUTH_SESSION=browser
```

脚本会把它们追加到认证用户名的路由 DSL 中，不会把密码写入命令行参数。

## 多目标探针合同

`test_socks5.sh` 和 `test_http_https.sh` 各自有至少两个默认 HTTPS 目标，
由不同服务提供方托管，用来降低单一第三方故障造成的误报：

- SOCKS5：`api.ipify.org`、`checkip.amazonaws.com`
- HTTP CONNECT：`www.cloudflare.com`、`api.ipify.org`

可以用 `GEOPROXY_PROBE_URLS` 覆盖目标列表。值是换行分隔的 HTTPS URL，
每行一个；空行会被忽略，非 HTTPS URL 会立即以配置错误退出：

```bash
export GEOPROXY_PROBE_URLS=$'https://health-a.example/\nhttps://health-b.example/'
./test/test_http_https.sh 7802 1
```

每轮探针从轮换起点开始，最多逐个尝试所有目标；任一目标返回 `2xx/3xx`
即判定该轮成功。收到 `4xx` 或 `5xx` 会记录为“目标站失败”，不会被当成
成功；curl 没有拿到 HTTP 响应时会单独记录为“代理链或传输失败”。只有
**全部目标失败**时，该轮才计为失败。每个目标的 URL、失败原因和耗时都会
输出，便于区分目标站故障与代理链故障。

有限轮数的退出码合同：

- `0`：所有已完成轮次至少有一个目标成功；
- 退出码 `1`：至少一轮出现全部目标失败；
- `2`：认证、轮数或目标配置无效。

持续模式按 `Ctrl+C` 停止并打印同样的轮次摘要；自动化门禁请使用有限轮数，
不要把真实外网可用性作为唯一门禁。

### 离线合同门禁

无法启动真实网关或执行 Bash 时，可在仓库根目录运行：

```pwsh
go test ./test -run TestProbe -count=1 -timeout 60s
```

该测试包含静态合同和真实 Bash 行为场景：fake curl 只接受 `.invalid` 保留域，
并验证回退、2xx/3xx、4xx/5xx、curl 非零、无效配置和未知目标保护。测试不访问
网络、不需要代理凭据；Windows 缺少 WSL Debian 或 Linux 缺少 `/bin/bash`
时会明确失败，不会静默跳过。

## 其他测试脚本

`test_proxy.sh`、`test_proxy.go` 和 `test_proxy.py` 保留原有的单目标出口
IP 检查，用于快速人工观察。它们不参与上述多目标退出码合同。

## 测试内容

| 脚本 | 默认端口 | 默认目标 | 默认间隔 |
|------|----------|----------|----------|
| `test_proxy.sh` / `.go` / `.py` | `127.0.0.1:7802` | `http://ip-api.com/json/?fields=countryCode,query` | 1s |
| `test_socks5.sh` | `127.0.0.1:7801` | 两个 HTTPS 出口 IP 服务，逐轮回退 | 1s |
| `test_http_https.sh` | `127.0.0.1:7802` | 两个 HTTPS CONNECT 目标，逐轮回退 | 2s |

共性行为：

1. 通过本地代理端口转发请求；三个 Bash 探针都要求
   `GEOPROXY_AUTH_USERNAME` 和 `GEOPROXY_AUTH_PASSWORD`。
2. 第 2 个参数为测试轮数；省略或设为 `0` 时持续运行。
3. 每次尝试输出目标、状态或 curl 错误和延迟。
4. 按 `Ctrl+C` 停止并打印轮次摘要。

## 按协议验证

### HTTP 代理

```bash
./test/test_proxy.sh 7802
./test/test_http_https.sh 7802 1
```

`test_http_https.sh` 验证 HTTPS CONNECT；只接受 `2xx/3xx`，不会把任意
`4xx` 当作成功。

### SOCKS5 代理

```bash
./test/test_socks5.sh 7801 1
```

脚本通过两个 HTTPS 出口服务验证 SOCKS5 转发；一个目标不可用时会尝试
另一个目标。

两个 Bash 探针使用 `curl -k`（即 `--insecure`）进行连通性验证，生产环境
应确保上游证书链可信。超时可通过 `GEOPROXY_PROBE_CONNECT_TIMEOUT` 和
`GEOPROXY_PROBE_TIMEOUT` 调整，轮间隔可通过 `GEOPROXY_PROBE_DELAY` 调整。

## 注意事项

1. 确保 GeoProxy 服务已启动：`./geoproxy`。
2. 首次启动需保存日志中一次性打印的代理认证用户名和密码；脚本不会使用
   默认密码，也不会从 WebUI 密码推断代理密码。
3. 脚本会在临时目录创建权限为用户私有的 curl 认证配置，并在退出时清理；
   不要把真实凭据或带凭据的目标 URL 写入仓库文件。
4. 可配合 WebUI（`http://localhost:7800`）查看实时状态。
