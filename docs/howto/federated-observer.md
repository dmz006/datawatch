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

## Base requirements

- `datawatch start` — daemon up on each host.
- Tailscale or Headscale mesh established between hosts (see
  [`tailscale-mesh.md`](tailscale-mesh.md)).
- Operator role on each host (peer-register is gated).

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

# Back on the PRIMARY:

# 3. Confirm peer is pushing.
datawatch observer peers
#  → lab-east-peer    shape:B   last_push:3s ago   ip:100.64.0.42

# 4. View envelopes from a specific peer.
datawatch observer envelopes --peer lab-east-peer
#  → backend:claude-docker  cpu_pct=4.2  rss_mb=512  ...

# 5. Cross-host envelope view (every peer).
datawatch observer envelopes --all-peers
#  → primary-kona      backend:claude-docker  cpu=2.1  ...
#    lab-east-peer     backend:claude-docker  cpu=4.2  ...

# 6. Remove a peer (rotates the token; the peer auto-re-registers
#    if it's still alive).
datawatch observer peer-remove lab-east-peer
```

### 4b. Happy path — PWA

1. On the PRIMARY: bottom nav → **Observer** → scroll to **Federated
   Peers** card.
2. Empty state shows the registration command. Click **Mint peer
   token** → modal with name + shape inputs → **Generate** → token
   copied to clipboard.
3. Hand the token to the peer's operator (private channel).
4. On the peer's PWA: Observer → Federated Peers → **+ Push to
   primary** → paste the primary's URL + token → **Save**.
5. Within ~5s the primary's Federated Peers card shows the new peer
   with a green health dot.
6. Click into a peer row → drill-down modal: last snapshot, push
   history, per-envelope detail.
7. Cog-nav badge: when any peer goes stale (no push in >60s), the
   gear icon shows a numeric badge. Click → navigates to Federated
   Peers + flashes the offending row.

## Other channels

### 5a. Mobile (Compose Multiplatform)

Observer → Federated Peers card. Same mint / register flow + per-peer
drill-down. Useful for monitoring the fleet from your phone.

### 5b. REST

```sh
# List.
curl -sk -H "Authorization: Bearer $TOKEN" $BASE/api/observer/peers

# Mint a registration token (primary).
curl -sk -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"name":"lab-east-peer","shape":"B"}' \
  $BASE/api/observer/peer-token-mint

# Register (called by the peer).
curl -sk -X POST -H "Authorization: Bearer $PRIMARY_TOKEN" \
  -d '{"name":"lab-east-peer","shape":"B","url":"https://100.64.0.42:8443"}' \
  $PRIMARY_BASE/api/observer/peers

# Remove.
curl -sk -X DELETE -H "Authorization: Bearer $TOKEN" \
  $BASE/api/observer/peers/lab-east-peer

# Envelopes.
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/observer/envelopes
curl -sk -H "Authorization: Bearer $TOKEN" \
  $BASE/api/observer/envelopes/all-peers
```

### 5c. MCP

Tools: `observer_peers`, `observer_envelopes`,
`observer_envelopes_all_peers`, `observer_peer_register`,
`observer_peer_remove`.

A coordinator LLM running on the primary can use
`observer_envelopes_all_peers` to see global resource availability
before deciding where to spawn the next agent.

### 5d. Comm channel

| Verb | Example |
|---|---|
| `observer peers` | Returns the peer list with health dots. |
| `observer envelopes` | Top-level envelopes summary. |
| `observer status` | One-liner across the federation. |

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
