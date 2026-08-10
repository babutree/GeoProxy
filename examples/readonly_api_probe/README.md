# 只读 API 简易对接 / 探测

对照 WebUI「开放 API」页与 `docs/READONLY_API_DESIGN.md`。

## 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/ping` | 鉴权探活 |
| GET | `/api/v1/nodes` | 节点目录（含 `connect`） |
| GET | `/api/v1/occupancy` | 占用快照（内网地址会脱敏） |

## 鉴权

二选一：

```http
Authorization: Bearer <key>
X-API-Key: <key>
```

Key 在 WebUI **设置 → API Key** 创建，明文只显示一次。默认限流 **60/min/key**。

## 运行

```bash
# 推荐：环境变量放 Key，避免进 shell 历史时仍可用 flag
export GEOPROXY_API_BASE='http://127.0.0.1:7800'
export GEOPROXY_API_KEY='你的只读Key'

go run . -base "$GEOPROXY_API_BASE" -key "$GEOPROXY_API_KEY"
```

常用过滤：

```bash
# 仅美国、仅 SOCKS5、仅网关型（加密隧道）
go run . -region us -protocol socks5 -connect gateway -limit 50

# 仅可直连节点
go run . -connect direct -limit 50

# Cloudflare 未拦截 + OpenAI/Claude 可达
go run . -cf open -ai openai,claude -limit 30

# 改用 X-API-Key 头
go run . -header x-api-key
```

Windows PowerShell：

```powershell
$env:GEOPROXY_API_BASE = 'http://127.0.0.1:7800'
$env:GEOPROXY_API_KEY = '你的只读Key'
go run . -base $env:GEOPROXY_API_BASE -key $env:GEOPROXY_API_KEY -region us -limit 10
```

## 对接要点（程序侧）

1. **看 `connect.mode`，不要猜 `address`**
   - `direct`：`protocol://host:port` 直连上游（可能有节点自己的 user/pass，本 API 不返回上游账密）。
   - `gateway`：必须走网关 `host:gateway_socks5_port` 或 `host:gateway_http_port`，用户名用 `username_hint`，**密码是网关代理密码**（WebUI 设置/首次启动日志，API 不下发）。
2. **`username_hint` 可能缺失**
   无稳定 `node_key` 或非法地域时，会有 `username_hint_error`，不要伪造 `127.0.0.1` 端口。
3. **默认只返回可用节点**；要全量加 `status=all`。
4. **推荐轮询 5–10 分钟**，勿顶着 60/min 刷爆。
5. **occupancy** 中环回/RFC1918 等会变成 `gateway-local`。

## curl 对照

```bash
curl -sS -H "Authorization: Bearer $GEOPROXY_API_KEY" \
  "$GEOPROXY_API_BASE/api/v1/ping"

curl -sS -H "Authorization: Bearer $GEOPROXY_API_KEY" \
  "$GEOPROXY_API_BASE/api/v1/nodes?region=us&limit=5"

curl -sS -H "X-API-Key: $GEOPROXY_API_KEY" \
  "$GEOPROXY_API_BASE/api/v1/occupancy"
```
