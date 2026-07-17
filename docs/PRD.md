# GeoProxy 网关 — 产品需求文档 (PRD)

> 版本: v1.0 (已定稿 — 用户确认"全部按推荐来")
> 基础项目: isboyjc/GoProxy (fork 改造)
> 核心原则: **能复用的优先复用** (最大化利用现有 custom/validator/proxy/storage/webui 资产, 非必要不重写)
> 文档目的: 锁定全部需求与关键设计决策, 作为 CSV 开发计划的唯一依据。
> 决策状态: 全部 15 个决策点已按推荐默认值定稿。
> 当前实现备注: 运行时节点来源为 `manual` / `subscription`; 文中涉及 `custom` 的表述主要指复用的订阅管理代码路径, 不代表公共免费代理来源。

---

## 1. 背景与目标

### 1.1 项目由来
原始 GeoProxy 是一个"免费公共代理抓取池"系统: 从 20+ 公开源抓取 HTTP/SOCKS5 代理, 验证后入池对外提供服务。经实测, 免费公共代理存在严重问题:
- 节点质量极低 (死节点、503 中转、TLS 中间人劫持节点混杂)
- 不稳定, 无法用于需要稳定出口的场景
- 存在安全风险 (投毒节点可截获流量)

### 1.2 本项目目标
将 GeoProxy 改造为一个**面向自有/付费节点的地域感知代理网关**, 核心能力:

1. **地域分流**: 客户端通过认证用户名中的地域标识, 定向使用指定国家/地区的出口节点。
2. **会话黏连 (Session Affinity)**: 同一 session 标识的请求, 优先固定走同一个出口节点, 保持出口 IP 稳定。
3. **双协议对外**: 同时提供 HTTP 和 SOCKS5 代理入口。
4. **多来源节点**: 支持付费订阅 (Clash/V2ray, 经 sing-box 转换) + 手动录入固定节点。
5. **全新 WebUI**: 默认白色主题, 未登录不展示任何信息 (强制登录门禁)。
6. **彻底移除免费公共代理抓取能力**。

### 1.3 非目标 (Out of Scope)
- 不再抓取任何公共免费代理源。
- 不做多租户计费/配额系统 (单账户模型, 见【决策点 2】)。
- 不做城市/州级地域粒度 (仅到国家级, 见【决策点 1b】)。
- 不做 L4 端口级地域分流 (采用用户名分流, 见【决策点 1】)。

---

## 2. 术语表

| 术语 | 含义 |
|------|------|
| 上游节点 (Upstream Node) | 真正承载出口流量的代理, 来自订阅或手动录入 |
| 入口 (Ingress) | 本网关对外暴露的 HTTP/SOCKS5 监听端口 |
| 地域 (Region) | 节点出口所在国家, ISO 3166-1 alpha-2 码 (us/jp/hk...) |
| 会话 (Session) | 客户端在用户名中声明的黏连标识, 决定出口节点绑定 |
| 会话绑定 (Binding) | session_id → 具体节点地址 的映射, 带 TTL |
| 用户名 DSL | 客户端认证用户名的结构化语法, 用于携带 region/session 参数 |

---

## 3. 核心功能需求

### 3.1 用户名 DSL (认证语法)

**【决策点 1 / 1a / 1b — 已定: kv 分段 + 国家级】**

客户端在代理认证的**用户名字段**中携带参数, 密码字段为全局固定密码。

**语法格式** (kv 分段, `-` 分隔; **顺序固定**):
```
<基础用户名>[-region-<国家码>][-unlock-<token>][-session-<会话id>]
```

**示例**:
| 用户名 | 行为 |
|--------|------|
| `username` | 不限地域、不黏连: 每请求随机选任意可用节点 |
| `username-region-us` | 限定美国出口, 不黏连: 每请求随机选一个美国节点 |
| `username-unlock-gpt` | 仅选 OpenAI 探测可达的节点 |
| `username-unlock-all` | 要求 OpenAI+Claude+Grok+Gemini+CF 均通过 |
| `username-region-jp-unlock-gpt-session-abc123` | 日本出口 + GPT 解锁 + 会话 abc123 黏连 |
| `username-session-xy` | 不限地域 + 会话 xy 黏连到某个任意节点 |

