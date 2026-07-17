# Data 目录说明

GeoProxy 的所有运行时数据和配置存储在数据目录中。

**默认配置**（`docker-compose.yml`）：使用 bind mount，将宿主机 `${HOST_DATA_DIR:-./data}` 挂载到容器 `/app/data`。Named Volume 仅在你改写 compose 时可选。

## 📁 目录内容

### 1. SQLite 数据库文件

**`proxy.db`** - 主数据库文件（约 1-10MB，取决于池子大小）

包含两张表：

**`proxies` 表**：上游节点数据（手动节点 + 订阅节点）
- 节点地址、协议类型
- 地域（region）、地域来源（region_source: manual/auto）、备注（note）
- 出口 IP、地理位置
- 延迟、质量等级
- 使用统计（使用次数、成功次数、失败次数）
- 状态信息（active/degraded/disabled）
- 来源（source: manual/subscription）、所属订阅 ID
- 时间戳（最后使用、最后检查、创建时间）

**`subscriptions` 表**：订阅信息
- 名称、URL、本地文件路径、格式
- 刷新间隔、最后拉取时间、最后成功时间
- 状态（active/paused）、节点数

> 注：旧版 `source_status`（公共源断路器）表已在本网关模型中废弃，启动时会自动 `DROP`。

### 2. SQLite WAL 模式文件

**`proxy.db-shm`** - 共享内存文件（临时）
- WAL 模式的索引文件
- 进程运行时存在，停止后可能自动删除

**`proxy.db-wal`** - 预写日志文件（临时）
- 存储未提交的数据库事务
- checkpoint 后会合并到主数据库
- 提高并发性能

### 3. 配置文件

**`config.json`** - 运行时配置（通过 WebUI 修改的配置）

包含：
- 端口配置（http_port、socks5_port、webui_port）
- 代理认证（proxy_auth_enabled、proxy_auth_username、proxy_auth_password）
- 会话与地域（session_ttl_minutes、default_region）
- 健康检查间隔（health_check_interval）、最大重试（max_retry）
- sing-box 路径（singbox_path）
- 地域过滤（allowed_countries、blocked_countries）

> 💡 **注意**：`config.json` 是运行时生成的，首次启动时不存在，使用默认配置。通过 WebUI 修改配置后会自动保存到此文件。

## 📍 数据位置

### Dokploy / 生产部署（Named Volume）

**卷名称**：`geoproxy-data`

**实际位置**（Linux）：
```bash
/var/lib/docker/volumes/geoproxy-data/_data/
```

**查看数据**：
```bash
# 进入运行中的容器
docker exec -it geoproxy sh

# 查看数据目录
ls -lh /app/data/

# 查看数据库
sqlite3 /app/data/proxy.db "SELECT COUNT(*) FROM proxies;"
```

**备份数据**：
```bash
# 从运行中的 compose 服务导出数据目录
docker compose exec -T geoproxy tar czf - -C /app/data . > geoproxy-backup-$(date +%Y%m%d).tar.gz
```

**恢复数据**：
```bash
# 停止服务
docker compose down

# 恢复备份到 compose 管理的数据卷
docker compose run --rm --no-deps --entrypoint sh geoproxy -c "cd /app/data && tar xzf -" < geoproxy-backup-20260328.tar.gz

# 重启服务
docker compose up -d
```

### 本地开发（可选相对路径挂载）

默认 `docker-compose.yml` 使用 Docker named volume。若你手动把 compose 的 `volumes` 改成 `./data:/app/data`，数据会保存在项目目录的 `./data` 文件夹中。

**查看数据**：
```bash
# 直接查看宿主机目录
ls -lh data/

# 查看数据库
sqlite3 data/proxy.db "SELECT COUNT(*) FROM proxies;"
```

**备份数据**：
```bash
# 简单打包
tar czf geoproxy-backup-$(date +%Y%m%d).tar.gz data/

# 或仅备份数据库
cp data/proxy.db backups/proxy-$(date +%Y%m%d).db
```

## 🐳 Docker 挂载配置

### 使用 Named Volume（推荐）

在 `docker-compose.yml` 中：

```yaml
volumes:
  - geoproxy-data:/app/data  # Named Volume

volumes:
  geoproxy-data:              # 定义卷
```

