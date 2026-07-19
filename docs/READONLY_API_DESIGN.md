# 只读节点 API 设计（对外开放，API Key 鉴权）

> 状态：阶段 A 已实现（nodes / occupancy / ping / API Key 管理 / WebUI 开放 API 页）。
> 目标：让外部程序用 API Key 拉取全部节点的 `协议 / IP / 端口 / 区域 / 纯净度 / AI 解锁 / CF 解锁` 等信息。
> 边界：本设计只加"读"，不改选路、不改订阅转发、不改现有 WebUI 鉴权。

## 1. 为什么要这个 API（问题回顾）

现状核实：

- 所有 `/api/*` 都过 `authMiddleware`，依赖浏览器 **session cookie + CSRF**，没有面向程序的鉴权。
- `/api/proxies` 已含全部字段，但只服务自己的前端，程序无法用 Token 直接取。
- 直连 `http/socks5/https` 节点本身就能被客户端**直连**，不必再经我们服务器转发；我们的价值是"采集 + 纯净度/解锁评估 + 目录"。
- 加密订阅节点（vmess/vless/trojan/ss/hysteria2…）**必须**经 sing-box 转成本地 socks5 才能用。

结论：新增一个**只读、API Key 鉴权**的出口，让程序自取节点目录；直连节点让客户端直连、加密节点仍走我们网关。

## 2. 核心约束：两类节点"怎么用"不同

| 类别 | `source` | 存储 `address` | 外部程序怎么用 |
|---|---|---|---|
| 直连节点 | manual / subscription | 真实 `IP:port` | **直连**该地址（http/socks5） |
| 加密节点 | subscription（tunnel） | `127.0.0.1:本地端口` | **只能经我们网关**（网关 socks5/http 端口 + DSL 用户名选路） |

因此 API 每个节点必须显式给出 `connect`（怎么连），不能只丢一个 `address` 让调用方猜。

- `dual_protocol=true` 的节点即"本地 mixed 端口"（tunnel）。
- `address` 以 `127.0.0.1` / `::1` 开头的即本地端口，外部不可直连。

### 2.1 为什么加密节点不能"把 127.0.0.1 换成公网 IP"

已从代码确认（`custom/singbox.go`）：每个加密节点的 mixed 端口只监听在 loopback：

```go
"listen": "127.0.0.1",
"listen_port": mixedPort,
```

- 这些本地端口**只有本机进程能连**，公网 IP 上根本没有监听——把 `127.0.0.1:20001` 显示成 `<公网IP>:20001` 会给调用方一个**连不上的假地址**。
- 若强行改成 `0.0.0.0` 让外部可直连，则这些 inbound **无鉴权**，等于对全网开放数千个无认证代理端口（灾难级安全事故）。

结论（已决策）：**加密节点一律走 `mode=gateway`**——connect 指向服务器公网 IP 上的网关入口端口（7801/7802）+ DSL 用户名，经网关代理认证转发。真实服务器 IP 只用于填充网关/直连入口地址，**绝不用于伪造加密节点的本地端口**。

### 2.2 服务器真实 IP 来源（已决策：出口探测 + 可配置覆盖）

`connect.host` 需要一个"外部可达的服务器地址"。取值优先级：

1. `config.PublicHost`（新增，可配置覆盖）——运维显式设置的域名或公网 IP，最高优先。
2. 出口 IP 探测——复用现有 `webui/public_ip.go` 已探测到的服务器出口 IP（进程内缓存）。
3. 兜底——请求 `Host` 头的主机名（去端口）。三者都拿不到时，`connect.host` 置空并在响应中标 `host_unresolved: true`，绝不填 `127.0.0.1`。

同一来源同时用于 `mode=gateway` 的 `gateway_host` 和 `mode=direct` 节点里本身已是公网 IP 的场景（direct 节点 host 用节点自身真实 IP，不受此逻辑影响）。

## 3. 鉴权：独立 API Key

- 与 WebUI 密码**完全独立**，互不影响。
- 存储：`config.json` 增加 `ReadOnlyAPIKeys []APIKey`，其中只存 key 的 **SHA-256 hash**，不落明文（与现有 `WebUIPasswordHash` 一致）。
  ```
  APIKey { ID string; Name string; Hash string; CreatedAt time; LastUsedAt time; Disabled bool }
  ```