**解析规则**:
- 基础用户名必须等于配置的 `AuthUsername`, 否则认证失败。
- 密码校验只使用解析出的 **base** + 密码; 后缀不是凭据。
- `region` 值为 2 位国家码 (大小写不敏感, 内部统一转小写)。
- `unlock` 为 AI/CF 解锁过滤: `gpt|openai|chatgpt|claude|gemini|grok|cf|all` (可用 `+`/`,` 组合; `all` 展开五维 AND)。
- `session` 值为任意字符串 (建议限制长度 ≤ 64, 字符集 `[a-zA-Z0-9_-]`)。
- 参数顺序固定: **region → unlock → session**。顺序错误会导致整段用户名解析失败, 从而认证失败 (即使密码正确)。
- 缺省即不启用对应能力; 无节点满足 unlock 时请求失败, 不静默降级。
- 密码必须等于配置的 `AuthPassword` (或其 hash), 否则认证失败。

**协议差异**:
- HTTP 代理: 用户名密码通过 `Proxy-Authorization: Basic` 头传递。
- SOCKS5 代理: 用户名密码通过 SOCKS5 用户名/密码认证子协商 (0x02) 传递。
- 两种协议解析出的用户名走同一套 DSL 解析逻辑。

> 备注: 用户名 DSL 携带的凭据在链路上是明文/Base64。生产环境建议客户端到网关之间再套 TLS 或置于可信网络。此为固有限制, 文档明示, 不做静默处理。

---

### 3.2 地域分流

**【决策点 3 — 已定: 无节点时直接失败, 不 fallback】**

- 请求携带 `region-<码>` 时, 只从该国家的可用节点中选取。
- 若该国家**无任何可用节点**, 直接返回失败:
  - HTTP: `503 Service Unavailable`, body 说明 `no available node for region: <码>`。
  - SOCKS5: 回复 `0x01` (general failure)。
- **不做静默降级到其他地域**（禁止静默 fallback）。
- 不带 region 时, 从全部可用节点选取。

**节点地域来源** (见【决策点 5b】):
- 订阅节点: 由 validator 探测出口 IP 归属地自动获得。
- 手动节点: 录入时手填地域优先; 留空则自动探测。
- 地域信息存储在 `proxies.region` 字段 (国家码), 与现有 `exit_location` 解耦 (exit_location 是展示用全称, region 是分流用国家码)。

---

### 3.3 会话黏连 (Session Affinity)

**【决策点 4a / 4b — 已定: TTL 10 分钟; 节点挂了换同地域重绑】**

**绑定模型**:
- 维护内存映射: `session_id → { node_address, region, last_active_time }`。
- 请求带 `session-<id>` 时:
  1. 查绑定表。若存在且节点仍可用且地域匹配 → 复用该节点。
  2. 若不存在 → 按 region (若有) 选一个节点, 建立绑定。
  3. 若绑定的节点已失效 → 在**同地域** (若原请求指定了 region) 或全局 (未指定 region) 重新选节点, 更新绑定。

**TTL 与清理**:
- TTL = 10 分钟 (可配置 `SessionTTLMinutes`, 默认 10)。
- `last_active_time` 每次命中刷新。
- 后台协程定期 (如每 1 分钟) 清理超过 TTL 未活动的绑定。
- 绑定表为纯内存结构 (重启即失效, 可接受; 见【决策点 9】)。

**黏连是"优先"非"强制"**:
- 节点挂了会换绑, 不会为了黏连而让请求失败。
- 与地域约束的优先级: **地域约束 > 会话黏连**。即换绑时仍须满足 region 限制; 若同地域已无节点, 按 3.2 直接失败。

**并发安全**: 绑定表用 `sync.RWMutex` 或 sharded map 保护。

---

### 3.4 节点来源管理

**【决策点 5a / 5b / 6 — 已定: 手动节点支持 http/socks5 + 加密协议(走 sing-box); 地域手填优先否则自动探测; 保留健康检查】**

#### 3.4.1 订阅节点 (复用现有 custom 模块, 剥离免费池耦合)
- 支持格式: Clash YAML / V2ray 链接 / Base64 / 纯文本 (auto 识别)。
- 加密协议节点 (vmess/vless/trojan/ss/hysteria2/anytls) 经内置 sing-box 转为本地 SOCKS5。
- 定时刷新 (每订阅可配 refresh_min)。
- 需**移除**: "通过免费池代理拉取订阅"逻辑 (manager.go 的 `fetchWithRetry` 里 `GetRandom` 分支)、"7 天无可用自动删除订阅"逻辑 (改为仅禁用+告警, 见【决策点 10】)。

