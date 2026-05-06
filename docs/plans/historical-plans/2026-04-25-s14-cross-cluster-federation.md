# S14+ — cross-cluster federation, per-pod alert routing, observer-driven autoscaling

**Status:** S14a foundation ✅ shipped v4.8.0 (2026-04-25); UI/MCP follow-up + S14b/S14c pending
**Filed:** 2026-04-25
**Predecessors:** all of S9 → S13 (BL171, BL172, BL173, S13 — observer
substrate + Shape A/B/C peers + agent peers)
**Target releases:** v4.8.0 (S14a foundation ✅), v4.8.x (rollup UI), v4.9.0 (S14b alerts +
autoscaling), v5.0.0 (S14c GPU-vendor expansion)
**GitHub:** [#20](https://github.com/dmz006/datawatch/issues/20) (umbrella)

## What was deferred from S13

The observer federation today is single-parent: every Shape A/B/C peer
pushes to one `/api/observer/peers/*` endpoint. Three things got
deferred from the BL171 / S13 designs because they were wider in
scope than the S9–S13 sprints:

1. **Cross-cluster federation tree** — multiple primary parents, each
   aggregating its own peer set, then a federation root that
   aggregates the parents.
2. **Per-pod alert routing** — observer envelopes carry status; today
   alerts are global. Operators want to route them by pod / agent /
   peer.
3. **Observer-driven autoscaling** — the orchestrator (BL117) could
   spawn extra agents when an envelope crosses a threshold, or stop
   spawning when the cluster is under pressure.
4. **GPU vendor expansion** — DCGM (NVIDIA) is wired in v4.5.0; ROCm
   (AMD) and Intel level_zero are scoped but not implemented.

This umbrella splits each into its own sprint so they ship in
manageable patches.

---

## S14a — cross-cluster federation tree

**Target:** v4.8.0
**Estimate:** 3–4 days
**Depends on:** nothing new — extends the existing peer registry.

### Goal

A datawatch primary can itself be a peer of a *root* datawatch.
Operators with multiple clusters (one per dev / staging / prod
environment) get a single pane of glass without merging the clusters.

### Design

Reuse the existing `/api/observer/peers/*` surface unchanged. The
intermediate primary just acts as both a parent (to its Shape A/B/C
peers) and a peer (to the root). On every collector tick the primary
pushes its **aggregated** envelope set to the root, with a
`shape: "P"` (primary) tag and per-source attribution preserved
(`envelope.source = "<peer-name>:<envelope-id>"`).

Wire shape additions:

```json
{
  "shape": "P",
  "peer_name": "datawatch-prod-east",
  "snapshot": {
    "v": 2,
    "host": {"name": "datawatch-prod-east", "shape": "primary", …},
    "envelopes": [
      {"kind": "session", "id": "ralfthewise-787e", "source": "local",  "cpu_pct": 23, …},
      {"kind": "session", "id": "agt_a1b2:session:x", "source": "agt_a1b2",  …},
      {"kind": "node",    "id": "node-3",   "source": "shape-c-1", …}
    ]
  }
}
```

Existing single-cluster operators see no change — the root is opt-in
via `observer.federation.parent_url` in the primary's config.

### Tasks

| # | Task | Notes |
|---|---|---|
| 1 | Primary-side root client — same `internal/observerpeer.Client`, started when `observer.federation.parent_url` is set. Push interval 10 s (root sees aggregate, not raw process tree). | Reuses S13 wiring 1:1. |
| 2 | Envelope rewriter — when pushing to the root, rewrite each envelope to include `source` (current peer name or "local"). | ~30 LOC in `internal/observer.Collector.snapshotForRoot()`. |
| 3 | Root-side roll-up — when receiving from a `shape: "P"` peer, fan out the inner envelopes into the root's envelope tree under a `cluster: <peer-name>` group. PWA already handles multi-source rendering via `envelope.source`. | Server side. |
| 4 | PWA "Cluster" filter pill on Federated peers card (P shape). | Mirrors the v4.7.0 Agents/Standalone/Cluster pills. |
| 5 | MCP/CLI: `observer_primary_list` alias (list peers where `host.shape == "primary"`). | Thin alias. |

### Open questions

- **Loop prevention** — primary A pushing to primary B which pushes
  back to A would loop forever. Mitigation: each push body includes a
  `chain` field with the ordered peer-name path; reject pushes whose
  chain already contains the receiver. Simple + correct.
- **Compaction** — process tree deltas at the root would balloon. The
  root receives only aggregated envelopes; raw process detail stays
  local to each primary and is fetched on demand via the existing
  `/api/observer/envelope?id=…` drill-down (proxy through the chain).

---

## S14b — per-pod alert routing + autoscaling

**Target:** v4.9.0
**Estimate:** 5–7 days
**Depends on:** S14a (some routing rules apply to remote envelopes).

### Goal

Operator can write alert rules like:

```yaml
alerts:
  - name: "agent CPU runaway"
    when:   "envelope.kind == 'session' && envelope.cpu_pct > 80 && for > 60s"
    notify: "signal://operator"
    cooldown: "5m"
```

…and have them fire against any envelope in the federated tree, with
notifications routed per-rule.

### Tasks

| # | Task |
|---|---|
| 1 | `internal/alertrules/` package — small DSL + evaluator over StatsResponse v2. |
| 2 | Rule storage in `<data_dir>/alerts/rules.json` (CRUD on `/api/alerts/rules`). |
| 3 | `Manager.tick()` evaluates rules against the latest envelopes; debounces matches by rule cooldown. |
| 4 | Routing — reuses the existing comm-channel registry (Signal/Telegram/etc.). Each rule names a channel. |
| 5 | PWA Settings → Alerts → Rules editor (CRUD UI). |
| 6 | MCP / CLI parity. |

### Autoscaling sketch

`internal/orchestrator/autoscale.go` reads observer envelopes for
each running graph node and consults a per-PRD policy:

- `cpu_above 70% for 2m` → spawn another agent for this PRD's
  fan-out children
- `cluster.nodes[*].mem_pct > 90% any` → block new spawns until
  pressure drops

Default: off. Operators opt in per PRD.

---

## S14c — GPU vendor expansion (ROCm + Intel)

**Target:** v5.0.0
**Estimate:** 2 days each (4 days total, parallelisable).

ROCm and Intel level_zero scope was reserved in BL173 but not
implemented. Same `internal/observer.GPUScraper` interface; new
implementations pull from `rocm-smi` (AMD) and `level_zero` (Intel).
Cluster sidecar gets a discovery step that picks the right scraper
based on which GPU library is present.

No design risk — purely "more of the same" once a host with each
vendor's GPU is available for testing.

---

## Sequencing

S14a (federation) is the prerequisite for the multi-cluster operator
story; S14b (alerts) builds on the federated envelope set; S14c (GPU
vendors) is independent.

Recommended order: **S14a → S14c → S14b**.

- S14a delivers an immediately useful capability (multi-cluster
  view).
- S14c is short and gates AMD/Intel cluster operators from running
  Shape C at all.
- S14b is the largest of the three and benefits from having S14a
  and S14c shipped first so its rules can reference both federated
  envelopes and per-vendor GPU metrics.

---

## Out of scope (deferred beyond v5.0.0)

- **Cross-cluster session migration** — moving a running F10 worker
  from one cluster to another. Big infrastructure change; defer.
- **Observer-driven cost optimisation** — picking cluster shapes
  based on per-envelope $ cost. Needs a cost model the observer
  doesn't currently carry.
- **Audit-log federation** — operator audit entries from every
  primary aggregated at the root. Useful but separate from observer.
