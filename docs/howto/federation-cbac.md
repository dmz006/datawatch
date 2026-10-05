# Federation Peer CBAC — Operator Guide

Datawatch v7.3.0 introduces cross-host session federation with
capability-based access control (CBAC). A federated peer is an AI agent running
on a remote datawatch instance that can connect to your instance using its own
bearer token. Every action it takes is gated against the capabilities you grant.

---

## Base requirements

- Two running datawatch instances (primary + secondary). See [federated-observer.md](federated-observer.md) for how to set up federation and configure `observer.peers.allow_register: true`.
- Remote Server entry on the primary pointing at the secondary. See [multi-servers.md](multi-servers.md).
- Token credentials for cross-instance calls stored as secrets. See [secrets-manager.md](secrets-manager.md).

> **Pre-conditions**: CBAC requires a working federated observer setup. Complete [federated-observer.md](federated-observer.md) first.

---

## Quick start

### 1. Register a peer with limited capabilities

```bash
# Register a remote instance as a federation peer.
# Default capabilities: ["federation-peer"] — health check + read its own
# entry only (v9.0.0, SEC-009). Grant more explicitly below if this peer
# needs it — the default no longer includes session/agent/observer reads
# or the ability to list other peers.
datawatch federation peer add peer-alpha \
  --url http://198.51.100.2:8080 \
  --token tok-peer-alpha \
  --capabilities federation-peer

# Verify it was registered:
datawatch federation peer list
```

Via comm channel (Telegram, Signal, etc.):
```
federation peer add peer-alpha http://198.51.100.2:8080 token=tok-peer-alpha
```

### 2. Grant specific capabilities

```bash
# Grant read-only access across all surfaces:
datawatch federation peer update peer-alpha \
  --capabilities read-only

# Grant session-operator (can send input + start/kill sessions):
datawatch federation peer update peer-alpha \
  --capabilities session-operator

# Grant multiple capabilities (mix groups and individual caps):
datawatch federation peer update peer-alpha \
  --capabilities "session-operator,analytics:read,dashboard:read"
```

### 3. Test connectivity

```bash
datawatch federation peer test peer-alpha
# → {"ok":true,"latency_ms":12,"version":"v7.3.0"}
```

---

## Capability model

### Built-in groups

| Group | Key capabilities |
|---|---|
| `monitor` | health:read, analytics:read, sessions/agents/alerts list |
| `session-viewer` | sessions:list/read, agents:list/read |
| `session-operator` | session-viewer + write/kill/input + pipeline start/cancel |
| `inference-admin` | llms:*, compute:* |
| `config-reader` | config:read, docs:read |
| `config-admin` | config:read/write |
| `analytics-viewer` | analytics:read, dashboard:read, audit:read |
| `autonomous-operator` | autonomous:list/read/write/run |
| `council-operator` | council:list/read/run |
| `federation-peer` | health:read, federation:self |
| `comm-bridge` | sessions:list/read/input, comm:read/write, alerts:list/read |
| `read-only` | all :read/:list caps across every surface |
| `full-control` | all 56 capabilities |

**`federation-peer` is intentionally minimal as of v9.0.0 (SEC-009, breaking
change)** — a newly registered peer can check daemon health and read its own
registered entry (`GET /api/federation/peers/self`, gated on `federation:self`,
distinct from `federation:list`/`federation:read` which can enumerate every
*other* peer too), nothing else. Earlier versions granted sessions/agents/
observers/alerts/dashboard reads and `federation:list/read` by default — if
you're upgrading and a peer integration breaks, it was almost certainly
relying on one of those; grant the specific capability or group it actually
needs (see "Grant specific capabilities" above).

### Individual surface:action capabilities

56 individual capabilities across 20 surfaces:

```
sessions:list   sessions:read   sessions:write  sessions:kill  sessions:input
agents:list     agents:read     agents:spawn     agents:terminate
observers:list  observers:read  observers:write
llms:list       llms:read       llms:write
compute:list    compute:read    compute:write
analytics:read  health:read
config:read     config:write
secrets:list    secrets:read    secrets:write
pipelines:list  pipelines:read  pipelines:start  pipelines:cancel
autonomous:list autonomous:read autonomous:write autonomous:run
council:list    council:read    council:run
federation:list federation:read federation:write federation:self
docs:read       audit:read
comm:read       comm:write
alerts:list     alerts:read
dashboard:read  dashboard:write
queue:read      queue:write
results:list    results:read    results:write
```

`queue:*` and `results:*` were added in v8.39.24 (Design A2 audit) — the
durable work queue (`/api/queue*`) and structured agent result store
(`/api/result-store*`) had no capability check of any kind before then.

