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
# Default capabilities: ["federation-peer"] — safe read + session input.
datawatch federation peer add peer-alpha \
  --url http://10.0.0.2:8080 \
  --token tok-peer-alpha \
  --capabilities federation-peer

# Verify it was registered:
datawatch federation peer list
```

Via comm channel (Telegram, Signal, etc.):
```
federation peer add peer-alpha http://10.0.0.2:8080 token=tok-peer-alpha
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
| `federation-peer` | health:read, sessions:list/read/input, agents:list/read, observers:list/read, alerts:list/read, dashboard:read, federation:list/read |
| `comm-bridge` | sessions:list/read/input, comm:read/write, alerts:list/read |
| `read-only` | all :read/:list caps across every surface |
| `full-control` | all 50 capabilities |

The `federation-peer` group is the safe default for new peers. It intentionally
excludes `sessions:write`, `secrets:*`, and `federation:write`.

### Individual surface:action capabilities

50 individual capabilities across 18 surfaces:

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
federation:list federation:read federation:write
docs:read       audit:read
comm:read       comm:write
alerts:list     alerts:read
dashboard:read  dashboard:write
```

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
    url: http://10.0.0.2:8080
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
