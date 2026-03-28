# 代理认证功能测试指南

本文档说明如何测试 GoProxy 的代理认证功能。

## 🔒 启用认证

### 使用 docker-compose

编辑 `.env` 文件：

```bash
PROXY_HOST=0.0.0.0
PROXY_AUTH_ENABLED=true
PROXY_AUTH_USERNAME=testuser
PROXY_AUTH_PASSWORD=testpass123
```

启动服务：
```bash
docker compose up -d
```

### 直接运行

设置环境变量后启动：

```bash
export PROXY_AUTH_ENABLED=true
export PROXY_AUTH_USERNAME=testuser
export PROXY_AUTH_PASSWORD=testpass123
go run .
```

## 🧪 测试认证功能

### 测试 1：无认证请求（应该失败）

```bash
curl -x http://127.0.0.1:7777 https://httpbin.org/ip -v
```

**预期结果**：
- HTTP 状态码：`407 Proxy Authentication Required`
- 响应头：`Proxy-Authenticate: Basic realm="GoProxy"`

### 测试 2：错误的用户名/密码（应该失败）

```bash
curl -x http://wronguser:wrongpass@127.0.0.1:7777 https://httpbin.org/ip -v
```

**预期结果**：
- HTTP 状态码：`407 Proxy Authentication Required`

### 测试 3：正确的认证（应该成功）

```bash
curl -x http://testuser:testpass123@127.0.0.1:7777 https://httpbin.org/ip
```

**预期结果**：
- HTTP 状态码：`200 OK`
- 返回代理的出口 IP 信息

### 测试 4：环境变量方式（应该成功）

```bash
export http_proxy=http://testuser:testpass123@127.0.0.1:7777
export https_proxy=http://testuser:testpass123@127.0.0.1:7777
curl https://httpbin.org/ip
```

### 测试 5：HTTPS CONNECT 隧道（应该成功）

```bash
curl -x http://testuser:testpass123@127.0.0.1:7776 https://www.google.com -I
```

## 🧩 多语言客户端示例

### Python (requests)

```python
import requests

proxies = {
    'http': 'http://testuser:testpass123@127.0.0.1:7777',
    'https': 'http://testuser:testpass123@127.0.0.1:7777',
}

response = requests.get('https://httpbin.org/ip', proxies=proxies)
print(response.text)
```

### Go

```go
package main

import (
    "fmt"
    "io"
    "net/http"
    "net/url"
)

func main() {
    proxyURL, _ := url.Parse("http://testuser:testpass123@127.0.0.1:7777")
    client := &http.Client{
        Transport: &http.Transport{
            Proxy: http.ProxyURL(proxyURL),
        },
    }
    
    resp, err := client.Get("https://httpbin.org/ip")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()
    
    body, _ := io.ReadAll(resp.Body)
    fmt.Println(string(body))
}
```

### Node.js

```javascript
const axios = require('axios');
const HttpsProxyAgent = require('https-proxy-agent');

const proxyUrl = 'http://testuser:testpass123@127.0.0.1:7777';
const agent = new HttpsProxyAgent(proxyUrl);

axios.get('https://httpbin.org/ip', { 
    httpAgent: agent,
    httpsAgent: agent 
})
.then(response => console.log(response.data))
.catch(error => console.error(error));
```

## 🔍 日志验证

启动时会输出认证状态：

```
proxy server listening on :7777 [随机轮换] [需认证 (用户: testuser)]
proxy server listening on :7776 [最低延迟] [需认证 (用户: testuser)]
```

无认证模式：

```
proxy server listening on :7777 [随机轮换] [无认证]
proxy server listening on :7776 [最低延迟] [无认证]
```

## 📝 注意事项

- 认证采用 **HTTP Basic Auth** 标准，兼容绝大多数客户端
- 密码存储为 **SHA256 哈希**，安全性高
- 认证失败不会删除代理或影响池子状态
- 两个端口（7776、7777）共享相同的认证配置
- 认证仅保护代理服务，WebUI 有独立的登录系统
