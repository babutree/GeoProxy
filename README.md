# GoProxy

> **智能代理池系统** — 基于 Go 的轻量级、低资源消耗、自适应的代理池服务

[![Docker Hub](https://img.shields.io/docker/v/isboyjc/goproxy?label=Docker%20Hub&logo=docker)](https://hub.docker.com/r/isboyjc/goproxy)
[![GitHub Container Registry](https://img.shields.io/badge/GHCR-latest-blue?logo=github)](https://github.com/isboyjc/GoProxy/pkgs/container/goproxy)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)

GoProxy 从多个公开代理源自动抓取 HTTP/SOCKS5 代理，通过严格验证（出口 IP + 位置 + 延迟）后加入智能代理池，对外提供统一的 HTTP 代理服务。系统采用质量分级、智能补充、自动优化等机制，确保代理池始终保持高质量和稳定性。

**GitHub**：[github.com/isboyjc/GoProxy](https://github.com/isboyjc/GoProxy)

![](https://cdn.isboyjc.com/img/Xnip2026-03-29_03-35-06.png)

## 📑 目录

- [核心特性](#-核心特性)
- [项目结构](#-项目结构)
- [快速开始](#-快速开始)
- [Docker 部署](#-docker-部署) - 推荐使用 docker-compose
- [配置说明](#️-配置说明) - 环境变量、池子参数、认证、地理过滤
- [WebUI 使用指南](#-webui-使用指南) - 双角色权限、操作功能
- [核心架构](#️-核心架构) - 智能池子、状态转换、质量分级
- [数据库 Schema](#-数据库-schema)
- [代理源](#-代理源)
- [测试代理服务](#-测试代理服务)
- [常见问题](#-常见问题)
- [致谢与声明](#-致谢与声明)

**扩展文档**：
- [Docker 镜像发布配置](.github/DOCKER_SETUP.md) - GitHub Actions CI/CD 设置
- [地理过滤配置指南](GEO_FILTER.md) - 国家代码、使用场景、测试方法
- [认证功能测试](test/AUTH_TEST.md) - 代理认证测试和多语言客户端示例
- [基础功能测试](test/TEST_GUIDE.md) - 测试脚本使用指南
- [架构设计文档](POOL_DESIGN.md) - 完整的系统设计和实现细节

## ✨ 核心特性

### 🎯 智能池子机制
- **固定容量管理**：可配置池子大小和 HTTP/SOCKS5 协议比例
- **质量分级**：S/A/B/C 四级评分（基于延迟），智能选择高质量代理
- **动态状态感知**：Healthy → Warning → Critical → Emergency 四级状态自适应
- **严格准入标准**：必须通过出口 IP、地理位置、延迟三重验证才可入池
- **智能替换**：新代理必须显著优于现有代理（默认快 30%）才触发替换

### 🚀 按需抓取
- **源分组策略**：快更新源（5-30min）用于紧急补充，慢更新源（每天）用于优化轮换
- **断路器保护**：连续失败的源自动降级/禁用，冷却后恢复
- **多模式抓取**：
  - **Emergency**：单协议缺失或池子 <10%，使用所有可用源
  - **Refill**：池子 <80%，使用快更新源
  - **Optimize**：池子健康时，随机抽取少量慢源优化替换

### 🏥 分层健康管理
- **轻量批次检查**：每次仅检查 20 个代理，避免资源浪费
- **智能跳过 S 级**：池子健康时跳过 S 级代理检查
- **定时优化轮换**：健康状态下，定期抓取优质代理替换池中延迟高的

### 🔄 智能重试机制
- **自动故障切换**：代理请求失败时立即切换到另一个代理重试（最多 3 次）
- **失败即删除**：连接失败或请求超时的代理立即从池子中移除
- **用户无感知**：自动重试在服务端完成，用户只会收到成功响应或最终失败提示
- **防重复尝试**：已尝试过的失败代理不会在同一请求中再次使用

### 🚪 双端口策略
- **7777 端口（随机轮换）**：每次请求随机选择代理，IP 高度分散，适合爬虫和数据采集
- **7776 端口（最低延迟）**：固定使用延迟最低的代理，除非失败才切换，适合长连接和流媒体
- **自动切换**：两个端口都支持失败自动重试，7776 失败后切换到次优代理
- **共享池子**：两个端口使用同一个代理池，统一管理和优化
- **可选认证**：支持 Basic Auth 认证保护，对外开放时可启用

### 🎨 黑客风格 WebUI
- **Matrix 美学**：荧光绿 + 纯黑背景，CRT 扫描线效果，JetBrains Mono 等宽字体
- **双角色权限**：访客模式（只读）+ 管理员模式（完全控制），可安全公网开放
- **实时仪表盘**：池子状态、质量分布可视化、协议统计，带荧光光晕效果
- **完整配置界面**：池子容量、延迟标准、验证参数、优化策略均可在线调整（管理员）
- **代理注册表**：详细展示地址、出口 IP、位置、延迟、质量等级、使用统计
- **中英文切换**：支持中文/英文界面切换，默认中文
- **交互优化**：点击地址复制、单个代理刷新、实时日志倒计时

### 📊 适用场景
- **小型 VPS**：低资源消耗（固定池子 + 按需抓取 + 限流查询）
- **稳定需求**：自动剔除失败代理，始终保持健康池子
- **质量优先**：S/A 级代理优先使用，自动优化延迟

## 📦 项目结构

```text
.
├── main.go                        # 程序入口，协调所有模块
├── config/                        # 配置系统（池子容量、延迟标准、验证参数等）
├── pool/                          # 🆕 池子管理器（入池判断、替换逻辑、状态计算）
├── fetcher/                       # 🆕 智能抓取器（源分组、断路器、按需抓取）
│   ├── fetcher.go                 # 多模式抓取逻辑
│   ├── source_manager.go          # 源状态管理和断路器
│   └── ip_query.go                # IP查询限流和多源降级
├── validator/                     # 代理验证（连接测试 + 出口IP检测 + 地理过滤）
├── checker/                       # 🆕 分批健康检查器
├── optimizer/                     # 🆕 优化轮换器（定时优化池子质量）
├── storage/                       # 🆕 扩展存储层（质量等级、使用统计、源状态表）
├── proxy/                         # 🆕 对外 HTTP 代理服务（双端口 + 可选认证）
├── webui/                         # 🆕 黑客风格 WebUI（健康仪表盘、配置界面、RBAC）
├── logger/                        # 内存日志收集
├── test/                          # 🧪 测试脚本与文档
│   ├── test_proxy.sh              # Bash 测试脚本
│   ├── test_proxy.go              # Go 测试脚本
│   ├── test_proxy.py              # Python 测试脚本
│   ├── TEST_GUIDE.md              # 基础功能测试指南
│   └── AUTH_TEST.md               # 🆕 认证功能测试指南
├── .github/                       # 🆕 GitHub Actions CI/CD
│   ├── workflows/docker-image.yml # 自动构建多架构镜像
│   └── DOCKER_SETUP.md            # Docker 镜像发布配置指南
├── .env.example                   # 🆕 环境变量配置模板
├── docker-compose.yml             # 🆕 Docker Compose 配置（自动更新镜像）
├── Dockerfile                     # Docker 构建文件
├── GEO_FILTER.md                  # 🆕 地理过滤配置指南
├── POOL_DESIGN.md                 # 完整架构设计文档
└── README.md                      # 本文件
```

## 🚀 快速开始

### 运行要求
- Go `1.25`
- CGO 编译环境（依赖 `github.com/mattn/go-sqlite3`）

### 本地运行

```bash
go run .
```

或先编译再启动：

```bash
go build -o proxygo .
./proxygo
```

程序启动后会：
1. 加载配置（环境变量 + `config.json`）
2. 初始化数据库和限流器
3. 清理不符合条件的代理（屏蔽国家出口、无地理信息）
4. 启动 WebUI（`:7778`）
5. 立即执行智能填充（按需抓取 + 严格验证）
6. 启动后台协程：
   - 状态监控（每 30 秒）
   - 健康检查（默认 5 分钟）
   - 优化轮换（默认 30 分钟）
7. 启动两个代理服务（支持可选认证）：
   - `:7776` - 最低延迟模式（稳定连接）
   - `:7777` - 随机轮换模式（IP 多样性）

### 默认端口
- **代理服务（随机轮换）**：`127.0.0.1:7777` - 每次请求随机选择代理，IP 多样性高
- **代理服务（最低延迟）**：`127.0.0.1:7776` - 固定使用延迟最低的代理，性能优先
- **WebUI**：`http://127.0.0.1:7778`
- **默认密码**：`goproxy`（可通过 `WEBUI_PASSWORD` 环境变量自定义）

### 使用代理

GoProxy 提供**两个代理端口**，满足不同场景需求：

#### 🎲 7777 端口 - 随机轮换模式

适合需要 **IP 多样性** 的场景（爬虫、数据采集、负载均衡）：

```bash
curl -x http://127.0.0.1:7777 https://httpbin.org/ip
```

**特点**：
- 每次请求随机选择一个代理
- 优先使用高质量（S/A 级）代理
- IP 地址高度分散

#### ⚡ 7776 端口 - 最低延迟模式

适合需要 **稳定连接** 的场景（长连接、流媒体、实时通信）：

```bash
curl -x http://127.0.0.1:7776 https://httpbin.org/ip
```

**特点**：
- 固定使用池中延迟最低的代理
- 除非该代理失败，否则不会切换
- 失败时自动删除并切换到下一个最低延迟代理
- 性能和稳定性最优

#### 环境变量配置

**本地使用（无认证）**：
```bash
# 使用随机模式
export http_proxy=http://127.0.0.1:7777
export https_proxy=http://127.0.0.1:7777

# 或使用稳定模式
export http_proxy=http://127.0.0.1:7776
export https_proxy=http://127.0.0.1:7776
```

**远程使用（带认证）**：
```bash
# 格式：http://username:password@host:port
export http_proxy=http://proxy:your_password@192.168.1.100:7777
export https_proxy=http://proxy:your_password@192.168.1.100:7777
```

#### 端口对比

| 特性 | 7777（随机轮换） | 7776（最低延迟） |
|------|-----------------|-----------------|
| **选择策略** | 随机选择（优先高质量） | 固定使用延迟最低的 |
| **IP 多样性** | ⭐⭐⭐⭐⭐ 高度分散 | ⭐ 基本固定 |
| **连接稳定性** | ⭐⭐⭐ 每次切换 | ⭐⭐⭐⭐⭐ 固定不变 |
| **性能表现** | ⭐⭐⭐⭐ 平均延迟 | ⭐⭐⭐⭐⭐ 最优延迟 |
| **适用场景** | 爬虫、数据采集、防封禁 | 长连接、流媒体、下载 |
| **失败切换** | 自动重试 3 次 | 失败后切换到次优代理 |

#### 自动重试机制说明

当你通过 GoProxy 发送请求时，如果上游代理失败，系统会**自动处理**：

1. **立即删除失败代理**：从池子中移除不可用的代理
2. **自动切换重试**：随机选择另一个可用代理重新发送请求（最多重试 3 次）
3. **用户完全无感知**：整个过程在服务端完成，你的应用只会收到成功响应或最终失败提示
4. **防止重复尝试**：同一请求中不会重复使用已失败的代理

**示例流程**：
```
用户请求 → 代理A失败(删除) → 自动切换代理B → 代理B成功 → 返回响应
```

这意味着即使池子中有部分失效代理，你的应用依然可以正常工作，系统会自动保持池子质量。

## 🐳 Docker 部署

> 💡 **自动构建**：GitHub Actions 自动构建多架构镜像（amd64/arm64），支持 Docker Hub 和 GHCR 双仓库推送。  
> 详细配置说明请查看 [`.github/DOCKER_SETUP.md`](.github/DOCKER_SETUP.md)

### 快速启动（推荐）

使用 docker-compose，支持环境变量配置，自动拉取最新镜像：

```bash
# 1. 复制环境变量模板
cp .env.example .env

# 2. 根据需要编辑 .env（可选，默认配置即可使用）
# vim .env

# 3. 启动服务（自动拉取最新镜像）
docker compose up -d

# 4. 访问 WebUI
# http://localhost:7778（默认密码：goproxy）
```

### 使用 Docker Hub 镜像

```bash
docker pull isboyjc/goproxy:latest
docker run -d --name proxygo \
  -p 127.0.0.1:7776:7776 -p 127.0.0.1:7777:7777 -p 7778:7778 \
  -e WEBUI_PASSWORD=your_password -v "$(pwd)/data:/app/data" \
  isboyjc/goproxy:latest
```

### 使用 GitHub Container Registry 镜像

```bash
docker pull ghcr.io/isboyjc/goproxy:latest
docker run -d --name proxygo \
  -p 127.0.0.1:7776:7776 -p 127.0.0.1:7777:7777 -p 7778:7778 \
  -e WEBUI_PASSWORD=your_password -v "$(pwd)/data:/app/data" \
  ghcr.io/isboyjc/goproxy:latest
```

### 环境变量配置

**核心配置**（`.env` 文件）：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PROXY_HOST` | `127.0.0.1` | 代理服务绑定地址（`0.0.0.0` = 对外开放） |
| `BLOCKED_COUNTRIES` | `CN` | 屏蔽的国家代码（逗号分隔，如 `CN,RU`，留空=不屏蔽） |
| `PROXY_AUTH_ENABLED` | `false` | 是否启用代理认证 |
| `PROXY_AUTH_USERNAME` | `proxy` | 代理认证用户名 |
| `PROXY_AUTH_PASSWORD` | 空 | 代理认证密码 |
| `WEBUI_PASSWORD` | `goproxy` | WebUI 登录密码 |

完整环境变量列表请查看 `.env.example` 文件。

**常用配置示例**：

```bash
# 场景 1：本地使用（默认，最安全）
# 直接运行 docker compose up -d 即可

# 场景 2：对外开放 + 启用认证（推荐）
cat > .env << EOF
PROXY_HOST=0.0.0.0
PROXY_AUTH_ENABLED=true
PROXY_AUTH_USERNAME=myuser
PROXY_AUTH_PASSWORD=secure_pass_123
WEBUI_PASSWORD=admin_pass_456
BLOCKED_COUNTRIES=CN
EOF
docker compose up -d

# 场景 3：屏蔽多个国家
cat > .env << EOF
BLOCKED_COUNTRIES=CN,RU,KP,IR
WEBUI_PASSWORD=admin_pass
EOF
docker compose up -d

# 场景 4：不屏蔽任何国家
cat > .env << EOF
BLOCKED_COUNTRIES=
WEBUI_PASSWORD=admin_pass
EOF
docker compose up -d
```

### 安全部署配置

**默认配置**：代理服务仅本地访问（`127.0.0.1`），WebUI 可对外访问。

**如需对外开放代理服务**，强烈建议启用认证：

| 使用场景 | 配置建议 | 安全级别 |
|---------|---------|---------|
| **个人 VPS + 本地使用** | `PROXY_HOST=127.0.0.1`（默认） | ⭐⭐⭐⭐⭐ 最安全 |
| **团队共享 + 认证** | `PROXY_HOST=0.0.0.0` + 启用认证 | ⭐⭐⭐⭐ 安全 |
| **内网 + 防火墙** | `PROXY_HOST=0.0.0.0` + iptables 白名单 | ⭐⭐⭐ 中等 |

**启用代理认证**（编辑 `.env`）：

```bash
PROXY_HOST=0.0.0.0
PROXY_AUTH_ENABLED=true
PROXY_AUTH_USERNAME=myuser
PROXY_AUTH_PASSWORD=secure_pass_123
```

**客户端使用（带认证）**：

```bash
# 环境变量方式
export http_proxy=http://myuser:secure_pass_123@server-ip:7777
export https_proxy=http://myuser:secure_pass_123@server-ip:7777

# curl 直接指定
curl -x http://myuser:secure_pass_123@server-ip:7777 https://httpbin.org/ip

# Python requests
proxies = {'http': 'http://myuser:secure_pass_123@server-ip:7777', 'https': '...'}
```

> 💡 **认证测试指南**：详细的认证功能测试和多语言客户端示例，请查看 [`test/AUTH_TEST.md`](test/AUTH_TEST.md)

## ⚙️ 配置说明

### 配置文件示例

所有配置均可通过 WebUI 的 **Configure Pool** 界面在线调整，也可以手动编辑 `config.json`：

```json
{
  "pool_max_size": 100,
  "pool_http_ratio": 0.5,
  "pool_min_per_protocol": 10,
  "max_latency_ms": 2000,
  "max_latency_healthy": 1500,
  "max_latency_emergency": 3000,
  "validate_concurrency": 300,
  "validate_timeout": 8,
  "health_check_interval": 5,
  "health_check_batch_size": 20,
  "optimize_interval": 30,
  "replace_threshold": 0.7
}
```

### 配置参数详解

**服务端口配置**

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `proxy_port` | `:7777` | 随机轮换代理端口 |
| `stable_proxy_port` | `:7776` | 最低延迟代理端口 |
| `webui_port` | `:7778` | WebUI 端口 |

**代理认证配置**

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `proxy_auth_enabled` | `false` | 是否启用代理认证（对外开放时建议启用） |
| `proxy_auth_username` | `proxy` | 代理认证用户名 |
| `proxy_auth_password_hash` | 空 | 代理认证密码 SHA256 哈希（通过环境变量 `PROXY_AUTH_PASSWORD` 设置原始密码） |

> 💡 **注意**：代理认证配置通过**环境变量**设置，不在 `config.json` 中。启动时从 `PROXY_AUTH_ENABLED`、`PROXY_AUTH_USERNAME`、`PROXY_AUTH_PASSWORD` 环境变量读取。

**池子容量配置**

| 参数 | 默认值 | 说明 | 推荐范围 |
| --- | --- | --- | --- |
| `pool_max_size` | `100` | 代理池总容量 | 50-500 |
| `pool_http_ratio` | `0.5` | HTTP 协议占比 | 0.3-0.8 |
| `pool_min_per_protocol` | `10` | 每协议最少保证数量 | 5-50 |

**延迟标准配置**

| 参数 | 默认值 | 说明 | 推荐范围 |
| --- | --- | --- | --- |
| `max_latency_ms` | `2000` | 标准模式最大延迟（毫秒） | 1000-3000 |
| `max_latency_healthy` | `1500` | 健康模式严格延迟（毫秒） | 800-2000 |
| `max_latency_emergency` | `3000` | 紧急模式放宽延迟（毫秒） | 2000-5000 |

**验证与健康检查配置**

| 参数 | 默认值 | 说明 | 推荐范围 |
| --- | --- | --- | --- |
| `validate_concurrency` | `300` | 并发验证数量 | 100-500 |
| `validate_timeout` | `8` | 验证超时（秒） | 5-15 |
| `health_check_interval` | `5` | 检查间隔（分钟） | 3-15 |
| `health_check_batch_size` | `20` | 每批检查数量 | 10-50 |

**优化配置**

| 参数 | 默认值 | 说明 | 推荐范围 |
| --- | --- | --- | --- |
| `optimize_interval` | `30` | 优化轮换间隔（分钟） | 15-120 |
| `replace_threshold` | `0.7` | 替换阈值（新代理需快 30%） | 0.5-0.9 |

### 不同场景配置建议

**小型 VPS（1C2G）**
```json
{
  "pool_max_size": 50,
  "pool_http_ratio": 0.5,
  "validate_concurrency": 100,
  "health_check_interval": 10,
  "health_check_batch_size": 10,
  "optimize_interval": 60
}
```

**中型服务器（2C4G+）**
```json
{
  "pool_max_size": 200,
  "pool_http_ratio": 0.6,
  "validate_concurrency": 300,
  "health_check_interval": 5,
  "health_check_batch_size": 30,
  "optimize_interval": 30
}
```

**低延迟优先**
```json
{
  "pool_max_size": 100,
  "max_latency_ms": 1000,
  "max_latency_healthy": 800,
  "optimize_interval": 15,
  "replace_threshold": 0.8
}
```

**高可用优先（需要更多代理）**
```json
{
  "pool_max_size": 300,
  "pool_http_ratio": 0.7,
  "pool_min_per_protocol": 20,
  "max_latency_ms": 3000
}
```

### 固定配置

以下配置在代码中固定或通过环境变量设置，无需在 `config.json` 中调整：

| 配置项 | 值 | 说明 |
| --- | --- | --- |
| `WebUIPort` | `:7778` | WebUI 端口 |
| `ProxyPort` | `:7777` | 随机轮换代理端口 |
| `StableProxyPort` | `:7776` | 最低延迟代理端口 |
| `ValidateURL` | `http://www.gstatic.com/generate_204` | 验证目标地址 |
| `IPQueryRateLimit` | `10 次/秒` | IP 查询限流 |
| `SourceFailThreshold` | `3` | 源降级阈值（连续失败） |
| `SourceDisableThreshold` | `5` | 源禁用阈值（连续失败） |
| `SourceCooldownMinutes` | `30` | 源禁用冷却时间 |
| `MaxRetry` | `3` | 代理请求失败重试次数 |

**环境变量配置**（启动时读取）：

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PROXY_AUTH_ENABLED` | `false` | 是否启用代理认证 |
| `PROXY_AUTH_USERNAME` | `proxy` | 代理认证用户名 |
| `PROXY_AUTH_PASSWORD` | 空 | 代理认证密码（原始密码，自动哈希） |
| `BLOCKED_COUNTRIES` | `CN` | 屏蔽的国家代码（逗号分隔，如 `CN,RU,KP`，留空=不屏蔽） |

## 🎨 WebUI 使用指南

访问地址：`http://127.0.0.1:7778`

### 👥 双角色权限系统

GoProxy WebUI 支持**访客模式**和**管理员模式**：

#### 访客模式（只读）

**无需登录**即可访问，可以查看所有数据但不能操作：

- ✅ 查看池子状态和质量分布
- ✅ 查看代理列表和详细信息
- ✅ 查看系统日志
- ✅ 点击复制代理地址
- ❌ 不能抓取代理
- ❌ 不能刷新延迟
- ❌ 不能删除代理
- ❌ 不能修改配置

**适用场景**：
- 团队成员监控代理池状态
- 展示给客户或第三方查看
- 公网开放访问（只读数据安全）

#### ⚡ 管理员模式（完全控制）

**登录后**拥有所有操作权限：

- ✅ 所有访客模式的查看功能
- ✅ 手动触发代理抓取
- ✅ 刷新所有代理延迟
- ✅ 刷新单个代理信息
- ✅ 删除指定代理
- ✅ 修改池子配置（容量、延迟标准、检查间隔等）

**默认密码**：`goproxy`（通过环境变量 `WEBUI_PASSWORD` 自定义）

### 健康仪表盘

**四宫格指标卡**
- **Pool Status**：当前池子状态（HEALTHY/WARNING/CRITICAL/EMERGENCY）
- **Total Proxies**：总代理数 / 池子容量
- **HTTP**：HTTP 代理数 / HTTP 槽位数 + 平均延迟
- **SOCKS5**：SOCKS5 代理数 / SOCKS5 槽位数 + 平均延迟

**质量分布可视化**
- 横向条形图展示 S/A/B/C 四级质量分布
- 实时显示各级别代理数量

### 代理注册表

**表格字段**
- **Grade**：质量等级（S/A/B/C，基于延迟计算）
- **Protocol**：协议类型（HTTP/SOCKS5）
- **Address**：代理地址（host:port），点击可复制
- **Exit IP**：代理的出口 IP 地址
- **Location**：出口地理位置（国旗 emoji + 国家代码 + 城市）
- **Latency**：连接延迟（毫秒，动态颜色编码：绿/黄/橙/红）
- **Usage**：使用统计（使用总次数 / 成功次数，成功率指标）
- **Action**：操作按钮（刷新单个代理、删除代理，管理员可见）

**操作功能**

**所有用户可用**：
- **协议筛选**：下拉选择协议类型（全部/HTTP/SOCKS5）
- **国家筛选**：下拉选择出口国家（全部/动态国家列表，带国旗 emoji）
- **点击复制地址**：点击代理地址单元格直接复制到剪贴板
- **查看数据**：池子状态、质量分布、系统日志

**管理员专属**（需登录）：
- **Fetch Proxies**：手动触发智能抓取
- **Refresh Latency**：重新验证所有代理并更新延迟
- **刷新单个代理**：点击行内刷新按钮验证单个代理
- **删除代理**：点击行内删除按钮移除指定代理
- **Configure Pool**：打开配置界面修改池子参数

### 配置界面（⚡ 管理员专属）

点击 **Configure Pool** 打开配置模态框，包含：

**Pool Capacity 部分**
- Max Size：池子总容量
- HTTP Ratio：HTTP 协议占比（0.5 = 50%）
- Min Per Protocol：每协议最小保证

**Latency Standards 部分**
- Standard：标准模式延迟阈值
- Healthy：健康模式严格延迟
- Emergency：紧急模式放宽延迟

**Validation & Health Check 部分**
- Validate Concurrency：并发验证数
- Validate Timeout：验证超时
- Health Check Interval：健康检查间隔
- Health Check Batch Size：每批检查数量

**Optimization 部分**
- Optimize Interval：优化轮换间隔
- Replace Threshold：替换阈值（0.7 = 新代理需快 30%）

保存后立即生效，系统会自动调整池子策略。

## 🏗️ 核心架构

### 智能池子生命周期

```text
[启动] → 状态监控 → 判断池子健康度
              ↓
         需要补充？
          ↙     ↘
        是        否
         ↓         ↓
    智能抓取    保持监控
    (多模式)       ↓
         ↓      优化轮换
    严格验证   (定时执行)
         ↓         ↓
    智能入池    替换劣质代理
    (替换逻辑)     ↓
         ↓    分批健康检查
         ↓    (剔除失败)
         ↓         ↓
         └─────────┘
              ↓
         持续优化循环
```

### 状态转换机制

```text
Healthy (总数≥95% 且 各协议≥80%槽位)
   ↓ 代理失效
Warning (总数<95% 或 任一协议<80%)
   ↓ 继续失效
Critical (总数<50% 或 任一协议<20%槽位)
   ↓ 继续失效
Emergency (总数<10% 或 单协议缺失)
   ↑
   └─ 自动触发紧急抓取 ─┘
```

> 💡 **自动补充阈值**：当总数低于 95% 时进入 Warning 状态并触发自动补充，确保池子始终接近满容量运行。

### 抓取模式选择

| 池子状态 | 抓取模式 | 使用源 | 触发条件 |
| --- | --- | --- | --- |
| Emergency | 紧急模式 | 所有可用源 | 单协议缺失或总数<10% |
| Critical/Warning | 补充模式 | 快更新源 | 总数<95%或协议不均 |
| Healthy | 优化模式 | 慢更新源（随机2-3个） | 定时触发（30分钟） |

### 质量分级标准

| 等级 | 延迟范围 | 说明 | 权重 |
| --- | --- | --- | --- |
| S | ≤500ms | 超快，优先使用，健康状态跳过检查 | 最高 |
| A | 501-1000ms | 良好，稳定可用 | 高 |
| B | 1001-2000ms | 可用，会被优化替换 | 中 |
| C | >2000ms | 淘汰候选，优先替换 | 低 |

## 🔧 数据库 Schema

### proxies 表

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER | 主键 |
| `address` | TEXT | 代理地址（UNIQUE） |
| `protocol` | TEXT | 协议类型（http/socks5） |
| `exit_ip` | TEXT | 出口 IP |
| `exit_location` | TEXT | 出口位置 |
| `latency` | INTEGER | 延迟（毫秒） |
| `quality_grade` | TEXT | 质量等级（S/A/B/C） |
| `use_count` | INTEGER | 使用次数 |
| `success_count` | INTEGER | 成功次数 |
| `fail_count` | INTEGER | 失败次数 |
| `last_used` | DATETIME | 最后使用时间 |
| `last_check` | DATETIME | 最后检查时间 |
| `created_at` | DATETIME | 创建时间 |
| `status` | TEXT | 状态（active/degraded/candidate_replace） |

### source_status 表

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | INTEGER | 主键 |
| `url` | TEXT | 源地址（UNIQUE） |
| `success_count` | INTEGER | 成功次数 |
| `fail_count` | INTEGER | 失败次数 |
| `consecutive_fails` | INTEGER | 连续失败次数 |
| `last_success` | DATETIME | 最后成功时间 |
| `last_fail` | DATETIME | 最后失败时间 |
| `status` | TEXT | 状态（active/degraded/disabled） |
| `disabled_until` | DATETIME | 禁用到期时间 |

## 🔍 代理源

系统内置 16 个代理源，分为快更新和慢更新两组：

**快更新源（5-30分钟更新）**
- proxifly/free-proxy-list (HTTP/SOCKS4/SOCKS5)
- ProxyScraper/ProxyScraper (HTTP/SOCKS4/SOCKS5)
- monosans/proxy-list (HTTP)

**慢更新源（每天更新）**
- TheSpeedX/SOCKS-List (HTTP/SOCKS4/SOCKS5)
- monosans/proxy-list (SOCKS4/SOCKS5)
- databay-labs/free-proxy-list (HTTP/SOCKS5)

系统会根据池子状态自动选择合适的源组：
- 紧急/补充模式：使用快更新源，快速填充
- 优化模式：随机选择慢更新源，精细优化

## 🚦 核心机制详解

### 1. 智能入池机制

每个代理在入池前需通过：
1. **连接验证**：能否成功连接 `http://www.gstatic.com/generate_204`
2. **出口 IP 检测**：获取代理的出口 IP
3. **地理位置查询**：获取出口 IP 的国家/城市
4. **延迟测试**：测量连接延迟
5. **质量评估**：根据延迟计算质量等级

**入池判断逻辑**
- ✅ 协议槽位未满：直接加入
- ✅ 槽位满但总量允许10%浮动：浮动加入
- 🔄 池子满且质量更优：替换延迟最高的现有代理（需快30%+）
- ❌ 池子满且质量不足：拒绝

### 2. 健康检查机制

**批次检查策略**
- 每次检查 20 个代理（可配置）
- 优先检查长时间未检查的
- 池子健康时跳过 S 级代理（降低资源消耗）

**检查结果处理**
- ✅ 验证通过：更新延迟、出口 IP、质量等级
- ❌ 验证失败：失败计数 +1，≥3次自动删除

### 3. 优化轮换机制

**触发条件**
- 池子状态：Healthy
- 定时触发：默认 30 分钟

**优化流程**
1. 从慢更新源随机抽取 2-3 个源
2. 抓取候选代理并验证
3. 筛选出延迟 ≤1500ms 的优质代理
4. 尝试替换池中 B/C 级代理（需快30%+）

**资源控制**
- 仅在池子健康时执行
- 抽取少量源，避免浪费
- 严格质量标准（≤1500ms）

### 4. 源管理与断路器

**状态跟踪**
- 记录每个源的成功/失败次数
- 连续失败 3 次：降级（Degraded）
- 连续失败 5 次：禁用 30 分钟（Disabled）
- 冷却期结束：自动恢复为 Active

**好处**
- 避免浪费资源在失效源上
- 自动恢复，无需人工干预
- 保护系统免受源故障影响

## 📖 常见问题

### Q: 为什么池子容量是固定的？
A: 固定容量可以：
- **可预测资源消耗**：内存、CPU、网络带宽均可控
- **提升代理质量**：通过严格准入和替换保持高质量
- **简化管理逻辑**：避免无限增长和复杂的淘汰策略

### Q: 如何调整池子大小和协议比例？
A: 
1. 访问 WebUI → 点击 **Configure Pool**
2. 修改 **Max Size** 和 **HTTP Ratio**
3. 点击 **Save Configuration**
4. 系统会自动调整槽位分配

示例：
- 池子大小 200，HTTP 比例 0.7 → HTTP 槽位 140，SOCKS5 槽位 60
- 池子大小 50，HTTP 比例 0.3 → HTTP 槽位 15，SOCKS5 槽位 35

### Q: 池子状态如何计算？
A: 
- **Healthy**：总数 ≥95% 且各协议 ≥80% 槽位
- **Warning**：总数 <95% 或任一协议 <80% 槽位
- **Critical**：总数 <50% 或任一协议 <20% 槽位
- **Emergency**：总数 <10% 或单协议缺失

### Q: 如何优化延迟？
A: 系统会自动优化，也可以手动调整：
1. 降低 `max_latency_healthy`（严格模式）
2. 增加 `optimize_interval` 频率（更频繁优化）
3. 调高 `replace_threshold`（要求新代理更快）
4. 点击 **Refresh Latency** 立即重新验证

### Q: 为什么有的代理没有出口 IP？
A: 
- IP 查询有限流（10 次/秒）
- 部分代理可能不支持 IP 查询
- 系统会在后续健康检查中补全信息
- 没有出口信息的代理会在启动时被自动清理

### Q: 如何配置地理过滤？
A: 
通过 `BLOCKED_COUNTRIES` 环境变量配置需要屏蔽的国家：

```bash
# 默认屏蔽中国大陆（CN）
BLOCKED_COUNTRIES=CN

# 屏蔽多个国家（逗号分隔）
BLOCKED_COUNTRIES=CN,RU,KP

# 不屏蔽任何国家（留空）
BLOCKED_COUNTRIES=
```

**工作机制**：
- **验证阶段**：检测到屏蔽国家出口直接拒绝入池
- **启动清理**：自动删除数据库中屏蔽国家的代理
- **精确匹配**：使用 ISO 3166-1 alpha-2 国家代码（CN、HK、US 等）

**常用国家代码**：`CN`=中国大陆 | `HK`=香港 | `RU`=俄罗斯 | `US`=美国 | `JP`=日本 | `SG`=新加坡

> 📖 **详细配置指南**：更多国家代码、使用场景、测试方法，请查看 [`GEO_FILTER.md`](./GEO_FILTER.md)

### Q: 资源消耗如何？
A: 
- **内存**：池子 100 个约 50MB，200 个约 100MB
- **CPU**：空闲时 <1%，验证时 10-30%（取决于并发数）
- **网络**：
  - IP 查询限流 10 次/秒
  - 按需抓取，避免无效流量
  - 健康检查批次小（20 个）

### Q: 代理服务如何启用认证？
A: 
1. 编辑 `.env` 文件：
   ```bash
   PROXY_AUTH_ENABLED=true
   PROXY_AUTH_USERNAME=myuser
   PROXY_AUTH_PASSWORD=mypass
   ```
2. 重启服务：`docker compose up -d`
3. 客户端使用：`http://myuser:mypass@server-ip:7777`

详细说明和测试方法请查看 [`test/AUTH_TEST.md`](./test/AUTH_TEST.md)

### Q: 代理认证和 WebUI 认证有什么区别？
A: 
- **代理认证**：保护 7776/7777 代理服务端口，防止代理被滥用
- **WebUI 认证**：保护 7778 管理后台，区分访客和管理员权限
- 两者独立配置，互不影响

## 🛠️ 开发与调试

### 查看日志

日志会输出到 stdout，同时在 WebUI 的 **System Log** 部分实时展示。

关键日志标识：
- `[pool]`：池子管理器
- `[fetch]`：抓取器
- `[source]`：源管理器
- `[health]`：健康检查器
- `[optimize]`：优化器
- `[monitor]`：状态监控器

### 数据库操作

```bash
# 查看当前代理
sqlite3 data/proxy.db "SELECT address, protocol, latency, quality_grade, status FROM proxies LIMIT 10;"

# 查看质量分布
sqlite3 data/proxy.db "SELECT quality_grade, COUNT(*) FROM proxies WHERE status='active' GROUP BY quality_grade;"

# 查看源状态
sqlite3 data/proxy.db "SELECT url, status, consecutive_fails FROM source_status;"

# 清空池子（慎用）
sqlite3 data/proxy.db "DELETE FROM proxies;"
```

## 🧪 测试代理服务

项目提供了三种测试脚本，用于验证代理服务功能和性能（位于 `test/` 目录）。

### 快速测试

```bash
# 测试随机轮换模式（默认 7777 端口）
./test/test_proxy.sh

# 测试最低延迟模式（7776 端口）
./test/test_proxy.sh 7776

# 使用 Go 脚本
go run test/test_proxy.go          # 默认 7777
go run test/test_proxy.go 7776     # 测试 7776

# 使用 Python 脚本
python test/test_proxy.py          # 默认 7777
python test/test_proxy.py 7776     # 测试 7776

# 按 Ctrl+C 停止测试并查看统计
```

测试脚本特点：
- **持续运行模式**：类似 `ping` 命令，持续发送请求
- 实时显示每次请求的出口 IP 和延迟
- 动态更新成功率统计
- 验证代理轮换机制
- 按 `Ctrl+C` 停止并显示完整统计报告

### 测试输出示例

```
PROXY 127.0.0.1:7777 (http://ip-api.com/json/?fields=countryCode,query): continuous mode

proxy from 🇺🇸 203.0.113.45: seq=1 time=1234ms
proxy from 🇩🇪 198.51.100.78: seq=2 time=987ms
proxy from 🇬🇧 192.0.2.123: seq=3 time=1567ms
proxy #4: request failed (timeout)
proxy from 🇯🇵 198.51.100.12: seq=5 time=890ms
...（持续运行，按 Ctrl+C 停止）

^C
---
50 requests transmitted, 47 received, 3 failed, 6.0% packet loss
```

**测试认证功能**：
```bash
# 启用认证后测试
curl -x http://myuser:mypass@127.0.0.1:7777 https://httpbin.org/ip

# 无认证请求（应该返回 407 错误）
curl -x http://127.0.0.1:7777 https://httpbin.org/ip
```

**更多测试指南**：
- 基础功能测试：[`test/TEST_GUIDE.md`](./test/TEST_GUIDE.md)
- 认证功能测试：[`test/AUTH_TEST.md`](./test/AUTH_TEST.md)

## 🙏 致谢与声明

本项目基于 [jonasen1988/proxygo](https://github.com/jonasen1988/proxygo) 进行魔改和增强。

### 原项目
- **项目地址**：https://github.com/jonasen1988/proxygo
- **作者**：jonasen1988
- **基础功能**：代理抓取、验证、存储、HTTP代理服务、WebUI管理

### 本项目增强功能
在原项目基础上，我们进行了大量改进和功能增强：

- 🆕 **智能池子机制**：固定容量管理、质量分级（S/A/B/C）、智能替换逻辑
- 🆕 **按需抓取策略**：源分组、断路器保护、Emergency/Refill/Optimize 多模式
- 🆕 **分层健康管理**：批次检查、智能跳过 S 级、定时优化轮换
- 🆕 **智能重试机制**：自动故障切换、失败即删除、防重复尝试
- 🆕 **双端口服务**：7777 随机轮换（IP 多样性）+ 7776 最低延迟（稳定连接）
- 🆕 **代理认证保护**：可选 Basic Auth 认证，对外开放时保护代理服务不被滥用
- 🆕 **黑客风格 WebUI**：Matrix 美学、实时仪表盘、完整配置界面、中英文切换
- 🆕 **双角色权限**：访客模式（只读）+ 管理员模式（完全控制），可安全公网开放
- 🆕 **扩展存储层**：质量等级、使用统计、源状态管理
- 🆕 **测试套件**：Bash/Go/Python 三种测试脚本，持续运行模式，显示国旗 emoji
- 🆕 **CI/CD 自动化**：GitHub Actions 自动构建多架构镜像（amd64/arm64），双仓库发布
- 🆕 **环境变量配置**：docker-compose + .env 文件，灵活配置各种部署场景

感谢原作者提供的基础实现，让我们能够在此之上构建更强大的代理池系统。

同时感谢 [LINUX DO](https://linux.do/) 社区的支持。

## 📝 License

MIT License