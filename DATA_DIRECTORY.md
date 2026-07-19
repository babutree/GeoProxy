# 数据目录与持久化

GeoProxy 的运行时状态由配置文件、SQLite 数据库和订阅文件组成。本文以当前
`config/config.go`、`storage/storage.go`、`storage/migrations.go` 与
`docker-compose.yml` 为事实源。

## 数据目录选择

### 原生运行

`DATA_DIR` 非空时，程序使用该路径（相对路径按进程当前工作目录解释；生产环境
建议使用绝对路径）。`Load` 和 `Save` 会先创建目录；路径不可创建、是普通文件或
权限不足时会立即报错，不会静默改写到别处。

`DATA_DIR` 未设置或为空时，默认目录为：

`os.UserConfigDir()/GeoProxy`

典型位置由 Go 的 `os.UserConfigDir` 决定：

- Windows：`%AppData%\GeoProxy`
- macOS：`~/Library/Application Support/GeoProxy`
- Linux/Unix：`$XDG_CONFIG_HOME/GeoProxy`，未设置时通常为
  `~/.config/GeoProxy`

正常启动入口 `config.Load` 会创建该目录；首次成功启动会生成并写入 `config.json`。
单独调用 `DefaultConfig` 只解析路径，不代表已完成持久化。

注意：订阅上传入口已经使用同一解析规则；`custom.NewManager` 的 sing-box 生命周期
仍直接读取 `DATA_DIR`，未设置时不会自动继承该原生默认目录。原生部署在该入口
完成收敛前应显式设置 `DATA_DIR`，避免 sing-box 临时配置落到 CWD（跟踪项：
`BUGFIX-075`）。

为避免升级时生成一套无法解释的新身份，若未设置 `DATA_DIR` 且当前工作目录已经
存在 `config.json`、`proxy.db`、WAL/SHM 文件、`data.db` 或
`subscriptions/` 等旧运行时标记，启动会显式失败。错误会列出检测到的标记并提示：
设置 `DATA_DIR` 指向旧目录，或人工迁移后再启动；程序不会自动迁移、复制或删除
旧文件。迁移前先停止服务并保留可回滚备份。

### Docker Compose

默认 Compose 使用 bind mount，不声明 named volume：

```yaml
volumes:
  - type: bind
    source: ${HOST_DATA_DIR:-./data}
    target: /app/data
environment:
  - DATA_DIR=/app/data
```

因此默认宿主机目录是 Compose 文件旁的 `./data`（即 `${HOST_DATA_DIR:-./data}`）；可在 `.env` 中设置
`HOST_DATA_DIR` 覆盖宿主机路径。容器内路径固定为 `/app/data`，除非同时修改
挂载目标和 `DATA_DIR=/app/data`。named volume 只能作为运维方自行改写 Compose
后的部署选择，不能按当前文件推断其名称或位置。

## 文件与首次启动

### `config.json`

首次成功执行 `config.Load` 会生成随机 WebUI 登录凭据和代理凭据并保存。实际
持久化键来自 `config.savedConfig`：

- `http_port`、`socks5_port`、`webui_port`
- `webui_password_hash`
- `proxy_auth_enabled`、`proxy_auth_username`、`proxy_auth_password`、
  `proxy_auth_password_hash`
- `session_ttl_minutes`、`max_sessions_per_proxy`、
  `proxy_cooldown_minutes`
- `default_region`、`blocked_countries`、`allowed_countries`
- `health_check_interval`、`max_retry`
- `singbox_path`、`singbox_shard_count`
- `readonly_api_keys`、`public_host`、`readonly_api_rate_per_min`

WebUI 密码只保存哈希；代理密码为了生成可复制的完整代理 URL，按当前产品
合同以明文保存。只读 API Key 只保存哈希。文件在 POSIX 系统由 `Save` 以
0600 权限写入；Windows 的访问控制以宿主机 ACL 为准。不要把该文件提交到 Git
或写入日志。

### SQLite 文件

- `proxy.db`：主数据库。
- `proxy.db-wal`、`proxy.db-shm`：SQLite WAL 模式的临时伴随文件。服务运行
  时可能存在，备份前应先停止服务或使用 SQLite 一致性备份流程。