#### 3.4.2 手动录入节点 (新增能力)
- WebUI 表单录入: 地址、协议、(可选)地域、(可选)备注。
- 支持协议:
  - `http` / `socks5`: 直接入库。
  - 加密协议 (单节点链接, 如 `vmess://...`): 走 sing-box 转换为本地 SOCKS5 后入库。
- 地域: 手填 (2 位国家码) 优先; 留空则由 validator 探测。
- 手动节点标记 `source = 'manual'`, 与 `subscription`(订阅) 区分。
- 手动节点失效: 禁用 (不删除), 探测唤醒 (同订阅节点策略)。

#### 3.4.3 健康检查 (保留并简化)
- 保留 validator 的连通性 + 出口 IP + 地域探测 + 延迟检测。
- 保留后台健康检查与探测唤醒。
- **移除**质量分级驱动的"优化替换"逻辑 (optimizer 整个模块删除) — 因为没有免费池的"抓新换旧"需求。
- 健康检查间隔可配。

---

### 3.5 双协议入口

保留并改造现有 4 端口模型 → 简化为 **2 个入口端口** (见【决策点 11】):

**【决策点 11 — 已定: 每协议单端口, 移除 random/lowest-latency 双端口】**

理由: 地域分流 + 会话黏连已经取代了"随机/最低延迟"的选择语义。选择策略统一为:
- 带 session → 黏连节点
- 不带 session → 该地域 (或全局) 内按策略选 (默认: 延迟最低优先, 见【决策点 12】)

| 端口 (默认) | 协议 | 说明 |
|-------------|------|------|
| 7802 | HTTP | HTTP 正向代理入口 (支持 CONNECT 隧道) |
| 7801 | SOCKS5 | SOCKS5 代理入口 |
| 7800 | WebUI | 管理面板 (强制登录) |

**【决策点 12 — 已定: 非黏连选择策略 = 延迟最低优先, 同延迟随机】**

---

### 3.6 WebUI (整体重做)

**【决策点 7a / 7b / 8 — 已定: 内嵌重写(原生HTML/CSS/JS + 轻量方案); 保持 Docker 单二进制 + 内置 sing-box; 面板范围见下】**

#### 3.6.1 技术形态
- 前端仍**内嵌**在 Go 二进制中 (保持单二进制 + docker 一键部署优势, 无 node 构建链)。
- 使用干净的原生 HTML/CSS/JS, 可引入极轻量库 (如 Alpine.js via CDN 或内嵌), 不引入 React/Vue 构建体系。
- **默认白色主题** (浅色设计), 视觉风格: 简洁、专业、留白充足。
- **未登录门禁**: 未登录时只显示登录框, 不加载/不返回任何业务数据 API (后端强制鉴权)。

#### 3.6.2 面板功能范围
1. **登录页**: 密码登录 (单管理员账户)。登录态用 session cookie / token。
2. **总览 (Dashboard)**: 节点总数、按地域分布、可用/禁用统计、当前活跃会话绑定数。
3. **节点管理**:
   - 列表: 按地域分组, 显示地址(脱敏)、协议、地域、延迟、状态、来源(订阅/手动)。
   - 手动节点: 增/删/改地域标注。
4. **订阅管理**: 添加(URL/文件上传)、刷新、暂停/启用、删除。
5. **会话监控**: 实时列表 — 哪个 session 绑定了哪个节点、地域、剩余 TTL。
6. **系统设置**: 端口、认证凭据(基础用户名+密码)、SessionTTL、健康检查间隔、地域黑白名单。
7. **日志**: 内存日志查看 (复用现有 logger)。

#### 3.6.3 鉴权模型简化
- 原项目有"访客只读 + 管理员"双角色。**本项目取消访客角色**: 未登录=无任何信息, 登录=完全管理权限 (单管理员)。
- 移除"访客贡献订阅"功能。

---

## 4. 数据模型改造

### 4.1 `proxies` 表 (改造)
新增/变更字段:
| 字段 | 类型 | 说明 |
|------|------|------|
| `region` | TEXT | 新增。国家码 (小写 alpha-2), 分流用。空=未知 |
| `source` | TEXT | 语义扩展: `subscription`(订阅) / `manual`(手动)。移除 `free` |
| `note` | TEXT | 新增。手动节点备注 (可选) |
| `region_source` | TEXT | 新增。`manual`(手填) / `auto`(探测), 决定探测是否覆盖 |