**优势**：
- ✅ 数据持久化，容器删除不丢失
- ✅ Docker 自动管理，无需关心具体路径
- ✅ 支持备份和迁移
- ✅ 适合生产环境和自动化部署

### 为什么需要持久化？

**持久化数据**：
- 容器重启/更新后手动节点与订阅节点数据不丢失
- 配置修改持久保存
- 订阅的最后成功刷新时间持续跟踪

**不挂载的后果**：
- ❌ 每次重启都需要重新拉取并验证订阅节点（耗时、耗资源）
- ❌ WebUI 的配置修改会丢失
- ❌ 手动录入的节点与订阅记录丢失

### Docker Compose 配置

```yaml
volumes:
  - ${DATA_DIR:-./data}:/app/data
```

**说明**：
- 宿主机目录：`./data`（docker-compose.yml 所在目录下）
- 容器内路径：`/app/data`
- 首次启动会自动创建 `data/` 目录

## 📊 数据大小

根据池子配置，预计文件大小：

| 池子大小 | proxy.db | proxy.db-wal | config.json | 总计 |
|---------|----------|--------------|-------------|------|
| 50 个代理 | ~1 MB | ~200 KB | ~500 B | ~1.2 MB |
| 100 个代理 | ~2 MB | ~400 KB | ~500 B | ~2.4 MB |
| 200 个代理 | ~4 MB | ~800 KB | ~500 B | ~4.8 MB |
| 500 个代理 | ~10 MB | ~2 MB | ~500 B | ~12 MB |

> 💡 **注意**：WAL 文件大小会动态变化，checkpoint 后会重置。

## 🔍 查看数据

### 查看数据库内容

```bash
# 进入 data 目录
cd data/

# 查看所有代理
sqlite3 proxy.db "SELECT address, protocol, exit_location, latency, quality_grade FROM proxies LIMIT 10;"

# 查看质量分布
sqlite3 proxy.db "SELECT quality_grade, COUNT(*) FROM proxies GROUP BY quality_grade;"

# 查看地域分布
sqlite3 proxy.db "SELECT region, COUNT(*) FROM proxies WHERE region != '' GROUP BY region ORDER BY COUNT(*) DESC;"

# 查看订阅状态
sqlite3 proxy.db "SELECT name, status, proxy_count, last_success FROM subscriptions;"
```

### 查看配置文件

```bash
cat data/config.json | jq .
```

如果还没有通过 WebUI 修改配置，这个文件可能不存在（使用默认配置）。

## 🧹 清理数据

### 重置代理池（保留配置）

```bash
# 仅清空代理表
sqlite3 data/proxy.db "DELETE FROM proxies;"

# 或删除整个数据库（下次启动重新创建）
rm -f data/proxy.db*
```

### 完全重置（包括配置）

```bash
# 删除整个 data 目录
rm -rf data/

# 下次启动会自动创建并使用默认配置
```

### Docker 容器重置

```bash
# 停止并删除容器
docker compose down

# 删除数据卷
rm -rf data/

# 重新启动（全新环境）
docker compose up -d
```

## 🔒 备份数据

### 手动备份

```bash
# 备份整个 data 目录
tar -czf geoproxy-backup-$(date +%Y%m%d).tar.gz data/

# 或仅备份数据库
cp data/proxy.db backups/proxy-$(date +%Y%m%d).db
```

### 定时备份脚本

```bash
#!/bin/bash
# backup.sh

BACKUP_DIR="backups"
DATE=$(date +%Y%m%d-%H%M%S)

mkdir -p $BACKUP_DIR
cp data/proxy.db "$BACKUP_DIR/proxy-$DATE.db"

# 保留最近 7 天的备份
find $BACKUP_DIR -name "proxy-*.db" -mtime +7 -delete

echo "✅ 备份完成: $BACKUP_DIR/proxy-$DATE.db"
```

### 恢复数据

```bash
# 停止服务
docker compose down

# 恢复数据库
cp backups/proxy-20260328.db data/proxy.db

# 重启服务
docker compose up -d
```

## ⚠️ 注意事项

1. **WAL 文件**：停止服务前会自动 checkpoint，无需手动处理
2. **并发安全**：多个容器不要共享同一个 data 目录（SQLite 不支持多进程写入）
3. **权限问题**：确保容器有权限读写 data 目录
4. **备份时机**：建议在服务停止时备份，避免数据不一致
5. **迁移数据**：直接复制整个 data 目录到新服务器即可

