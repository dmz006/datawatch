# Planning routing documentation and parity gate (eb9f0001)

Run on 2026-11-05 in the task worktree. Every check below was run, with `env -u TMUX -u TMUX_TMPDIR` for Go and node. No daemon was started.

## Checks run

| Check | Command | Result | Counts |
|---|---|---|---|
| Lint | `make lint` | Pass (after the fix below) | golangci-lint: 0 issues |
| Go, full | `go test ./... -count=1` | Pass, exit 0 | 88 packages `ok`, 0 `FAIL`, 12 with no test files |
| Node, full | `node --test internal/server/web/*.test.js` | Pass, exit 0 | 2035 tests, 2035 pass, 0 fail, 0 skipped |
| Docs check | `.datawatch/verify/planning-routing-docs.sh` | Pass | four tool names in `docs/mcp.md` and `docs/cursor-mcp.md`, `## Parity surface` present |
| Autonomous package, re-run after the lint fix | `go test ./internal/autonomous/ -count=1` | Pass | `ok` |

The first `make lint` run failed with one finding: `internal/autonomous/planning_ladder.go:423` QF1012 (`WriteString(fmt.Sprintf(...))`). It was replaced with `fmt.Fprintf(&b, ...)`, with the same output, and lint then passed. The full Go run started before that edit and covers the same behaviour; the autonomous package was re-run after it.

## What changed in the docs

- `docs/mcp.md`: parameter table, reply and example for `planning_routing_get`, `planning_routing_set`, `planning_routing_test` and `planning_scorecard`.
- `docs/cursor-mcp.md`: a row for each of the four tools.
- `docs/plans/parity-issues/eb9f0001.md`: the REST contract with request and response shapes.
- `docs/testing-tracker.md`: planning routing rows state what was validated.
- `docs/plans/proposals/2026-11-01-planning-ladder-executor.md`: `## Parity surface` is present.

## Routes

This release **adds four REST routes**. Earlier text that said no routes were added was wrong.

| Method | Path |
|---|---|
| GET | `/api/autonomous/planning-routing` |
| PUT | `/api/autonomous/planning-routing` |
| POST | `/api/autonomous/planning-routing/test` |
| GET | `/api/autonomous/planning-routing/scorecard` |

Request and response shapes are in `docs/plans/parity-issues/eb9f0001.md`.

## Not done

- No live-daemon check. Routing was never run on a daemon with real decomposes; the REST, MCP, CLI and comm rows were exercised over in-process `httptest` handlers and fakes only.
- No Android or iOS work (excluded from this release).
- No version bump, changelog entry or `gh` call, as asked.
