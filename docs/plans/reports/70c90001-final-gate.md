# 70c90001 final gate

Overall: pass for everything that runs without a daemon. The live run on a sandbox daemon was not done (see the last section).

## Commands run

All Go commands ran with the tmux environment variables unset. No daemon or real LLM session was started and no version variable was changed.

| Check | Command | Packages / files | Passed | Failed | Skipped |
|---|---|---|---|---|---|
| Go, whole tree | `go test ./...` | 93 packages with tests | 93 | 0 | 0 |
| Go, touched packages again after the last edits | `go test -count=1 ./internal/server/ ./cmd/datawatch/ ./internal/trace/... ./internal/stats/ ./internal/metrics/ ./internal/mcp/ ./internal/config/` | 9 | 9 | 0 | 0 |
| Node, whole suite | `node --test internal/server/web/*.test.js` | all `*.test.js` | 2219 tests | 0 | 0 |
| Go vet | `go vet` on the touched packages | 6 | 6 | 0 | 0 |
| Howto coverage | `bash scripts/check-howto-coverage.sh` | `docs/howto` | pass | 0 | 0 |
| Curated howto tools | `bash scripts/check-curated-howtos.sh` | 23 howtos | pass | 0 | 0 |
| Local-leak check | `bash scripts/check-no-local-leaks.sh` | tracked tree | 0 findings | 0 | 0 |
| Internal-ref check | `bash scripts/check-no-internal-refs.sh` | user-facing surfaces | pass | 0 | 0 |
| Smoke script syntax | `bash -n scripts/release-smoke.sh` | 1 | pass | 0 | 0 |
| Story syntax | `node --check scripts/test-stories/pwa/trace-card.mjs` | 1 | pass | 0 | 0 |

## Routes added

5 (`GET /api/trace/session/{id}`, `/api/trace/automaton/{id}`, `/api/trace/rollup`, `/api/trace/coverage`, `/api/trace/stats`), documented in both openapi copies, the MCP mapping and the smoke script. 4 MCP tools added (`trace_session`, `trace_automaton`, `trace_rollup`, `trace_coverage`).

## Not run

- `bash scripts/release-smoke.sh` section "Agent tracing" and story TS-847 need a sandbox daemon and the Playwright harness; the building automaton runs under a no-daemon rule that conflicts with the checklist's live-test item. Owed at merge.
