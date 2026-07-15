# GoProxy 测试脚本

本目录包含用于测试 GoProxy 代理服务的脚本。所有脚本都采用**持续运行模式**（类似 `ping` 命令），按 `Ctrl+C` 停止并显示统计。

## 📝 脚本列表

| 脚本 | 语言 | 依赖 | 运行模式 | 推荐度 |
|------|------|------|----------|--------|
| `test_proxy.sh` | Bash | curl + Python3 | 持续运行 | ⭐⭐⭐ |
| `test_socks5.sh` | Bash | curl + Python3 | 持续运行 | ⭐⭐⭐ |
| `test_http_https.sh` | Bash | curl + Python3 | 持续运行 / 可限次 | ⭐⭐⭐ |
| `test_proxy.go` | Go | 标准库 | 持续运行 | ⭐⭐ |
| `test_proxy.py` | Python | `requests` | 持续运行 | ⭐⭐ |

## 🚀 快速使用

### Bash 脚本（推荐）

**HTTP 代理测试**（探测出口 IP）：
```bash
# 先填入首次启动日志或 WebUI Settings 中的实际代理认证信息；不要把密码写入脚本。
export GOPROXY_AUTH_USERNAME=acct
export GOPROXY_AUTH_PASSWORD='replace-with-your-proxy-password'

# 测试 HTTP 代理端口（默认 7802）
./test/test_proxy.sh 7802

# 按 Ctrl+C 停止并查看统计
```

**HTTP 代理 HTTPS 隧道测试**（CONNECT，随机访问多个 HTTPS 站点）：
```bash
# 复用上面的 GOPROXY_AUTH_USERNAME / GOPROXY_AUTH_PASSWORD。

# 持续运行
./test/test_http_https.sh 7802

# 或指定次数后退出
./test/test_http_https.sh 7802 10
```

**SOCKS5 代理测试**：
```bash
# 复用上面的 GOPROXY_AUTH_USERNAME / GOPROXY_AUTH_PASSWORD。

# 测试 SOCKS5 代理端口（默认 7801）
./test/test_socks5.sh 7801

# 按 Ctrl+C 停止并查看统计
```

### Go 脚本

```bash
# 运行测试（可选端口参数，默认 7802）
go run test/test_proxy.go
go run test/test_proxy.go 7802

# 或编译后运行
cd test
go build -o test_proxy test_proxy.go
./test_proxy 7802
```

### Python 脚本

```bash
# 安装依赖
pip install requests

# 运行测试（可选端口参数，默认 7802）
python test/test_proxy.py
python test/test_proxy.py 7802
```

## 📊 测试内容

| 脚本 | 默认端口 | 探测目标 | 间隔 |
|------|----------|----------|------|
| `test_proxy.sh` / `.go` / `.py` | `127.0.0.1:7802` | `http://ip-api.com/json/?fields=countryCode,query` | 1s |
| `test_socks5.sh` | `127.0.0.1:7801` | `https://httpbin.org/ip`（成功后再查国家） | 1s |
| `test_http_https.sh` | `127.0.0.1:7802` | 随机 HTTPS 站点（Google/OpenAI/GitHub 等） | 2s |

共性行为：
1. 通过本地代理端口转发请求；`test_proxy.sh` / `test_socks5.sh` / `test_http_https.sh` / `test_proxy.go` / `test_proxy.py` 都要求 `GOPROXY_AUTH_USERNAME` 和 `GOPROXY_AUTH_PASSWORD`，缺失时会直接报错退出
2. **持续发送**（类似 `ping`），`test_http_https.sh` 可用第 2 个参数限次
3. 实时输出成功/失败与延迟
4. 按 `Ctrl+C` 停止并打印统计摘要

可选路由参数：设置 `GOPROXY_AUTH_REGION=us` 会把认证用户名扩展为 `acct-region-us`，设置 `GOPROXY_AUTH_SESSION=browser` 会追加 `-session-browser`。只在环境变量中提供真实密码，不要写入仓库文件。

## 🔀 测试不同协议端口

### HTTP 代理

```bash
export GOPROXY_AUTH_USERNAME=acct
export GOPROXY_AUTH_PASSWORD='replace-with-your-proxy-password'
./test/test_proxy.sh 7802
./test/test_http_https.sh 7802
```

**观察要点**：
- **7802**：HTTP 代理；`test_proxy.sh` 应返回出口 IP
- **CONNECT**：`test_http_https.sh` 应对 HTTPS 目标返回 2xx/3xx

### SOCKS5 代理

```bash
export GOPROXY_AUTH_USERNAME=acct
export GOPROXY_AUTH_PASSWORD='replace-with-your-proxy-password'
./test/test_socks5.sh 7801
```

**观察要点**：
- **7801**：SOCKS5 代理，应返回出口 IP

> 提示：`test_socks5.sh` 与 `test_http_https.sh` 使用 `curl -k` 跳过 TLS 证书校验，便于连通性测试；生产环境请使用可信上游。

## 🔍 预期输出

```
PROXY 127.0.0.1:7802 (http://ip-api.com/json/?fields=countryCode,query): continuous mode

proxy from 🇺🇸 203.0.113.45: seq=1 time=1234ms
proxy from 🇩🇪 198.51.100.78: seq=2 time=987ms
proxy from 🇬🇧 192.0.2.123: seq=3 time=1567ms
proxy #4: request failed (timeout)
proxy from 🇯🇵 198.51.100.12: seq=5 time=890ms
proxy from 🇫🇷 192.0.2.234: seq=6 time=1456ms
...
（持续运行，按 Ctrl+C 停止）

^C
---
50 requests transmitted, 47 received, 3 failed, 6.0% packet loss
```

**输出风格**：
- 简洁清晰，类似 `ping` 命令
- 一行一个结果
- 显示国旗 emoji、出口 IP、序号、延迟
- 统计信息简洁明了

**观察要点**：
- 每次请求的出口 IP 应该不同（证明代理轮换）
- 延迟应该在合理范围（< 2000ms）
- 丢包率应该 < 10%
- 可以长时间运行观察稳定性

## 📝 注意事项

1. 确保 GoProxy 服务已启动：`./goproxy`
2. 首次启动需保存日志中一次性打印的代理认证用户名和密码；脚本不会使用默认密码，也不会从 WebUI 密码推断代理密码
3. 可配合 WebUI (http://localhost:7800) 查看实时状态
