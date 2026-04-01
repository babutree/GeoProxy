# 地理过滤配置指南

GoProxy 支持通过国家代码过滤代理的出口位置，让你可以灵活控制代理池的地理分布。支持黑名单（屏蔽指定国家）和白名单（仅允许指定国家）两种模式。

## 🌍 配置方式

### 过滤模式

GoProxy 提供两种互斥的过滤模式：

| 模式 | 环境变量 | 说明 |
|------|---------|------|
| 黑名单 | `BLOCKED_COUNTRIES` | 屏蔽指定国家，其余放行（默认 `CN`） |
| 白名单 | `ALLOWED_COUNTRIES` | 仅允许指定国家，其余拒绝 |

> **优先级**：白名单非空时生效，黑名单被忽略。白名单为空时黑名单生效。

### 环境变量配置

```bash
# === 黑名单模式（默认） ===

# 默认：屏蔽中国大陆（CN）
BLOCKED_COUNTRIES=CN

# 屏蔽多个国家（逗号分隔）
BLOCKED_COUNTRIES=CN,RU,KP,IR

# 不屏蔽任何国家（留空）
BLOCKED_COUNTRIES=

# === 白名单模式 ===

# 仅允许美国、日本、韩国、新加坡的代理入池
ALLOWED_COUNTRIES=US,JP,KR,SG

# 仅允许欧美代理
ALLOWED_COUNTRIES=US,CA,GB,DE,FR,NL,SE
```

### WebUI 动态配置

管理员登录 WebUI 后，在配置面板的「地理过滤」区域可以动态修改黑名单和白名单，保存后立即生效，无需重启。

> **配置优先级**：WebUI 保存（config.json）> 环境变量 > 默认值。首次启动时环境变量生效，一旦通过 WebUI 保存过，后续以 config.json 为准。

### Docker Compose 配置

编辑 `.env` 文件：

```bash
# 黑名单模式：屏蔽中国大陆和俄罗斯
BLOCKED_COUNTRIES=CN,RU

# 或白名单模式：仅允许美日韩新
ALLOWED_COUNTRIES=US,JP,KR,SG
```

启动服务：
```bash
docker compose up -d
```

### Docker Run 配置

```bash
# 黑名单模式
docker run -d --name proxygo \
  -p 127.0.0.1:7776:7776 -p 127.0.0.1:7777:7777 -p 7778:7778 \
  -e BLOCKED_COUNTRIES=CN,RU \
  -e WEBUI_PASSWORD=your_password \
  -v "$(pwd)/data:/app/data" \
  ghcr.io/isboyjc/goproxy:latest

# 白名单模式
docker run -d --name proxygo \
  -p 127.0.0.1:7776:7776 -p 127.0.0.1:7777:7777 -p 7778:7778 \
  -e ALLOWED_COUNTRIES=US,JP,KR,SG \
  -e WEBUI_PASSWORD=your_password \
  -v "$(pwd)/data:/app/data" \
  ghcr.io/isboyjc/goproxy:latest
```

### 本地运行配置

```bash
# 黑名单模式
export BLOCKED_COUNTRIES=CN,RU,KP
go run .

# 白名单模式
export ALLOWED_COUNTRIES=US,JP,KR,SG
go run .
```

## 🗺️ 工作机制

### 过滤逻辑

```
代理验证时：
  if 白名单非空:
      出口国家在白名单中 → 放行
      出口国家不在白名单中 → 拒绝
  else if 黑名单非空:
      出口国家在黑名单中 → 拒绝
      出口国家不在黑名单中 → 放行
  else:
      全部放行
```

### 双重过滤

地理过滤在两个阶段生效：

**1. 启动清理阶段**
- 程序启动时自动扫描数据库
- 白名单模式：删除所有不在白名单中的代理
- 黑名单模式：删除所有屏蔽国家出口的代理
- 日志输出示例：
  - `🧹 已清理 X 个非白名单国家出口代理 (允许: [US JP KR])`
  - `🧹 已清理 X 个屏蔽国家出口代理 (屏蔽: [CN RU])`

**2. 验证阶段**
- 新抓取的代理在验证时检查出口位置
- 根据当前过滤模式决定是否允许入池
- 不符合条件的代理不会占用池子容量

**3. 运行时更新**
- 通过 WebUI 修改过滤配置后立即生效
- 已在池中的代理会在下一轮健康检查时自然淘汰

### 国家代码识别

系统使用 **ISO 3166-1 alpha-2** 标准的两位国家代码：

```
出口位置格式：CC City
示例：
  CN Beijing      → 国家代码 CN（中国大陆）
  HK Hong Kong    → 国家代码 HK（香港）
  US New York     → 国家代码 US（美国）
  RU Moscow       → 国家代码 RU（俄罗斯）
```

匹配规则：提取 `exit_location` 前两个字符作为国家代码进行匹配

## 📋 常用国家代码

### 亚洲
| 代码 | 国家/地区 | 代码 | 国家/地区 |
|------|----------|------|----------|
| `CN` | 中国大陆 | `HK` | 香港 |
| `TW` | 台湾 | `MO` | 澳门 |
| `JP` | 日本 | `KR` | 韩国 |
| `SG` | 新加坡 | `IN` | 印度 |
| `TH` | 泰国 | `VN` | 越南 |
| `KP` | 朝鲜 | `IR` | 伊朗 |

