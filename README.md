# GoProxy

> Geo-aware proxy gateway for private upstream nodes. GoProxy exposes one HTTP proxy, one SOCKS5 proxy, and an authenticated WebUI while routing traffic by username DSL, region, and short-lived session affinity.

[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)](https://go.dev/)

## What It Does

GoProxy stores upstream nodes you control, validates their reachability and exit metadata, and selects an available node for each HTTP or SOCKS5 client request. Routing can be constrained by region and stabilized with a session key in the proxy username.

Supported upstream sources:

- Manual nodes persisted in SQLite (`source=manual`). Direct `http://` and `socks5://` links are stored directly; encrypted single-link nodes use `sing-box` conversion and require a working `sing-box` runtime.
- Subscription nodes from Clash/V2ray/Base64/plain text inputs (`source=subscription`). Encrypted subscription protocols are converted through `sing-box` into local SOCKS5 upstreams.

There is no active public proxy pool or automatic public source collector in the current gateway model.

## Default Ports

| Port | Service | Config key |
|------|---------|------------|
| `7800` | WebUI | `WEBUI_PORT` |
| `7801` | SOCKS5 proxy | `SOCKS5_PORT` |
| `7802` | HTTP proxy | `HTTP_PORT` |

## Quick Start

### Docker Compose

```bash
cp .env.example .env
docker compose up -d --build
```

On first start, the gateway auto-generates a WebUI password and proxy
authentication credentials, and prints them **once** to the container log:

```bash
docker compose logs | grep "首次启动"
```

Save those credentials, then open the WebUI at `http://localhost:7800` and log
in. You can change all credentials later under **Settings**. The credentials are
persisted (hashed for the WebUI password) in `config.json` inside the data
volume; they are not shown again on later restarts.

### Local Run

```bash
go run .
```

Local subscription conversion requires `sing-box` on `PATH` or a custom `SINGBOX_PATH`.

On Windows, use the project build/test environment before Go commands:

```powershell
$env:PATH="C:\Program Files\Go\bin;C:\ProgramData\mingw64\mingw64\bin;"+$env:PATH
$env:CGO_ENABLED='1'
$env:GOPROXY="https://goproxy.cn,direct"
```

## Proxy Authentication And Username DSL

Proxy authentication is enabled by default. The base username (default `acct`)
and password are auto-generated on first boot and printed once to the log; the
password is stored in `config.json`. Change them in the WebUI **Settings** page.

> Proxy credentials must be stored in recoverable form (not just a hash) because
> the SOCKS5 username/password auth scheme (RFC 1929) compares the plaintext the
> client sends. `config.json` therefore holds the proxy password in clear text;
> it lives only in the data volume, is written with mode `0600`, and is excluded
> from git. The WebUI login password is stored as a hash, not clear text.

### How routing is passed

Standard proxy protocols (HTTP Basic auth, SOCKS5 user/pass) only carry a
**username** and a **password** — there is no separate field for region or
session. This gateway therefore encodes routing parameters **inside the proxy
username string itself** (a "username DSL"). You put the whole string into your
client's proxy-username field.

The full username has the form:

```
<base>[-region-<cc>][-session-<id>]
```

For example, `acct-region-jp-session-browser` is parsed by the server as:

| Part | Value | Role |
|------|-------|------|
| base | `acct` | Must equal the configured proxy username (default `acct`, editable in Settings). This is the only part checked against the credential. |
| region | `jp` | Selects a `jp` region node. |
| session | `browser` | Sticky key: same key reuses the same exit node within the TTL. |

Only the **base** takes part in password authentication (base must equal the
configured proxy username, and the password must equal the configured proxy
password). The `-region-` and `-session-` parts are routing hints, not
credentials.

### Username forms

| Username | Meaning |
|----------|---------|
| `acct` | Authenticate as `acct`; use any available node. |
| `acct-region-us` | Use an available `us` region node. |
| `acct-session-browser` | Bind session key `browser` to one node for the configured TTL. |
| `acct-region-jp-session-app01` | Use `jp` nodes and keep session `app01` sticky when possible. |

> Replace `acct` with the configured proxy username (default `acct`, editable in
> the WebUI Settings). If your base username is `myuser`, the strings become
> `myuser-region-us`, etc. The base prefix is fixed by config; the suffixes are
> chosen per request.

### The region code

- `region` is a two-letter code (ISO alpha-2, e.g. `us`, `jp`, `hk`), normalized
  to lowercase. It must match the `region` stored on a node.
- If no node exists in the requested region, the request fails explicitly
  (`no available node for region: X`) — it does **not** silently fall back to
  another region.
- If `DEFAULT_REGION` is set, requests without a `-region-` suffix use that
  default region. Otherwise a request without a region uses any available node.

### The session key

- The `session` value is an **arbitrary identifier that you invent**. There is
  no preset list and no registration step — the server does not check the value
  against anything, only its format.
- Allowed characters: ASCII letters, digits, `_`, and `-`; maximum 64
  characters; it may not be empty.
- Examples are all valid: `browser`, `chrome1`, `edge02`, `task-a`,
  `user_42`, `bot-us-01`.
- **What it does:** each distinct session key is bound to a single exit node for
  the session TTL (`SESSION_TTL_MINUTES`, default 10). Reusing the same key keeps
  the same exit IP; using a different key is an independent binding that may land
  on a different node. Omitting `-session-` gives no stickiness — each request is
  routed fresh by lowest latency.
- Practical use: give each client/task its own key. `...-session-chrome1` and
  `...-session-edge02` in the same region each lock onto (potentially different)
  exit IPs and do not interfere with each other. Change the key to deliberately
  rotate the exit.

## Using The Proxy

The examples below use the base username `acct` and password `change-me` as placeholders. Replace them with your actual credentials (the auto-generated proxy password shown once in the logs on first boot, or whatever you later set in the WebUI). Proxy authentication is enabled by default.

### HTTP proxy (plain HTTP target)

```bash
curl -x http://acct-region-us:change-me@localhost:7802 http://example.com/
```

### HTTP proxy (HTTPS target, via CONNECT tunnel)

An HTTPS target goes through the proxy as a `CONNECT` tunnel. The proxy credentials are sent in the `Proxy-Authorization` header; the DSL suffix stays in the username.

```bash
curl -x http://acct-region-us:change-me@localhost:7802 https://www.gstatic.com/generate_204
```

### SOCKS5 proxy (works for both HTTP and HTTPS targets)

SOCKS5 tunnels raw TCP, so the same command works whether the target is HTTP or HTTPS. Credentials can be embedded in the URL or passed with `-U`:

```bash
# Credentials embedded in the proxy string
curl --socks5 acct-region-jp-session-browser:change-me@localhost:7801 https://www.gstatic.com/generate_204

# Equivalent, using -x with the socks5:// scheme
curl -x socks5://acct-region-jp-session-browser:change-me@localhost:7801 https://www.gstatic.com/generate_204

# Equivalent, passing credentials separately
curl --socks5 localhost:7801 -U acct-region-jp-session-browser:change-me https://www.gstatic.com/generate_204
```

To resolve the target hostname at the exit node instead of locally, use the `socks5h` scheme:

```bash
curl -x socks5h://acct-region-us:change-me@localhost:7801 https://www.gstatic.com/generate_204
```

> When authentication is enabled, a client that offers no credentials is rejected during the SOCKS5 handshake (no acceptable method), and HTTP requests receive `407 Proxy Authentication Required`.

### Environment variables

```bash
export http_proxy=http://acct:change-me@localhost:7802
export https_proxy=http://acct:change-me@localhost:7802
export ALL_PROXY=socks5://acct-session-browser:change-me@localhost:7801
```

## WebUI

The WebUI is a password-only authenticated admin surface. Unauthenticated users see only the neutral login screen, and business APIs require an authenticated session.

Panels currently include:

- Overview and node list with status, latency, source, region, exit IP, risk scores, Cloudflare intercept, and AI reachability badges (OpenAI / Claude / Grok / Gemini).
- Global node map (land outline, region dots, session arcs, gateway marker).
- Manual node add/edit/delete for manual nodes only; star, test, and copy full proxy URL.
- Subscription management for adding, refreshing, pausing, enabling, and deleting subscription nodes, including optional custom request headers (JSON, e.g. `User-Agent`) for sources that reject the default client UA.
- Active session monitor for username DSL session bindings.
- Settings for authentication, default region, country filters, session TTL, health interval, retry count, and `sing-box` path.
- Logs.

Subscription nodes are managed through subscription operations. Manual-node delete/edit actions do not apply to subscription-owned nodes.

## Manual And Subscription Nodes

- Manual direct nodes use direct HTTP or SOCKS5 links.
- Manual encrypted single-link nodes use `sing-box` conversion and require `SINGBOX_PATH` to point to a usable `sing-box` binary.
- Subscription nodes may come from URL, uploaded/pasted content, Clash/V2ray/Base64/plain text formats, and supported encrypted protocols through `sing-box`.
- Encrypted tunnel nodes are exposed as a single local **mixed** inbound per node (one port serves both SOCKS5 and HTTP). They are stored with `dual_protocol` and shown as dual protocol badges in the WebUI.
- Tunnel nodes are sharded across multiple independent `sing-box` processes (`SINGBOX_SHARD_COUNT`, default `4`). Only shards whose node set changed are reloaded.
- Subscription URL fetch supports custom headers so operators can set `User-Agent` or other required headers when the provider returns 401 for the default client.
- Subscription URL fetch rejects private, link-local, and non-global unicast targets (SSRF guard).
- Tests that exercise encrypted single-link conversion may skip when `sing-box` is not installed.
- Public/free proxy scraping is not part of the current runtime model.

## Local Bypass And Inbound Hardening

- HTTP, CONNECT, and SOCKS5 clients that target loopback, `localhost` / `.local`, RFC1918 private ranges, or IPv6 ULA are **bypassed** to the gateway host (direct dial, not via an upstream node).
- Link-local addresses (including cloud metadata such as `169.254.169.254`) are **not** bypassed; they stay on the upstream path so the gateway does not dial host credential endpoints.
- HTTP inbound applies `ReadHeaderTimeout` (from the validation timeout, default 10s) to limit half-header stall risk.
- SOCKS5 inbound bounds handshake/auth/request framing and clears the protocol deadline after the request so long-lived tunnels are not cut short.
- Upstream SOCKS5 handshakes are deadline-bounded so a silent peer cannot hang a dial forever.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBUI_PORT` | `7800` | WebUI listen port. |
| `SOCKS5_PORT` | `7801` | SOCKS5 gateway listen port. |
| `HTTP_PORT` | `7802` | HTTP gateway listen port. |
| `SESSION_TTL_MINUTES` | `10` | Sticky session binding TTL. |
| `DEFAULT_REGION` | empty | Optional default region for requests without `-region-`. |
| `ALLOWED_COUNTRIES` | empty | Comma-separated allowlist; takes priority when set. |
| `BLOCKED_COUNTRIES` | empty | Comma-separated denylist used only when allowlist is empty. |
| `HEALTH_CHECK_INTERVAL` | `5` | Health-check interval in minutes. |
| `MAX_RETRY` | `3` | Retry count for failed upstream attempts. |
| `SINGBOX_PATH` | `sing-box` | `sing-box` binary path. |
| `SINGBOX_SHARD_COUNT` | `4` | Number of independent `sing-box` processes for encrypted tunnel nodes. |
| `DATA_DIR` | empty | Optional directory for SQLite DB and generated subscription files. |
| `TZ` | `Asia/Shanghai` | Container timezone. |

Credentials (WebUI password, proxy auth username/password) are **not** set via
environment variables. On first boot the server generates them, prints them once
to the container log, and stores them in `config.json` under `DATA_DIR`. Edit
them afterwards in the WebUI **Settings** panel. All other settings are seeded
from the environment on first boot and then persisted to `config.json`, which is
the source of truth on subsequent starts. To reset everything, delete
`config.json` from the data volume.

### Lost the WebUI password?

The first-boot credentials are printed to the log only once. If you missed them,
you cannot recover the WebUI password (only its hash is stored), but you can
reset it without losing subscriptions or other settings.

The default Docker Compose deployment bind-mounts host `./data` to container
`/app/data`. The persisted files below therefore live under `./data` beside
`docker-compose.yml` unless you override `HOST_DATA_DIR`.

Reset only the WebUI password (keeps username, filters, and all subscription
nodes). This removes the stored hash so the next start regenerates and prints a
new WebUI password:

```bash
docker compose exec goproxy sh -c "sed -i '/webui_password_hash/d' /app/data/config.json"
docker compose restart goproxy
docker compose logs goproxy | grep -A6 首次启动
```

The proxy password (used by SOCKS5/HTTP clients) is stored in clear text and can
be read directly without a reset:

```bash
docker compose exec goproxy cat /app/data/config.json
```

To reset **all** credentials and settings (subscription nodes in `proxy.db` are
kept), delete the whole config file and restart:

```bash
docker compose exec goproxy rm /app/data/config.json
docker compose restart goproxy
docker compose logs goproxy | grep -A6 首次启动
```

## Data Model

Runtime state is stored in SQLite:

- `proxies`: manual and subscription upstream nodes, region metadata, status, latency, and usage counters.
- `subscriptions`: subscription URL/file metadata, refresh interval, status, and last successful refresh.

Existing legacy rows using old source values are migrated into the current `manual` or `subscription` model during startup.

## Deployment Notes

This fork must be deployed from the local source tree. Do not deploy the upstream `isboyjc/goproxy` container image for this geo-gateway build. No prebuilt image is published for this fork; build it locally from this repository.

Docker Compose builds the local `Dockerfile` by default:

```bash
cp .env.example .env
docker compose up -d --build
```

By default, compose writes runtime data to host `./data` and sets the container
application data directory to `/app/data`. Override the host path with
`HOST_DATA_DIR=/absolute/or/relative/path`; do not change the container
`DATA_DIR=/app/data` unless you also change the volume target.

If `docker images` shows extra `<none>` entries after local rebuilds, first
confirm the tagged final image exists with `docker image ls goproxy-geo`. The
`<none>` entries are usually dangling build cache or superseded local image
layers from multi-stage rebuilds, not an alternate deployable image. On a Docker
host, inspect and clean them explicitly when needed:

```bash
docker image ls --filter dangling=true
docker image prune
docker builder prune
```

This repository sets the compose build target to the final `runtime` stage, but
local dangling-image behavior still depends on the Docker builder and cache mode.

Security recommendations:

- On first boot the gateway prints an auto-generated WebUI password and proxy
  credentials **once** to the container log (`docker compose logs`). Save them,
  then log in and change them in Settings.
- Proxy authentication is enabled by default with the generated credentials.
- `config.json` is persisted with mode `0600` (owner read/write only).
- Restrict inbound firewall rules to trusted clients when possible.
- Treat upstream node credentials and subscription URLs as secrets.
- Prefer subscription custom headers over embedding secrets in the subscription
  name or public logs when a provider requires a special `User-Agent`.

## Verification

```powershell
go test ./...
go build ./...
$auditTerms = @(
  ('90' + '00'), ('90' + '01'), ('90' + '02'),
  ('77' + '76'), ('77' + '77'), ('77' + '78'), ('77' + '79'), ('77' + '80'),
  ([string][char]20813 + [char]36153 + [char]20195 + [char]29702),
  ([string][char]20844 + [char]24320 + [char]25235 + [char]21462),
  ('free' + '_only'), ('Custom' + 'FreePriority'),
  ([string][char]26234 + [char]33021 + [char]20195 + [char]29702 + [char]27744)
)
$auditPattern = $auditTerms -join '|'
rg $auditPattern README.md .env.example docker-compose.yml Dockerfile test docs POOL_DESIGN.md GEO_FILTER.md -n
git status --short
```

Real end-to-end proxy verification needs actual upstream nodes or subscriptions. Encrypted node conversion additionally needs `sing-box`; Docker-level checks need Docker or a compatible compose runtime.

## Related Docs

- [POOL_DESIGN.md](POOL_DESIGN.md): legacy architecture note for the removed pool design.
- [GEO_FILTER.md](GEO_FILTER.md): current country filter and region-routing notes.
- [test/README.md](test/README.md): proxy test scripts.

## Acknowledgment / 致谢

This project is a geo-gateway fork built on top of [isboyjc/GoProxy](https://github.com/isboyjc/GoProxy).
Thanks to the original author and contributors for the foundational work.

本项目是基于 [isboyjc/GoProxy](https://github.com/isboyjc/GoProxy) 改造的地域网关分支，
感谢原项目作者与贡献者的基础工作。

## Disclaimer / 免责声明

This project is for learning, research, and management of user-provided upstream proxy resources. Users are responsible for ensuring their upstream nodes, subscriptions, and traffic comply with applicable laws, policies, and service terms.

本项目仅供学习交流和技术研究使用。

- 本项目抓取的代理均来自互联网公开资源，不保证其可用性、稳定性和安全性。
- 用户应自行承担使用本项目的一切风险，包括但不限于网络安全风险、法律风险等。
- 请遵守当地法律法规，不得将本项目用于任何违法违规活动。
- 订阅导入功能仅为方便用户管理自有代理资源，用户应确保其订阅来源合法合规。
- 访客贡献的订阅由贡献者自行负责，项目维护者不对其内容承担任何责任。
- 本项目不提供任何形式的代理服务，不对通过本系统传输的内容负责。
- 作者不对因使用本项目造成的任何直接或间接损失承担责任。

使用本项目即表示您已阅读并同意以上声明。

## License

[MIT](LICENSE)
