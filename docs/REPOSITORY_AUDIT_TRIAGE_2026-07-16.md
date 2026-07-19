# GeoProxy 全仓审计问题复核与证据分级

> - 复核日期：2026-07-16
> - 代码基线：`main@7becd9f`
> - 复核范围：上一轮全仓审计提出的功能、安全、生命周期、WebUI、部署、文档和抽象问题。
> - 本文只做事实判定与修复建议，不修改生产代码、不删除运行数据、不清理 Git 历史。
> - 文中不记录订阅文件里的主机、口令或其他真实凭据值。
> - **状态边界**：本文是 `main@7becd9f` 的历史快照，不是当前 OPEN 清单。
>   当前状态以 `Bug-Fix TO DO list.csv` 和 `docs/bug-grok-0718.md` 为准。

> **审计完成度说明：** 原汇总中的 11 个 P0/P1 编号项已经逐项回到源码、配置或文档核对，P2 的代表项也分别归入确认 bug、条件性风险、测试盲区、设计债或误报。这里的“逐项复核”不等于已经穷尽仓库中所有未知 bug，也不等于每项都做过真实浏览器、Docker、远端 registry 或 6000 节点动态复现。2026-07-16 的补充审计继续覆盖了品牌、项目链接、Docker/Compose 标识，并根据用户提供的浏览器错误新增 BUG-08。

## 1. 判定标准

本次不以“代码看起来可疑”直接判 bug，而使用以下证据等级：

1. **明确契约**：`AGENTS.md`、设计文档、API 文档、用户可见文案或已有测试明确规定行为。
2. **生产调用链**：问题必须能从实际入口走到问题代码；未被生产调用的代码不直接算现存功能 bug。
3. **可构造反例**：存在合法输入或合法状态，使实现给出错误结果、假成功、状态分裂或安全泄露。
4. **反证检查**：查找保护逻辑、补偿、后续校验、明确设计取舍和生产装配约束。
5. **影响边界**：区分必现、条件触发、仅非标准装配、仅文档错误和纯维护债。

最终分类：

- **确认 bug**：当前实现违反明确契约，且生产调用链或合法调用可触发。
- **条件性 bug / 风险**：缺陷机制成立，但需要特定输入、部署或失败条件。
- **文档 / 部署 bug**：源码或部署真相与面向用户的说明冲突，会导致错误使用。
- **测试盲区**：尚未证明生产行为错误，但关键新行为缺少防回归证据。
- **设计债**：存在重复、死代码、抽象分裂或可维护性问题，但当前无错误行为证据。
- **误报 / 严重度夸大**：原观察被保护逻辑、明确契约或生产调用关系反驳。

## 2. 最终结论矩阵

### 2.1 确认 bug

| 编号 | 结论 | 优先级 | 核心理由 |
|---|---|---:|---|
| BUG-01 | Git 跟踪了含凭据形态字段的真实订阅文件 | P0-处置 | 违反仓库红线；凭据是否仍有效未知，但历史暴露已发生 |
| BUG-02 | Sticky 绑定绕过节点 `user_paused` 与父订阅暂停 | P1 | 首选查询过滤暂停，sticky 快路径不检查，违反“不参与选路”契约 |
| BUG-03 | WebUI 配置保存造成运行态配置分裂 | P1 | UI 仅声明端口需重启，但代理认证、默认地域、重试、健康间隔、validator 过滤、sing-box 路径并未统一热更新 |
| BUG-04 | 订阅验证与恢复路径吞写库错误并统计假成功 | P1 | `Enable/Disable/Update*` 错误不检查，计数和日志仍报告成功 |
| BUG-05 | 刷新订阅 A 会旁路拉取订阅 B，只改变 B 的 sing-box 运行态 | P1 | B 的新节点进入运行态，但 B 的 DB 代理记录和刷新时间不更新 |
| BUG-06 | 通用代理删除接口可令订阅隧道 DB / sing-box 运行态分裂 | P2 | 仅手工节点走 Manager；订阅节点直接删 DB，运行态不删 |
| BUG-07 | 部分 WebUI 状态接口吞存储错误并返回可信的假数据 | P2 | `apiStats` 等忽略 error，数据库失败会被编码成 0 / 空值并返回 200 |
| BUG-08 | 固定静态资源 URL 的一小时新鲜缓存可让新 HTML 调用旧 JS，导致“添加订阅”按钮失效 | P1 | `7becd9f` 同时修改按钮函数名和 JS，但资源 URL 未版本化；旧 JS 有 `openSubModal`、没有 `submitSubscription`，与浏览器错误完全吻合 |

### 2.2 条件性 bug / 风险

| 编号 | 结论 | 优先级 | 触发条件 |
|---|---|---:|---|
| RISK-01 | 订阅重定向会跨主机转发非标准自定义密钥头 | P1-条件 | 用户把秘密放在 `X-Token`、`X-API-Key` 等自定义头，源站再跨域重定向 |
| RISK-02 | 订阅节点暂停状态以本地端口地址为身份，端口复用时可能错绑给替换节点 | P2-条件 | 被暂停旧隧道节点移除，同时新节点复用相同 mixed 端口 |
| RISK-03 | `apiSessions` 在 `affinity == nil` 的非标准装配下 panic | P3 | 通过公开构造器传入 nil；正式 `main` 不触发 |
| RISK-04 | SOCKS5 `Accept` 持续错误时可能忙循环 | P3 | listener 持续返回错误，例如资源耗尽或不可恢复错误 |

### 2.3 文档 / 部署 bug

| 编号 | 结论 | 优先级 | 核心证据 |
|---|---|---:|---|
| DOC-01 | `DATA_DIRECTORY.md` 把 Named Volume 写成默认，实际 compose 是 bind mount | P1 | 文档与 `docker-compose.yml` 直接相反 |
| DOC-02 | **SUPERSEDED（2026-07-19，BUGFIX-031）**：历史基线的 README / GEO_FILTER / CLAUDE DSL 缺少 `-unlock-` | 历史 P1 | 当前 README/GEO_FILTER 已包含 `unlock` 与完整固定顺序；本行仅保留基线证据 |
| DOC-03 | `.env.example` 中部分变量不会被 compose 传入容器 | P1 | 示例列出 max sessions / cooldown / shard count，compose 未引用这些变量 |
| DOC-04 | README 中文免责声明仍描述公共代理抓取和访客贡献 | P1 | 当前产品明确只管理用户节点，访客贡献入口已移除 |
| DOC-05 | README 声称不发布预构建镜像，workflow 却配置每次 main 推送 GHCR | P2 | 源码级发布口径冲突；远端镜像实际可用性本次未验证 |
| DOC-06 | `config.Load` 注释声称代理密码不以明文落盘，实际 `Save` 明文持久化 | P1-安全文档 | 同一文件内注释与序列化字段直接矛盾 |
| DOC-07 | `PauseProxy` 注释仍称写 `status='paused'`，实现写 `user_paused=1` | P3 | 维护注释与迁移后的状态模型不一致 |
| DOC-08 | 已确定的 GeoProxy 外部品牌仍与 GeoProxy、容器、镜像和项目链接标识混用 | P1-项目契约 | WebUI 与 origin 已是 GeoProxy，但 README、运行标识、Compose、Dockerfile、发布镜像名和部分本地项目资产仍未统一 |