### `subscriptions/`

WebUI 上传的本地订阅内容写入数据目录下的 `subscriptions/`，数据库只保存
文件路径和订阅元数据。不要把订阅 URL、请求头或文件内容写入日志、测试 fixture
或 Git。

## SQLite schema

以下列清单由 `storage.New` 初始化并由 migrations 补齐；启动迁移会检查缺失列，
不会要求人工执行 ALTER。旧的 `source_status` 表会在初始化时删除。应用不使用
独立 `schema_version` 表，迁移顺序以源码中的幂等检查为准。

### `proxies`

`id`、`address`、`protocol`、`region`、`region_source`、`note`、
`exit_ip`、`exit_location`、`latency`、`quality_grade`、`use_count`、
`success_count`、`fail_count`、`last_used`、`last_check`、`created_at`、
`status`、`user_paused`、`source`、`subscription_id`、`ipapiis_score`、
`ipapi_flags`、`ipapi_flags_seen`、`starred`、`cf_blocked`、
`dual_protocol`、`ai_reachability`、`proxy_username`、`proxy_password`、
`node_key`

说明：

- `source` 为 `manual` 或 `subscription`；`subscription_id=0` 表示手动节点。
- `dual_protocol=1` 表示 sing-box mixed 入站同时支持 HTTP 与 SOCKS5；它不是
  对 `protocol` 字段的替换。
- `ipapiis_score=-1`、`cf_blocked=-1` 和空的 `ai_reachability` 表示尚未
  得到对应探测结果。
- `proxy_username`、`proxy_password` 是上游认证凭据，只用于拨号，不得记录
  到日志；`node_key` 是稳定节点身份，临时 mixed 端口变化不应改变它。

### `subscriptions`

`id`、`name`、`url`、`file_path`、`format`、`refresh_min`、
`last_fetch`、`status`、`proxy_count`、`created_at`、`contributed`、
`last_success`、`headers`

`headers` 保存经校验的自定义请求头 JSON；`last_success` 只在本次刷新得到
可用节点时推进。订阅 URL、文件路径和请求头均属于敏感配置，备份包应按部署的
访问控制保护。

## 备份与恢复

备份必须在服务停止后进行，以便把 WAL 内容合并到主库或一并保留：

```powershell
$ErrorActionPreference = 'Stop'
docker compose stop geoproxy
Compress-Archive -Path .\data\* -DestinationPath .\geoproxy-backup.zip
docker compose start geoproxy
```

如果设置了 `HOST_DATA_DIR`，将示例中的 `.\data` 替换为实际宿主机目录。恢复
前停止并关闭 Compose，再把备份解压回同一目录，确认文件权限后启动：

```powershell
$ErrorActionPreference = 'Stop'
docker compose down
Expand-Archive -Path .\geoproxy-backup.zip -DestinationPath .\data -Force
docker compose up -d
```

恢复到不同服务器时，先核对 `HOST_DATA_DIR`、容器 `/app/data` 挂载和读写权限。
不要把旧 `config.json` 与新目录中的文件混合覆盖；需要迁移时整体复制并保留原
目录备份。

## 排障与重置

- 日志提示未设置 `DATA_DIR` 且发现 CWD 旧文件：先设置 `DATA_DIR` 指向旧目录，
  或停服后人工迁移；不要反复重启生成新目录。
- 日志提示无法创建数据目录：检查路径是否为普通文件、父目录权限和磁盘空间。
- 仅重置配置：停服后备份并删除目标数据目录中的 `config.json`，下次启动会重新
  生成凭据；`proxy.db` 不会因此删除。
- 重置节点库：停服后备份并删除 `proxy.db` 及其 WAL/SHM 文件；下次启动会按
  当前 schema 重建空库。
- 完全重置会同时删除配置、数据库和 `subscriptions/`；这是破坏性操作，必须先
  确认备份可读。

运行时文件、数据库、订阅内容和凭据均不应提交到 Git。变更数据目录策略时，必须
同步更新 Compose、`.env.example`、本页和相关测试合同。
