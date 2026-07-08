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

Open the WebUI at `http://localhost:7800`. The default WebUI password is `goproxy`; set `WEBUI_PASSWORD` before production use.

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

Enable proxy authentication when exposing proxy ports outside a trusted network:

```env
PROXY_AUTH_ENABLED=true
PROXY_AUTH_USERNAME=acct
PROXY_AUTH_PASSWORD=change-me
```

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
| base | `acct` | Must equal `PROXY_AUTH_USERNAME`. This is the only part checked against the configured credential. |
| region | `jp` | Selects a `jp` region node. |
| session | `browser` | Sticky key: same key reuses the same exit node within the TTL. |

Only the **base** takes part in password authentication (base must equal
`PROXY_AUTH_USERNAME`, and the password must equal `PROXY_AUTH_PASSWORD`). The
`-region-` and `-session-` parts are routing hints, not credentials.

### Username forms

| Username | Meaning |
|----------|---------|
| `acct` | Authenticate as `acct`; use any available node. |
| `acct-region-us` | Use an available `us` region node. |
| `acct-session-browser` | Bind session key `browser` to one node for the configured TTL. |
| `acct-region-jp-session-app01` | Use `jp` nodes and keep session `app01` sticky when possible. |

> Replace `acct` with whatever you set in `PROXY_AUTH_USERNAME`. If your base
> username is `myuser`, the strings become `myuser-region-us`, etc. The base
> prefix is fixed by config; the suffixes are chosen per request.

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

The examples below assume proxy authentication is enabled (`PROXY_AUTH_ENABLED=true`) with username `acct` and password `change-me`. When authentication is disabled, drop the `user:pass@` part.

### HTTP proxy (plain HTTP target)

```bash
curl -x http://acct-region-us:change-me@localhost:7802 http://httpbin.org/ip
```

### HTTP proxy (HTTPS target, via CONNECT tunnel)

An HTTPS target goes through the proxy as a `CONNECT` tunnel. The proxy credentials are sent in the `Proxy-Authorization` header; the DSL suffix stays in the username.

```bash
curl -x http://acct-region-us:change-me@localhost:7802 https://httpbin.org/ip
```

### SOCKS5 proxy (works for both HTTP and HTTPS targets)

SOCKS5 tunnels raw TCP, so the same command works whether the target is HTTP or HTTPS. Credentials can be embedded in the URL or passed with `-U`:

```bash
# Credentials embedded in the proxy string
curl --socks5 acct-region-jp-session-browser:change-me@localhost:7801 https://httpbin.org/ip

# Equivalent, using -x with the socks5:// scheme
curl -x socks5://acct-region-jp-session-browser:change-me@localhost:7801 https://httpbin.org/ip

# Equivalent, passing credentials separately
curl --socks5 localhost:7801 -U acct-region-jp-session-browser:change-me https://httpbin.org/ip
```

To resolve the target hostname at the exit node instead of locally, use the `socks5h` scheme:

```bash
curl -x socks5h://acct-region-us:change-me@localhost:7801 https://httpbin.org/ip
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

- Overview and node list with status, latency, source, region, and exit IP.
- Manual node add/edit/delete for manual nodes only.
- Subscription management for adding, refreshing, pausing, enabling, and deleting subscription nodes.
- Active session monitor for username DSL session bindings.
- Settings for authentication, default region, country filters, session TTL, health interval, retry count, and `sing-box` path.
- Logs.

Subscription nodes are managed through subscription operations. Manual-node delete/edit actions do not apply to subscription-owned nodes.

## Manual And Subscription Nodes

- Manual direct nodes use direct HTTP or SOCKS5 links.
- Manual encrypted single-link nodes use `sing-box` conversion and require `SINGBOX_PATH` to point to a usable `sing-box` binary.
- Subscription nodes may come from URL, uploaded/pasted content, Clash/V2ray/Base64/plain text formats, and supported encrypted protocols through `sing-box`.
- Tests that exercise encrypted single-link conversion may skip when `sing-box` is not installed.
- Public/free proxy scraping is not part of the current runtime model.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WEBUI_PORT` | `7800` | WebUI listen port. |
| `SOCKS5_PORT` | `7801` | SOCKS5 gateway listen port. |
| `HTTP_PORT` | `7802` | HTTP gateway listen port. |
| `WEBUI_PASSWORD` | `goproxy` | WebUI admin password. |
| `PROXY_AUTH_ENABLED` | `false` | Enables proxy username/password auth. |
| `PROXY_AUTH_USERNAME` | `acct` | Base proxy username before DSL suffixes. |
| `PROXY_AUTH_PASSWORD` | empty | Proxy password. Required when auth is enabled. |
| `SESSION_TTL_MINUTES` | `10` | Sticky session binding TTL. |
| `DEFAULT_REGION` | empty | Optional default region for requests without `-region-`. |
| `ALLOWED_COUNTRIES` | empty | Comma-separated allowlist; takes priority when set. |
| `BLOCKED_COUNTRIES` | empty | Comma-separated denylist used only when allowlist is empty. |
| `HEALTH_CHECK_INTERVAL` | `5` | Health-check interval in minutes. |
| `MAX_RETRY` | `3` | Retry count for failed upstream attempts. |
| `SINGBOX_PATH` | `sing-box` | `sing-box` binary path. |
| `DATA_DIR` | empty | Optional directory for SQLite DB and generated subscription files. |
| `TZ` | `Asia/Shanghai` | Container timezone. |

Saved WebUI settings are written to `config.json` under `DATA_DIR` and override environment defaults after the first save.

## Data Model

Runtime state is stored in SQLite:

- `proxies`: manual and subscription upstream nodes, region metadata, status, latency, and usage counters.
- `subscriptions`: subscription URL/file metadata, refresh interval, status, and last successful refresh.

Existing legacy rows using old source values are migrated into the current `manual` or `subscription` model during startup.

## Deployment Notes

This fork must be deployed from the local source tree. Do not deploy the upstream `isboyjc/goproxy` container image for this geo-gateway build.

Docker Compose builds the local `Dockerfile` by default:

```bash
cp .env.example .env
docker compose up -d --build
```

Security recommendations:

- Change `WEBUI_PASSWORD` before exposing the WebUI.
- Enable proxy authentication for non-local deployments.
- Restrict inbound firewall rules to trusted clients when possible.
- Treat upstream node credentials and subscription URLs as secrets.

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

## Disclaimer

This project is for learning, research, and management of user-provided upstream proxy resources. Users are responsible for ensuring their upstream nodes, subscriptions, and traffic comply with applicable laws, policies, and service terms.

## License

[MIT](LICENSE)
