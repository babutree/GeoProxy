# Legacy Pool Design

This document is retained only as a migration marker. The previous autonomous pool architecture has been removed from the current geo-aware gateway model and must not be used as deployment or behavior documentation.

Current runtime summary:

- WebUI listens on `WEBUI_PORT` (`7800` by default).
- SOCKS5 proxy listens on `SOCKS5_PORT` (`7801` by default).
- HTTP proxy listens on `HTTP_PORT` (`7802` by default).
- Upstream nodes are stored in SQLite as `manual` or `subscription` sources.
- Subscription inputs may be URL, pasted file content, or supported node text formats; `sing-box` converts encrypted subscription nodes into local SOCKS5 upstreams.
- Routing is handled by username DSL suffixes such as `acct-region-us`, `acct-session-browser`, and `acct-region-jp-session-app01`.
- Selection uses available node status, requested/default region, and short-lived session affinity.

Use `README.md`, `.env.example`, and `docker-compose.yml` as the current deployment references.
