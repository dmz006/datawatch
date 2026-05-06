# F16: Proxy Mode

**Date:** 2026-04-02
**Version:** v1.1.0 (planning)
**Effort:** 1-2 weeks
**Priority:** medium

---

## Overview

Proxy mode enables a single datawatch instance to act as a relay between messaging
channels and multiple remote datawatch instances. Users interact with one Signal/Telegram
group (or one PWA) and commands are routed to the correct remote server automatically.

### Use cases

1. **Multi-machine management** — one phone group controls sessions on laptop, NAS, server
2. **Kubernetes** — proxy pod exposes one ingress, routes to worker pods running sessions
3. **Team gateway** — one internet-facing proxy relays to internal instances behind NAT

### What exists today

| Component | Current state |
|-----------|--------------|
| Config: `servers` list | Done — `RemoteServerConfig` with name, URL, token, enabled |
| CLI: `--server` flag | Done — routes CLI commands to named remote server |
| API: `/api/proxy/{server}/{path}` | Done — HTTP request forwarding with token injection |
| API: `/api/servers` | Done — lists configured servers with auth status |
| Web UI: server picker | Partial — `state.activeServer` exists but unused in API calls |
| WebSocket relay | Missing — no remote WS connection or output streaming |
| Messaging routing | Missing — router can't forward to remote instances |
| Session aggregation | Missing — `/api/sessions` returns local only |

---

## Phase 1: Web UI proxy routing (1-2 days)

**Goal:** When a server is selected in the web UI, all API calls and WS connection route
through the proxy to that remote instance.

### 1.1 Route API calls through proxy

**File:** `internal/server/web/app.js`

When `state.activeServer` is set (non-null, non-"local"):
- `apiFetch(path)` rewrites URL: `/api/sessions` → `/api/proxy/{activeServer}/api/sessions`
- All GET, POST, PUT, DELETE calls transparently proxied
- Server picker button shows active server name in header

### 1.2 WebSocket proxy

**File:** `internal/server/ws.go`, `internal/server/api.go`

Add `/api/proxy/{server}/ws` endpoint that:
1. Opens a WS connection to the remote server's `/ws` endpoint
2. Relays all messages bidirectionally (local client ↔ remote hub)
3. Injects bearer token for auth on the remote side
4. Handles disconnection: auto-reconnect with backoff, notify client

Client-side: when `activeServer` is set, connect WS to `/api/proxy/{server}/ws`
instead of `/ws`.

### 1.3 Session detail proxying

When viewing a remote session:
- Subscribe to output via proxied WS
- Send input via proxied `/api/command` or `/api/sessions/start`
- xterm.js renders remote session output identically to local

### Files

- `internal/server/web/app.js` — `apiFetch()` URL rewriting, WS endpoint switching
- `internal/server/api.go` — WS proxy handler
- `internal/server/ws.go` — remote WS relay logic

---

## Phase 2: Session aggregation (1-2 days)

**Goal:** The sessions list shows sessions from ALL configured remote servers, unified
into one view.

### 2.1 Aggregated sessions API

**File:** `internal/server/api.go`

New endpoint: `GET /api/sessions?aggregate=true` (or always aggregate when servers exist)

1. Fetch `/api/sessions` from each enabled remote server in parallel (with timeout)
2. Merge with local sessions
3. Tag each session with `server: "prod"` or `server: "local"`
4. Return unified list sorted by timestamp

### 2.2 Web UI unified session list

**File:** `internal/server/web/app.js`

- Session cards show server badge: `[prod]`, `[local]`, `[pi]`
- Clicking a remote session sets `activeServer` and navigates to session detail
- "All servers" view is default; filter by server via dropdown

### 2.3 Server health indicators

- Session list header shows server status badges (green = healthy, red = unreachable)
- Fetch `/healthz` from each remote on a timer (30s)
- Unreachable servers show count of last-known sessions grayed out

### Files

- `internal/server/api.go` — aggregated sessions handler, parallel fetch
- `internal/server/web/app.js` — server badges, unified list, server filter

---

## Phase 3: Messaging routing (2-3 days)

**Goal:** Commands sent via Signal/Telegram are routed to the correct remote server
based on the session ID or hostname.

### 3.1 Remote-aware router

**File:** `internal/router/router.go`

When a command targets a session not on the local host:

1. `handleSend(cmd)` — if session ID not found locally, check remote servers
2. `handleStatus(cmd)`, `handleTail(cmd)`, `handleKill(cmd)` — same fallback
3. For each remote, call `GET /api/proxy/{server}/api/sessions` and match session ID
4. Forward the command via `POST /api/proxy/{server}/api/command`
5. Return the remote response to the messaging channel

### 3.2 Session discovery cache

**File:** `internal/router/remote.go` (new)

- Maintain a cached map: `sessionID → serverName`
- Refresh on `list` command or every 60 seconds
- Used for fast routing without querying all servers per command

### 3.3 `new` command routing

When `new: <task>` is sent from a messaging channel:
- Default: create on local instance (current behavior)
- `new @prod: <task>` — route to named server
- `new @pi: /path/to/project: <task>` — route with explicit server + project dir

### 3.4 `list` aggregation

