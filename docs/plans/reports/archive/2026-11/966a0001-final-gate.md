# 966a0001 End-to-End Gate — Counts-Only Report

Result: FAIL (check 2)

| # | Check | Result | Counts |
|---|-------|--------|--------|
| 1 | Retry-prompt flake loop (50 iterations, 3 modes) | pass | modes clean: 3 of 3; failed runs: 0; race reports: 0 |
| 2 | Build / vet / race test of package | fail | build: pass; vet: pass; race test: fail — failed test runs: 5, race reports: 5 |
| 3 | Formatter check on touched files | not run (stopped at check 2) | — |
| 4 | Diff scope against main | not run (stopped at check 2) | — |

Package verdict: not ok (build ok, vet ok, `go test -race` failed with 5 data-race reports).
Files changed: not counted (stopped at check 2).

No fixes were made in this task.
