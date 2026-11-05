# c1410001 Final Gate — counts report

**Date:** 2026-11-05

**How it was run:** every check ran from the repository root with the TMUX
environment variables unset. No daemon was started, no model was pulled, `gh`
was not used, and no version variable was changed.

| # | Check | Result | Counts |
|---|-------|--------|--------|
| 1 | `go vet` — the seven changed packages | **PASS** | complaints: 0 |
| 2 | `go test` — the seven changed packages | **PASS** | packages: 7 pass, 0 fail |
| 3 | node tests — the new web test files (4 files) | **PASS** | tests: 42 pass, 0 fail, 0 skipped, 0 cancelled |
| 4 | internal-ref leak audit (operator-facing surfaces) | **PASS** | leaks: 0 |
| 5 | embedded docs sync check | **PASS** | drift: 0 |

**Gate:** 5/5 checks **PASS**.