### 2.4 测试盲区，不等同生产 bug

| 编号 | 盲区 | 判断 |
|---|---|---|
| TEST-01 | `/api/subscription/update` 无功能测试，也未列入订阅写接口鉴权表 | 确认测试遗漏；路由本身已套 `authMiddleware`，不能据此宣称鉴权绕过 |
| TEST-02 | 无 sticky + `user_paused` / 父订阅 paused 回归测试 | 直接解释 BUG-02 为何未被绿测发现 |
| TEST-03 | 无跨主机 redirect + 非标准自定义密钥头测试 | 直接解释 RISK-01 未被现有 SSRF 测试覆盖 |
| TEST-04 | 无 WebUI 保存后真实 HTTP / SOCKS5 入站立即生效测试 | 直接解释 BUG-03 未被 handler 单测发现 |
| TEST-05 | 无 DB 写失败下注阅验证计数与日志一致性测试 | 直接解释 BUG-04 未被现有 happy-path 验证发现 |
| TEST-06 | 资产测试只断言固定 URL、ETag 与 Cache-Control 存在，不验证跨版本 HTML / JS 缓存一致性或真实点击添加订阅 | 直接解释 BUG-08 为何在静态字符串测试全绿时仍能在浏览器出现 |

### 2.5 原汇总逐项闭环状态

| 原汇总项 | 是否逐项核对 | 最终去向 |
|---|---|---|
| P0-1 订阅 YAML 含凭据并入库 | 是 | BUG-01，确认安全处置项 |
| P0-2 跟踪约 13MB `proxygo` | 是 | 确认仓库卫生 / 来源审计问题；不与凭据暴露同列 P0 功能 bug |
| P1-3 sticky 不检查两类暂停 | 是 | BUG-02，确认 bug |
| P1-4 暂停订阅不卸载 sing-box | 是 | 原严重度误报；当前契约只定义“不参与选路”，资源释放策略尚未定义 |
| P1-5 入站配置热更新不生效 | 是 | 扩展为 BUG-03：不只是 HTTP/SOCKS5，而是多组件运行态分裂 |
| P1-6 订阅 HTTP 无 `CheckRedirect` | 是 | RISK-01 仅确认跨 origin 非标准秘密头转发；私网 redirect SSRF 被每跳 Dial 校验反证 |
| P1-7 验证写库错误被吞 | 是 | BUG-04，确认 bug |
| P1-8 DSL 文档缺 unlock | 是 | DOC-02；历史确认，当前由 BUGFIX-031 修复并标记 SUPERSEDED |
| P1-9 数据目录文档与 compose 相反 | 是 | DOC-01，确认部署文档 bug |
| P1-10 镜像发布口径冲突 | 是 | DOC-05，确认源码级发布契约漂移；tag / manifest 仍未验证 |
| P1-11 中文免责声明仍是公共池模型 | 是 | DOC-04，确认产品 / 法律文档 bug |
| P2 测试遗漏代表项 | 是 | `/api/subscription/update`、sticky、配置、redirect、写库失败分别进入 TEST-01 至 TEST-05；“暂停后必须清 portMap”因资源契约未定，暂不能作为必需回归 |
| P2 抽象与所有权发散 | 是 | 配置、可用性口径和生命周期旁路分别映射到 BUG-02/03/05/06；双份 dial、旧 Checker、Manager 直写 DB 保留为设计债 |
| P2 WebUI 代表项 | 是 | CSRF 与管理员密码读取为误报 / 设计取舍；nil affinity 为 RISK-03；“无 UI 写面”本身不是 bug |
| P2 品牌、链接、Docker/Compose 漂移 | 第二轮逐文件补审 | DOC-08 与第 8 节 GeoProxy 迁移 TODO；本轮只登记，不替换 |
| P2 语义细节 | 是 | 地址暂停身份为 RISK-02；跨订阅 re-fetch 为 BUG-05；`GetRandom` 为未使用设计债；HTTP 407 映射证据不足 |

因此，原汇总中的条目不是全部按原结论照单全收，而是已经逐项判定；确认、降级、重定义和反证结果都保留在本文。新增 BUG-08 来自本轮用户实际错误，不属于原汇总。

## 3. 确认 bug 的证据与反证

## BUG-01：Git 跟踪真实订阅文件并包含凭据形态内容

### 支持证据

- `git ls-files subscriptions proxygo` 显示 `subscriptions/sub_1775301718713.yaml` 已被 Git 跟踪。
- 文件中存在 `password:`、`server:` 等真实订阅节点字段。本文不记录具体值。
- `git log -- subscriptions/sub_1775301718713.yaml` 表明该文件已进入历史，而非仅工作区未跟踪文件。
- `webui/subscription_handlers.go:135-156` 会在 `DATA_DIR/subscriptions` 下生成 `sub_*.yaml`；当 `DATA_DIR` 为空时目录就在仓库工作目录。
- `.gitignore:21-28` 忽略数据库、`config.json` 和 `data/`，但没有忽略根目录 `subscriptions/`。
- `AGENTS.md:14` 明确规定真实订阅 URL 和凭据不得进入源码、fixture、日志、Git 或 agent memory。

### 反证与边界

- 本次没有验证文件中的凭据现在是否仍有效，因此不能声称仍可连接上游。
- 即使凭据已失效，**敏感材料曾进入 Git 历史**仍是既成事实；“已失效”只能降低后续危害，不能反证暴露。
- 本文不建议未经授权直接 purge 历史；历史重写、远端强制更新和凭据轮换都属于需要明确批准的外部/破坏性动作。

### 最终判定

**确认安全缺陷，P0 处置优先级。** P0 指先隔离和轮换，不代表已经证明存在正在进行的入侵。

## BUG-02：Sticky 绑定绕过暂停状态

### 支持证据

首次选路使用的 storage 查询明确过滤暂停：

- `storage/regions.go:18-30`：要求 `user_paused = 0`，并排除父订阅 `status='paused'`。
- `storage/proxy_queries.go:63-75`：通用可用集同样过滤两类暂停。

Sticky 快路径则不同：

- `selector/selector.go:214-233`：通过 `GetProxyByID` 读取已绑定节点。
- `storage/regions.go:157-166`：`GetProxyByID` 只按 ID 读取，不过滤父订阅状态。
- `selector/selector.go:281-283`：`proxyAvailable` 只检查 `status active/degraded` 与 `fail_count < 3`，不检查 `UserPaused`。
- `storage/proxy_updates.go:366-367` 的契约写明用户暂停节点“不参与选路”。
- `docs/DESIGN_LANGUAGE.md:329` 写明暂停订阅的节点整体不参与选路。

因此可构造确定反例：

