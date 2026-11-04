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

## Version / release line confirmation

- Confirmed: no version variable changed. `var Version` in `cmd/datawatch/main.go` and `internal/server/api.go` shows no version-related diff between the merge-base with main and HEAD.
- Confirmed: no README release line changed. `README.md` shows zero diff between the merge-base with main and HEAD.
- The release version is the next patch above main, taken at merge time.