---

## Custom capability groups

Create reusable named groups with exactly the capabilities you need:

```bash
# Create a custom group for read-only analytics + session listing:
datawatch federation group add analytics-reader \
  --caps "analytics:read,dashboard:read,sessions:list,health:read"

# Apply it to a peer:
datawatch federation peer update peer-alpha --capabilities analytics-reader

# List all groups (builtins + custom):
datawatch federation group list

# Delete a custom group:
datawatch federation group delete analytics-reader
```

Via REST API:
```bash
curl -X POST http://localhost:8080/api/federation/groups \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"analytics-reader","caps":["analytics:read","dashboard:read","sessions:list","health:read"]}'
```

---

## YAML-seeded federation peers

Declare federation peers in your config file for automatic registration at startup:

```yaml
servers:
  - name: peer-alpha
    url: http://198.51.100.2:8080
    token: tok-peer-alpha
    enabled: true
    federated: true
    auth_type: token
    capabilities:
      - federation-peer
      - analytics:read
```

YAML-seeded peers have `builtin: true` — they cannot be deleted or modified at
runtime (same protection as YAML-seeded plain servers). Runtime-registered peers
override YAML seeds with the same name.

---

## Verify capability enforcement

Test that capability gates work correctly:

```bash
# Peer token with sessions:list should succeed (200):
curl -H "Authorization: Bearer tok-peer-alpha" \
  http://localhost:8080/api/sessions

# Peer token without sessions:write should fail (403):
curl -X POST -H "Authorization: Bearer tok-peer-alpha" \
  http://localhost:8080/api/sessions/start \
  -d '{"profile":"default"}'

# Unknown token should fail (401):
curl -H "Authorization: Bearer bad-token" \
  http://localhost:8080/api/sessions
```

---

## Cross-host session input

A federation peer can send input to a session on your instance. From a remote
peer's perspective, the path format is:

```
POST /api/sessions/<peer_name>/<session_id>/input
{"text": "hello"}
```

The local daemon looks up `<peer_name>` in the server registry, resolves its URL,
and proxies the request with the peer's registered token.

From the comm channel:
```
send peer-alpha/sess-abc: hello world
```

---

## Rotating a peer's token

`GET`/list responses never return a peer's real bearer token — only
`token_present` (a token is set) and `token_prefix` (its first 4 characters,
enough to recognize which token you're looking at, not enough to reconstruct
it). This means you can no longer read a token back out once it's set; keep
the value somewhere you control (a password manager, the secrets vault) when
you first register a peer.

To rotate a peer's token, `PUT` the new value explicitly — you cannot copy a
`GET` response and re-submit it to "refresh" the token, since that response
never contained one:

```bash
curl -X PUT -H "Authorization: Bearer <admin-token>" \
  http://localhost:8080/api/federation/peers/peer-alpha \
  -d '{"token": "new-token-value"}'
```

Any other field update (label, capabilities) that omits `token` from the
request body leaves the existing token untouched — the update handler only
overwrites fields actually present in the request.

---

## Enforcement points (BL316 S1)

| Entry point | Capability required |
|---|---|
| GET /api/sessions | sessions:list |
| POST /api/sessions/start | sessions:write |
| POST /api/sessions/{id}/input | sessions:input |
| DELETE/POST /api/sessions/kill | sessions:kill |
| POST /api/mcp/call | comm:write |
| WebSocket MsgCommand | sessions:input |
| WebSocket MsgNewSession | sessions:write |
| POST /api/federation/peers | federation:write (admin only) |
| GET /api/federation/peers/self | federation:self (in the default group) |

---

## PWA Federation Peers panel

The Observer tab includes a **Federation Peers** card where you can:

- View all registered peers with their URL, enabled status, and capability pills
- Test connectivity (shows latency and remote version)
- Add new peers via a form
- Delete peers

If you are connected with a federated peer token, the Add/Delete buttons will
return 403 — the panel shows a "Read-only (peer token)" message.

---

## MCP tools (BL316 S2)

```
federation_peer_list         — list all registered peers
federation_peer_add          — register a new peer
federation_peer_get          — get one peer by name
federation_peer_update       — update peer config/capabilities
federation_peer_delete       — remove a peer
federation_peer_test         — ping peer /api/health

federation_group_list        — list builtin + custom groups
federation_group_list_builtins — list builtin groups only
federation_group_add         — create a custom group
federation_group_get         — get a group by name
federation_group_update      — update a custom group
federation_group_delete      — delete a custom group
```