保留: address, protocol, exit_ip, exit_location, latency, status, use_count, success_count, fail_count, last_used, last_check, created_at, subscription_id。

移除相关: quality_grade 相关的优化替换逻辑 (字段可保留但不再驱动淘汰)。

### 4.2 `subscriptions` 表 (基本保留)
- 移除 `contributed` 字段的使用 (访客贡献取消)。字段可保留兼容, 逻辑不再走。

### 4.3 `source_status` 表
- 整表删除 (仅服务于公共源抓取的断路器, 本项目无公共源)。

### 4.4 会话绑定
- **纯内存** (见【决策点 9】), 不落库。结构:
```go
type Binding struct {
    NodeAddress string
    Region      string
    LastActive  time.Time
}
// map[sessionID]Binding, 受 RWMutex 保护
```

**【决策点 9 — 已定: 会话绑定纯内存, 重启失效】**
理由: 会话黏连是短时效 (10 分钟) 行为, 重启丢失可接受; 落库增加复杂度和写压力。

---

## 5. 需删除 / 保留 / 新建 模块清单

### 5.1 删除
| 模块/文件 | 原因 |
|-----------|------|
| `fetcher/` | 公共代理抓取, 不再需要 |
| `optimizer/` | 免费池质量优化替换, 不再需要 |
| `pool/` | 免费池状态机 (healthy/warning/...) 与 slot 容量管理, 不再需要 |
| `main.go` 中 smartFetchAndFill / startStatusMonitor | 抓取填充逻辑 |
| storage 中 free 相关方法 | GetWorstProxies, DeleteInvalid, DeleteBlockedCountries(free部分) 等 |
| `source_status` 表及相关方法 | 断路器 |

### 5.2 保留 (需改造)
| 模块 | 改造点 |
|------|--------|
| `storage/` | 加 region/note/region_source 字段; 加地域查询方法; 去除 free 逻辑 |
| `validator/` | 保留连通性/出口IP/地域/延迟检测; 确保输出国家码写入 region |
| `custom/` | 作为订阅管理代码路径继续复用; 剥离免费池耦合 (拉订阅不再走免费代理); 取消 7 天删除; 支持 manual 来源 |
| `proxy/server.go` (HTTP) | selectProxy 改为按 region + session 选择; 接入 DSL 解析与绑定表 |
| `proxy/socks5_server.go` | 同上; 认证子协商解析 DSL |
| `webui/` | 整体重做 (见 3.6) |
| `config/` | 移除池/抓取相关配置; 新增 region 默认、SessionTTL、新端口等 |
| `logger/` | 基本保留 |

### 5.3 新建
| 模块 | 职责 |
|------|------|
| `auth/` (新) | 用户名 DSL 解析 (region/session 提取) + 凭据校验 |
| `affinity/` (新) | 会话绑定表管理 (增删查、TTL 清理、并发安全) |
| `selector/` (新, 或并入 storage) | 节点选择策略: 按 region 过滤 + 延迟最低/随机 + 排除失败节点 |

---

## 6. 端到端请求流程 (目标态)

### 6.1 HTTP 请求
```
客户端 --Basic Auth(username-region-us-session-x)--> :7802 HTTP 入口
  1. 校验密码; 解析用户名 DSL → {region:us, session:x}
  2. affinity.Get("x"):
       命中且节点存活且region匹配 → 用该节点
       否则 → selector.Pick(region=us) → 建/更新绑定
  3. 无 us 节点 → 503 "no available node for region: us"
  4. 有节点 → dialViaProxy → CONNECT 隧道 / HTTP 转发
  5. 失败 → 换同地域节点重试 (≤ MaxRetry), 更新绑定; 记录节点失败
```

### 6.2 SOCKS5 请求
```
客户端 --SOCKS5 auth(username=username-region-jp-session-y,password)--> :7801
  1. 握手协商 0x02 用户名密码认证
  2. 校验密码; 解析 DSL → {region:jp, session:y}
  3. 同 6.1 步骤 2-5 (SOCKS5 CONNECT 转发)
```

---

## 7. 配置项 (环境种子 / config.json)