### 欧洲
| 代码 | 国家 | 代码 | 国家 |
|------|------|------|------|
| `RU` | 俄罗斯 | `GB` | 英国 |
| `DE` | 德国 | `FR` | 法国 |
| `NL` | 荷兰 | `SE` | 瑞典 |
| `UA` | 乌克兰 | `PL` | 波兰 |

### 美洲
| 代码 | 国家 | 代码 | 国家 |
|------|------|------|------|
| `US` | 美国 | `CA` | 加拿大 |
| `BR` | 巴西 | `MX` | 墨西哥 |
| `AR` | 阿根廷 | `CL` | 智利 |

完整国家代码列表：[ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2)

## 🎯 使用场景

### 场景 1：屏蔽中国大陆（默认）

```bash
BLOCKED_COUNTRIES=CN
```

**适用**：
- 需要海外 IP 代理
- 避免被识别为中国大陆流量
- 保留香港、澳门、台湾代理

### 场景 2：屏蔽多个敏感地区

```bash
BLOCKED_COUNTRIES=CN,RU,KP,IR,SY
```

**适用**：
- 合规要求（避免某些国家的 IP）
- 地缘政治考虑
- 防止特定地区的代理质量问题

### 场景 3：仅使用欧美代理（白名单模式）

```bash
ALLOWED_COUNTRIES=US,CA,GB,DE,FR,NL,SE
```

**适用**：
- 需要精确控制代理来源国家
- 只需要特定地区的 IP
- 比黑名单排除大量国家更简洁

### 场景 4：仅使用亚太代理（白名单模式）

```bash
ALLOWED_COUNTRIES=JP,KR,SG,HK,TW
```

**适用**：
- 需要亚太地区低延迟代理
- 针对亚太区域的业务场景

### 场景 5：不做地理限制

```bash
BLOCKED_COUNTRIES=
```

**适用**：
- 需要最大化代理池容量
- 对地理位置无特殊要求
- 测试和开发环境

## 📊 实时查看

### 查看当前屏蔽配置

启动日志会显示：
```
[main] 🧹 已清理 15 个屏蔽国家出口代理 (屏蔽: [CN RU KP])
```

### 查看池中国家分布

通过 WebUI 的**出口国家筛选器**可以看到当前池中所有国家的代理分布。

### 数据库查询

```bash
# 查看所有代理的国家分布
sqlite3 data/proxy.db "
  SELECT SUBSTR(exit_location, 1, 2) AS country, COUNT(*) AS count 
  FROM proxies 
  GROUP BY country 
  ORDER BY count DESC;
"

# 查看特定国家的代理
sqlite3 data/proxy.db "
  SELECT address, exit_ip, exit_location, latency 
  FROM proxies 
  WHERE exit_location LIKE 'US %';
"
```

## ⚠️ 注意事项

1. **大小写不敏感**：国家代码会自动转为大写（`cn` → `CN`）
2. **空格自动处理**：前后空格会自动去除
3. **白名单优先**：白名单非空时黑名单被忽略
4. **运行时可调**：通过 WebUI 修改后立即生效，无需重启
5. **已有代理处理**：配置变更后，已入池代理在下一轮健康检查时自然淘汰
6. **持久化**：通过 WebUI 保存的配置写入 config.json，重启后优先于环境变量
7. **香港独立识别**：
   - 中国大陆代码：`CN`
   - 香港代码：`HK`（独立的国家代码）
   - 设置 `BLOCKED_COUNTRIES=CN` 不会影响香港代理

## 🧪 测试验证

### 测试 1：屏蔽中国大陆

```bash
# 启动服务
export BLOCKED_COUNTRIES=CN
go run .

# 查看日志（应该显示清理信息）
# [main] 🧹 已清理 X 个屏蔽国家出口代理 (屏蔽: [CN])

# 查看 WebUI 的代理列表（不应该有 CN 开头的出口位置）
```

### 测试 2：屏蔽多个国家

```bash
export BLOCKED_COUNTRIES=CN,RU,KP
go run .

# 使用测试脚本验证
./test/test_proxy.sh

# 观察输出的国旗 emoji（不应该有 🇨🇳 🇷🇺 🇰🇵）
```

### 测试 3：不屏蔽任何国家

```bash
export BLOCKED_COUNTRIES=
go run .

# 查看日志（不应该有清理信息）
# 代理列表中可能出现各种国家的代理
```

## 💡 最佳实践

1. **默认配置**：保持默认 `BLOCKED_COUNTRIES=CN`，适合大多数场景
2. **精确控制**：需要特定国家代理时，使用白名单模式（`ALLOWED_COUNTRIES`）比排除大量国家更简洁
3. **生产环境**：根据业务合规要求设置过滤规则
4. **测试环境**：可以两个都留空以获取更多代理
5. **动态调整**：通过 WebUI 实时调整过滤规则，观察效果后再决定最终配置
6. **配合筛选**：利用 WebUI 的国家筛选器查看各国代理分布，辅助决策
