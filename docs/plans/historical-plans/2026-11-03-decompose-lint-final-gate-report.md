# Final Gate Report — decompose-lint (counts only)

Date: 2026-11-03
All commands run with TMUX and TMUX_TMPDIR unset.

## Counts per check

| Check (command) | Passed | Failed | Result |
|---|---|---|---|
| `go build ./...` | 1 | 0 | PASS |
| `go vet ./internal/config/ ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./cmd/datawatch/` | 1 | 0 | PASS |
| `go test ./internal/config/ ./internal/autonomous/ ./internal/server/ ./internal/mcp/ ./internal/router/ ./cmd/datawatch/` | 7889 | 0 | PASS |
| `node internal/server/web/app-decompose-lint.test.js` | 9 | 0 | PASS |
| `bash scripts/check-no-internal-refs.sh` | 1 | 0 | PASS |
| `bash scripts/check-howto-coverage.sh` | 1 | 0 | PASS |
| `bash scripts/check-docs-sync.sh` | 1 | 0 | PASS |

Failing checks: none.

Counting notes:
- The go test row counts top-level go test functions from `go test -v` (7889 pass, 0 fail, plus 12 skipped on the listed packages, not counted in either column).
- The node test row uses the node test runner's own pass/fail summary (9 pass, 0 fail, 0 skipped).
- The script rows count the script as passed when it exits 0 (1) or failed when it exits non-zero (1); check-docs-sync passed after the embedded docs copy under `internal/server/web/docs` was regenerated to include this report.

## Version / release line confirmation

- No version variable changed: the `var Version` lines in `cmd/datawatch/main.go` and `internal/server/api.go` have no diff between the merge-base with main and HEAD.
- No README release line changed: `README.md` has zero diff between the merge-base with main and HEAD.
- The release version is the next patch above main, taken at merge time.