| 配置 | 默认 | 说明 |
|------|------|------|
| `HTTP_PORT` | 7802 | HTTP 入口端口 |
| `SOCKS5_PORT` | 7801 | SOCKS5 入口端口 |
| `WEBUI_PORT` | 7800 | WebUI 端口 |
| `SESSION_TTL_MINUTES` | 10 | 会话黏连 TTL |
| `DEFAULT_REGION` | (空) | 未指定 region 时的默认地域, 空=全局 |
| `ALLOWED_COUNTRIES` | (空) | 地域白名单 (仅这些地域节点入库) |
| `BLOCKED_COUNTRIES` | 未设置时 `CN`; 显式空值时为空 | 地域黑名单；仅在白名单为空时生效 |
| `HEALTH_CHECK_INTERVAL` | 5 | 健康检查间隔(分钟) |
| `MAX_RETRY` | 3 | 单请求换节点重试上限 |
| `SINGBOX_PATH` | sing-box | sing-box 路径 (Docker 内置) |
| `SINGBOX_SHARD_COUNT` | 4 | sing-box 分片进程数 |
| `TZ` | Asia/Shanghai | 时区 |

凭据不再通过 `WEBUI_PASSWORD`、`PROXY_AUTH_USERNAME` 或 `PROXY_AUTH_PASSWORD` 环境变量注入。首次启动会生成 WebUI 登录密码和代理认证凭据，打印一次到日志，并持久化到 `config.json`; 后续修改通过 WebUI Settings 或配置保存路径完成。

移除: 所有 POOL_* / 抓取 / SOURCE 相关配置。

---

## 8. 部署形态 (不变)
- Docker 单容器 + docker-compose, 镜像内置 sing-box。
- 数据持久化: bind mount `./data` (SQLite + config.json + sing-box 配置)。
- 部署方式: 从本地源码树构建 (`docker compose up -d --build`), 不依赖已发布的远程镜像。
- 目标运行环境: 国外服务器 (AlmaLinux + Podman, 已验证)。

---

## 9. 待你确认的决策点汇总 (均已按推荐填入)

| # | 决策点 | 采纳的默认值 |
|---|--------|--------------|
| 1 | 地域分流方式 | 单端口 + 用户名 kv 分段 DSL |
| 1a | DSL 语法 | `username-region-us-session-x` (kv 分段) |
| 1b | 地域粒度 | 国家级 (alpha-2) |
| 2 | 账户体系 | 单账户 (基础用户名+密码, region/session 为后缀) |
| 3 | 无节点处理 | 直接失败, 不 fallback |
| 4a | 会话 TTL | 10 分钟 |
| 4b | 绑定节点失效 | 换同地域重绑 (优先非强制) |
| 5a | 手动节点协议 | http/socks5 + 加密协议(sing-box) |
| 5b | 手动节点地域 | 手填优先, 留空自动探测 |
| 6 | 健康检查 | 保留 (连通/出口IP/地域/延迟) + 探测唤醒 |
| 7a | 前端栈 | 内嵌原生 HTML/CSS/JS + 轻量库 |
| 7b | 部署 | Docker 单二进制 + 内置 sing-box |
| 8 | 面板范围 | 总览/节点/订阅/会话/设置/日志 |
| 9 | 会话绑定存储 | 纯内存, 重启失效 |
| 10 | 订阅长期无可用 | 仅禁用+告警, 不自动删除 |
| 11 | 入口端口 | 每协议单端口 (取消 random/lowest 双端口) |
| 12 | 非黏连选择策略 | 延迟最低优先, 同延迟随机 |

---

## 10. 风险与开放问题

| 风险/问题 | 说明 | 处理 |
|-----------|------|------|
| 凭据明文传输 | DSL 用户名/密码在 HTTP Basic / SOCKS5 auth 明文 | 文档明示; 建议 TLS/可信网络 |
| 单节点地域探测准确性 | GeoIP 归属地可能与实际不符 | 手填优先; 探测作兜底 |
| sing-box 单节点链接解析 | 手动录入加密单链接的解析覆盖度 | 复用 parser, 不足则迭代 |
| 会话绑定重启丢失 | 内存态, 重启后所有黏连重置 | 已接受 (TTL 短时效) |
| 地域无节点=失败 | 用户可能因缺节点频繁失败 | 明确报错信息, WebUI 展示各地域节点数 |

---