- 传递：请求头 `Authorization: Bearer <key>` 或 `X-API-Key: <key>`。
- 校验：常量时间比较（`crypto/subtle`），命中任一未禁用 key 即通过。
- 生成/吊销：管理入口在 `webui/apikey_handlers.go` 中独立生成 API Key，独立调用 `crypto/rand` 生成 16 字节随机 bearer secret，再编码为十六进制；它不复用 config 包的其它随机凭据 helper。管理入口本轮先给最小实现（见 §7），首次可用环境变量 `READONLY_API_KEYS`（逗号分隔明文，仅首启导入后转存 hash）注入。
- 只读：这些 key 只能访问 `GET /api/v1/nodes`、`GET /api/v1/occupancy` 和 `GET /api/v1/ping`，不能触碰任何写接口或 WebUI 管理接口。
- 限流：默认 **60 req/min/key**（令牌桶，内存），配置项 `READONLY_API_RATE_PER_MIN` 可调；命中返回 429。推荐调用方轮询间隔 **5–10 分钟**（对齐健康检查 `HealthIntervalMinutes` 默认 5，数据最快 5 分钟才变一次）——推荐间隔与硬限流是两个独立概念，硬限流仅防刷爆、不干扰正常按需拉取。

### 3.1 配置与限流

- `PUBLIC_HOST`、`READONLY_API_KEYS` 和 `READONLY_API_RATE_PER_MIN` 只在首次启动、尚无 `config.json` 时导入。已有配置以 `config.json` 为准。
- Server 构造时按已保存的 `ReadOnlyAPIRatePerMin` 创建并固定限流器速率；修改该值后需要重启服务才会生效。`ensureAPIKeyLimiter` 只是测试或防御性惰性初始化入口，用于处理手工构造且尚未设置限流器的 `Server`，不会把运行中的速率切换成热配置。
- 每个 API Key 独立维护令牌桶。空闲桶会定期清理，桶数量达到上限时拒绝创建新桶，避免未受限的内存增长。

### 3.2 API Key 生命周期

- 管理员可在 WebUI 设置页创建、吊销和删除 API Key；创建响应中的明文 Key 仅显示一次。
- Key 名称不能为空。服务只持久化 Key 的 SHA-256 哈希，并使用常量时间比较验证请求。
- 已有 Key 保持兼容；新旧 Key 都使用同一哈希格式。
- 首次构造 `Server` 时限流器已经按配置初始化；后续请求路径调用 `ensureAPIKeyLimiter` 仅作为防御性惰性初始化，不改变已固定的速率或 API Key 生命周期。

## 4. 端点

### `GET /api/v1/nodes`

鉴权：API Key。方法：仅 GET。

查询参数（全部可选，组合为 AND）：

| 参数 | 含义 | 示例 |
|---|---|---|
| `region` | 区域码过滤（ISO alpha-2，小写） | `region=us` |
| `protocol` | `http` / `socks5` | `protocol=socks5` |
| `source` | `manual` / `subscription` | `source=manual` |
| `connect` | `direct` / `gateway`（直连 vs 必须经网关） | `connect=direct` |
| `status` | 默认只返回可用；`all` 返回含停用/失败 | `status=all` |
| `max_abuse` | ipapi.is 滥用分上限（0–1） | `max_abuse=0.1` |
| `cf` | `open`（未拦截）/ `blocked` | `cf=open` |
| `ai` | 需可达的 AI，逗号分隔（openai/claude/grok/gemini） | `ai=openai,claude` |
| `limit` / `offset` | 分页，`limit` 默认 500、上限 2000 | `limit=100` |

默认（不传 `status`）：仅 `active/degraded && !user_paused && fail_count<3`，与选路口径一致。

稳定契约：

