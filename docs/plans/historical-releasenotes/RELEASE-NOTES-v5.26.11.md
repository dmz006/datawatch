# datawatch v5.26.11 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.10 → v5.26.11
**Patch release** (no binaries — operator directive).
**Closed:** Autonomous-run effort-enum mismatch (every task failed pre-spawn) + smoke covers full PRD lifecycle.

## What's new

### Autonomous PRD run is no longer dead-on-arrival

End-to-end validation of the v5.26.9 fix uncovered the *next* layer down. Every PRD run was failing at `/api/sessions/start` with:

```
spawn: session start: 400 Bad Request — invalid effort: must be one of quick, normal, thorough
```

Root cause: two enums for the same concept were never reconciled.

| Where | Values |
|-------|--------|
| `internal/autonomous/models.go` `Effort` | `low`, `medium`, `high`, `max`, `quick`, `normal`, `thorough` |
| `internal/session/store.go` `EffortLevels` | `quick`, `normal`, `thorough` |

The PWA New PRD modal exposes the autonomous enum (defaults to `low`). Decomposer accepts it. Storage round-trips it. Run hits `/api/sessions/start` which validates against the session enum and rejects. Tasks went straight to `TaskFailed` with no `session_id` ever populated.

**Fix:** `mapEffortToSession` in `cmd/datawatch/main.go`'s spawn callback translates before POST:

- `low → quick`
- `medium → normal`
- `high | max | thorough → thorough`
- `quick | normal` pass through unchanged
- empty → empty (daemon falls back to `session.default_effort`)

Decoupling the two enums via translation keeps both APIs stable; alternative would have been collapsing them into one but that ripples into every CLI/MCP/PWA surface.

### `release-smoke.sh` now exercises the full PRD lifecycle

The pre-v5.26.11 smoke covered CRUD + decompose, but stopped before run. Operator-asked: validate that claude can run autonomously end-to-end. The new section 7b adds:

```
== 7b. Autonomous PRD full lifecycle (decompose → approve → run → spawn) ==
  PASS  decompose OK
  PASS  approve → approved
  PASS  run → running
  PASS  spawn round-trip survived effort-enum translation
        (spawned=1, post_spawn=[], pre_spawn=[])
```

Specifically asserts **zero pre-spawn failures** (i.e. no `invalid effort` rejections) — this is the explicit regression check for the bug closed above. Post-spawn failures (verifier rejecting, etc.) are tolerated since they depend on whether the worker LLM can actually complete the task. The smoke uses `backend=shell` so it doesn't burn LLM tokens; the spawn-round-trip path is identical regardless of backend.

After spawn, the smoke calls `DELETE /api/autonomous/prds/{id}` (cancel, not hard-delete) to avoid running tasks left over from a smoke run; cleanup_all in the EXIT trap then hard-deletes everything.

Initial run: **29 PASS / 0 FAIL / 2 SKIP**.

## Configuration parity

No new config knob.

## Tests

- 1397 Go unit tests passing.
- **Functional smoke: 29 PASS / 0 FAIL / 2 SKIP** via `scripts/release-smoke.sh`.

## Known follow-ups

- Worker session actually completing a task (decompose → approve → run → **complete**) needs a real LLM round-trip; smoke covers up to spawn.
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
# Restart the daemon: datawatch restart
# Try a New PRD with backend=claude-code, effort=low.
# Decompose, Approve, Run.
# Tasks will now spawn real worker sessions (status: in_progress).
# Operators running their own smoke before tag:
#   ./scripts/release-smoke.sh
```