## 11. 验收总标准 (整体)
1. 客户端用 `username-region-us-session-x` 经 HTTP/SOCKS5 均能定向到美国节点, 且同 session 多次请求出口 IP 稳定。
2. 切换 session id, 出口节点随之改变 (可能不同)。
3. 请求不存在节点的地域, 明确返回失败, 不静默走其他地域。
4. 订阅 + 手动节点均可用; 加密节点经 sing-box 正常转换。
5. WebUI 未登录零信息泄露; 登录后可完成节点/订阅/会话/设置全流程管理; 默认白色主题。
6. 彻底无公共代理抓取行为 (无对外抓取请求)。
7. Docker 单容器可一键部署, 数据持久化正常。
8. `go build` 通过; 关键路径 (DSL 解析、地域选择、会话绑定 TTL) 有单元测试。

---

## 12. 复用映射表 (核心原则落地)

> 原则: **能复用优先复用**。下表列出每个目标能力应基于哪段现有代码改造, 而非从零重写。CSV 任务须遵循此映射。

| 目标能力 | 复用的现有资产 | 复用方式 | 需新增的最小增量 |
|----------|----------------|----------|------------------|
| 用户名 DSL 解析 | `proxy/server.go:checkAuth` (Basic 解码), `socks5_server.go:socks5Auth` (子协商) | 保留解码/认证框架, 在拿到 username 字符串后插入 DSL 解析 | `auth.ParseUsername()` 纯函数 |
| 密码校验 | 现有 `subtle.ConstantTimeCompare` + `ProxyAuthPasswordHash` | 完全复用, 不改 | 无 |
| 地域字段存储 | `proxies` 表 + `scanProxy` + `AddProxyWithSource` | 加列 `region` 走现有 migration 模式 (initSchema 里的 `ALTER TABLE ADD COLUMN`) | 加 3 个字段 + 对应 scan |
| 地域探测写入 | `validator` 已探测 `exit_location` (含国家码) | 复用探测结果, 截取国家码写入 `region` | `exit_location`→`region` 映射函数 |
| 节点选择 (按 region + 延迟) | `storage.GetLowestLatencyExcludeFiltered` / `GetAllFiltered` | 在其基础上加 region 过滤参数, 复用 exclude/latency 排序 | `GetByRegionExclude()` 方法 |
| 会话黏连 | 无现成, 但节点存活判断复用 `RecordProxyUse`/`DisableProxy` | 新建 affinity 模块, 节点失效判断复用现有状态 | `affinity/` 新模块 |
| HTTP 转发/CONNECT 隧道 | `proxy/server.go:handleHTTP/handleTunnel/dialViaProxy` | **几乎完全复用**, 仅把 `selectProxy` 换成 DSL+region+affinity 版 | 改 `selectProxy` 签名与实现 |
| SOCKS5 转发 | `socks5_server.go` 全部握手/请求/转发逻辑 | **几乎完全复用**, 仅改 `selectSOCKS5Proxy` + auth 解析 | 改 select + auth |
| 重试换节点 | 两个 server 里的 `for attempt` 重试循环 | 完全复用, 换节点时接入 affinity 重绑 | 重绑调用 |
| 订阅拉取/解析 | `custom/parser.go` + `manager.go:RefreshSubscription` | 复用解析与 sing-box 转换链路 | 剥离免费池耦合分支 |
| sing-box 集成 | `custom/singbox.go` 全部 | 完全复用 (手动加密节点也走它) | 无 (手动节点复用 Reload) |
| 手动节点录入 | `AddProxyWithSource(source='manual')` | 复用入库方法, 传新 source 值 | WebUI 表单 + API handler |
| 健康检查/探测唤醒 | `checker/` + `custom/manager.go:probeDisabled` | 复用, 去掉 free 分支 | 无实质新增 |
| WebUI 服务/鉴权/API 框架 | `webui/server.go` + `dashboard.go` 的路由与 session 机制 | 复用后端路由/鉴权骨架 | 重写前端 (html.go) + 调整 API |
| 配置加载/持久化 | `config` 的 Load/Save/savedConfig 模式 | 复用整套机制, 增删字段 | 改字段清单 |
| 日志 | `logger/` | 完全复用 | 无 |

### 12.1 复用红线 (CSV 任务必须遵守)
- **禁止**为已有等价能力另起炉灶 (如重写 SOCKS5 握手、重写 Basic 解码、重写 sing-box 调用)。
- 改造现有函数时, **优先加参数/加分支**, 而非复制粘贴新函数。
- 新建模块仅限现有代码确实没有的能力: `auth` (DSL)、`affinity` (会话绑定)。selector 逻辑**并入 storage**, 不单独建包 (复用现有查询方法族)。
- 删除模块 (fetcher/optimizer/pool) 时确认无被保留模块引用后再删, 避免编译断裂。
