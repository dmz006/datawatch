---
docs:
  index: true
  topics: [multi-server, federation, remote-servers, pwa, aggregation, proxy, "header dropdown", "switching servers", "Showing:"]
exec_params:
  - {name: server_name, required: false, description: "Short name for the remote server (e.g. nas, prod)"}
  - {name: server_url, required: false, description: "Base URL of the remote datawatch instance (e.g. http://192.0.2.136:8080)"}
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
# How-to: Multi-Server Management

Datawatch can manage and monitor multiple remote datawatch instances from a
single PWA session. You register remote servers, then switch views between
them — or select "All" to see aggregated data from every server at once.

## What it is

Five surfaces added in v7.2.0:

| Surface | What was added |
|---------|---------------|
| PWA Settings → Comms → Servers | Add / edit / delete / test remote server connections |
| PWA header dropdown | One server selector in the fixed app header, present on every page |
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

| | Multi-server | Federated Observer |
|-|----------------------|-------------------|
| **Direction** | This PWA actively queries remotes | Remotes push stats to this daemon |
| **Surface** | Sessions / Alerts / Automata / Observer / Dashboard | Observer view → Federated Peers card |
| **Use case** | Operate across hosts from one browser tab | Aggregate process/GPU/net telemetry |
| **Auth** | Bearer token per server | Push token per peer |

You can use both simultaneously: register a server for picking it in the
header dropdown AND
configure it as a federated peer for push stats.

## Register a remote server

### PWA

1. Open **Settings → Comms → Servers**.
2. Click **+ Add server**.
3. Fill in **Name** (short slug, e.g. `nas`), **URL** (base URL including port,
   e.g. `http://192.0.2.136:8080`), and **Bearer token**.
   The example URL is a placeholder; the real URL for a given server lives in
   your operator config and is not committed to this repository.
4. Click **Test** to verify connectivity — a green toast confirms the version.
5. Click **Save**.

The server appears in the list immediately and is available in the
header dropdown on every view.

### REST

```bash
# Create
curl -X POST https://localhost:8443/api/servers \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"nas","url":"http://192.0.2.136:8080","token":"remote-token","enabled":true}'

# List
curl https://localhost:8443/api/servers -H "Authorization: Bearer $TOKEN"

# Get single
curl https://localhost:8443/api/servers/nas -H "Authorization: Bearer $TOKEN"

# Update
curl -X PUT https://localhost:8443/api/servers/nas \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"nas","url":"http://192.0.2.136:8080","token":"new-token","enabled":true}'

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
datawatch server add --name nas --url http://192.0.2.136:8080 --token xxx
datawatch server test nas
datawatch server delete nas
```

### YAML (datawatch.yaml)

YAML-seeded servers are read-only in the UI (marked `builtin: true`). Use
them to seed servers at deploy time:

```yaml
servers:
  - name: nas
    url: http://192.0.2.136:8080
    token: ${secret:nas-token}
    enabled: true
```

`${secret:nas-token}` resolves from the secrets store at startup so you don't
store raw tokens in config files.

## Switch between servers (header dropdown)

Every page has a **server dropdown** in the fixed app header — a native
`\<select\>` with `id="serverDropdown"`; on a phone it truncates long names
with an ellipsis rather than wrapping a row of chips. The entries are:

| Entry | Behaviour |
|------|-----------|
| **All servers** | Fetch from all servers via aggregated endpoints; WebSocket stays on local |
| **This server** (labelled with this daemon's real hostname once `/api/health` resolves it; the generic **This server** word until then — never the word "Local") | Show only this daemon's data (default) |
| **\<name\>** | Proxy to the named remote server; REST calls go via `/api/proxy/{name}/...` |

The dropdown is shared by all pages, so **All servers** can be chosen
anywhere — but the **Observer** and **Settings** views are the two that have
no "all servers" form: while **All servers** is the active entry they are
deliberately not re-rendered, so they keep showing the single server whose
data they last rendered (operator decision, 2026-10-07).

The active entry persists in `state.activeServer` for the duration of the
session, and the dropdown follows you as you navigate between pages.

### Switch in place

Selecting an entry acts on the page you are on, immediately. Choosing a
federated host re-renders the tab you are currently looking at — Sessions,
Alerts, Automata, Observer, Settings or Dashboard — in place, against that
server:
the **Showing: \<name\>** line above the view updates to the host you
picked and the list/content becomes that host's data, without you leaving
and re-opening the view. Picking **This server** or **All servers** takes the same page
back to that view the same way.

On the **Dashboard** the switch is per panel rather than one list: each
panel re-fetches for the active host. Only the **Automata** and **Cost**
panels aggregate under **All servers** (`/api/autonomous/prds/aggregated`,
`/api/cost/aggregated`); the other panels have no aggregated variant.

> **Confirm this on your setup before relying on it.** The in-place switch
> is recorded under **[Unreleased] → Fixed** in the
> [changelog](../../CHANGELOG.md) (found 2026-11-08), and its entry records
> that no live verification had been run when it was recorded. It is
> implemented in this tree's `internal/server/web/app.js` — `selectServer`
> re-renders the tab you're on, and the **Automata** re-render spans the
> whole tab, **Automation** sub-tab included — with PWA tests for the
> re-render. Confirm it live on a two-host setup; the full checklist is in
> [operations.md](../operations.md), under "Still to review (operator)"
> in the multi-server section.

Behaviours worth knowing:

- **Re-selecting the active server is a no-op.** Choosing the entry that is
  already active does not reload the view, refetch data or reset scroll
  position and filters.
- **A real switch clears the previous server's data first.** On any
  genuine picker switch (not a no-op re-selection), the old server's
  session list is dropped before the view redraws — the tab cannot keep
  showing the previous server's sessions. Sub-pages (session detail, the
  New session form) are not re-rendered by a switch; they keep their
  contents until you leave them.
