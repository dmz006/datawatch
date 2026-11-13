---
docs:
  index: true
  topics: [observer, federation, peers]
exec_params:
  - {name: name, required: true, description: "Peer name (alphanumeric + dashes)"}
  - {name: shape, required: false, default: "B", description: "A=agent, B=standalone, C=cluster sidecar"}
exec_steps:
  - tool: observer_peers_list
    description: List currently registered peers
    args: {}
    read_only: true
  - tool: observer_peer_register
    description: Mint a bearer token for the new peer
    args:
      name: "{{params.name}}"
      shape: "{{params.shape}}"
    read_only: false
  - tool: observer_peer_stats
    description: Confirm the new peer surfaces in the aggregate view
    args:
      name: "{{params.name}}"
    read_only: true
  - tool: observer_pull_status
    description: Read per-peer observer pull states (pull mode; off by default)
    args: {}
    read_only: true
---
# How-to: Federated observer

Run datawatch on more than one host and see the combined picture from
any one of them — process envelopes, network footprint, agent fleet,
session activity — aggregated through peer push. Useful when you want
one operator surface for a multi-host lab or a primary host plus
remote agent clusters.

## What it is

Each datawatch instance is identified by `name` + `shape`:

- **Shape A** = Agent — a worker pushing into a primary.
- **Shape B** = Standalone — a sibling primary that exchanges with
  others.
- **Shape C** = Cluster — a single representative for a whole
  Kubernetes cluster.

Peers register with each other via `datawatch observer peer-register`
(or auto-register from the agent fleet). Once registered, each pushes
periodic snapshots (process envelopes, system stats, optional memory
summaries) over the same Tailscale mesh used for agent workloads.

### Self-as-peer (v7)

`GET /api/observer/peers` now includes a synthetic entry for the local
daemon itself (when observer is enabled). The entry has `is_self: true`
and `shape: "A"`, so the Federated Peers panel shows the complete host
picture — local + remote — without a second lookup.

### Observer-to-ComputeNode binding (v7)

Each observer peer can be explicitly bound to a Compute Node so that
LLM workloads on that node are attributed to the correct observer:

- `Node.observer_peer` — explicit binding field on a Compute Node.
  Auto-set on first datawatch-stats push via name-match; can be
  overridden manually.
- **Free peers** — peers with no bound Compute Node. Use
  `GET /api/observer/peers/free` to list them.
- **By-node view** — `GET /api/observer/peers/by-node` groups peers by
  their bound CN: `{by_node: {<cn-name>: [...peers]}, unbound: [...]}`.
  The PWA "Group by ComputeNode" toggle uses this endpoint.
- **Federation meta-peers** — `GET /api/federation/meta-peers` shows
  cross-instance observer attribution per node.

### datawatch-stats multi-parent (v7)

`datawatch-stats` can push to multiple daemon instances simultaneously:

```bash
datawatch-stats \
  --datawatch https://primary:8443,https://secondary:8443 \
  --insecure-tls
```

Each parent gets its own peer registration + per-parent token file
(`peer-primary_8443.token`, `peer-secondary_8443.token`). A slow parent
does not delay pushes to others.

## Base requirements

- `datawatch start` — daemon up on each host.
- Tailscale or Headscale mesh established between hosts (see
  [`tailscale-mesh.md`](tailscale-mesh.md)).
- Operator role on each host (peer-register is gated).
- **`observer.peers.allow_register: true`** must be set in `~/.datawatch/datawatch.yaml` on the **primary** host before any peer (including `datawatch-stats` sidecars and agent workers) can register. Without this flag, all inbound peer-register calls are rejected — no error is surfaced on the registering peer; it simply cannot push.

> **Pre-conditions for peer registration**: The primary daemon's config must include `observer.peers.allow_register: true` (see the Setup block below). This flag gates all inbound observer peer registrations. When automating or testing observer peer operations, confirm this flag is present before calling `observer_peer_register` or `compute_node_attach_observer`.

## Setup

