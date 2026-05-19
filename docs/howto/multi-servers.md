---
docs:
  index: true
  topics: [multi-server, federation, remote-servers, pwa, aggregation, proxy]
exec_params:
  - {name: server_name, required: false, description: "Short name for the remote server (e.g. nas, prod)"}
  - {name: server_url, required: false, description: "Base URL of the remote datawatch instance (e.g. http://192.168.1.100:8080)"}
exec_steps:
  - tool: mcp__datawatch__get_version
    description: Check local daemon version
    args: {}
    read_only: true
  - tool: mcp__datawatch__backends_list
    description: List all configured backends on this instance
    args: {}
    read_only: true
---
# How-to: Multi-Server Management (BL312)

Datawatch can manage and monitor multiple remote datawatch instances from a
single PWA session. You register remote servers, then switch views between
them — or select "All" to see aggregated data from every server at once.

## What it is

Five surfaces added in v7.2.0:

| Surface | What was added |
|---------|---------------|
| PWA Settings → Comms → Servers | Add / edit / delete / test remote server connections |
| PWA per-tab picker | Server chip bar on Sessions, Alerts, Automata, Observer, Dashboard |
| REST | `POST/GET/PUT/DELETE /api/servers/{name}`, `POST /api/servers/{name}/test` |
| MCP | `server_list`, `server_add`, `server_delete`, `server_test` tools |
| CLI | `datawatch server {list,add,delete,test}` |

## Base requirements

- `datawatch start` — daemon up.
- Network reachability to each remote datawatch instance.
- Token credentials for remote instances stored as secrets (use `${secret:NAME}` in server config). See [secrets-manager.md](secrets-manager.md).

> **Note**: Multi-server is distinct from Federated Observer. For peer-to-peer metrics and memory sharing, see [federated-observer.md](federated-observer.md).

## Relationship to Federated Observer

**Multi-server** and **Federated Observer** are complementary but distinct:

| | Multi-server (BL312) | Federated Observer |
|-|----------------------|-------------------|
| **Direction** | This PWA actively queries remotes | Remotes push stats to this daemon |
| **Surface** | Sessions / Alerts / Automata / Observer / Dashboard | Observer view → Federated Peers card |
| **Use case** | Operate across hosts from one browser tab | Aggregate process/GPU/net telemetry |
| **Auth** | Bearer token per server | Push token per peer |

You can use both simultaneously: register a server for per-tab switching AND
configure it as a federated peer for push stats.

## Register a remote server

### PWA

1. Open **Settings → Comms → Servers**.
2. Click **+ Add server**.
3. Fill in **Name** (short slug, e.g. `nas`), **URL** (base URL including port,
   e.g. `http://192.168.1.100:8080`), and **Bearer token**.
4. Click **Test** to verify connectivity — a green toast confirms the version.
5. Click **Save**.

The server appears in the list immediately and is available in the per-tab
picker on all views.

### REST

```bash
# Create
curl -X POST https://localhost:8443/api/servers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"nas","url":"http://192.168.1.100:8080","token":"remote-token","enabled":true}'

# List
curl https://localhost:8443/api/servers -H "Authorization: Bearer $TOKEN"

# Get single
curl https://localhost:8443/api/servers/nas -H "Authorization: Bearer $TOKEN"

# Update
curl -X PUT https://localhost:8443/api/servers/nas \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"nas","url":"http://192.168.1.100:8080","token":"new-token","enabled":true}'

# Test connectivity
curl -X POST https://localhost:8443/api/servers/nas/test \
  -H "Authorization: Bearer $TOKEN"

# Delete
curl -X DELETE https://localhost:8443/api/servers/nas \
  -H "Authorization: Bearer $TOKEN"
```

### MCP

```
server_list                       # list all registered servers
server_add name=nas url=http://... token=xxx
server_test name=nas
server_delete name=nas
```

### CLI

```bash
datawatch server list
datawatch server add --name nas --url http://192.168.1.100:8080 --token xxx
datawatch server test nas
datawatch server delete nas
```

### YAML (datawatch.yaml)

YAML-seeded servers are read-only in the UI (marked `builtin: true`). Use
them to seed servers at deploy time:

```yaml
servers:
  - name: nas
    url: http://192.168.1.100:8080
    token: ${secret:nas-token}
    enabled: true
```

`${secret:nas-token}` resolves from the secrets store at startup so you don't
store raw tokens in config files.

## Switch between servers (per-tab picker)

Every main view (Sessions, Alerts, Automata, Observer, Dashboard) has a
**server chip bar** injected at the top. The chips are:

| Chip | Behaviour |
|------|-----------|
| **All** | Fetch from all servers via aggregated endpoints; WebSocket stays on local |
| **Local** | Show only this daemon's data (default) |
| **\<name\>** | Proxy to the named remote server; REST calls go via `/api/proxy/{name}/...` |

The active chip persists in `state.activeServer` for the duration of the session.

## All-servers (aggregated) mode

Selecting **All** uses two dedicated endpoints that fan out in parallel:

| View | Aggregated endpoint |
|------|---------------------|
| Sessions | `GET /api/sessions/aggregated` |
| Alerts | `GET /api/alerts/aggregated` |
| Automata | `GET /api/autonomous/prds/aggregated` |

Each item in the response carries a `server` field (e.g. `"server":"nas"`)
identifying its origin. The PWA renders a **[nas]** tag on each card.

Observer and Dashboard views use the picker to switch — they don't have a
separate "All" aggregation; "All" falls back to local data on those views.

## Proxy mode (specific server)

When you select a named server chip, the PWA routes through the local
daemon's proxy:

```
GET /api/sessions  →  GET /api/proxy/{name}/sessions  →  remote /api/sessions
WS  /ws            →  WS  /api/proxy/{name}/ws         →  remote /ws
```

The proxy authenticates with the remote server's stored bearer token —
you never expose the remote token to the browser directly.

Runtime-registered servers (added via REST/MCP/CLI) are available for proxy
routing immediately. YAML-seeded servers are also proxy-reachable.

## Troubleshooting

**Test button returns red / error toast**
- Verify the URL is reachable from the browser (not just from the daemon host).
- Check that the remote bearer token matches what the remote daemon is configured with.
- If TLS: ensure the remote's CA cert is trusted by the browser.

**[remote] badge missing on aggregated cards**
- The remote server may be offline; the aggregated endpoint skips unreachable
  servers and returns only what succeeded.

**Proxy 502 / 504**
- The daemon can reach the remote (test passes) but a specific API call is
  failing. Check the remote daemon's log: `datawatch logs --tail 50`.

**YAML-seeded servers show "read-only"**
- This is intentional. Builtin entries can't be deleted via the UI; remove
  them from `datawatch.yaml` and restart.

## See also

- [`howto/federated-observer.md`](federated-observer.md) — push-based peer stats aggregation
- [`howto/secrets-manager.md`](secrets-manager.md) — store remote bearer tokens as `${secret:...}` refs
- [`howto/daemon-operations.md`](daemon-operations.md) — start / configure remote instances
- [`architecture-overview.md`](../architecture-overview.md) — how proxy routing and server store fit together
