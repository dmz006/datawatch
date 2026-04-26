# datawatch v5.12.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.11.0 → v5.12.0
**Closed:** BL180 Phase 2 cross-host federation correlation (Q5c)

## What's new

### BL180 Phase 2 — Cross-host federation correlation

Operator answer to design Q5c: *"don't close until cross-host works"*.
v5.12.0 ships the cross-host half. The eBPF kprobe Q1 stays open as
a separate cycle (was backed out cleanly in v5.6.0; will resume with
`BPF_MAP_TYPE_LRU_HASH` + userspace TTL pruner).

Before v5.12.0:

```
host A: session opencode-x1y2 ──[outbound TCP to 10.0.0.7:11434]──> ???
host B: backend ollama on 10.0.0.7:11434                           Caller=""
```

After v5.12.0:

```
host A: session opencode-x1y2 ──[outbound TCP to 10.0.0.7:11434]──┐
                                                                   │
host B: backend ollama on 10.0.0.7:11434                           ├─> federation
        Callers: [{caller: "workstation-2:session:opencode-x1y2",  │   aggregator joins
                   conns: 3, kind: session}]                       ┘   listen ↔ outbound
```

### Models

```go
// New fields on Envelope:
ListenAddrs   []ListenAddr   // (IP, Port) tuples this envelope listens on
OutboundEdges []OutboundEdge // (target_ip, target_port, pid, conns) outbound TCP candidates

type ListenAddr struct { IP string; Port uint16 }
type OutboundEdge struct { TargetIP string; TargetPort uint16; PID int; Conns int }
```

The local procfs correlator (`internal/observer/conn_correlator.go`)
now populates these on every tick alongside the existing same-host
`Callers []CallerAttribution`.

### CorrelateAcrossPeers

`internal/observer/conn_correlator.go.CorrelateAcrossPeers(byPeer
map[string][]Envelope, localPeerName string)` is the federation
aggregator's join. The primary collects every peer's
last-pushed envelope list (plus its own) under a peer-name key, then
this function:

1. Indexes every (peer, IP, Port) → envelope listener.
2. For each peer's outbound edges, finds a matching listener owned by
   a *different* peer.
3. Appends a `CallerAttribution{Caller: "<peer>:<envelope-id>", …}`
   to the matched server envelope.
4. Re-sorts each modified envelope's `Callers` so the loudest is
   first; updates the back-compat `Caller`/`CallerKind` aliases.

Same-peer pairs are skipped — those went through the local
`CorrelateCallers` pass and weren't matchable then.

### Surfaces (configuration parity)

| Surface | Reachable as |
|---------|--------------|
| REST | `GET /api/observer/envelopes/all-peers` |
| MCP | `observer_envelopes_all_peers` tool |
| CLI | `datawatch observer envelopes-all-peers` |

The PWA Federated Peers card already has the data path; an iterative
follow-up will add a "cross-peer Callers" row to the per-peer
drill-down. The wire shape is the same as the existing per-peer
endpoint so the rendering work is incremental.

### Tests

7 new unit tests in `internal/observer/cross_peer_correlator_test.go`:

- happy path (host A session → host B backend)
- wildcard listener match (`0.0.0.0:port` accepts inbound from any IP)
- same-peer pair must NOT cross-attribute (local correlator owns that)
- single-peer call is a no-op
- Callers list sorted desc by `Conns` (loudest first)
- outbound edge with no matching listener anywhere is dropped
- when the client peer matches `localPeerName`, caller ID isn't
  prefixed (keeps single-host renders clean for the primary's own
  envelopes)

Total daemon test count: **1352 passed**.

## Known follow-ups (still open)

- **BL180 Phase 2 eBPF kprobes** — the `__sys_connect` + `inet_csk_accept`
  + `conn_attribution` map. Backed out cleanly mid-edit in v5.6.0
  ("never compiled successfully"); resume with `BPF_MAP_TYPE_LRU_HASH`
  + userspace TTL pruner per the BL292 commit notes. Needs a kernel
  test path the dev workstation doesn't have today (CAP_BPF +
  vmlinux.h + clang/llvm).
- **BL190 expand-and-fill** — extend the `howto-shoot.mjs` recipe map
  per-howto and inline more screenshots into the remaining 9 howtos.

## Upgrade path

```bash
datawatch update                                          # check + install
datawatch restart                                         # apply the new binary

# Once you have ≥2 federated peers (`datawatch observer peer list`),
# fetch the cross-host view:
datawatch observer envelopes-all-peers
#  → {"by_peer": {"local": [...], "<peer-1>": [...], ...}}
```