1. session S 绑定到 active 节点 P。
2. 用户将 P 设置 `user_paused=1`，或暂停 P 的父订阅。
3. S 再次 Resolve。
4. `stickyBoundProxy` 仍返回 P；不会进入会过滤暂停的 `GetByRegion`。

### 反证与边界

- 新 session 和失去 sticky 的 session 不受影响，因为它们走 `GetByRegion`。
- `unlock`、region、status、fail count 在 sticky 路径已有检查；缺陷仅是“可用性谓词不完整”，不是整个 sticky 机制失效。
- “已有 session 应不受暂停影响”没有设计依据。相反，现有注释和设计文档明确写“不参与选路”，没有为 sticky 设置例外。

### 最终判定

**确认功能 bug，P1。** 最小修复不能只加 `!UserPaused`，还必须有父订阅暂停校验；最好把“可选路节点”收敛成 storage 的单一查询/谓词，避免第三种口径。

## BUG-03：WebUI 配置保存后运行态分裂

### 支持证据

WebUI 给用户的契约是：

- `webui/config_handlers.go:38-39` 只把三个端口列为只读 / 需要重启。
- `webui/dashboard.go:245-254` 把认证、TTL、默认地域、健康间隔、重试次数、sing-box 路径和国家过滤都作为可保存设置。
- `dashboard_assets.go` 保存成功后只提示“配置已保存”，没有提示其余字段需重启。

配置发布方式是不可变快照替换：

- `config/config.go:311-323`：`Save` 创建 `saved` 并把 `globalCfg` 替换为新指针，旧指针不会被原地修改。

但生产组件读取配置的方式不统一：

- HTTP：`proxy/server.go:89-143,191` 持有启动时 `s.cfg`，认证、默认地域、重试次数读旧快照。
- SOCKS5：`proxy/socks5_server.go:63-65,102,170-173,196-273` 同样读启动快照。
- 健康检查：`checker/health_checker.go:37,50-55,164-180` 缓存旧 cfg，ticker 间隔在启动时固定。
- Validator：`validator/validator.go:36-52,778-800` 缓存 `config.Get()` 当时的 cfg；测试 `validator_test.go:81-128` 甚至明确把旧快照不可变作为防竞态设计。
- sing-box：`custom/manager.go:97-109` 只在 Manager 构造时读取 `SingBoxPath` 和分片数。
- `main.go:66-70` 创建 `configChanged` channel，但全仓没有消费者。

只有部分字段真的即时生效：

- `SessionTTLMinutes`：`config_handlers.go:108-109` 显式调用 `affinity.SetTTL`。
- 国家过滤：保存时立即批量禁用 DB 节点；custom 的部分路径使用 `config.Get()`。
- selector 的 session cap / cooldown 使用 `config.Get()`，但当前 WebUI 保存 payload 没有这两个字段。

这不是“所有设置都完全不生效”，而是**同一次保存产生多套运行态**。

### 反证与边界

- 配置已正确落盘，进程重启后会统一生效，所以不是持久化失败。
- 端口明确拒绝运行时修改，这部分行为正确。
- 国家过滤存在即时 DB 操作，因此不能笼统声称所有过滤都完全不生效；真正问题是旧 Validator 仍可能按旧过滤规则判定。

### 最终判定

**确认功能与产品契约 bug，P1。** 应先定义逐字段的 `live` / `restart required` 矩阵，再统一实现。短期最安全做法是把实际不能热更的字段标为需重启；长期可给 HTTP/SOCKS/health/validator/Manager 注入统一配置快照提供器。

## BUG-04：订阅验证写库失败仍报告成功

### 支持证据

- `custom/manager.go:1053-1088` 的 `validateCustomProxies` 不检查以下调用的 error：
  - `UpdateSubscriptionProxyExitInfo`
  - `EnableSubscriptionProxy`
  - `DisableSubscriptionProxy`
  - `UpdateSubscriptionSuccess`
- 函数在调用 `EnableSubscriptionProxy` 后无条件 `valid++`。
- `storage/proxy_updates.go:95-130,349-363` 的这些方法并非 best-effort：没有匹配行、父订阅暂停或 DB 错误都会返回 error。
- `custom/manager.go:288-316` 的禁用节点恢复路径同样在 Enable / Update 失败后增加 `recovered` 并更新成功订阅集合。

一个无需模拟 DB 损坏的反例是：手动刷新 paused 订阅。父订阅暂停会使 `EnableSubscriptionProxy` 的 SQL 条件不匹配并返回 `requireRowsAffected` 错误，但调用方仍增加 `valid`，并打印“可用”。

### 反证与边界

- DB 事务替换订阅代理的主路径会检查 error；缺陷集中在验证结果写回和统计。
- 父订阅暂停仍会阻止节点真正进入选路，因此这是“假成功 / 状态与日志分裂”，不是直接绕过暂停选路。
- 正常 DB 且 active 订阅的 happy path 可成功，所以现有集成测试通过不能反证失败路径。

### 最终判定

**确认正确性 bug，P1。** 写回成功才可增加 valid/recovered；关键写失败必须显式记录，并由上层决定该刷新是否失败。

## BUG-05：跨订阅 re-fetch 只更新运行态

### 支持证据

`RefreshSubscription(A)` 在构造目标 sing-box 节点集时调用：

- `custom/manager.go:389-405` → `collectAllTunnelNodesExcludingSubscription(A, ...)`。
- `custom/manager.go:535-585` 先把当前所有 `singbox.GetNodes()` 放入 nodeMap，再遍历其他 active 订阅。
- 若订阅 B 到期，它会直接 fetch + Parse B，并把 B 的新 NodeKey 加入 nodeMap。
- 该路径不调用 `replaceSubscriptionProxies(B)`，也不更新 B 的 `last_fetch` / `proxy_count`。
- 旧 B 运行态节点也没有被排除，所以 B 的旧、新节点可能同时驻留。

因此 A 的刷新可以改变 B 的 sing-box 进程和端口占用，但 storage 仍只认识 B 的旧代理记录。

### 反证与边界

- 未入库的 B 新节点不会直接被 selector 选中，所以不是立即流量误路由。
- B fetch 失败时函数 `continue`，而旧运行态已在 nodeMap，通常不会把 B 整体删掉。
- 影响主要是运行态 / DB 漂移、端口浪费和后续身份映射复杂化，不应夸大为立刻丢流量。

### 最终判定

**确认生命周期 bug，P1。** 合并 A 的运行态时只应信任既有运行态快照和 A 的新解析结果；B 的拉取必须由 B 自己的完整刷新事务完成。

## BUG-06：通用删除订阅隧道节点绕过 Manager

### 支持证据

- `/api/proxy/delete` 是已注册、需管理员 session + CSRF 的可调用接口：`webui/server.go:279`。
- `webui/api_handlers.go:99-129` 只对 `SourceManual` 调用 `customMgr.DeleteManualNode`。
- 对 `SourceSubscription` 节点直接执行 `storage.DeleteProxyByID` / `storage.Delete`。
- `custom/manager.go:1219-1246` 已证明订阅运行态删除需要先 Reload、DB 失败时补偿；直接删 DB 绕过了这个所有权边界。
- 对隧道订阅节点，sing-box 仍保留 NodeKey 与 mixed 端口。下一次 collect 又以 `singbox.GetNodes()` 为底，幽灵运行态不会自然保证消失。

