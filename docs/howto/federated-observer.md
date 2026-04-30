# How-to: Federated observer (multi-host stats + envelope tree)

Run datawatch on more than one box and see the combined picture
(CPU / memory / GPU / sessions / per-process envelopes) in one PWA.
Each box runs its own daemon; one of them is the **primary**, the
others register as **peers** and push snapshots upstream.

## When to use this

- You're developing on a workstation and running real workers in a
  k8s testing cluster.
- You want one place to see "session X on host A is hitting ollama
  on host B".
- You want one PWA, one set of credentials, federated stats.

Three peer shapes, all wire-compatible:

| Shape | Where it runs | Best for |
|-------|---------------|---------|
| **A** | Inside the parent daemon as a Go plugin | Same-host visibility (default) |
| **B** | Standalone `datawatch-stats` Linux binary | Headless servers / VMs / minimal footprint |
| **C** | Container (`ghcr.io/dmz006/datawatch-stats-cluster`) | Kubernetes / Docker hosts |

## 1. Pick a primary

The primary is whichever daemon you'll point your PWA at. No
special config — every datawatch daemon can act as primary.

Verify it's reachable from the boxes that will become peers:

```bash
curl -sk https://primary.local:8443/api/version
#  → {"version":"5.x.y","host":"primary.local"}
```

Open `observer.peers.allow_register: true` on the primary (default
in v5.x):

```bash
datawatch config set observer.peers.allow_register true
datawatch reload
```

## 2. Add a Shape A peer (in-process)

Already on by default — every daemon includes the Shape A plugin.
Nothing to do.

```bash
datawatch observer peer list
#  → (no peers yet — Shape A is local, not registered as a peer)
```

## 3. Add a Shape B peer (standalone Linux binary)

First, on the **primary**, mint a peer token (this is the only time
the cleartext token is shown):

```bash
datawatch observer peer register workstation-2 B
#  → name: workstation-2
#    shape: B
#    token: dwp_…   (capture this NOW — only chance)
```

On the peer box:

```bash
# Install datawatch-stats (same-binary path as datawatch)
curl -L -o ~/.local/bin/datawatch-stats \
  https://github.com/dmz006/datawatch/releases/latest/download/datawatch-stats-linux-amd64
chmod +x ~/.local/bin/datawatch-stats

# Persist the token (datawatch-stats reads --token-file or $HOME/.datawatch-stats/peer.token)
mkdir -p ~/.datawatch-stats && echo "dwp_…" > ~/.datawatch-stats/peer.token
chmod 600 ~/.datawatch-stats/peer.token

# Point at the primary; --name is how it shows up in the PWA
datawatch-stats --datawatch https://primary.local:8443 \
  --name workstation-2 \
  --token-file ~/.datawatch-stats/peer.token \
  --shape B &
```

The peer registers, pushes a snapshot every `peer_push_seconds`
(5 s default), and shows up under Settings → Monitor → Federated
peers in the primary's PWA.

## 4. Add a Shape C peer (container / Kubernetes)

Mint the token on the primary as in step 3 (`datawatch observer
peer register k8s-cluster-prod C`), then:

```bash
docker pull ghcr.io/dmz006/datawatch-stats-cluster:latest
docker run -d --name datawatch-stats \
  -e DATAWATCH_PARENT=https://primary.local:8443 \
  -e DATAWATCH_PEER_NAME=k8s-cluster-prod \
  -e DATAWATCH_PEER_TOKEN=dwp_… \
  --pid=host --net=host --privileged \
  ghcr.io/dmz006/datawatch-stats-cluster:latest
```

For a Kubernetes Daemonset, see [`docs/operations.md`](../operations.md)
or the example manifest in `docker/k8s/`.

## 5. Verify the federation

```bash
datawatch observer peer list
#  → workstation-2     reachable=true   last_push=2s ago   shape=daemon
#    k8s-cluster-prod  reachable=true   last_push=4s ago   shape=cluster

curl -sk https://primary.local:8443/api/observer/peers/workstation-2/stats | jq '.host.name, .cpu.pct, .sessions.total'
#  → "workstation-2"
#    24.5
#    3
```

In the PWA: Settings → Monitor → Federated peers shows a card per
peer with its CPU / memory / GPU sparklines and a click-through to
its envelope tree:

![Settings → Monitor — System Statistics + federated peers](screenshots/settings-monitor.png)

The same dashboard on a phone — every panel reflows to the narrow
viewport so the federation view works wherever you happen to be:

![Settings → Monitor — mobile viewport](screenshots/settings-monitor-mobile.png)

