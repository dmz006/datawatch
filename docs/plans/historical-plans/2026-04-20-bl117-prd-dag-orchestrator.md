# BL117 — PRD-driven DAG orchestrator with guardrail sub-agents

**Status:** v1 design, implementation targeted for **v4.0.0** (Sprint S8).
**Builds on:** BL24 autonomous substrate (v3.10.0) + BL33 plugin framework (v3.11.0) + F10 agent spawn (v3.0.0) + BL103 validator agent.
**Operator directive 2026-04-20:** "When S8 is done release will be 4.0 … It's a big change."

---

## 1. What we're building

An orchestrator that takes a **PRD (or a set of PRDs)**, plans a DAG of execution steps across them, and runs each step under a **guardrail sub-agent overlay** — every step is attested by one or more independent validators (rules / security / release-readiness / docs-diagrams-architecture) before the DAG advances.

Where BL24 is "one PRD → one flat task list with per-task verifier", BL117 is "many PRDs → orchestrated DAG with per-node guardrails and a release gate at the end."

---

## 2. Relationship to existing primitives

| Primitive | Role in BL117 |
|---|---|
| **BL24 autonomous** | Provides PRD → Stories → Tasks decomposition per PRD. BL117 consumes the PRD model + store without re-deriving. |
| **F10 spawn** | Every orchestrator node is a `session.Manager.Start` worker (or a `BL103 validator` worker for guardrails). |
| **F15 pipeline.Executor** | The task-level DAG executor. BL117's orchestrator is one level above — it composes PRDs into a meta-DAG; the per-PRD task DAG still runs through `pipeline.Executor`. |
| **BL103 validator agent** | Primary guardrail implementation. Each guardrail is a BL103-style worker with a specialized system prompt (rules / security / release / docs). |
| **BL96 wake-up stack** | Guardrails get the parent PRD + sibling results prepended to their L0 wake-up — no re-discovery. |
| **BL104 peer broker** | Guardrails can publish per-node findings to siblings, enabling cross-concern dependencies (security block → release gate reads the block). |
| **BL33 plugins** | Custom guardrails ship as plugins registering the new `on_guardrail` hook (added in v4.0). |
| **BL9 audit** | Every DAG node + every guardrail finding is an audit entry (actor=`orchestrator` or `guardrail:<name>`). |
| **BL6 cost** | Rollup per PRD → per DAG → total, already automatic via session cost tagging. |

---

## 3. Scope — v1 (what ships in v4.0.0)

**In:**

- `internal/orchestrator/` package: `Node`, `Graph`, `Runner`, `Guardrail`, `Verdict` types + JSONL-backed store under `<data_dir>/orchestrator/`.
- **Graph construction** from a set of PRD IDs (BL24 PRDs). Operator specifies dependencies between PRDs explicitly; the decomposition per PRD is BL24's job.
- **Runner** that walks the PRD-DAG, for each PRD:
  1. Reuses BL24 `autonomous.Manager.Run(prdID)` to execute the PRD's task DAG.
  2. Spawns the configured guardrail set after PRD completion.
  3. Each guardrail's `Verdict` is `pass | warn | block`; `block` halts the DAG.
- **4 built-in guardrail types** (all as BL103-style validator images, distroless):
  - `rules` — operator-supplied checklist compliance (`orchestrator.guardrails.rules.checklist` YAML).
  - `security` — static scan + `autonomous.SecurityScan` + delta-diff review.
  - `release-readiness` — tests pass, lint clean, CHANGELOG entry present, version bumped.
  - `docs-diagrams-architecture` — required docs exist + reference the right BL IDs.
- **Extensibility hook** — plugins register `on_guardrail` and can return a `Verdict`.
- **REST surface**: `/api/orchestrator/graphs` CRUD, `/api/orchestrator/graphs/{id}/run`, `/api/orchestrator/verdicts`.
- **Full parity**: MCP + CLI + comm via `rest` + YAML `orchestrator:` block.
- **Disabled by default**; opt in via `orchestrator.enabled: true`.
- Tests: graph construction, topo-sort, guardrail dispatch, verdict aggregation, block propagation.
- Operator doc at `docs/api/orchestrator.md`.

**Out (deferred to v4.0.x patches):**

- Graph authoring UI (Gantt-style). REST + CLI enough for v1.
- Cross-cluster orchestration across multiple remote datawatch instances.
- Live-edit of a running graph (pause node, swap guardrail mid-run).
- Cost circuit-breaker at graph level (`orchestrator.max_usd_per_graph`).

---

## 4. Data model (sketch)