### 反证与边界

- 当前前端节点行不调用此通用删除端点，普通用户 UI 路径较难触发。
- 接口是管理员写接口，不是未认证攻击面。
- 直接 HTTP/SOCKS5 订阅节点没有 sing-box 运行态，删 DB 不会造成该类资源泄漏。

### 最终判定

**确认条件明确的 API 生命周期 bug，P2。** 最好删除无明确产品语义的通用端点；若保留，订阅节点删除必须定义“临时排除、用户暂停、永久从订阅排除”中的一种语义，而不能只删 DB。

## BUG-07：状态 API 吞错误并返回假数据

### 支持证据

- `webui/api_handlers.go:24-43` 对四个 Count 调用使用 `value, _ := ...`，任何 SQLite 错误都被编码为 0，并返回 HTTP 200。
- `webui/api_handlers.go:56-63` 获取订阅名失败时静默返回空名称。
- `custom/manager.go:1091-1107` 的状态接口同样忽略多个 storage error。
- 项目规则明确禁止用静默 fallback 掩盖边界失败。

### 反证与边界

- 正常 DB 下计数准确；问题只在 DB 错误、关闭或锁异常路径。
- stats 是观测接口，不改变路由状态，因此影响低于选路和持久化缺陷。

### 最终判定

**确认观测正确性 bug，P2。** 应在任何关键计数失败时记录模块日志并返回 500，不能把“读取失败”伪装成“数量为 0”。

## BUG-08：新 HTML 可命中旧 JS 缓存，添加订阅按钮报函数未定义

### 用户可见证据

- 用户在“添加订阅”弹窗点击“添加”后观察到：`Uncaught ReferenceError: submitSubscription is not defined at HTMLButtonElement.onclick`。
- 这不是后端 `/api/subscription/add` 返回失败；异常发生在内联 `onclick` 解析函数时，请求尚未发出。

### 根因证据

- `webui/dashboard.go:265` 的按钮调用 `submitSubscription()`。
- `webui/dashboard_assets.go:592` 的当前基线源码确实定义了 `submitSubscription`，所以“当前源码忘记实现函数”不是根因。
- `git show 7becd9f^:webui/dashboard_assets.go` 显示上一版本已有 `openSubModal()` 与 `addSubscription()`，但没有 `submitSubscription()`。
- `7becd9f` 同一个提交把 HTML 按钮从 `addSubscription()` 改成 `submitSubscription()`，并在新 JS 中增加该函数。
- HTML 始终引用固定 URL `/assets/dashboard.js`：`webui/dashboard.go:268`。
- `webui/server.go:402-406` 对该固定 URL 返回 `Cache-Control: max-age=3600, must-revalidate`。资源在一小时 fresh 期间可以直接复用，不要求先向服务端校验 ETag；ETag 只有发生重新验证时才有作用。

因此存在确定的合法部署时序：

1. 浏览器缓存旧 `/assets/dashboard.js`；旧脚本能打开弹窗，因为它已有 `openSubModal()`。
2. 服务升级到 `7becd9f`，HTML 开始调用新函数名。
3. 浏览器拿到新 HTML，但继续复用仍 fresh 的同 URL 旧 JS。
4. 点击“添加”时找不到 `submitSubscription`，产生用户报告的 ReferenceError。

### 运行态旁证与边界

- 本轮访问本机 `127.0.0.1:7800/assets/dashboard.js`，确认响应确实使用固定 URL、强 ETag 和一小时缓存。
- 当前本机 `GeoProxy.exe` 下发的 JS 与 `main@7becd9f` 源码并非完全相同，包含重复的订阅提交 / 更新函数，而基线源码各只有一份。这说明运行二进制、浏览器缓存和审计基线之间还存在构建产物来源漂移；不能用“工作树源码里有函数”反驳浏览器错误。
- 本轮没有读取用户浏览器缓存条目的响应时间和 ETag，因此“具体是哪一份旧构建”仍未知；但旧版源码、缓存策略和错误形态已经形成完整可触发链。
- 现有 `assets_route_test.go` 只要求 Cache-Control 非空和 ETag 正确，没有模拟版本升级后新 HTML 与旧资产缓存组合。

### 最终判定

**确认 WebUI / 部署一致性 bug，P1。** 修复应使用内容指纹资产 URL（例如 hash 文件名或 hash query）或强制每次重新验证，不能继续依赖“固定 URL + 一小时 fresh cache + ETag”。还应增加真实浏览器测试：打开弹窗、点击添加、确认无 ReferenceError，并确认合法 payload 发出一次 POST。

## 4. 条件性问题的严格边界

## RISK-01：重定向泄露非标准自定义头，但“重定向 SSRF 到内网”是误报

### 成立部分

- `custom/manager.go:789-805` 创建的 `http.Client` 没有自定义 `CheckRedirect`。
- `buildSubscriptionRequest` 接受任意 JSON string header，不限制名称。
- Go 1.26 标准库 `net/http/client.go:808-827` 只把 Authorization、Cookie、Proxy-Authorization 等列为敏感头；`X-Token`、`X-API-Key` 等任意自定义头会被复制到重定向请求。
- README 还建议供应商需要时使用订阅自定义头，因此“自定义头可能包含秘密”是项目认可的真实用法。

只要可信订阅源返回跨主机 30x，非标准秘密头就可能被发到目标主机。这是成立的机密性缺口。

### 不成立部分

- `custom/manager.go:971-1000` 的自定义 `DialContext` 会对**每次实际拨号**重新解析并拒绝私网、链路本地和保留地址。
- 因此“公网 URL 302 到 `169.254.169.254` 即绕过 SSRF 防护”不成立。
- Go 标准库会在不受信跨域时剥离标准 Authorization/Cookie；原报告笼统说所有认证头都会泄露也不准确。

### 最终判定

**条件性安全 bug，P1。** 修复目标应精确表述为“每跳 URL 策略 + 跨 origin 自定义头策略”，而不是重复实现已有的私网 Dial 防护。

## RISK-02：`user_paused` 地址身份在端口复用时错绑

### 成立部分

- `custom/manager.go:635-678` 按旧 `address` 快照暂停位，并按新 entry 的 address 恢复。
- 隧道 address 是 `127.0.0.1:<mixedPort>`，不是上游节点稳定身份。
- `custom/singbox.go:381-421` 会保留仍存在 NodeKey 的端口，但会释放已移除节点端口并让新节点复用低位空洞。
- 因此“暂停的旧节点被替换，同时新节点复用同一端口”时，新节点会错误继承暂停。

### 原报告不准确的部分

- 对仍存在的同一 NodeKey，端口映射被明确保持稳定；`csv_bugs_test.go:87-104` 也锁定这一点。
- 因此“同一节点正常刷新时常因端口变化丢失暂停”没有证据。

