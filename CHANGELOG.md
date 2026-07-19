# 更新日志

所有重要的项目变更都会记录在此文件中。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，
版本号遵循 [语义化版本 2.0.0](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 变更

- **WebUI 布局**：按设计稿对齐外壳（总控/运维分组、主题仅顶栏、设置独立页、总览左分布右栏）
- **主题令牌**：生产 CSS 改为 `data-theme="space"|"day"`（对齐设计稿）；兼容旧 localStorage `light`/`dark`
- **总览节点分布**：动画分布图替换世界地图；按地域与延迟档聚合，连线表示 session 绑定；可暂停
- **活跃会话表/卡片**：对齐设计稿详情（Proxy ID、DSL 地域、协议、来源、最近活跃、冷却、占用条、路由标签）；出口节点优先真实 `exit_ip`，本机 `127.0.0.1:mixed` 仅作绑定地址；定时刷新保留展开状态
- **节点统一管理弹窗**：订阅与手工节点共享「管理」入口；地域/备注/删除走应用内弹窗，替换浏览器 `prompt`/`confirm`
- **节点表 AI/Cloudflare**：表头与筛选用 Cloudflare / ChatGPT / Claude / Gemini / Grok；AI 沿用已发布的 `GPT/Cld/Grk/Gem` 短标签与胶囊筛选样式，状态为绿色畅通、红色阻断、淡灰未探测
- **节点名称/来源列去重**：名称列不再回退显示订阅名（来源列已展示），无备注时显示脱敏地址
- **备注/地域编辑**：非破坏性路径对订阅节点开放（删除走来源无关的 `/api/proxy/delete`）
- **轨道分布动画丝滑化**：卫星改为单一 `transform` 合成层动画；引力透镜改为平滑鹅卵石轮廓；beam 能量色读 `--sun-energy`
- **轨道几何即时重算**：侧栏折叠/展开与返回总览时重建 stage，避免网关/轨道/S 标记数秒错位
- **星标视觉**：未点亮亦用琥珀色描边星，点亮带光晕，提升辨识度
- **节点表列宽**：收紧 ip-api 标记与 Cloudflare 列间距；AI 解锁四标记强制单行不再挤成 2×2
- **节点清单分页**：默认每页 20 条，可选 20/50/100；筛选条件变化回到第 1 页（替换无限滚动分批）
- **总览布局**：右侧改为「如何连接」，「地域分布」下移，避免地域过多撑高星系卡片
- **星系会话连线**：按「地区+品质/延迟档」匹配卫星；品质空/D 时回退到该地区现有 S–C 轨道
- **地域分布**：倒序、TopN（不足则全显）、S/A/B/C/会话/均延、国家/地区中文名、查看全部页
- **顶部协议统计**：HTTP/SOCKS5 可用计入 `dual_protocol` mixed 节点（与列表双徽章一致）
- **P0-04 入口能力统一**：WebUI、可用统计、`GetByProtocol`、只读节点 API 及协议选取 helper 对 HTTP/SOCKS5 均计入 `dual_protocol` mixed 节点；未知协议仍精确匹配存储协议
- **会话 DSL 展示**：地域请求为 `region-xx`（去掉多余前导 `-`）
- **节点身份锁定**：SQLite 持久化 `node_key`；订阅刷新按 key upsert 保 id；`-node-key-…` 锁定稳定配置身份（兼容旧 host:port）；复制优先输出 key DSL（非出口 IP、非临时本地端口）
- **节点复制安全边界**：网关节点缺少稳定 `node_key` 时不再把临时本地端口复制为锁定凭据，改为明确提示刷新订阅或重新导入；直连节点复制保持不变
- **节点状态**：`disabled` 且无 `last_check` 显示「待验证」，有验证记录或失败次数超限显示「不可用」
- **示例凭据**：文档/默认用户名占位改为 `username`，连接示例主机改为 `YOUR-HOST-IP`
- **DSL 文档与连接提示**：README / GEO_FILTER / PRD / WebUI 统一 `region → unlock → node → session`；优先使用 `key-<base64url(nodeKey)>` 稳定身份，保留 `host:port` 兼容入口，并明确入口节点、最终出口、fail-closed 与 node 优先 session 语义
- **部署文档**：`DATA_DIRECTORY.md` 默认数据路径改为 bind mount `./data`；README 镜像与中文免责声明对齐当前网关模型
- **仓库卫生**：`.gitignore` / `.dockerignore` 排除 `subscriptions/`、`proxygo`、`shard-*/`；从索引移除已跟踪运行时订阅与二进制（历史清理需另授权）

### 修复

- **带认证的 http/socks 节点**：解析、存储、拨号与验证全链路支持 `user:pass@host:port`；凭据仅存于 DB 与内存握手，绝不入日志
- **验证失败状态**：`DisableProxyByID` 同步写 `last_check`，验证失败节点显示「不可用」而非永久「待验证」
- **节点复制携带凭据**：直连节点「复制」在存有账密时生成 `scheme://user:pass@host:port`
- **传输层映射**：`network=http`→sing-box http transport，`raw`/`none`→裸 TCP；消除 clash-meta 大批误跳过
- **shadowsocksr 诚实跳过**：解析阶段按节点显式跳过（sing-box 1.13 无原生支持）
- **Reality 缺省指纹**：缺 `client-fingerprint` 时补默认 utls，避免 sing-box 拒绝
- **校验剔除后误报端口不完整**：`pruneInvalidNodes` 丢弃的节点记入 `assembly.rejected`，commit 层可跳过，避免整订阅回滚成 0
- **订阅刷新保留 user_paused**：刷新 DELETE+INSERT 后按 address 回写用户停用，避免手动停用被静默撤销
- **sticky + unlock 回归**：预绑 session 在 unlock 不匹配时 rebind，并补真 sticky 测试
- **sticky 尊重暂停**：`user_paused` 与父订阅 `paused` 时 sticky 不得继续粘住旧节点
- **入站配置热更新**：HTTP/SOCKS5 请求路径读 `config.Get()` 已发布快照，避免 WebUI 改密后仍用启动配置
- **订阅验证写库错误**：Enable/Disable/Update 失败不再计 valid/recovered 假成功
- **跨订阅 collect**：刷新 A 不再旁路 re-fetch 订阅 B 只改运行态
- **通用删除**：`/api/proxy/delete` 走 Manager，订阅隧道节点同步卸载 sing-box
- **状态 API**：stats/订阅名读取失败返回错误，不再把失败编码成 0/空
- **静态资源缓存**：dashboard 资产改为 `no-cache` + ETag 再验证，HTML `no-store`，避免新 HTML 调用旧 JS
- **订阅重定向**：限制跳数，跨 origin 不转发非标准自定义密钥头
- **WebUI sessions**：affinity 为 nil 时返回空列表而非 panic
- **SOCKS5 Accept**：持续错误时记录并退避，避免忙循环
- **HTTP CONNECT 中继**：保留 Hijack 预读首包并支持 TCP half-close 延迟响应；仅在首个上游字节成功写回客户端后记节点成功，Hijack、成功响应写入或首字节写入失败时不再假成功，并释放对应会话绑定
- **session 与健康轮转**：session 首次绑定改用全候选 deterministic weighted rendezvous，稳定 ID/NodeKey 不受输入顺序影响；健康检查移除 S 级永久跳过，按最久未检查节点轮转，并在成功/失败写回推进 `last_check`
- **出口信息多源降级**：validator 并发查询 ip-api 与 ipapi.is；单源故障可降级，两源出口 IP/国家冲突时 fail-closed；备用源单独成功不会伪造 ip-api 风险标记已探测
- **跨订阅 NodeKey 所有权**：刷新、删除订阅或删除单行时，仅在没有其它 owner 后回收全局 sing-box 运行态；旧空 key 仅按相同地址兼容，查询失败保持运行态
- **代理日志**：HTTP/SOCKS5 启动、拨号和失败日志统一使用模块前缀与中文描述，避免日志合同漂移
- **WebUI 深色侧栏未选项**：button 默认背景重置为透明，未选中色改用 `--muted`
- **浅色主题命令示例框**：`.cmd`/`.code-block` 在 day 主题改为白底深字，避免偏黑突兀
- **运行日志高度**：日志区相对视口再减约 40px，避免略超出屏幕
- **节点分布控制文案**：暂停按钮改为「暂停动画 / 恢复动画」
- **移动端节点筛选**：筛选按钮保留固有命中宽度并自然换行，避免 Cloudflare 与 AI 按钮文字溢出、点击区域互相覆盖，同时保持已发布的 AI 胶囊、短标签和三态色值
- **运行时数据目录**：订阅上传与 sing-box 分片统一使用配置解析结果；空根或不可用路径显式失败，不再静默回退到当前工作目录

### 新增

- **订阅修改 API**：`POST /api/subscription/update`，WebUI 支持编辑名称/URL/间隔/请求头
- **会话占用上限**（可选）：max_sessions_per_proxy / MAX_SESSIONS_PER_PROXY，默认 0 不限制；>0 时新 session 绑定受每节点上限约束
- **代理节点冷却 CD**（可选）：proxy_cooldown_minutes / PROXY_COOLDOWN_MINUTES，默认 0 关闭；>0 时新 session 首次绑定后，冷却期内其他新 session 不选该节点；同 session 粘性不受影响；无 session 的 Pick 忽略冷却
- **节点占用可观测 API**：已认证 `GET /api/proxy-occupancy` 返回每节点 `proxy_id` / `address` / `active_sessions` / `max_sessions` / `cooldown_remaining_seconds`（返回真实冷却剩余秒数）；无密码字段

- **sing-box 分片多进程**
 - `ShardedSingBox` 将隧道节点按稳定哈希切到 N 个独立进程（默认 4，可配置）
 - 仅重载节点集变化的分片；真实进程级平滑重载与 6000 节点规模验证
 - 双入站收敛为单 `mixed` 端口（每节点 1 端口同时服务 SOCKS5+HTTP）
 - 分片崩溃/停止后的主动恢复，以及停止后禁止 Reload 复活

- **节点与风险展示**
 - 节点星标、Cloudflare 拦截列、一键复制代理凭据
 - 出口 IP 风险分双源展示；AI 服务可达性探测（OpenAI/Claude/Grok/Gemini）与前端徽章
 - `dual_protocol` 显式标记 mixed 节点；协议双标签；复制完整代理 URL
 - 全球节点分布地图（轮廓 + 会话弧线 + 网关定位）

- **订阅与接入**
 - 订阅自定义请求头（含 User-Agent），用于对默认 UA 返回 401 的订阅源
 - 内网/本地目标直连 bypass（HTTP / CONNECT / SOCKS5）
 - 代理密码可持久化并经已认证 config API 下发，支持前端拼完整 URL
- **批量导入手工节点**：WebUI「批量导入」与 `POST /api/manual-node/import`；支持多行 `socks5://`/`http://`/`https://`，从行内抽取 URL（前缀/行中/行尾注释均可），导入前批内去重、跳过已存在 manual 节点，返回 added/skipped/failed 报告
- **节点多选批量删除**：列表勾选 + `POST /api/manual-node/batch-delete`；来源筛选（手工/订阅）
- **对外开放只读 API（API Key）**
 - `GET /api/v1/nodes`：节点目录（协议/区域/纯净度/CF/AI、`connect.mode=direct|gateway`）；加密节点走网关入口，不暴露 `127.0.0.1` 本地端口
 - `GET /api/v1/occupancy`：每节点占用与真实冷却剩余秒数
 - `GET /api/v1/ping`：鉴权探活
 - 鉴权：`Authorization: Bearer` 或 `X-API-Key`；密钥仅存 SHA-256 hash；默认限流 60 req/min/key（`READONLY_API_RATE_PER_MIN`）
 - 配置：`PUBLIC_HOST` 指定网关对外地址；`PUBLIC_HOST` 与 `READONLY_API_KEYS` 仅首次启动时导入
 - WebUI：设置页 API Key 创建/吊销/删除（明文仅创建时显示一次）；「开放 API」页说明端点与示例 curl
- **用户名 DSL 解锁过滤**：`<base>[-region-cc][-unlock-token][-session-id]`（顺序固定）；`gpt/claude/gemini/grok/cf/all` 按节点 AI/CF 探测结果过滤选路，无匹配则失败不降级
- **AI 探测双层信号**：稳定 API（401/缺 key）为主 + OpenAI/Claude/Gemini 产品层明确地区锁/放行指纹为辅；CF 仍单独字段

### 修复

- 订阅拉取失败会携带 HTTP 状态码与截断、脱敏的响应片段；5xx 与 429 最多短暂重试一次，仍禁止通过上游节点回源。
- 长期禁用的订阅隧道节点会从 sing-box 运行态移除并释放 mixed 端口；过期探测结果不会写回已被复用的端口。
- 会话首绑/换绑：容量与冷却检查与写入串行化，并发首绑不再突破 `max_sessions_per_proxy`，冷却也原子生效；同 session 并发 Resolve 不会拆成多节点
- 手动隧道节点：Reload 成功后 DB 写失败会回滚运行态；删除手工隧道节点同步移出 sing-box（统一走 Manager，通用删除接口不再旁路）
- 订阅刷新：删除旧代理失败时返回错误，不再继续半刷新/假成功
- 分片 Reload：后续分片失败时回滚已变更分片；补偿失败聚合报告
- WebUI 同址歧义地址映射为 409（不再一律 404）
- GetByRegion 去掉冗余 SQL `RANDOM()`，改为确定性排序
- 不完整/Partial 重载不再删除旧订阅代理；分片 Partial 纳入健康恢复
- 订阅删除仅走存储事务；headers 非法 JSON 在添加时拒绝
- dual_protocol 置位失败不再静默成功；端口空洞可复用
- HTTP 入站 SOCKS5 上游握手超时；link-local 不网关直连
- GetProxyByAddress 同址多身份显式歧义错误；复制凭据 toast 不回显密码
- 手工节点导入可正确识别带 userinfo 的 `socks5://` / `http://` URL，入库地址只保留 host:port；批量导入说明同步为支持前缀、行内和行尾说明
- 本地 Bash 测试脚本改为要求显式代理认证环境变量，缺失凭据时清晰报错；配置文档同步首次启动凭据生成与国家黑名单默认值
- 手工 HTTP/SOCKS 节点入库默认 `disabled`，导入/添加后并发验证（出口/纯净度/CF/AI）通过才 `active`；复制直连节点不再拼接网关 DSL 密码
- AI 探测：Claude/Grok/Gemini 改为稳定 API 端点，避免官网 HTML 指纹导致大面积误报 ✗；看不懂的响应记未探测（–）而非不可达

- sing-box 重启时端口 bind 竞态（等待旧监听释放后再启动）
- 端口高水位泄漏与分片端口段超限保护
- 分片崩溃后因 assignedKeys 跳过导致永不恢复
- 订阅 URL SSRF 防护（拒绝私网/link-local/非全局单播目标）
- 云 metadata 固定地址不再走代理直连 bypass
- SOCKS5 帧过读、非法 RSV、上下游握手/入站协议超时与长连接 deadline 清除
- HTTP 入站 `ReadHeaderTimeout`，降低半请求头挂死风险
- 批量代理写入事务回滚；同址多身份歧义更新拒绝
- 配置临时文件发布；畸形配置拒绝静默覆盖
- WebUI：尾随 JSON 拒绝、413 一致、登出 CSRF/POST、订阅上传唯一文件与失败清理
- 配置国家过滤失败时运行态/全局/磁盘一致性回滚
- 健康检查重叠执行互斥；日志截断释放底层缓冲
- AI 403 记为不可达；session TTL 边界；selector 绑定稳定性

### 变更

- **WebUI 设计语言 v2（Signal）**：重做设计令牌（表面分层、文字层级、accent/signal 信号色、分级 elevation、暗色 hairline 内高光、圆角/间距/字体/动效令牌），旧变量名保留为别名避免回归
 - 侧边栏新增 PC 可见的显式「收起菜单」折叠按钮（原顶栏小箭头保留），折叠态 localStorage 持久化不变
 - 深色模式顶栏图标按钮（刷新/GitHub/菜单/折叠）补齐 `background`，不再继承浏览器浅灰底
 - 「开放 API」导航与页面标题统一改为「API」
 - 「如何连接」卡片补充 curl 占位符说明（`username`=认证用户名、`PASSWORD` 须替换为真实密码），出口 IP 提示改写为明确禁止直连、须走网关端口+认证
 - 节点复制凭据：代理密码为空时用字面量 `PASSWORD` 占位并提示替换，仍不在成功 toast 回显含真实密码的 URL
 - 节点表 CF / AI 表头统一为图标+短标签；AI 列改用 ✓（可达）/ ✗（不可达）/ –（未探测）紧凑标记，移除渲染异常的品牌 SVG 图标
 - 全球节点分布地图：新增海洋径向渐变底与经纬网格层、提升陆地对比、节点发光脉冲、会话流动线改用 signal 青色（viewBox 与国家坐标投影不变）
- 仓库不再跟踪本地专用说明与内部计划类文件（仅保留项目必需文档）

## [v0.4.1] - 2026-04-04

### 修复

- 修复发布/部署配置漂移：Docker Compose 默认数据落点统一为宿主机 `./data`，地域黑名单默认值与 README/PRD 保持一致
- 升级 sing-box 从 1.11.8 到 **1.13.5**，修复 anytls 等新协议不支持导致订阅节点启动失败的问题
- sing-box 启动前新增 `sing-box check` 配置预检，配置无效时输出详细错误而非静默崩溃
- 捕获 sing-box stderr 输出到 `[sing-box]` 日志，便于排查运行时错误
- 检测 sing-box 进程启动后立即退出的情况，避免误报"端口未就绪"
- Docker healthcheck 从 `wget` 改为 `curl`（debian-slim 无 wget），Dockerfile 增加 curl 安装
- 修复 `docker-compose.dokploy.yml` 服务未加入 `dokploy-network` 的问题
- 修复中英文切换时订阅池统计模块动态文字未更新的问题

## [v0.4.0] - 2026-04-04

### 新增

- **订阅代理导入**
 - 支持通过 WebUI 添加 Clash/V2ray 订阅 URL 或上传配置文件
 - 格式全自动识别：Clash YAML、V2ray 链接（vmess/vless/trojan/ss/hysteria2/anytls）、Base64 编码、纯文本
 - 内置 sing-box 协议转换：加密协议节点自动转为本地 SOCKS5 代理，Docker 镜像自带 sing-box 二进制
 - 订阅定时刷新：可配置刷新间隔，自动拉取最新节点并替换旧节点
 - 添加订阅时先验证（拉取+解析通过后才入库），失败不产生垃圾数据

- **订阅代理保护机制**
 - 软删除：订阅代理健康检查失败不删除只禁用（`status='disabled'`）
 - 探测唤醒：定时探测禁用的订阅代理，恢复可用后自动启用
 - 地理过滤全局化：免费代理删除、订阅代理禁用，探测唤醒时也检查地理规则
 - 自动清理：连续 7 天无可用节点的订阅自动移除

- **5 种代理使用模式**
 - 混合·订阅优先：优先使用订阅代理，无可用时降级到免费
 - 混合·免费优先：优先使用免费代理，无可用时降级到订阅
 - 混合·平等：不区分来源，按延迟/随机选择
 - 仅订阅代理：只使用订阅导入的代理
 - 仅免费代理：只使用公开抓取的代理

- **访客贡献订阅**
 - 未登录用户可通过「贡献订阅」入口提交订阅 URL 或上传配置文件
 - 提交前自动验证，通过后才入库
 - 管理员可刷新、暂停、删除贡献的订阅
 - 贡献订阅在列表中有橙色「贡献」标记

- **WebUI 增强**
 - 免费池 / 订阅池分离展示，各自独立统计
 - 订阅管理面板：订阅列表（名称 + 可用数 + 禁用数）、添加/刷新/暂停/删除
 - 代理列表中订阅代理带黄色标签显示所属订阅名称 + 左侧黄色竖线
 - 系统设置从侧边栏移至顶部齿轮图标，重组为：代理模式 → 免费池 → 订阅池 → 验证检查 → 地理过滤
 - 新增 ~70 个 i18n 翻译 key，覆盖所有新增 UI 元素

- **代理使用统计**
 - HTTP/SOCKS5 代理服务在请求成功/失败时记录使用次数（`RecordProxyUse`）

### 变更

- `Proxy` 结构体新增 `Source`（free/custom）和 `SubscriptionID` 字段
- `Count()`/`CountByProtocol()` 仅统计免费代理（slot 计算不受订阅代理影响）
- 批量删除方法（`DeleteInvalid`/`DeleteBlockedCountries`/`DeleteNotAllowedCountries`/`DeleteWithoutExitInfo`）仅作用于免费代理
- `GetWorstProxies` 排除订阅代理，优化器不替换订阅代理
- Dockerfile 集成 sing-box 二进制（自动检测 amd64/arm64 架构）

### 修复

- 修复 `AddProxy` 未显式设置 `source='free'` 的问题
- 修复 WebUI「刷新代理」「刷新延迟」对订阅代理执行硬删除的问题（改为禁用）
- 修复 `validateCustomProxies` 将所有代理硬编码为 socks5 协议导致 HTTP 直连代理验证失败
- 修复 `CustomPriority` 和 `CustomFreePriority` 可同时为 true 的互斥问题

## [v0.3.0] - 2026-04-01

### 新增

- **地理过滤增强**
 - 支持国家白名单（`ALLOWED_COUNTRIES`）和黑名单（`BLOCKED_COUNTRIES`）配置
 - 白名单优先级高于黑名单：白名单非空时仅允许指定国家，否则使用黑名单屏蔽
 - 支持通过环境变量、配置文件、WebUI 动态配置地理过滤规则
 - 启动时自动清理违反当前过滤规则的已入池代理
 - 详细文档：`GEO_FILTER.md`

- **项目指南文档**
 - 新增 `CLAUDE.md`，提供项目架构、设计模式、代码规范的完整指导
 - 包含模块依赖流程图、后台协程说明、端口映射表等

- **HTTPS 可用性验证增强**
 - HTTP 协议代理入池前增加 HTTPS CONNECT 隧道验证
 - 随机访问真实 HTTPS 网站（Google/GitHub/OpenAI 等）确认可用性
 - 失败自动切换验证站点重试，确保入池的 HTTP 代理都能访问 HTTPS
 - 新增测试脚本：`test/test_http_https.sh` 用于持续测试 HTTPS 访问能力

### 变更

- 默认 HTTP 协议占比从 50% 调整为 30%（配置 `PoolHTTPRatio: 0.3`）
- 地理过滤配置优先级：`config.json` > 环境变量
- WebUI 地理过滤设置界面支持动态修改白名单/黑名单

### 修复

- 修复地理过滤在验证器和存储层的逻辑一致性问题
- 修复启动时地理过滤清理逻辑，正确处理白名单优先场景
- 修复代理池补充逻辑：当 HTTP 和 SOCKS5 协议都缺失时，同时补充两个协议，而非先后补充
- 修复槽位计算问题：调整默认配置比例为 0.3（3:7），符合 HTTP/SOCKS5 实际使用场景

## [v0.2.0] - 2026-03-30

### 新增

- **SOCKS5 协议支持**
 - 实现完整的 SOCKS5 代理服务器（支持 CONNECT 命令）
 - 提供两个 SOCKS5 端口：`:7779`（随机轮换）+ `:7780`（最低延迟）
 - SOCKS5 服务仅使用 SOCKS5 上游代理，避免 HTTP 代理不支持 CONNECT 的问题
 - 协议并发验证：SOCKS5 和 HTTP 分组并发验证，SOCKS5 无额外检测，优先填充
 - 新增测试脚本：`test/test_socks5.sh` 用于测试 SOCKS5 代理

- **配置增强**
 - 新增 `SOCKS5Port` 和 `StableSOCKS5Port` 配置项
 - 支持通过环境变量配置 SOCKS5 端口
 - 优化代理池槽位分配逻辑，支持 HTTP/SOCKS5 比例配置

### 变更

- 存储层新增协议筛选方法 `CountByProtocol`、`GetRandomByProtocol`、`GetLowestLatencyByProtocol`
- 代理池管理器适配双协议槽位计算
- Docker Compose 配置新增 SOCKS5 端口映射

## [v0.1.0] - 2026-03-29

### 新增

- **代理认证功能**
 - HTTP 和 SOCKS5 代理服务支持可选的用户名密码认证
 - 支持通过环境变量配置代理认证开关、用户名和密码（当前版本改为首次启动生成并落盘）
 - 默认关闭，开启后可保护代理服务不被未授权访问

- **环境变量支持**
 - WebUI 管理密码配置（早期版本默认 `GeoProxy`；当前版本改为首次启动生成并落盘）
 - `DATA_DIR`：自定义数据目录路径（默认当前目录）
 - `BLOCKED_COUNTRIES`：屏蔽特定国家的代理（如 `CN,RU,KP`）

- **数据目录集中管理**
 - 支持通过 `DATA_DIR` 环境变量指定数据存储位置
 - 配置文件 `config.json` 和数据库 `proxy.db` 统一存放在数据目录

- **智能抓取机制**
 - 智能状态监控：Healthy / Warning / Critical / Emergency 四级状态
 - 按需抓取：根据池子状态自动选择合适的抓取模式
 - 源断路器：连续失败的代理源自动降级或禁用，冷却后恢复

- **WebUI 增强**
 - 实时日志流显示：支持查看最近 1000 条系统日志
 - 代理质量分布图表：S/A/B/C 各等级代理数量可视化
 - 延迟趋势图：HTTP 和 SOCKS5 平均延迟变化趋势

### 变更

- 验证超时从 8 秒增加到 10 秒，适应较慢的代理网络
- 健康检查批次大小从 10 个增加到 20 个，提高检查效率
- 优化配置参数命名，统一使用 `MaxLatency` 前缀

### 文档

- 完善 README.md，新增快速导航、Docker 部署、测试指南等章节
- 新增 `.env.example` 示例环境变量文件
- 更新 Docker Compose 配置示例
- 新增 GitHub Container Registry 镜像源说明

## [v0.0.1] - 2026-03-27

### 新增

- 项目初始化
- 基础 HTTP 代理池功能
- WebUI 管理界面
- SQLite 数据持久化
- 代理验证和健康检查
- Docker 支持

---

## 版本说明

- **主版本号**：不兼容的 API 变更
- **次版本号**：向下兼容的功能新增
- **修订号**：向下兼容的问题修复

## 相关链接

- [项目仓库](https://github.com/babutree/GeoProxy)
- [GitHub Container Registry](https://github.com/babutree/GeoProxy/pkgs/container/GeoProxy)
- [问题反馈](https://github.com/babutree/GeoProxy/issues)