`list` command returns sessions from all servers:
```
[local] Sessions (2):
  [a3f2] running | claude-code | 10:30 | myproject
  [b1c3] waiting_input | aider | 10:45 | api-refactor
[prod] Sessions (1):
  [d4e5] running | claude-code | 09:15 | deploy-pipeline
[pi] Sessions (0): idle
```

### Files

- `internal/router/router.go` — remote fallback in command handlers
- `internal/router/remote.go` — session discovery cache, remote command forwarding
- `cmd/datawatch/main.go` — wire remote config into router

---

## Phase 4: PWA reverse proxy (1-2 days)

**Goal:** Access any remote instance's full PWA through the proxy, without direct
network access to the remote.

### 4.1 Full PWA proxying

**File:** `internal/server/server.go`

Route: `/remote/{server}/` → proxies the full PWA from the remote instance.

1. Fetch HTML/JS/CSS from remote's `/` endpoint
2. Rewrite asset URLs to go through proxy
3. Rewrite API calls in served JS to use `/api/proxy/{server}/`
4. Rewrite WS endpoint to use `/api/proxy/{server}/ws`

This allows accessing `http://proxy:8080/remote/prod/` to get the prod
instance's full web UI, tunneled through the proxy.

### 4.2 Ingress / DNS integration

For Kubernetes:
- Single ingress exposes proxy on `datawatch.example.com`
- Path-based routing: `/remote/worker-1/`, `/remote/worker-2/`
- Or: proxy aggregates all workers into one unified UI (Phase 2)

For Tailscale:
- Proxy instance on Tailscale network
- Remote instances on same Tailscale network or direct LAN
- Phone accesses `http://proxy-tailscale-ip:8080` for all instances

### Files

- `internal/server/server.go` — PWA proxy route registration
- `internal/server/api.go` — PWA content rewriting

---

## Phase 5: Resilience (1-2 days)

**Goal:** Handle remote server failures gracefully.

### 5.1 Connection management

- Persistent HTTP client pool per remote server (connection reuse)
- Configurable timeout per server (default 10s)
- Circuit breaker: after 3 consecutive failures, mark server as down for 30s
- Background health check restores server when `/healthz` responds

### 5.2 Offline queuing

- Commands to unreachable servers are queued in memory (max 100)
- When server comes back online, replay queued commands in order
- Notify messaging channel: "Server {name} back online, replaying 3 queued commands"

### 5.3 Failover for sessions

- If a remote server goes down mid-session, show clear status in web UI
- Session cards show "server unreachable" badge
- When server recovers, session state auto-refreshes

### Files

- `internal/proxy/pool.go` — HTTP client pool, circuit breaker, health checker
- `internal/proxy/queue.go` — offline command queue
- `internal/server/web/app.js` — server status badges, offline indicators

---

## Configuration

```yaml
# Existing config — no new fields needed for Phase 1-3
servers:
  - name: prod
    url: http://203.0.113.10:8080
    token: "bearer-token"
    enabled: true
  - name: pi
    url: http://203.0.113.50:8080
    token: "another-token"
    enabled: true

# New fields for Phase 4-5 (optional)
proxy:
  enabled: true                 # enable proxy aggregation mode
  health_interval: 30           # seconds between remote health checks
  request_timeout: 10           # seconds per remote request
  offline_queue_size: 100       # max queued commands per server
  circuit_breaker_threshold: 3  # failures before marking server down
  circuit_breaker_reset: 30     # seconds before retrying downed server
```

### Access methods

| Method | How |
|--------|-----|
| **YAML** | `servers:` list + optional `proxy:` section |
| **CLI** | `datawatch setup server` (existing wizard) |
| **Web UI** | Settings → Comms → **Servers** card (existing); Settings → General → **Proxy** card (new) |
| **REST API** | `GET /api/servers`, `PUT /api/config` for proxy settings |
| **Comm channel** | `configure proxy.enabled=true`, server management via setup wizard |

---

## Implementation order

| Phase | Effort | Dependencies | Value |
|-------|--------|-------------|-------|
| 1. Web UI proxy routing | 1-2 days | None | PWA works with any remote server |
| 2. Session aggregation | 1-2 days | Phase 1 | Unified multi-server view |
| 3. Messaging routing | 2-3 days | Phase 2 | Signal/Telegram controls all servers |
| 4. PWA reverse proxy | 1-2 days | Phase 1 | Access remote PWA through proxy |
| 5. Resilience | 1-2 days | Phase 2-3 | Production-ready for k8s |

Phases 1-3 deliver the core value. Phases 4-5 are for production/k8s deployments.

---

## Files summary

| File | Changes |
|------|---------|
| `internal/server/web/app.js` | apiFetch URL rewriting, WS switching, server badges, aggregated list |
| `internal/server/api.go` | WS proxy handler, aggregated sessions, PWA rewriting |
| `internal/server/ws.go` | Remote WS relay |
| `internal/server/server.go` | New route registrations |
| `internal/router/router.go` | Remote command fallback |
| `internal/router/remote.go` | New: session discovery cache, remote forwarding |
| `internal/proxy/pool.go` | New: HTTP client pool, circuit breaker |
| `internal/proxy/queue.go` | New: offline command queue |
| `internal/config/config.go` | ProxyConfig struct (Phase 5) |
| `cmd/datawatch/main.go` | Wire proxy config |
| `docs/operations.md` | Proxy mode documentation |
| `docs/setup.md` | Multi-server proxy setup guide |