### 最终判定

**部分成立并重定义为条件性 P2 bug。** 暂停应绑定 NodeKey 或独立持久稳定 ID，而不是本地端口地址。

## RISK-03：`apiSessions` nil affinity

- `webui/sessions.go:26-37` 直接调用 `s.affinity.List()`。
- `webui.New` 接受 `*affinity.Store`，未拒绝 nil。
- 正式 `main.go:55-70` 始终注入非 nil store，现有测试构造也如此。

**判定：非生产主路径的健壮性 bug，P3，不应列 P1。** 可以在构造器 fail-fast，或与 occupancy 一样返回空数组。

## RISK-04：SOCKS5 Accept 错误忙循环

- `proxy/socks5_server.go:53-58` 对任何 Accept error 立即 `continue`，无日志、退避或退出。
- 持续错误（如资源耗尽）会形成紧循环。
- 正常监听不会触发；当前服务也没有显式 graceful-close 路径。

**判定：低优先级可靠性 bug，P3。** 应记录错误并退避；明确 listener 关闭时退出。

## 5. 文档与部署 bug

## DOC-01：数据目录文档与 compose 相反

- `DATA_DIRECTORY.md:5,59-100,122-132` 多次声称默认使用 `GeoProxy-data` Named Volume。
- `docker-compose.yml:15-18` 实际默认 bind `${HOST_DATA_DIR:-./data}` 到 `/app/data`。
- README `Deployment Notes` 与 compose 一致，说明 `DATA_DIRECTORY.md` 是过时文档，而非 compose 暂时偏离。

**判定：确认文档 bug，P1。** 会直接误导备份、恢复和数据定位。

## DOC-02：公开 DSL 文档缺 unlock（SUPERSEDED）

- `auth/dsl.go:11-14` 和 `AGENTS.md:35` 的正式语法均为 `<base>[-region-<cc>][-unlock-<token>][-session-<id>]`。
- 基线 `README.md:84-114` 曾写 region → session；该历史说法由 BUGFIX-031 修复。
- 基线 `CLAUDE.md:70`、`GEO_FILTER.md` 示例也曾缺 unlock；当前文件已同步完整 DSL。
- `CHANGELOG.md:60` 和 `DESIGN_LANGUAGE.md:360-362` 已使用完整语法。

**历史判定：确认行为文档 bug，P1。当前状态：SUPERSEDED（BUGFIX-031）。**
外部集成应以当前 `README.md`、`GEO_FILTER.md` 和 `AGENTS.md` 为准。

## DOC-03：`.env.example` 与 compose 不闭环

- `.env.example:25-46` 列出 `MAX_SESSIONS_PER_PROXY`、`PROXY_COOLDOWN_MINUTES`、`SINGBOX_SHARD_COUNT`。
- `docker-compose.yml:21-31` 未把这些变量传给容器。
- Compose 读取 `.env` 只用于模板替换；未在 compose 文件引用的变量不会自动成为容器环境。
- compose 使用 `HOST_DATA_DIR`，但 `.env.example` 没有列出。
- 只读 API 的首次启动环境键在 `config` 中存在，但 compose/example 也没有完整入口。

**判定：确认部署配置 bug，P1。** 用户即使取消注释示例变量，容器行为也不会改变。

## DOC-04：免责声明仍描述已移除产品模型

- README 英文说明管理用户提供的上游资源。
- README 中文 `389-393` 却称抓取互联网公开代理、访客贡献订阅。
- `CLAUDE.md:107` 和 WebUI 路由表明确没有访客贡献入口。

**判定：确认产品 / 法律文档 bug，P1。** 应改成用户自有或获授权订阅模型。

## DOC-05：镜像发布口径冲突

- README `307` 声称本 fork 不发布预构建镜像。
- `.github/workflows/docker-image.yml:3-15,77-103` 配置 main/tag 推送 GHCR，并在有凭据时推 Docker Hub。

**判定：确认源码层面的发布契约漂移，P2。** 本次没有访问远端 registry，因此不能进一步断言某个 tag 当前一定可拉取。需要二选一：删除发布 workflow，或更新 README 并给出正式镜像与支持策略。

## DOC-06：代理密码落盘安全说明自相矛盾

- `config/config.go:160-162` 注释写“明文仅保存在 firstBoot，不写入磁盘明文（磁盘只存 hash）”。
- 同文件 `196-198` 明确把代理密码保留明文。
- `savedConfig.ProxyAuthPassword` 与 `Save:255-276` 明确把明文写入 `config.json`。
- `webui/config_handlers.go:28-30` 也明确管理端会下发该明文。

**判定：确认安全文档 bug，P1。** 实现是有意设计取舍，但注释必须如实描述：WebUI 登录密码仅 hash；代理认证密码为支持复制 URL 而明文落盘和下发给已认证管理员。

## DOC-07：PauseProxy 注释过时

- `storage/proxy_updates.go:366` 写状态置 `paused`。
- 实现 `368-379` 只写 `user_paused=1`，status 保持 active/degraded/disabled 底色。
- migrations 也已把旧 `status='paused'` 迁移为 `user_paused`。

**判定：确认维护文档 bug，P3。** 不影响运行，但会诱导后续开发重新引入双状态模型。

## DOC-08：GeoProxy 品牌、项目链接与部署标识未完成统一

### 已确认的目标与当前真相

- 用户已明确当前项目品牌应为 **GeoProxy**，不再把品牌是否统一视为未决产品问题。
- Git `origin` 已是 `https://github.com/babutree/GeoProxy.git`；`upstream` 是原项目 `https://github.com/isboyjc/GoProxy.git`，二者角色正确。
- 生产 WebUI 的标题、登录页和 GitHub 图标已经使用 `GeoProxy` / `https://github.com/babutree/GeoProxy`。
- `CHANGELOG.md` 的项目仓库和 issues 链接已经指向 `babutree/GeoProxy`。

### 确认仍需迁移的范围

- 对外文档品牌：`README.md` 标题、简介与正文，`CLAUDE.md` 项目简介，`GEO_FILTER.md`，`DATA_DIRECTORY.md`，`test/README.md` 及测试脚本头仍称 `GeoProxy`。
- 当前项目链接：README 只在致谢区链接上游项目，缺少清晰的当前仓库 / issues / releases 或 packages 入口；本地被 `.git/info/exclude` 排除的 `docs/orbit-dashboard.html` 仍写 `https://github.com/babutree/GeoProxy`。该旧 URL 当前会重定向到 GeoProxy，但仍不是应发布的 canonical URL。
- HTTP 与外发标识：`proxy/server.go` 的 Basic realm 仍是 `GeoProxy`；`validator/validator.go` 的探测 User-Agent 仍是 `GeoProxy-ai-probe/1.0`；`custom/parser.go` 的调试文件仍用 `GeoProxy` 前缀。
- Dockerfile：构建和启动二进制仍叫 `proxy-pool`，与“已移除公共池”及 GeoProxy 品牌均冲突。
- Compose：服务键、默认容器名和网络仍为 `GeoProxy` / `GeoProxy-net`，本地镜像名为混合形式 `GeoProxy-geo:local`。
- 发布链路：workflow 的 GHCR / Docker Hub image name 仍为 `babutree/GeoProxy`；CHANGELOG 的 package URL 也指向 `/pkgs/container/GeoProxy`。Docker registry 名必须使用小写，若迁移建议目标为 `babutree/geoproxy`，不能写大写 `GeoProxy` 镜像引用。
- 构建 / 测试接口：`CLAUDE.md` 使用 `proxygo`；测试脚本和 `AGENTS.md` 使用 `GeoProxy_AUTH_*`；ignore 文件同时残留 `GeoProxy`、`proxy-pool`。这些必须与最终命名一次性联动，不能只改文案。
- Go module：`go.mod` 仍是本地模块名 `GeoProxy`，所有内部 import 依赖该值。后续应一次性迁移为 canonical module path `github.com/babutree/GeoProxy`，而不是只把 import 前缀机械替换成 `geoproxy`。

