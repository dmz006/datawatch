# Release Notes — v3.2.0 (Intelligence group, partial)

**2026-04-19.** Intelligence backlog group ships **BL28** (quality
gates) and **BL39** (circular dependency detection). **BL24**
(autonomous task decomposition, 1-2 weeks) and **BL25** (independent
verification, depends on BL24) are intentionally deferred to a
dedicated future release — see "Deferred from this release" below.

---

## Highlights

- **BL39 — Circular dependency detection.** `pipeline.NewPipeline`
  now returns `(*Pipeline, error)` and rejects DAGs with cycles
  before they can be started. The cycle-detection algorithm
  upgraded from "nodes with positive in-degree" (Kahn-leftover) to
  DFS three-coloring with parent-pointer reconstruction, so the
  reported cycle is an actual ordered path (`A → B → C → A`).
- **BL28 — Quality gates.** When `pipeline.quality_gates.enabled`
  is set, the Executor captures a `RunTests(test_command)` baseline
  before the pipeline runs and a second result after the last task
  completes. `CompareResults` produces a `Regression` flag plus a
  human-readable summary. With `block_on_regression: true`, the
  pipeline transitions to `failed` instead of `completed` when
  current.fail_count > baseline.fail_count.

---

## Configuration changes

```yaml
pipeline:
  # ... existing fields unchanged ...
  quality_gates:
    enabled: false              # opt-in
    test_command: ""            # e.g. "go test ./..."
    timeout: 300                # seconds
    block_on_regression: false  # promote to true once trusted
```

Existing configs work unchanged — empty/absent block disables BL28.

`pipeline.NewPipeline` now returns `(*Pipeline, error)`. The only
in-tree caller (`pipeline.adapter.go`) is updated; if you import
this package directly you'll need to handle the cycle-error.

---

## Deferred from this release

**BL24 — Autonomous task decomposition** (LLM-driven PRD →
subtask DAG with retry + result aggregation) and **BL25 —
Independent verification** (separate-backend verifier with
fail-closed gating) are scoped at 1-2 weeks and 2-3 days
respectively, with BL25 directly depending on BL24's subtask
substrate.

These were grouped into the Intelligence release plan, but
implementing them within v3.2.0 alongside BL28/BL39 would have
either delayed shipping the smaller wins or compromised quality
on the bigger ones. Splitting them out lets v3.2.0 ship clean
infrastructure improvements now and gives BL24/BL25 their own
plan + design review later.

Tracking: backlog README "Intelligence (2 remaining)" section.

---

## Container images

| Image | Change | Action |
|---|---|---|
| `parent-full` | Daemon embeds the new pipeline behaviour (cycle reject + quality-gate hook) | **Rebuild required** |
| All agent / lang / tools / validator images | No change | No rebuild |

```bash
make container-parent-full PUSH=true CONTAINER_TAG=v3.2.0
```

Helm: `version: 0.4.0`, `appVersion: v3.2.0`.

---

## Testing

- **969 tests / 47 packages**, all passing (+4 vs. v3.1.0).
- New: `TestNewPipeline_RejectsCycle`,
  `TestDetectCycles_PathFormat`, `TestBL28_SetQualityGates`,
  `TestBL28_CompareResults_SummaryFormat`.
- Pre-existing scaffolded `TestQualityGate_*` cases now pass
  against the wired-in `quality.go` implementation.

---

## Upgrading from v3.1.0

```bash
datawatch update           # single host
helm upgrade dw datawatch/datawatch -n datawatch \
  --set image.tag=v3.2.0   # cluster
```

---

## What's next

- **v3.3.0 — Observability group**: BL10 session diffing, BL11
  anomaly detection, BL12 historical analytics + charts, BL86
  remote GPU stats.
- **v3.4.0 — Operations group**: BL17 hot config reload (SIGHUP),
  BL22 RTK auto-install, BL37 system diagnostics, BL87 `datawatch
  config edit`.
- **v3.5.0 (TBD) — BL24 autonomous task decomposition + BL25
  independent verification**, planned together because BL25's
  verifier-orchestration depends on BL24's subtask substrate.
