# 地理过滤配置指南

GoProxy 支持通过国家代码过滤代理的出口位置，让你可以灵活控制代理池的地理分布。

## 🌍 配置方式

### 环境变量配置

通过 `BLOCKED_COUNTRIES` 环境变量设置需要屏蔽的国家代码：

```bash
# 默认：屏蔽中国大陆（CN）
BLOCKED_COUNTRIES=CN

# 屏蔽多个国家（逗号分隔）
BLOCKED_COUNTRIES=CN,RU,KP,IR

# 不屏蔽任何国家（留空）
BLOCKED_COUNTRIES=
```

### Docker Compose 配置

编辑 `.env` 文件：

```bash
# 屏蔽中国大陆和俄罗斯
BLOCKED_COUNTRIES=CN,RU
```

启动服务：
```bash
docker compose up -d
```

### Docker Run 配置

```bash
docker run -d --name proxygo \
  -p 127.0.0.1:7776:7776 -p 127.0.0.1:7777:7777 -p 7778:7778 \
  -e BLOCKED_COUNTRIES=CN,RU \
  -e WEBUI_PASSWORD=your_password \
  -v "$(pwd)/data:/app/data" \
  ghcr.io/isboyjc/goproxy:latest
```

### 本地运行配置

```bash
export BLOCKED_COUNTRIES=CN,RU,KP
go run .
```

## 🗺️ 工作机制

### 双重过滤

地理过滤在两个阶段生效：

**1. 启动清理阶段**
- 程序启动时自动扫描数据库
- 删除所有屏蔽国家出口的代理
- 日志输出：`🧹 已清理 X 个屏蔽国家出口代理 (屏蔽: [CN RU])`

**2. 验证阶段**
- 新抓取的代理在验证时检查出口位置
- 如果出口国家在屏蔽列表中，直接拒绝入池
- 不会占用池子容量

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

匹配规则：`exit_location LIKE 'CC %'`（国家代码 + 空格 + 城市）

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

### 场景 3：仅使用欧美代理

```bash
# 屏蔽亚洲、非洲、中东等地区（示例，需根据实际需求调整）
BLOCKED_COUNTRIES=CN,IN,TH,VN,ID,PH,BD,PK,IR,IQ,SA,EG,NG,ZA
```

### 场景 4：不做地理限制

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
3. **重启生效**：修改配置后需要重启服务
4. **已有代理清理**：启动时会清理数据库中的屏蔽国家代理
5. **香港独立识别**：
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
2. **生产环境**：根据业务合规要求设置屏蔽国家
3. **测试环境**：可以设置为空（`BLOCKED_COUNTRIES=`）以获取更多代理
4. **定期调整**：根据代理质量和可用性调整屏蔽列表
5. **配合筛选**：利用 WebUI 的国家筛选器查看各国代理分布，辅助决策