### 明确不得误替换的内容

- `README.md` 致谢、`docs/PRD.md` 历史背景和 Git `upstream` 中的 `isboyjc/GoProxy` 是原项目名称与来源链接，必须保留。
- PowerShell / Go 工具链的标准环境变量 `GeoProxy` 与 `https://goproxy.cn` 不是本项目品牌，必须保留。
- CHANGELOG 中“早期版本默认 `GeoProxy`”属于历史事实，不能改写成从未发生过的 GeoProxy 历史。
- 协议、数据库字段和通用词 `proxy` 不是品牌残留，不应进行无差别替换。

### 最终判定

**确认项目文档 / 部署契约 bug，P1。** 目标品牌已经明确，但迁移跨越文档、运行标识、Compose、registry、脚本接口和 Go module 路径；必须按第 8 节 TODO 原子实施并验证。本轮只登记，不修改上述文件。

## 6. 误报与严重度夸大

| 原结论 | 最终判定 | 反证 |
|---|---|---|
| 暂停订阅后 sing-box 仍运行是高危 bug | **误报 / 资源策略未定义** | 当前明确契约只要求暂停节点不参与选路；未承诺释放本地端口。修复 BUG-02 后，保留运行态主要是资源优化议题 |
| `RefreshSubscription` 必须拒绝 paused 订阅 | **误报** | 调度 `RefreshAll` / `checkAndRefresh` 已跳过 paused；UI 在 paused 卡片仍显示“刷新”，说明手动刷新是现有产品行为。真正 bug 是写库失败仍计成功 |
| 重定向可绕过 SSRF 访问 metadata | **误报** | 每个实际 dial 都经过 `safeSubscriptionDialContext` 的 IP 校验；成立的是非标准自定义头跨域泄露 |
| 前端不发 `X-CSRF-Token` 是 CSRF 漏洞 | **误报** | 后端接受同源 Origin/Referer；session cookie 是 SameSite=Lax；两者都缺失时 fail-closed 403，而非放行。显式 token 可增强兼容性，但缺少它本身不是漏洞 |
| 管理端下发代理密码明文是实现 bug | **设计取舍，不是未授权泄露** | API 需要管理员 session，代码和功能明确用于复制完整代理 URL；只读 API 禁止密码的契约不适用于管理员 config API。风险必须文档化，见 DOC-06 |
| 静态 dashboard CSS/JS 无鉴权是漏洞 | **误报** | `assets_route_test.go:83-95` 明确锁定公开资产契约；资产不含业务数据或凭据。公开 JS 会暴露 API 形状，但不是认证绕过 |
| `GetRandom` 排除 degraded 会造成生产选路无节点 | **误报为功能 bug；保留设计债** | 全仓生产调用没有使用 `GetRandom`；主 selector 走 `GetByRegion`，包含 degraded。该函数口径不一致但当前是未使用 API |
| `recoverFailedShards` 不更新 assignedKeys 会提交错误状态 | **证据不足 / 严重度夸大** | 恢复目标就是该 shard 已加载节点，assignedKeys 应保持最后一次提交；生产 `SingBoxProcess.Reload` 对 Partial 返回 error，下一轮 `shardNeedsReloadForRuntime` 还会严格检查。可复用 commit helper 加固，但未证明错误提交 |
| 端口释放超时仍启动会假成功 | **误报** | 启动后还有 10 秒端口就绪门；未全就绪设置 Partial 并返回 error，不会以成功提交。继续尝试可能浪费时间，但不是静默成功 |
| shardCount `<1` 收敛为 1 违反边界契约 | **误报** | 正式 config 加载已保证正值；构造器 clamp 是防除零保护。若希望严格配置错误，应在 config 边界报错，而非删除底层防御 |
| 空 region 会“跨地域静默 fallback” | **误报** | README 明确 `username` 使用任意可用节点，设置页写“空=全局”。只有显式 region 无节点时才禁止换区，selector 已满足 |
| 无 session 的 Pick 忽略 occupancy/cooldown 是 bug | **误报** | `LEASE_DESIGN.md` 和已有测试明确把限制定义为 sticky session 准入，不是连接级限流 |
| HTTP 把畸形 DSL 统一映射 407 一定违反“显式错误” | **证据不足** | parser 显式返回 error；认证边界合并错密和坏用户名符合常见安全策略。可增加模块日志，但不应向未认证客户端泄露精确解析原因 |
| 不等长 ConstantTimeCompare 是严重认证漏洞 | **严重度夸大** | 正常密码路径比较固定长度 SHA-256 hex；base 用户名通常非秘密。可对 base 哈希后比较做 hardening，但没有可利用影响证据 |
| 管理端订阅列表返回 URL / headers / file_path 是泄露 | **误报** | 这些字段是管理员编辑订阅所需数据，路由受管理员 session 保护；只读 API 不返回它们。会话失陷属于更高层威胁，不等于该响应越权 |
| `proxygo` 被跟踪是 P0 功能 bug | **严重度夸大** | 跟踪 13MB Mach-O 二进制是仓库卫生和供应链来源问题，但未进入当前 Docker runtime，也无生产调用。应移出源码仓库，但不能与凭据暴露等同 |
| `submitSubscription` / `updateSubscription` 在 `main@7becd9f` 源码中重复定义 | **误报（审计基线）** | 基线 `dashboard_assets.go` 各只有一个定义；但本机运行二进制下发了不同资产，且固定 URL 缓存会造成跨版本失配，后者已单独确认为 BUG-08 |
| “无 UI 的写 API”天然就是 bug | **误报原则** | API 可服务脚本或兼容调用；是否有 UI 不是正确性判据。`/api/proxy/delete` 的真实 bug 是运行态所有权旁路（BUG-06），不是“无 UI” |
| update 不支持替换本地 file_content 是 bug | **证据不足 / 产品范围** | 当前 update 契约是元数据编辑并保留 file_path/format；未承诺替换上传内容。若产品需要，应另立功能规格 |
| NodeKey JSON marshal fallback 当前会碰撞 | **证据不足** | 正常解析后的 Raw 已规范化为可 JSON 编码结构，且现有测试覆盖凭据和传输参数差异。fallback 理论上较弱，但无合法输入触发证据 |
| `parseSpeed` 失败默认 100 Mbps 是 bug | **证据不足** | 未找到必须拒绝缺失/坏带宽字段的协议或产品契约；该值是可选传输提示，不能仅因有默认值判错 |

