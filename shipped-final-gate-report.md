# Shipped Final Gate — full result (read-only run)

- Run: 2026-11-01 10:05:56 UTC (worktree root, `bash .datawatch/verify/shipped-final-gate.sh`)
- Wall time: 4 s, exit code **1** → **GATE: FAIL** (stopped at the PWA node-test step; script is stop-on-first-failure)
- No files were changed by this run (read-only task honored).

## Step-by-step

| # | Step | Result | Evidence |
|---|------|--------|----------|
| 1 | Unscoped tmux-kill audit (`scripts/check-no-unscoped-tmux.sh`) | PASS | `OK: no unscoped tmux kills` |
| 2 | `bash -n scripts/merge-branches-bump-version.sh` (syntax check) | PASS | no output, exit 0 |
| 3 | Internal-ref audit (`scripts/check-no-internal-refs.sh`) | PASS | `✓ PASS: no internal refs in user-facing surfaces.` |
| 4 | `go vet` 7 packages | PASS | no findings |
| 5 | `go test -count=1 -run 'TestShipped\|Locale'` 7 packages | PASS | autonomous 0.461s ok; git ok (no tests to run); config 0.003s ok; server 0.362s ok; mcp 0.015s ok; router 3.218s ok; cmd/datawatch 0.011s ok |
| 6 | **PWA node test: `node --test internal/server/web/app-shipped.test.js`** | **FAIL (gate stops here)** | `Could not find 'internal/server/web/app-shipped.test.js'` |
| 7 | `bash scripts/tests/merge-helper-shipped-record.sh` | (not reached by gate) PASS | run separately — see "Beyond the stop point" |
| 8 | `make lint` | (not reached by gate) FAIL | run separately — see "Beyond the stop point" |
| – | `OK: shipped final gate passed` | **never printed** | — |

## Failing step

The gate's 6th step fails because **`internal/server/web/app-shipped.test.js` does not exist**
(anywhere in the repo; verified by directory listing and repository-wide `find`; no other test
files match). The gate script is the only place that references this file. The PWA directory
contains many `app-*.test.js` files, but no `app-shipped.test.js` — the gate script references
a test that was never created (or the test file was removed without dropping the gate reference).

## Beyond the stop point (steps the gate would have reached, run separately to characterize)

- **Step 7 — merge-helper shipped-record shell test: PASS (rc=0, 0s).**
  All scenarios passed: record automaton + all-members automation; member without merge commit →
  no automation call; non-`automaton/<id>` branch → skipped; `autonomous.shipped.enabled` off →
  skipped; failing stub doesn't fail the step (warning printed); daemon unreachable → step exits 0.
  Final line: `ok: all shipped-record assertions passed`.

- **Step 8 — `make lint`: FAIL (rc=2, 35s).** Pre-lint audits passed
  (`bash scripts/check-no-direct-ignore-writes.sh` → `OK: generated ignore files match the registry`),
  then `golangci-lint run ./...` reported **7 issues** and `make` exited 1:
  - `errcheck` × 3 — unchecked `w.Write` return in `internal/router/shipped_comm_test.go` lines 26, 77, 109
  - `staticcheck` QF1001 (De Morgan) × 2 — `internal/server/prd_list_view.go` lines 145 (`!(statusShipped && shipEnabled && prd.IsShipped())`) and 155 (`!(shipEnabled && prd.IsShipped())`)
  - `unused` × 1 — const `shippedCountsUsage` never used, `internal/router/sx2_parity.go:446`
  - `unused` × 1 — field `body` never used, `internal/router/sx2_parity_shipped_backfill_test.go:15`
  - Linter summary: `7 issues: errcheck: 3, staticcheck: 2, unused: 2` → `make: *** [Makefile:277: lint] Error 1`

## Verdict

**Final gate FAILs** with two independent problems:
1. Broken/missing reference in the gate itself: the PWA test file `internal/server/web/app-shipped.test.js` does not exist, so the gate hard-fails at step 6.
2. Even past that, `make lint` fails with 7 lint issues in shipped-feature code (test helpers, `prd_list_view.go`, `sx2_parity.go` and its backfill test).

All other gate steps (tmux audit, internal-ref audit, `go vet`, all `TestShipped|Locale` tests,
merge-helper shipped-record test) pass. Per the task scope, no code was changed.