## 6. Per-caller attribution across hosts

This is the cross-host piece of BL180 Phase 2 — federation-aware
join of the `Envelope.Caller` field. **Status: open** — until it
ships, attribution is per-host (a session on host A talking to
ollama on host B sees `Caller=""` on the ollama envelope).

When it lands you'll see entries like:

```
ollama backend (k8s-cluster-prod):
  Callers:
    session:opencode-x1y2 (workstation-2)   60%
    session:openwebui-c3d4 (workstation-2)  40%
```

The PWA exposes this through the **↔ Cross-host view** button on
the Federated peers card (Settings → Monitor). The modal collapses
local + every peer into one screen with cross-host attributions
flagged 🔗 cross:

![Cross-host envelope view — local + peer envelopes with cross-host Caller rows](screenshots/cross-host-modal.png)

Reachable via REST too: `GET /api/observer/envelopes/all-peers`,
MCP `observer_envelopes_all_peers`, CLI `datawatch observer envelopes-all-peers`.

## Token rotation

Peer tokens auto-rotate every `observer.peers.token_ttl_seconds`
(default 1 h) with a `token_ttl_rotation_grace_s` overlap (default
60 s) so peers don't drop a snapshot during the rotation. Operators
typically don't touch this.

To force-revoke a peer (de-registers + rotates the token; the peer
will auto-re-register on its next push if still running):

```bash
datawatch observer peer delete workstation-2
```

## Reachability across channels

| Channel | Action | Command |
|---------|--------|---------|
| CLI | configure primary | `datawatch config set observer.peers.allow_register true` |
| CLI | mint peer token | `datawatch observer peer register <name> <shape>` |
| CLI | run Shape B peer | `datawatch-stats --datawatch <url> --name <peer> --token-file <path> --shape B` |
| CLI | list peers | `datawatch observer peer list` |
| CLI | revoke peer | `datawatch observer peer delete <peer>` |
| REST | configure | `PUT /api/config` (`observer.peers.*` block) |
| REST | per-peer stats | `GET /api/observer/peers/{name}/stats` |
| REST | revoke | `DELETE /api/observer/peers/{name}` |
| MCP | per-peer stats | `observer_peer_stats` |
| Chat | (no chat verbs yet — REST + CLI cover it) | — |
| PWA | observe | Settings → Monitor → Federated peers (sparklines + drill-down) |

## Production-cluster reachability check (BL173-followup)

The cluster→parent push handler (`handlePeerPush` in
`internal/server/observer_peers.go`) is exercised end-to-end on every
release by `scripts/release-smoke.sh` section 8 — register peer →
push snapshot → confirm aggregator returns the new peer. The smoke
runs against the local daemon and consistently passes.

What's still operator-side: confirming the **network path** from a
production-cluster pod to the parent daemon. The dev-workstation
parent isn't reachable from the testing-cluster pod overlay (NAT /
overlay-routing gap), so the testing cluster only verifies the
handler shape, not the live wire.

When you have a production cluster handy, run from inside one of
its pods to verify reachability + auth:

```bash
# 1. From the parent host: mint a peer token
datawatch observer peer register prod-pod-test C

# 2. From the prod-cluster pod (with the token plumbed in via Secret):
curl -sk -X POST \
  -H "Authorization: Bearer $DW_TOKEN" \
  -H "Content-Type: application/json" \
  https://<parent-host>:8443/api/observer/peers/prod-pod-test/stats \
  -d '{"hostname":"prod-pod-test","timestamp":"'"$(date -Iseconds)"'","cpu_percent":12.3,"memory_percent":34.5}'
# Expect: {"ok":true}

# 3. Back on the parent: confirm the peer appears in the aggregate
curl -sk -H "Authorization: Bearer $DW_TOKEN" \
  https://localhost:8443/api/observer/aggregate | jq '.peers[].name'
# Expect: includes "prod-pod-test"

# 4. Cleanup
datawatch observer peer delete prod-pod-test
```

If step 2 fails with a connection error, the gap is networking
(firewall / Service / NodePort / overlay routing); if it fails with
401/403, the gap is auth/token plumbing. Both are deploy-side
concerns, not daemon code.

## See also

- [How-to: Container workers](container-workers.md) — every spawned worker auto-peers as a Shape A
- [`docs/architecture-overview.md`](../architecture-overview.md) — the Shape A/B/C diagram
- [`docs/api/observer.md`](../api/observer.md) — full envelope + peer reference
- [`docs/operations.md`](../operations.md) — production deploy patterns + systemd units