## 7. 设计债：存在，但不应冒充功能 bug

| 项目 | 证据 | 为什么不是当前功能 bug |
|---|---|---|
| `configChanged` 死 channel | main 创建、WebUI 写入、无消费者 | 它是 BUG-03 的失败机制和死抽象；单独看不会改变行为 |
| `checker.Checker` 与 `HealthChecker` 双实现 | `main` 只使用 `HealthChecker`；旧 Checker 无生产调用 | 当前不会执行旧“一次失败即禁用”策略，但维护者可能改错实现 |
| HTTP / SOCKS5 两份 `dialViaProxy` | 两个 server 各有实现 | 现有测试分别覆盖域名长度、超时等关键边界，尚无行为分叉证据 |
| Manager 直接 `GetDB()` 管事务 | `replaceSubscriptionProxies` 等直接操作 DB | 事务所有权偏离 storage，但目前用于保证跨多语句原子性；需要设计 API 后再收敛 |
| 旧手工节点 JS 函数和 `renderWorldMap` 别名 | 当前 UI 不直接调用，测试仍锁字符串 | 增加维护噪音，不产生错误结果 |
| `GetRandom` 可用性口径不同 | 只接受 active，而其他查询接受 degraded | 当前无生产调用；删除或统一即可 |
| `portOffset` / `portRangeSize` 历史字段 | 端口实际由空洞扫描分配 | 误导阅读，但当前分配逻辑由 portMap + segment 扫描决定 |
| 品牌名 GeoProxy / GeoProxy / module GeoProxy / binary proxy-pool 并存 | UI、README、module、Docker 产物分别不同 | 用户已明确外部品牌统一为 GeoProxy，因此不再是未决产品问题；迁移范围与保留项见 DOC-08。module 路径仍需作为原子兼容变更实施 |
| `proxygo` 跟踪二进制 | 初始提交即存在，当前为 Mach-O | 应移除并补 ignore/checksum 策略，但不是当前运行时 bug |
| pause 后保留 sing-box 运行态 | storage 层只定义不选路，未定义释放端口 | 可新增“暂停即释放资源”策略，但需要处理恢复、共享 NodeKey 和端口稳定性，不是小修 |

## 8. 建议修复顺序与验收证据

### 第一组：安全处置

1. 移除 Git 跟踪的真实订阅文件并补仓库级 ignore。
2. 轮换相关订阅 / 节点凭据。
3. 评估是否重写远端历史；这是破坏性动作，必须单独授权。
4. 增加 CI secret scan，至少覆盖 `subscriptions/`、代理协议链接、password/token 字段。

验收：

- `git ls-files subscriptions` 无运行时订阅。
- `.gitignore` 覆盖根目录 subscriptions、shard 运行目录和敏感导出文件。
- secret scanner 对当前 HEAD 通过；若未 purge 历史，文档明确历史仍含旧材料。

### 第二组：路由与配置一致性

1. 修复 sticky 可用性谓词，覆盖节点暂停和父订阅暂停。
2. 为每个 WebUI 设置字段定义热更新矩阵。
3. 代理入站改为读取统一快照，或把不能热更的字段明确标为重启生效。
4. health ticker / validator / sing-box path 按矩阵实现，不再依赖无人消费 channel。

必须新增：

- `TestStickyBindingRejectedWhenUserPaused`
- `TestStickyBindingRejectedWhenParentSubscriptionPaused`
- HTTP 与 SOCKS5 配置保存后认证、默认地域、MaxRetry 生效测试
- health interval / validator country filter 的热更新或“明确需重启”测试

### 第三组：订阅生命周期

1. 验证写库 error 必须参与成功判定。
2. 删除跨订阅 opportunistic re-fetch，保持一个订阅一个完整事务。
3. 暂停状态从本地地址迁移为稳定 NodeKey / 持久节点身份。
4. 通用删除接口收敛到 Manager，或移除不清晰的订阅节点删除能力。
5. redirect 跨 origin 时只保留明确白名单头；每跳重新校验 URL，限制跳数。

必须新增：

- paused 订阅验证不得假报 valid/recovered
- A 刷新不得改变 B 的运行态或 DB
- mixed 端口复用不得把暂停转移给另一 NodeKey
- subscription tunnel 通过通用 API 删除后 DB / runtime 一致
- 跨域 redirect 不转发任意密钥头；私网 redirect 仍被 Dial 防护拒绝

### 第四组：文档和维护债

1. 修正 DATA_DIRECTORY、DSL、compose env、免责声明和代理密码落盘说明。
2. 决定是否正式发布镜像，再统一 README 与 workflow。
3. 删除旧 Checker、死 JS 和未使用 storage API。
4. 按下列 GeoProxy 迁移清单统一外部品牌；Go module 必须作为独立原子变更验证，不能夹在文案替换中。

### 第五组：GeoProxy 品牌、项目链接与容器命名迁移 TODO

> 状态：**TODO，当前审计任务不执行。** 目标外部品牌已确定为 `GeoProxy`；下表属于后续整改任务，不代表文件已经修改。