- `limit` 不传时默认 500；显式传入时必须是 `1..2000` 的整数。
- `offset` 不传时默认 0；显式传入时必须是非负整数。
- `max_abuse` 显式传入时必须是 `0..1` 的数字；未探测纯净度（`ipapiis_score=-1`）不通过该过滤。
- `cf` 只接受空值、`open`、`blocked`。
- `status` 只接受空值或 `all`；空值表示默认可用节点过滤。
- `connect` 只接受空值、`direct`、`gateway`。
- 上述参数非法时返回 HTTP 400，不静默忽略。

### 响应

```jsonc
{
  "total": 1234,          // 所有查询过滤条件（含 connect）命中后的分页前总数
  "count": 100,           // 当前页条数
  "nodes": [
    {
      "id": 42,
      "protocol": "socks5",
      "source": "manual",
      "region": "us",
      "region_source": "manual",

      // 怎么连：direct=直连 node.host:port；gateway=经我们网关
      "connect": {
        "mode": "direct",
        "host": "1.2.3.4",
        "port": 1080,
        "dual_protocol": false
      },

      // 出口与延迟
      "exit_ip": "1.2.3.4",
      "exit_location": "US / California / Santa Clara",
      "latency_ms": 83,
      "quality_grade": "A",

      // 纯净度（原始多字段，不聚合）
      "purity": {
        "ipapiis_abuse_score": 0.02,   // -1=未探测
        "ipapi_flags": ["hosting"],    // 空数组=本次无命中
        "ipapi_flags_seen": true       // false=未探测，flags 不可信
      },

      // 解锁
      "cf_blocked": 0,                 // -1未探测 0未拦截 1拦截
      "ai_reachability": {             // -1未探测 0可达 1不可达；缺字段=未探测
        "openai": 0, "claude": 1, "grok": -1, "gemini": 0
      },

      "last_check": "2026-07-12T16:21:24Z",
      "status": "active"
    },
    {
      "id": 77,
      "protocol": "socks5",
      "source": "subscription",
      "region": "jp",
      "connect": {
        "mode": "gateway",           // 加密节点：外部不可直连
        "host": "203.0.113.200",     // 服务器公网 IP（覆盖/探测得来；取不到则为空）
        "gateway_socks5_port": 7801,
        "gateway_http_port": 7802,
        "username_hint": "username-region-jp-node-key-Z2F0ZXdheS1qcC00Mg-session-api",
        "note": "需网关代理认证；密码见部署配置。host 为空时请用部署域名/请求 Host。"
      },
      "exit_ip": "203.0.113.9",
      "latency_ms": 120,
      "purity": { "ipapiis_abuse_score": 0.0, "ipapi_flags": [], "ipapi_flags_seen": true },
      "cf_blocked": 0,
      "ai_reachability": { "openai": 0 },
      "status": "active"
    }
  ]
}
```

#### `username_hint` 稳定契约

`username_hint` 只用于 `mode=gateway` 且具有稳定 `node_key` 的节点。它必须
锁定响应中的具体节点，不能只按地域再次随机选路。固定 DSL 顺序为：

```text
<base>[-region-<cc>]-node-key-<base64url(node_key)>-session-api
```

例如存储态 `node_key=gateway-jp-42` 编码为
`Z2F0ZXdheS1qcC00Mg`，完整提示为
`username-region-jp-node-key-Z2F0ZXdheS1qcC00Mg-session-api`；经
`auth.ParseUsername` 解析后 `Node` 必须精确等于 `key-gateway-jp-42`。
同地域节点的 `node_key` 不同，提示也必须不同。

地域按以下规则处理：

- 语法合法地域必须恰好由两个 ASCII 字母组成，输入大小写统一规范化为小写；
  这里只做语法校验，不检查 ISO 3166 分配表。例如 `KR` 生成
  `username-region-kr-node-key-...-session-api`；`ZZ` 即使未分配也合法。
- 地域为空或仅含空白时表示无地域约束，生成
  `username-node-key-...-session-api`，省略整个 `-region-` 段。
- 遗留数据库中的语法非法非空地域（例如 `unknown`、`USA`、`u1`）
  不会被拼入 DSL，也不会静默降级为无地域约束。该节点仍正常返回，
  但 `connect` 省略 `username_hint`，并返回稳定错误字段：

