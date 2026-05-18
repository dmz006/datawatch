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
        url: https://100.64.0.1:8443    # primary's tailnet IP
        token: ${secret:PRIMARY_TOKEN}
        shape: B                        # or A / C
```

`datawatch reload` on each.

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
  --primary-url https://100.64.0.1:8443 \
  --name lab-east-peer \
  --shape B
#  → registered; pushing every 5s

# On the PEER — start datawatch-stats pushing to multiple primaries:
datawatch-stats \
  --datawatch https://100.64.0.1:8443,https://100.64.0.2:8443 \
  --insecure-tls

# Back on the PRIMARY:

# 3. Confirm peers are pushing (includes is_self:true entry for local daemon).
datawatch observer peers
#  → (self)           shape:A   is_self:true  ip:local
#    lab-east-peer    shape:B   last_push:3s ago   ip:100.64.0.42
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
10. Cog-nav badge: when any peer goes stale (no push in >60s), the
    gear icon shows a numeric badge. Click → navigates to Federated
    Peers + flashes the offending row.

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
  -d '{"name":"lab-east-peer","shape":"B","url":"https://100.64.0.42:8443"}' \
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
        url: https://100.64.0.1:8443
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
- **Cog badge stuck on N stale.** A registered peer hasn't pushed in
  >60s. Either bring it back or remove the peer record. Click the
  badge in the PWA to navigate to it.

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
- [ ] Cog-icon stale badge with breadcrumb to flashing peer row
- [ ] CLI `datawatch observer peers` output

---

## See also

- [datawatch-definitions](../datawatch-definitions.md)
- [howto/daemon-operations](daemon-operations.md)
- [architecture-overview](../architecture-overview.md)
- [api/observer](../api/observer.md)