- **\<label\> (unreachable).** A peer whose background reachability check
  failed (a 4-second-capped `/api/proxy/\<name\>/api/health` probe) keeps its
  entry, suffixed ** (unreachable)** — the entry stays listed and selectable,
  and choosing it is exactly how the authoritative error surfaces. The marker
  patches the single entry in place (it never re-renders the whole dropdown
  mid-interaction).
- **Back to \<hostname\>.** Extra one-tap ways to return to local data
  (where \<hostname\> is this daemon's own name), in addition to the
  always-present **This server** entry — they all perform the same "go back
to
  local" action:
  - the error panel, when a remote server cannot be reached and no
    sessions are loaded, offers an explicit **Back to \<hostname\>**
    button;
  - a clickable **🌐 \<name\>** indicator on the **Sessions toolbar** —
    it appears while a remote server is the active one and clicking it
    returns you to local data; it is the `.server-indicator` chip in
    `renderSessionsView` in `internal/server/web/app.js`
    (`onclick="selectServer(null)"`), covered by the PWA test
    `internal/server/web/app-select-server-controls.test.js`;
  - on the **Servers card** under Settings → Comms → Remote Servers, the
    row for the active remote server carries a **Back to \<hostname\>**
    button next to its Select/Connected button.
- **Showing: \<name\> header line.** Every page carries a
  **Showing: \<name\>** line above the view naming the server whose data
  is on screen: the server's hostname, or **All servers** in all-servers
  mode.

## All-servers (aggregated) mode

Selecting **All** uses two dedicated endpoints that fan out in parallel:

| View | Aggregated endpoint |
|------|---------------------|
| Sessions | `GET /api/sessions/aggregated` |
| Alerts | `GET /api/alerts/aggregated` |
| Automata | `GET /api/autonomous/prds/aggregated` |

Each item in the response carries a `server` field (e.g. `"server":"nas"`)
identifying its origin. The PWA renders a **[nas]** tag on each card.

The **Observer** and **Settings** views are the two for which selecting
**All servers** has no effect — they have no aggregated variant, so the
in-place re-render is deliberately skipped for them and they keep showing
the single server whose data they last rendered (operator decision,
2026-10-07). On the Dashboard, **All servers** aggregates per panel:
the Automata and Cost panels use the aggregated endpoints above
(`/api/autonomous/prds/aggregated`, `/api/cost/aggregated`); the other
Dashboard panels have no aggregated variant and show the selected server's
data.

## Proxy mode (specific server)

When you select a named server in the header dropdown, the PWA routes
through the local
daemon's proxy:

```
GET /api/sessions  →  GET /api/proxy/{name}/sessions  →  remote /api/sessions
WS  /ws            →  WS  /api/proxy/{name}/ws         →  remote /ws
```

The proxy authenticates with the remote server's stored bearer token —
you never expose the remote token to the browser directly.

Runtime-registered servers (added via REST/MCP/CLI) are available for proxy
routing immediately. YAML-seeded servers are also proxy-reachable.

## Version skew between a primary and its peers

A federated peer's daemon can run a different release than the daemon
serving this page (the primary). The PWA makes that difference visible
without changing behavior:

- **Version-skew note on the About page.** While a peer is selected,
  **Settings → About** reads the version from the *peer's* daemon (through
  the local proxy). If the peer reports a different build than the primary,
  an informational note appears under the version line: "This server is
  running vX, a different release than vY, the one serving this page.
  Behavior is unchanged and no reload is needed." The note is hidden while
  the two builds match or while you are looking at the local server.
- **Switching servers never reloads the page.** Each server's reported
  version is recorded under that server's own key. A version seen for the
  first time on a given server is only recorded — it is never read as an
  update. So selecting a peer whose build differs from the primary's, and
  switching back to local, fire no reload and no prompt (the page itself is
  only ever served by the primary; nothing about the switch reloads it).
- **When the update prompt appears.** The "Update available — tap to
  reload" banner (and the automatic reload that follows once you are
  idle), shown at most once per version change, fires when the selected
  server reports a *different* build than the one last recorded for *that
  server*. That can be the primary reporting a newer build while you are
  on it, or a selected peer whose own daemon changed builds while that
  peer was the active server. A peer that merely runs a different release
  from the primary does not trigger it: only a version change attributed
  to that server does.
- **What the `Showing: <server>` line means.** The About, General,
  Plugins, Comms, Compute and Automata settings tabs each carry a
  "Showing:" line. It names the daemon whose data those tabs actually
  display: the peer's name when a peer is selected, the local hostname when
  you are viewing the local server. When it shows a peer's name, the rows
  below it — version, plugins, config, comms, compute, automata — describe
  *that* peer, not your local daemon.

What to do: keep the status quo and align the versions on your own
schedule. Both daemons keep working across the release difference, so the
skew note is informational rather than an error. When it suits your
rollout, upgrade whichever side is older — the peer to the primary's
release, or the primary forward — and the note disappears the next time
the About tab sees two matching builds.

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
