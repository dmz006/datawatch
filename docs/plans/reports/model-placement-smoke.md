# Report: live smoke — place a missing model on a real compute node

- Date: 2026-11-08
- Scope: `<repo>` (the task worktree)
- Test: `go test ./internal/autonomous/ -run TestSmokeModelPlacement -v` (file: `internal/autonomous/smoke_model_placement_test.go`)
- Method: fully in-process — per-node fake Ollama servers (httptest) reporting loaded model (`/api/ps`), version + free memory (`/api/version`) and serving the fake model pull (`/api/pull`); fake nodes with different free memory; the real planner (`compute.PlaceMissingModel`), the real admission gate and the real `inference.ModelLoader` pull path. No daemons, no real pulls, no real compute nodes.

## Results

| # | Check | Result |
|---|-------|--------|
| 1 | Missing model placed on the node with the most free memory | PASS |
| 2 | Node below the free-memory headroom refused | PASS |
| 3 | Node whose pinned model must not be displaced refused (pin named in refusal; `allow_model_swap` restores eligibility) | PASS |
| 4 | Model not allowed to pull (policy `refuse`) refused with the planner's reasons — in the error, the alert and with no load attempt | PASS |
| 5 | Audit record written for each decision (refuse → `placement_refused`, auto_load → `placement_loaded`, ask → `placement_asked` + `placement_approved` + `placement_loaded`, each with node/model/policy/reason) | PASS |
| 6 | Policy defaults leave behaviour unchanged (unset reads `ask` / 10% mem / 5 GB disk / 30 min; no advisor → old failover behaviour, no placement alert or audit event) | PASS |
| 7 | Placed model actually pulled and loaded on the chosen node (fake puller served exactly one pull with reported progress; model resident afterwards) | PASS |

**Counts: 7 passed, 0 failed** (7 top-level checks; check 5 comprises 3 passing audit sub-checks → 11 subtests total, all PASS).

## Regression runs

| Suite | Result |
|-------|--------|
| `./internal/autonomous/` (full package, 5247 tests) | PASS |
| `./internal/audit/` + `./internal/config/` (684 tests) | PASS |

## Notes

- One interpretation call: the codebase has no per-model allow/deny list; "not allowed to pull" is implemented as the `compute_placement.missing_model: "refuse"` gate, which is the refusal that blocks the load (and therefore any pull) and carries per-node reasons.
- Two defects found and fixed in the smoke test itself during the run: the planner only emits refusal lines when every candidate is refused (check 3 asserts via a pinned-only and a `allow_model_swap` control scenario), and inline fake-node literals needed a live httptest server handle.
- No production code was changed; no version variables, daemons, real pulls, nodes or `gh` were touched.