| TODO | 范围 | 后续实施要求 | 验收证据 |
|---|---|---|---|
| BRAND-01 | `README.md`、`CLAUDE.md`、`GEO_FILTER.md`、`DATA_DIRECTORY.md`、`test/README.md`、测试脚本标题及仍维护的本地审计文档 | 将当前项目的 `GeoProxy` 文案统一为 `GeoProxy`；不改上游名称和历史事实 | 分类 grep 只剩明确白名单中的上游 / 历史 / Go 工具链引用 |
| BRAND-02 | 当前项目链接 | README 增加当前仓库、issues、发布 / package 入口；把 `docs/orbit-dashboard.html` 的 `babutree/GeoProxy` 改为 canonical `babutree/GeoProxy`；保留 `isboyjc/GoProxy` 致谢 | 链接清单逐个返回预期状态；当前项目链接和上游链接标签不混淆 |
| BRAND-03 | `proxy/server.go`、`validator/validator.go`、`custom/parser.go` | realm 改为 `GeoProxy`；探测 UA 和调试临时文件前缀统一为 `geoproxy` | HTTP 407 realm 测试、UA 单测 / 抓包、调试路径测试或静态断言 |
| BRAND-04 | `docker-compose.yml`、`.env.example`、README / DATA_DIRECTORY 命令 | 服务、默认容器、网络统一为 `geoproxy`，本地镜像统一为 `geoproxy:local`；同步所有 `docker compose exec/logs/restart` 命令 | `docker compose config` 通过；启动后 service、container、image、network 名一致；文档命令可执行 |
| BRAND-05 | `Dockerfile`、`.dockerignore`、`.gitignore`、`CLAUDE.md`、测试运行说明 | 将过时二进制 `proxy-pool` / `proxygo` 统一为 `geoproxy`，同步 COPY、CMD、ignore 和运行命令 | `go build -o geoproxy .`、Docker build、容器启动和 healthcheck 通过；旧跟踪二进制另按 P0 授权处置 |
| BRAND-06 | `.github/workflows/docker-image.yml`、CHANGELOG package URL、README 镜像说明 | registry 目标统一为小写 `babutree/geoproxy`；当前新 GHCR package 路径返回 404，必须先发布并验证新 package / tag，再切换文档，不得留下“已发布但不可拉取”的假说明 | Actions 成功；对目标 tag 执行真实 `docker pull`；GHCR / Docker Hub 链接与 workflow 完全一致 |
| BRAND-07 | `AGENTS.md`、`test/*` 和测试文档 | 将项目自定义 `GeoProxy_AUTH_*` 原子迁移为 `GEOPROXY_AUTH_*`；不要改 Go 官方 `GeoProxy` | 所有 shell / Go / Python 探针使用新变量；缺变量失败测试与实际 E2E 通过 |
| BRAND-08 | `go.mod` 与全仓 Go imports | 原子改为 `github.com/babutree/GeoProxy` 并一次性更新 imports；不增加无明确需求的双路径兼容层 | `go list ./...`、`go test ./...`、`go build ./...`、`go vet ./...` 全通过；无旧本地 module import |
| BRAND-09 | 备份文件名、临时文件名、本地被 exclude 的 `docs/DEV_PLAN.csv` / `docs/BUGS_ANALYSIS.md` / `docs/orbit-dashboard.html` | 同步仍在使用的项目资产；历史计划若不再维护则明确归档，不得继续以“已修复”状态误导当前基线 | 受跟踪文件与明确纳入发布的本地资产二次扫描；历史文件有维护 / 归档标识 |
| BRAND-KEEP | 上游、历史和工具链白名单 | 保留 `isboyjc/GoProxy`、Git upstream、PRD 原项目描述、CHANGELOG 历史默认值、Go `GeoProxy` / `GeoProxy.cn` | 白名单逐项人工复核，防止全局替换破坏来源署名或 Go 构建配置 |

迁移顺序应为：先确定 registry / module 技术目标并完成代码与部署联动，再更新用户文档和发布链接，最后做带白名单的残留扫描。Compose 服务改名会影响现有 `docker compose exec GeoProxy` 运维命令并可能留下旧容器 / 网络，实施时必须写清停机与清理步骤；本轮不执行这些操作。

### 第六组：订阅按钮静态资源一致性

1. 为 dashboard CSS / JS 使用内容指纹 URL，或改为每次必须重新验证的缓存策略。
2. 明确 HTML 自身缓存策略，保证一个页面版本只能引用同版本资产。
3. 增加跨版本缓存回归测试，不只检查 ETag 是否存在。
4. 增加浏览器 E2E：打开添加订阅弹窗、点击添加、断言无 ReferenceError，并观察一次 `/api/subscription/add` 请求。
5. 在修复前先确认部署二进制对应 commit 和实际下发资产 hash，避免仅修改源码却继续运行旧产物。

## 9. 本次验证范围与局限

### 已直接验证

- 逐段读取生产调用链、SQL 过滤、配置发布、WebUI 路由、订阅刷新、sing-box reload 和 Go 标准库 redirect 头复制逻辑。
- `git ls-files` 验证订阅 YAML 与 `proxygo` 的跟踪状态；没有打印凭据值到本文。
- 对受跟踪文件及本地项目资产执行品牌、仓库 URL、registry、Dockerfile、Compose、realm、User-Agent、测试变量和二进制名扫描，并按“应迁移 / 联动迁移 / 必须保留”分类。
- `git remote -v` 确认 origin 为 `babutree/GeoProxy`、upstream 为 `isboyjc/GoProxy`。
- 当前仓库、issues、上游仓库和现有 GHCR `babutree/GeoProxy` package 网页均返回 HTTP 200；旧 `babutree/GeoProxy` URL 以 301 跳转到 `babutree/GeoProxy`，仍应改为 canonical URL。
- 拟迁移的 GHCR `babutree/geoproxy` package 网页当前返回 HTTP 404，验证了 BRAND-06 必须遵循“先发布成功，再切换文档 / workflow 消费方”的顺序。
- 对 `7becd9f` 前后 dashboard 资产做历史差异检查，并读取本机 `/assets/dashboard.js` 的 Cache-Control / ETag，确认 BUG-08 的跨版本失配机制。
- 已运行并通过：
  - `go test ./auth ./selector ./affinity ./proxy ./webui ./config -count=1`
  - `go test ./storage ./custom ./checker ./validator -count=1 -short`
  - `go vet ./auth ./selector ./affinity ./proxy ./webui ./storage ./custom ./config`

### 未验证

- 未验证已入库凭据当前是否仍有效。
- 未通过 registry 认证枚举 GHCR tag / manifest，也未执行真实 `docker pull`；package 网页可见不等于任一 tag 一定可拉取。
- 本轮访问 Docker Hub 页面超时，未确认旧 / 新 Docker Hub 仓库是否存在或可见。
- 未运行真实 6000 节点 sing-box 测试，因为本轮未修改分片/生命周期代码；已有问题判定不依赖该规模测试。
- 未执行真实浏览器跨域攻击；CSRF 判定依据后端 fail-closed 逻辑、cookie 属性与现有测试。
- 未读取用户浏览器缓存中旧 dashboard.js 的具体 ETag / 时间，也未在本轮修复或执行添加订阅 E2E；BUG-08 的触发链由用户错误、相邻版本源码和缓存策略共同支持。
- 条件性问题在修复前仍应增加最小回归测试，以把静态可构造反例固化为可重复证据。

## 10. 总结

上一轮审计的方向有价值，但把“代码异味、资源策略、测试缺口、文档漂移和安全 bug”混在同一个 P0/P1 列表里，导致若干严重度夸大。

经反证后，最重要的真实问题是：

1. **敏感订阅材料已进入 Git 历史。**
2. **sticky 路由绕过暂停。**
3. **WebUI 保存配置后运行态分裂。**
4. **订阅验证会吞写库错误并报告假成功。**
5. **跨订阅刷新和通用删除绕过运行态所有权。**
6. **部署与安全文档存在会导致错误操作的硬冲突。**
7. **固定 URL 的 dashboard 资产缓存会导致跨版本 HTML / JS 失配，已实际表现为添加订阅按钮失效。**
8. **GeoProxy 品牌目标已明确，但项目链接、Docker/Compose、registry 与技术标识尚未完成原子迁移。**

最关键的误报是：暂停不立即卸载 sing-box、手动刷新 paused 订阅、私网 redirect SSRF、前端未显式发送 CSRF token、管理员读取代理密码、公开静态资产。这些现象本身真实，但原报告缺少契约边界或忽略了现有防护，不能按原严重度处理。