On the **primary** (the host you'll log into):

```yaml
# ~/.datawatch/datawatch.yaml
observer:
  enabled: true
  peers:
    allow_register: true               # accept inbound registrations
    push_interval: 5s                  # default push cadence
```

On each **peer** that should push to the primary:

```yaml
observer:
  enabled: true
  peers:
    push_to:
      - name: primary-kona
        url: https://198.51.100.1:8443    # primary's tailnet IP
        token: ${secret:PRIMARY_TOKEN}
        shape: B                        # or A / C
```

`datawatch reload` on each.

> The peer URLs in this guide are placeholders; each node's real URL lives in
> its operator config and is not committed to this repository.

## Two happy paths

### 4a. Happy path — CLI

```sh
# On the PRIMARY:

# 1. Mint a peer-registration token + share with the peer.
datawatch observer peer-token-mint --name lab-east-peer
#  → token: ops_...
#    expires: 2026-05-13T14:00:00Z

# (Manually share with the peer's operator. On the PEER:)

# 2. Register.
datawatch secrets set PRIMARY_TOKEN "ops_..."
datawatch observer peer-register \
  --primary-url https://198.51.100.1:8443 \
  --name lab-east-peer \
  --shape B
#  → registered; pushing every 5s

# On the PEER — start datawatch-stats pushing to multiple primaries:
datawatch-stats \
  --datawatch https://198.51.100.1:8443,https://198.51.100.2:8443 \
  --insecure-tls

# Back on the PRIMARY:

# 3. Confirm peers are pushing (includes is_self:true entry for local daemon).
datawatch observer peers
#  → (self)           shape:A   is_self:true  ip:local
#    lab-east-peer    shape:B   last_push:3s ago   ip:198.51.100.142
#                                compute_node:datawatch-ollama

# 4. View free peers (not yet bound to a Compute Node).
datawatch compute node observer-free

# 5. Bind an observer peer to a Compute Node.
datawatch compute node attach-observer datawatch-ollama lab-east-peer

# 6. View peers grouped by Compute Node.
datawatch compute node observer-by-node
#  → datawatch-ollama: [lab-east-peer]
#    (unbound):        []

# 7. View envelopes from a specific peer.
datawatch observer envelopes --peer lab-east-peer
#  → backend:claude-docker  cpu_pct=4.2  rss_mb=512  ...

# 8. Cross-host envelope view (every peer).
datawatch observer envelopes --all-peers
#  → primary-kona      backend:claude-docker  cpu=2.1  ...
#    lab-east-peer     backend:claude-docker  cpu=4.2  ...

# 9. Remove a peer.
datawatch observer peer-remove lab-east-peer
```

### 4b. Happy path — PWA

1. On the PRIMARY: bottom nav → **Observer** → scroll to **Federated
   Peers** card.
2. The list now always includes a **local self-peer** row (marked
   "(this daemon)") so you see the complete host picture in one table.
3. Empty remote-peers state shows the registration command. Click
   **Mint peer token** → modal with name + shape inputs → **Generate**
   → token copied to clipboard.
4. Hand the token to the peer's operator (private channel).
5. On the peer's PWA: Observer → Federated Peers → **+ Push to
   primary** → paste the primary's URL + token → **Save**.
6. Within ~5s the primary's Federated Peers card shows the new peer
   with a green health dot and the bound Compute Node name (or a
   "free" pill if not yet bound).
7. **Group by ComputeNode toggle** — a toggle in the card header
   switches between flat list and per-CN buckets. Persists in
   localStorage. Each bucket shows the peers observing that node;
   unbound peers appear below.
8. To bind a free peer: Compute Nodes panel → edit a node → select
   from the **Observer Peer** dropdown (pre-populated with free peers
   only).
9. Click into a peer row → drill-down modal: last snapshot, push
   history, per-envelope detail.
10. Peer connectivity: there is no badge on the Settings nav button. A
    peer that last pushed more than a minute ago shows an inline
    "Stale (age)" note on its row in the Observer peer list; a peer that
    has never pushed shows "Never pushed". The daemon's own row is
    skipped. For a longer outage the daemon raises one system alert
    (see "Silent-peer alert" below).

> **Operator review.** Confirm the default `silent_alert_minutes: 5`
> suits your lab, and recapture any Settings-nav screenshots that still
> show the old peer-stale badge. Story TS-852 checks at 390 px that
> nothing covers the Settings nav label.

### Silent-peer alert

The daemon watches every registered observer peer (never its own row).
When a peer has not pushed for longer than
`observer.peers.silent_alert_minutes` it raises a system alert.

- **Setting.** `observer.peers.silent_alert_minutes` is a whole number
  of minutes. The default is 5. `0` turns the watcher off.
- **Once per outage.** The warning alert, "Observer peer silent:
  <name>", names the peer, how long it has been silent and the time of
  its last push. You get it once, not again on every check while the
  peer stays down. A peer that never pushed is measured from its
  registration time and shows "never" as its last push.
- **Recovery alert.** When the peer pushes again, an info alert,
  "Observer peer recovered: <name>", closes the outage. If it goes
  silent again later, that is a new outage and a new alert.
- **Momentary gaps never alert.** A peer that is quiet for less than the
  threshold, such as a late push or a short network blip, raises
  nothing and needs no clean-up.
- **Stale indicator.** Separate from the alert, a peer that last pushed
  more than a minute ago shows an inline "Stale (age)" note on its row
  in the Observer peer list ("Never pushed" if it has not pushed at
  all). The note appears well before the alert fires, so you can spot a
  struggling peer early.

Ways to read or change the setting:

| Access method | How |
|---|---|
| YAML | Set `silent_alert_minutes` under `observer.peers` in `~/.datawatch/config.yaml`. |
| CLI | `datawatch config set observer.peers.silent_alert_minutes <n>` |
| Web UI | Settings, Observer section, "Silent peer alert after (minutes; 0 = disabled)" field. |
| REST | `PUT /api/config` with `observer.peers.silent_alert_minutes`; read it back with `GET /api/config`. |
| MCP | `config_set` with key `observer.peers.silent_alert_minutes`; read it back with `get_config` section `observer`. |
| Comm channel | `configure observer.peers.silent_alert_minutes=<n>` |

## Pull mode

Push mode makes the **peer** send its stats to the primary. Pull mode is
the other direction: the **primary's daemon fetches the peer's v2 stats
on a schedule**, and the peer does nothing — no registration, no push
token, no inbound registration gate on the peer. Pull mode is **off by
default**: it needs the global `observer.pull` block enabled *and* the
individual peer opted in (`observer_pull: true`).

### Push vs pull

| | Push mode | Pull mode |
|---|---|---|
| **Who initiates** | The peer daemon (or its `datawatch-stats` sidecar) pushes into the primary | The primary's daemon pulls `GET /api/observer/stats?v=2` from the peer |
| **Credential** | A peer-minted registration token the peer presents on push (peer stores it in `push_to`) | The peer's bearer token stored in the primary's federation peer store; the primary sends it as `Authorization: Bearer …` on the outbound fetch, and the peer enforces the `observers:read` capability on it |
| **Firewall direction** | Inbound to the primary (the peer must be able to reach it) | Outbound from the primary (the peer just needs its API reachable on the mesh) |
| **Snapshot trigger** | The peer's own push cadence (`push_interval`) | The primary's per-peer ticker: `observer_pull_interval_s` per peer, else the global `observer.pull.default_interval_s` (default **30 s**, minimum **5 s**); each fetch is bounded by the per-peer timeout (`observer_pull_timeout_s`, else `observer.pull.default_timeout_s`, default **5 s**) |
| **Staleness rules** | A peer with no push in >60 s goes stale (cog badge) | Per-peer `stale` flag plus a `stale_reason`: `none` \| `timeout` \| `error` \| `forbidden` (401/403) \| `not_permitted` (peer lacks the `observers:read` capability — no request is even issued) \| `bad_payload` (stats v2 decode failed) \| `unknown` |
| **Shadow state** | A registered peer that never pushed shows as a shadow row: visible in the merged peer list but contributes no data and is excluded from the envelope aggregate | A federation peer with pull off and no push is a shadow row; a peer that is *also* pull-target shows `shadow: true` on its pull row, and appears **once** in the merged peer list |
| **Compute-node binding** | Bindable; `Node.observer_peer` auto-sets by name-match on first push | A peer with pull configured is bindable through the same attach endpoint; a shadow peer (no push, no pull) is refused with `409` |
| **On by default?** | Off until the peer is registered with `allow_register` open on the primary | **Off**: `observer.pull.enabled` defaults to `false` and every peer's `observer_pull` defaults to `false` |

A slow or blocked peer never delays the others: each target pulls in its
own goroutine with its own timeout, and a failed peer is simply marked
stale with a reason until the next fetch succeeds.

### Configuring pull

Global defaults (primary side), `~/.datawatch/datawatch.yaml`:

```yaml
observer:
  pull:
    enabled: true              # off by default — global gate for the pull collector
    default_interval_s: 30     # per-peer fallback (minimum 5)
    default_timeout_s: 5       # per-fetch timeout fallback
```

Per peer (any access method), on the primary:

```bash
# CLI — opt the peer in; interval/timeout 0 = use the global defaults.
datawatch federation peer add lab-west \
  --url https://198.51.100.99:8443 --token <PEER_BEARER_TOKEN> \
  --observer-pull --observer-pull-interval 30 --observer-pull-timeout 5

# or flip an existing peer
datawatch federation peer update lab-west --observer-pull
```

The Web UI's federation peer form has the **Observer pull** opt-in plus
pull interval and pull timeout fields (blank = server default). The REST
surface is `PUT /api/federation/peers/<name>` with `observer_pull`,
`observer_pull_interval_s`, `observer_pull_timeout_s`, and the global
defaults via `PUT /api/config` keys `observer.pull.enabled`,
`observer.pull.default_interval_s`, `observer.pull.default_timeout_s`.

The peer being pulled needs no operator work: it only serves
`GET /api/observer/stats?v=2` (observer enabled) to a token that carries
the `observers:read` capability.

### Five access methods

| Method | Configure pull | Read pull status |
|---|---|---|
| **YAML** | `observer.pull.{enabled,default_interval_s,default_timeout_s}` in `~/.datawatch/datawatch.yaml` + `datawatch reload` | — (status is live state, not config) |
| **CLI** | `datawatch config set observer.pull.*`; `federation peer add/update --observer-pull [--observer-pull-interval N] [--observer-pull-timeout N]` | `datawatch observer pull status` (`--json` for the raw payload) |
| **Web UI** | Federation peer form: **Observer pull** checkbox + interval/timeout fields | Peer rows show a **source** badge (push / pull / shadow) and a stale chip with the reason |
| **REST API** | `PUT /api/config` (`observer.pull.*` keys); `POST`/`PUT /api/federation/peers[/name]` (`observer_pull*` fields) | `GET /api/observer/pull`; the merged `GET /api/observer/peers` rows carry `source`, `shadow` and a `pull` object |
| **Comm channel** | `rest PUT /api/config …` / `rest PUT /api/federation/peers/<name> …` passthrough | `observer pull status` |

### Where to see pull status

| Surface | What you get |
|---|---|
| **Web UI** | Observer → Federated Peers: source badge per row (push / pull / shadow), stale chip with reason; the peer form's pull fields |
| **REST API** | `GET /api/observer/pull` → `{peers: [{name, source: "pull", shadow, stale, stale_reason, last_success_at, interval_s, timeout_s}]}` (empty array when no peer is opted in; bearer tokens are never returned) |
| **Comm channel** | `observer pull status` — the same rows, table-rendered |
| **MCP** | `observer_pull_status` tool (read-only, `observers:read`) — same rows for an LLM or automation |

### For the operator to review

- **Deterministic collector tests — no behavior change.** The pull collector's unit tests used to race the state store: the fake fetcher signalled when `Fetch` returned, but the collector records the result only after that, so a test reading `Last` right after the fetch signal could see the previous value. The tests no longer read it immediately: a helper (`waitForLast`) polls the recorded state every 5 ms inside a loop that is itself bounded by a 5-second deadline, and when the deadline expires it takes one final read of the state so a failing test reports the latest recorded value, not a stale one. The collector tests are now stable. Review flag: this is a test-only change — the collector, its settings, the staleness reasons and all five surfaces above behave exactly as described here, so no config or workflow changes on your side; if the collector suite ever flaps on a heavily loaded machine, re-run it with `env -u TMUX -u TMUX_TMPDIR go test ./internal/observer/ -count=1 -run '^TestObserverPullCollector'` before treating it as a real regression.
- **Pull mode is still not validated live.** The pull rows in the [testing tracker](../testing-tracker.md) remain **Validated: No** — every pull test runs against fakes and stand-in daemons; a two-daemon sandbox pull is still outstanding.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Observer → Federated Peers card. Same mint / register flow + per-peer
drill-down. Useful for monitoring the fleet from your phone.

### 5b. REST

```sh
# List (includes is_self:true entry for local daemon).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/observer/peers
# Each entry now carries compute_node (bound CN name, or "" if free).

# Mint a registration token (primary).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"lab-east-peer","shape":"B"}' \
  $BASE/api/observer/peer-token-mint

# Register (called by the peer).
curl -sk -X POST -H "Authorization: Bearer $PRIMARY_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"lab-east-peer","shape":"B","url":"https://198.51.100.142:8443"}' \
  $PRIMARY_BASE/api/observer/peers

# Remove.
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  $BASE/api/observer/peers/lab-east-peer

# Envelopes.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/observer/envelopes
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/observer/envelopes/all-peers

# Free peers (no bound Compute Node).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/observer/peers/free

# By-node grouping.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/observer/peers/by-node
# → {"by_node":{"datawatch-ollama":[...]}, "unbound":[...]}

# Federation meta-peers (cross-instance attribution).
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/federation/meta-peers

# Bind an observer peer to a Compute Node.
curl -sk -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"peer":"lab-east-peer"}' \
  $BASE/api/compute/nodes/datawatch-ollama/observer-peer

# Detach the binding.
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  $BASE/api/compute/nodes/datawatch-ollama/observer-peer
```

### 5c. MCP

Tools: `observer_peers_list`, `observer_envelopes`,
`observer_envelopes_all_peers`, `observer_peer_register`,
`observer_peer_delete`, `observer_peers_free`, `observer_peers_by_node`,
`observer_pull_status` (pull mode; see the Pull mode section above),
`federation_meta_peers`, `compute_node_attach_observer`,
`compute_node_detach_observer`.

A coordinator LLM running on the primary can use
`observer_envelopes_all_peers` to see global resource availability
before deciding where to spawn the next agent. Use `observer_peers_free`
to find unbound peers that can be attached to a new Compute Node.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `observer peers` | Returns the peer list with health dots (includes local self-peer). |
| `observer envelopes` | Top-level envelopes summary. |
| `observer status` | One-liner across the federation. |
| `compute node attach-observer <node> <peer>` | Bind a peer to a Compute Node. |
| `compute node detach-observer <node>` | Remove the binding. |
| `compute node observer-free` | List free (unbound) observer peers. |
| `compute node observer-by-node` | List peers grouped by Compute Node. |
| `compute node federation-meta-peers` | Cross-instance meta-peers view. |
| `observer pull status` | Per-peer observer pull states (pull mode): source, shadow, stale + reason, last success, interval and timeout. |

### 5e. YAML

`observer.*` block:

```yaml
observer:
  enabled: true
  peers:
    allow_register: true              # primary only
    push_interval: 5s
    push_to:                          # peer-side: where to push
      - name: primary-kona
        url: https://198.51.100.1:8443
        token: ${secret:PRIMARY_TOKEN}
        shape: B
        push_memory_summaries: true   # opt-in: forward memory snippets
```

`datawatch reload` picks up changes; restart only required for major
restructuring.

## Diagram

```
                     ┌────────────────┐
                     │ Primary        │
                     │ (Shape B)      │
                     └────┬───────────┘
                          ▲ push every 5s
                          │
                ┌─────────┴─────────┐
                │                   │
        ┌───────▼─────────┐   ┌─────▼──────┐
        │ Peer (Shape A)  │   │ Peer       │
        │ (agent worker)  │   │ (Shape C)  │
        └─────────────────┘   │ (cluster   │
                              │  rep)      │
                              └────────────┘

   All over the same Tailscale mesh. No public exposure.
```

## Install the datawatch-stats peer

For a host that only runs the `datawatch-stats` observer reporter (no
full daemon), the repo ships a standalone installer:

```bash
bash install/install-stats.sh --datawatch <parent-url>
```

Common options:

| Option | Meaning |
|--------|---------|
| `--datawatch URL` | Parent datawatch base URL (required, or `DATAWATCH_URL` env) |
| `--name NAME` | Stable peer name reported to the parent (default: hostname) |
| `--version <version>` | Pin a specific release instead of fetching the latest |
| `--archive PATH` | Install from a local tarball — no download, no version fetch |
| `--service` | Also install and enable a systemd service (user, or system with `--root`) |
| `--root` | System-wide install: `/usr/local/bin` + system service (needs sudo) |

Without `--service`, the installer prints the exact manual start
command. The bearer token minted by the parent is stored under
`~/.datawatch-stats/peer.token` (or `/var/lib/datawatch-stats/peer.token`
for `--root`) and is never written to stdout or logs.

### Release archives

A release publishes the observer peer as its **own** asset:

```
datawatch-stats_<version>_linux_<amd64|arm64>.tar.gz   # the stats binary
datawatch_<version>_linux_<arch>.tar.gz                # the main daemon archive
```

`datawatch_<version>_linux_<arch>.tar.gz` is the **main daemon**
release, not the stats archive. The installer first looks for the
standalone `datawatch-stats_<version>_linux_<amd64|arm64>.tar.gz`
asset; for older releases that shipped the `datawatch-stats` binary
inside the main archive, it automatically falls back to
`datawatch_<version>_linux_<arch>.tar.gz` and extracts the binary
from there. Either way the result is the same: you always end up
with a working `datawatch-stats`.

### Offline / air-gapped installs

`--archive PATH` skips the download (and the version lookup) and
installs from a tarball you staged on the host — the standalone
stats archive or a legacy main-daemon archive both work, as long as
the `datawatch-stats` binary is inside:

```bash
# On the air-gapped host, after the archive has been copied in:
bash install/install-stats.sh \
  --datawatch https://primary.tailnet.internal:8443 \
  --name edge-observer \
  --archive /tmp/datawatch-stats_<version>_linux_amd64.tar.gz
```

> **Note**: the archive must actually contain the `datawatch-stats`
> binary; the installer errors out otherwise. For a fully offline pin,
> combine `--archive` with `--version <version>` if the filename does
> not already encode it.

## Common pitfalls

- **Peer can't reach primary.** Mesh is up but `push_to.url` wrong
  (probably `127.0.0.1` instead of the tailnet IP). Use the primary's
  100.x.y.z address.
- **Token expired.** Peer-mint tokens default to 7-day TTL. Mint a
  fresh one + re-register on the peer.
- **Stale peer in the list.** Peer crashed; daemon restarted but the
  push config got removed. Either re-register on the peer or
  `observer peer-remove` on the primary to clean up.
- **Memory summaries not flowing.** Opt-in: `push_memory_summaries:
  true` on the peer. Off by default to avoid leaking content across
  trust boundaries.
- **Peer row shows "Stale" or "Never pushed".** A registered peer
  hasn't pushed in over a minute (or ever). Either bring it back or
  remove the peer record. The Settings nav button no longer carries a
  badge; look at the peer row in the Observer peer list.
- **"Observer peer silent" alert.** The peer was silent longer than
  `observer.peers.silent_alert_minutes`. One alert per outage; an
  "Observer peer recovered" alert follows when it pushes again.

## Linked references

- See also: [`tailscale-mesh.md`](tailscale-mesh.md) — mesh setup.
- See also: [`secrets-manager.md`](secrets-manager.md) — token storage.
- See also: [`cross-agent-memory.md`](cross-agent-memory.md) — memory federation.
- Architecture: `../architecture-overview.md` § Observer + federation.

## Screenshots needed (operator weekend pass)

- [ ] Observer → Federated Peers card with mixed health dots
- [ ] Mint peer token modal
- [ ] Peer drill-down with last snapshot
- [ ] Cross-host envelope view
- [ ] Peer row with the inline "Stale (age)" and "Never pushed" notes
- [ ] CLI `datawatch observer peers` output

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/daemon-operations](daemon-operations.md)
- [architecture-overview](../architecture-overview.md)
- [api/observer](../api/observer.md)
