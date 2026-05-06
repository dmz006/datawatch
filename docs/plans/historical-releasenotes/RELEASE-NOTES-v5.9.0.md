# datawatch v5.9.0 — release notes

**Date:** 2026-04-26
**Spans:** v5.8.0 → v5.9.0
**Closed:** BL191 Q4 (recursive child-PRDs)

## What's new

### BL191 Q4 — Recursive child-PRDs (option (a) shortcut)

Per the BL191 design doc (`docs/plans/2026-04-26-bl191-autonomous-prd-lifecycle.md`),
Q4 had two paths:

- **(a) shortcut**: `Task.SpawnPRD bool` flag — when true, the task
  spec is treated as a *child PRD spec* rather than a worker prompt.
- **(b) full graph**: child PRDs as nodes in a BL117 orchestrator graph.

v5.9.0 ships **(a)**. Path (b) stays open as a future cut once
operators have a feel for how recursive PRDs behave in practice.

### Models

```go
// PRD adds:
ParentPRDID  string  // genealogy: which PRD spawned this one
ParentTaskID string  // which specific parent task spawned this one
Depth        int     // 0 for root PRDs; child = parent.Depth + 1

// Task adds:
SpawnPRD   bool    // when true, treat Spec as a child PRD spec
ChildPRDID string  // ID of the spawned child (filled by executor)
```

### Executor

`internal/autonomous/executor.go.recurseChildPRD` is the new branch:

1. Refuse if `parent.Depth + 1 > Config.MaxRecursionDepth`.
2. Create child PRD via `Store.CreatePRDWithParent` with parent-task
   linkage + bumped depth.
3. `Manager.Decompose(child)` (re-uses parent's LLM defaults).
4. If `Config.AutoApproveChildren`, `Manager.Approve(child, "autonomous", …)`.
   Otherwise leave the parent task in `TaskBlocked` waiting for the
   operator to call `/approve` on the child.
5. `Manager.Run(child)` synchronously walks the child task DAG.
6. Map child outcome → parent task: `PRDCompleted` → `TaskCompleted`
   with a synthesized `VerificationResult{OK: true}`; anything else →
   `TaskFailed` with a breadcrumb pointing at `ChildPRDID`.

### Config

```yaml
autonomous:
  max_recursion_depth: 5    # 0 = recursion disabled
  auto_approve_children: true
```

`max_recursion_depth: 5` was picked as the default because it covers
"PRD spawns 5 levels of decomposition" without leaving room for an
LLM hallucination to spiral the daemon out.

`auto_approve_children: true` is the operator-friendly default — every
level otherwise hangs on review and recursion is unusable. Operators
who need a human-in-the-loop on every child set it false; the parent
task lands in `TaskBlocked` and the operator approves the child via
the usual surfaces.

### Surfaces (configuration parity)

| Surface | Reachable as |
|---------|--------------|
| REST | `GET /api/autonomous/prds/{id}/children` |
| MCP | `autonomous_prd_children` tool |
| CLI | `datawatch autonomous prd-children <id>` |
| Chat | `autonomous children <id>` |
| YAML | `autonomous.max_recursion_depth`, `autonomous.auto_approve_children` |

### Tests

5 new unit tests cover:

- happy path: parent task with `SpawnPRD=true` spawns a child, child
  reaches `PRDCompleted`, parent task reaches `TaskCompleted` with
  `ChildPRDID` and a synthesized verification result.
- depth limit: parent at `MaxRecursionDepth` refuses to spawn —
  parent task fails with a clear error mentioning the limit.
- recursion disabled: `MaxRecursionDepth=0` refuses every spawn.
- operator-gated: `AutoApproveChildren=false` leaves the parent task
  `TaskBlocked` with the child sitting in `PRDNeedsReview`.
- store: `Store.ListChildPRDs` returns only matching children sorted
  oldest-first (siblings under the same parent, no orphans).

Total daemon test count: **1339 passed**.

## Known follow-ups (still open)

- **BL180 Phase 2** — eBPF kprobes (resume) + cross-host federation correlation
- **BL191 Q6** — guardrails-at-all-levels (apply the BL117 guardrail
  set at story + task level, not just PRD level)
- **BL190** — PWA screenshot rebuild for the now-13-doc howto suite
  (chrome plugin removed; will use puppeteer-core + seeded JSONL fixtures)

## Upgrade path

```bash
datawatch update                        # check + install
datawatch restart                       # apply the new binary; preserves tmux sessions

# Smoke-test the new chat verb:
datawatch autonomous prd-children <prd-id>     # CLI
# returns {"children": []} for a parent with no SpawnPRD tasks yet.
```
