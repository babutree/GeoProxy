# Geo Filtering And Region Routing

GeoProxy supports two related controls:

- Country filters decide whether validated upstream nodes remain available.
- Username DSL region suffixes decide which region a client request should use.

## Configuration

| Variable | Description |
|----------|-------------|
| `ALLOWED_COUNTRIES` | Comma-separated allowlist. When non-empty, only these countries are allowed. |
| `BLOCKED_COUNTRIES` | Comma-separated denylist used only when `ALLOWED_COUNTRIES` is empty. |
| `DEFAULT_REGION` | Optional default two-letter region for requests without a username region suffix. |

Examples:

```env
ALLOWED_COUNTRIES=US,JP,SG
BLOCKED_COUNTRIES=
DEFAULT_REGION=us
```

```env
ALLOWED_COUNTRIES=
BLOCKED_COUNTRIES=CN,RU
DEFAULT_REGION=
```

Saved WebUI settings are stored in `config.json` under `DATA_DIR` and override environment defaults on later starts.

## Username DSL

Proxy authentication usernames may include these suffixes:

```text
username-region-us
username-session-browser
username-region-jp-unlock-gpt-session-app01
```

Rules:

- Region must be two ASCII letters and is normalized to lowercase.
- Session can contain ASCII letters, digits, `_`, and `-`, up to 64 characters.
- Optional `-unlock-<token>` appears after region and before session when present.
- A request-specific region overrides `DEFAULT_REGION`.
- A session key keeps the client bound to the same available node until the session TTL expires or the node becomes unavailable.

## Runtime Behavior

Country filtering is applied during validation and maintenance. Nodes that fail policy are disabled rather than silently hidden as healthy candidates. Existing rows are preserved for inspection unless explicitly deleted through the admin surface or subscription deletion flow.

The current gateway validates user-provided manual and subscription nodes only. It does not scrape public/free proxy sources.

The selector chooses among nodes whose status is `active` or `degraded` and whose fail count is below the retry threshold. When a region is requested, only matching node regions are considered.

## Country Codes

Use ISO 3166-1 alpha-2 codes:

| Code | Region |
|------|--------|
| `US` | United States |
| `JP` | Japan |
| `SG` | Singapore |
| `HK` | Hong Kong |
| `TW` | Taiwan |
| `CN` | China mainland |
| `GB` | United Kingdom |
| `DE` | Germany |

Values are case-insensitive in configuration, but normalized values are stored and compared as two-letter codes.