```jsonc
{
  "mode": "gateway",
  "username_hint_error": "cannot generate username hint: node region must be empty or a 2-letter country code"
}
```

遗留 gateway 节点若没有稳定 `node_key`，同样保留节点但省略
`username_hint`，返回固定错误：

```jsonc
{
  "mode": "gateway",
  "username_hint_error": "cannot generate username hint: gateway node has no stable node key"
}
```

无 key 时禁止回落到 `127.0.0.1:临时端口` 或其它 address pin。隧道刷新导致
mixed 端口变化时，只要 `node_key` 不变，提示必须保持不变。direct 节点不使用
此提示，连接合同不变。

单个脏节点的提示生成失败只影响该节点的 `connect`，不会使整个
`GET /api/v1/nodes` 响应变成 HTTP 500。

设计要点：

- `connect.mode` 是调用方唯一需要看的"怎么用"开关。
- `total` 是所有查询过滤条件（包含 `connect`）生效后的分页前总数；`count` 是当前页实际返回节点数量。
- 直连节点给 `host/port`，程序直接 `protocol://host:port` 用。
- 加密节点**不暴露** `127.0.0.1:port`（对外无意义），改给网关端口 + 按稳定 NodeKey 锁定的 DSL 用户名提示；密码不下发（安全）。
- 纯净度保持**原始多字段**，调用方自行定阈值。
- 未探测一律用 `-1` / `seen=false` / 缺字段表达，不伪造"干净"。

### `GET /api/v1/occupancy`（只读占用快照）

鉴权：API Key。方法：仅 GET。返回每个有活跃绑定的代理节点的占用快照：

```jsonc
[
  {
    "proxy_id": 42,
    "address": "203.0.113.90:8080",   // 私有/内网地址会被脱敏，见下
    "active_sessions": 1,
    "max_sessions": 2,
    "cooldown_remaining_seconds": 0,
    "note": ""                          // 脱敏时给出原因说明
  }
]
```

地址脱敏策略：

- 只读端点**绝不下发私有/内网绑定地址**。命中以下任一类别的 `address` 会被替换为 `"gateway-local"`，并在 `note` 标注 `"private/internal address redacted"`：
  - 环回：IPv4 `127.0.0.0/8`、`localhost`、IPv6 `::1`。
  - RFC1918：`10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16`。
  - CGNAT 共享地址空间：`100.64.0.0/10`。
  - 链路本地：IPv4 `169.254.0.0/16`、IPv6 `fe80::/10`。
  - IPv6 ULA：`fc00::/7`（含 `fd00::/8`）。
  - 未指定地址：`0.0.0.0` / `::`。
  - 非 IP 主机名（无法证明为公网）一律按内网处理并脱敏。
- **公网地址原样返回**，`note` 为空，方便调用方使用。
- 脱敏仅施加于只读端点 `apiV1Occupancy`；`buildProxyOccupancyRows` 本身不改，因此**管理端 `/api/proxy-occupancy`（session+CSRF）行为不变，仍显示真实绑定地址**（运维需要看真实拓扑）。这是"读写口径不同"的显式决策，不是回退或降级。

## 5. 安全边界

- API Key 只读，物理隔离于写接口：写接口继续走 `authMiddleware`（session+CSRF），API Key 不被 `authMiddleware` 接受；只读接口始终要求 API Key，单独的 WebUI session 不足以访问。
- **绝不下发**：WebUI 密码、代理认证密码、API Key 明文、订阅原始 URL/凭据。
- tunnel 节点绝不下发 `127.0.0.1:port`（避免误导 + 轻微信息泄露）。
- 只读 occupancy 端点绝不下发私有/内网绑定地址（环回 / RFC1918 / CGNAT 100.64/10 / 链路本地 / IPv6 ULA / 未指定），一律脱敏为 `gateway-local`；仅公网地址可见。管理端 `/api/proxy-occupancy` 不受此脱敏影响，仍显示真实地址。
- key hash 存储，明文只在创建时返回一次。
- 限流防拉爆；`LastUsedAt` 用于运行期的吊销排查，每次请求不会立即写入 `config.json`。