```go
type NodeKind string
const (
    NodeKindPRD       NodeKind = "prd"        // BL24 PRD execution
    NodeKindGuardrail NodeKind = "guardrail"  // BL103 validator
)

type Node struct {
    ID        string
    GraphID   string
    Kind      NodeKind
    PRDID     string   // when Kind==prd
    Guardrail string   // when Kind==guardrail ("rules" | "security" | …)
    DependsOn []string
    Status    NodeStatus
    Verdict   *Verdict // nil until guardrail/PRD completes
    ...
}

type Verdict struct {
    Outcome  string   // pass | warn | block
    Severity string   // info | low | medium | high | critical
    Summary  string
    Issues   []string
    VerdictAt time.Time
    ValidatorID string
}

type Graph struct {
    ID      string
    Title   string
    PRDIDs  []string            // convenience — all PRDs touched by this graph
    Nodes   []Node
    Status  GraphStatus         // draft | active | completed | blocked | cancelled
    CreatedAt, UpdatedAt time.Time
}
```

---

## 5. Guardrail contract

Every guardrail is invoked with:

```json
{
  "graph_id": "…", "node_id": "…", "prd_id": "…",
  "kind": "post_prd",
  "summary": "<result from the just-finished PRD node>",
  "diff_sha": "<git HEAD after the PRD ran>"
}
```

And returns:

```json
{"outcome": "block", "severity": "high",
 "summary": "release-readiness: CHANGELOG missing for v4.0.0",
 "issues": ["CHANGELOG.md has no [4.0.0] entry"]}
```

`outcome == "block"` halts the DAG and records the verdict to the store; operator intervention required. `outcome == "warn"` records the verdict but does not halt. `outcome == "pass"` clears the node.

---

## 6. Configuration (full parity)

```yaml
orchestrator:
  enabled:                false
  default_guardrails:     ["rules", "security", "release-readiness", "docs-diagrams-architecture"]
  guardrail_timeout_ms:   120000
  guardrail_backend:      ""        # empty = inherit session.llm_backend
  max_parallel_prds:      2         # per-graph PRD parallelism

  guardrails:
    rules:
      checklist:
        - "Every new BL has a design doc under docs/plans/"
        - "Every new REST endpoint is listed in docs/api-mcp-mapping.md"
    security:
      extra_patterns: []
    release_readiness:
      required_files: ["CHANGELOG.md", "charts/datawatch/Chart.yaml"]
    docs_diagrams_architecture:
      required_docs: []
```

---

## 7. REST + parity surface

```
POST   /api/orchestrator/graphs                   create graph; body: {title, prd_ids, [deps]}
GET    /api/orchestrator/graphs                   list all
GET    /api/orchestrator/graphs/{id}              fetch with node tree + verdicts
DELETE /api/orchestrator/graphs/{id}              cancel
POST   /api/orchestrator/graphs/{id}/run          start runner
GET    /api/orchestrator/verdicts                 list all verdicts (paginated)
GET    /api/orchestrator/config / PUT             reuse the config pattern from BL24/BL33
```

MCP tools and CLI follow the existing naming pattern (`orchestrator_graph_list`, `datawatch orchestrator graph-list` etc.). Comm via the existing `rest` passthrough.

---

## 8. Testing strategy

- Unit: graph topo-sort, cycle detection, verdict aggregation (`block` dominates over `warn`/`pass`), guardrail dispatch with a fake `GuardrailFn` (matches the BL24 `DecomposeFn`/`VerifyFn` pattern for testability).
- Integration: full graph run with 2 fake PRDs + 1 fake guardrail; assert DAG state transitions.
- Live smoke against a running daemon after release.

---

## 9. v4.0.0 release checklist

In addition to the usual sprint close-out:

- [ ] BL117 implementation + tests passing.
- [ ] `docs/api/orchestrator.md` written (operator + AI-ready).
- [ ] BL117 entry in `docs/plans/2026-04-11-backlog-plans.md` marked ✅ shipped.
- [ ] `docs/plans/README.md` sprint summary: S8 marked shipped → v4.0.0.
- [ ] `docs/plans/RELEASE-NOTES-v4.0.0.md` §4 "v4.0.0 additions" filled in.
- [ ] `docs/test-coverage.md` v4.0.0 section added.
- [ ] `docs/config-reference.yaml`: `orchestrator:` block appended.
- [ ] `docs/api-mcp-mapping.md`: orchestrator endpoints + tools listed.
- [ ] `CHANGELOG.md` [4.0.0] entry.
- [ ] Chart bumped (`version: 0.14.0`, `appVersion: v4.0.0`).
- [ ] `cmd/datawatch/main.go` + `internal/server/api.go`: `Version = "4.0.0"`.

---

## 10. Non-goals (explicitly)

- BL117 is **not** a replacement for BL24 — it composes BL24 PRDs into a larger graph. Single-PRD flows continue to use BL24 directly.
- BL117 is **not** a replacement for F10 — the guardrails and PRD executors all spawn F10 workers under the hood.
- BL117 does **not** introduce a new session type — everything runs as `session.Session` + BL103 validator sessions.
- BL117 does **not** change memory/KG semantics — it records to the existing KG via the `orchestrator:` namespace.