## 6. TDD 测试矩阵（实现阶段）

storage：
- `TestListNodesForAPIAppliesFilters`：region/protocol/source/cf/ai/max_abuse 组合过滤正确。
- `TestListNodesForAPIStatusDefaultExcludesUnusable` 与 `status=all` 对比。
- `TestListNodesForAPIPaginationStable`：limit/offset 稳定序（latency,id）。

webui（api key）：
- `TestReadOnlyAPIRejectsMissingOrBadKey` → 401。
- `TestReadOnlyAPIAcceptsValidKey` → 200。
- `TestReadOnlyAPIKeyCannotAccessWriteEndpoints` → 401/403（key 不得写）。
- `TestReadOnlyAPIRateLimited` → 429。
- `TestReadOnlyAPINeverLeaksSecrets`：响应不含密码/URL/明文 key/`127.0.0.1`。
- `TestNodesAPITunnelNodeReportsGatewayConnect`：加密节点 `connect.mode=gateway` 且无 `127.0.0.1`。
- `TestNodesAPIDirectNodeReportsDirectConnect`：直连节点 `connect.mode=direct` 且 host=真实 IP。
- `TestApiV1NodesGatewayHintReportsInvalidStoredRegionWithoutAffectingValidNeighbor`：
  非法遗留地域的节点仍返回但无可复制 `username_hint`，并带稳定
  `username_hint_error`；同一响应中的合法 gateway 邻居仍有可解析 hint。
- `TestApiV1NodesGatewayHintsPinSameRegionNodesByStableNodeKey`：同地域节点的
  hint 不同，解析后分别精确命中各自 `key-<node_key>`。
- `TestApiV1NodesGatewayHintsResolveOnlyToTheirPinnedNodeKey`：将 API 返回的
  hint 交给真实 `selector.Resolve` 和 SQLite storage，必须只命中响应节点的
  NodeKey；相同 NodeKey 多 owner 的 fail-closed 由
  `TestGetProxyByNodeKeyFailsClosedForCrossSubscriptionOwners` 覆盖。
- `TestApiV1NodesGatewayWithoutNodeKeyReturnsStableHintError`：无 key 节点省略
  hint、返回稳定错误，且不回落或泄漏 mixed 地址。
- `TestApiV1NodesGatewayHintSurvivesTemporaryPortChange`：本地端口变化而
  NodeKey 不变时 hint 保持不变。
- `TestV1OccupancyHidesLoopbackAddress`：只读 occupancy 环回地址脱敏为 `gateway-local`。
- `TestV1OccupancyHidesPrivateAndInternalAddresses`：RFC1918 / CGNAT / 链路本地 / IPv6 ULA / IPv6 环回 / IPv6 链路本地 均不下发原始地址。
- `TestV1OccupancyShowsPublicAddress`：公网地址原样返回、无脱敏 note。
- `TestAdminOccupancyStillExposesPrivateAddress`：管理端 `/api/proxy-occupancy` 仍显示真实私有地址（读写口径分离，管理端行为不变）。

config：
- `TestReadOnlyAPIKeyHashRoundTrip`：明文不落盘、hash 校验通过、禁用 key 失效。

## 7. 实现分期

- **阶段 A（已完成）**：
  - config：`APIKey` + hash 存取 + `READONLY_API_KEYS` 首启导入 + `PublicHost`/`PUBLIC_HOST` + `READONLY_API_RATE_PER_MIN`。
  - storage：`ListNodesForAPI(filter)`。
  - webui 只读：`apiKeyMiddleware`、`GET /api/v1/nodes`、`GET /api/v1/occupancy`、`GET /api/v1/ping`、`connect` 组装与过滤、限流、脱敏。
  - webui 管理：API Key CRUD（session+CSRF）+ 设置页 UI +「开放 API」tab。
- **阶段 B（后续，可选）**：Key 过期时间、按 Key 的用量统计、IP allowlist。

## 8. 不做 / 明确排除

- 不做 API Key 的写权限。
- 不改选路、订阅转发、现有 session 鉴权。
- 不暴露加密节点本地端口。
- 不聚合纯净度为单一分值（保持原始多字段）。
