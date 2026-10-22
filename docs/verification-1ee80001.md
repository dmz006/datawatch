# End-to-end verification — PRD 1ee80001 (test/tmp-leak cleanup change)

Date: 2026-07-22 (rerun at 06:14–06:17 local)
Mode: read-only verification; no edits made.

## Result: ALL 5 CHECKS PASS

| # | Check | Result | Evidence |
|---|-------|--------|----------|
| 1 | `go build ./...` | PASS | Exit 0, 2s, no output |
| 2 | `go test -count=1 -timeout 60m ./internal/mcp/ ./internal/server/ ./internal/session/ ./internal/plugins/ ./internal/llm/backends/pi/` | PASS | Exit 0, 122s; all 5 packages `ok` (mcp 1.476s, server 22.531s, session 121.246s, plugins 15.694s, pi 0.025s). `TestNoNewTmpLeakEntries` independently confirmed: `--- PASS: TestNoNewTmpLeakEntries (37.36s)` in internal/session — this test runs an inner `go test` and asserts the system temp dir gains no new entries with the dw-test-home-, svchelper-, sched-test-, datawatch-pi-task-, datawatch-pi-status- or datawatch-pi-out- prefixes |
| 3 | `gofmt -l` on the 8 named files | PASS | All 8 files exist, all print nothing: internal/mcp/home_isolation_test.go, internal/session/home_isolation_test.go, internal/server/home_isolation_test.go, internal/plugins/supervisor_test.go, internal/session/schedule_test.go, internal/session/oneshot_reap.go, internal/session/oneshot_scratch_cleanup_test.go, internal/session/tmp_leak_regression_test.go |
| 4 | Diff scope (no CHANGELOG.md / version files) | PASS | `git diff --name-only --diff-filter=ACMR` empty (unstaged, staged, and vs HEAD all clean); the change is already landed as five per-task commits (see below), all test/cleanup-scoped, none touching CHANGELOG.md or any version file. Last CHANGELOG-touching commit predates the task commits |
| 5 | Spot-check of core guarantees | PASS | See below |

### Step 5 spot-check details

1. **TestMain leak-safety (defer before `m.Run()`)**
   - `internal/mcp/home_isolation_test.go`: `defer os.RemoveAll(home)` registered before `m.Run()` (plus explicit remove after).
   - `internal/session/home_isolation_test.go`: same pattern.
   - `internal/server/home_isolation_test.go`: same pattern.
   - `internal/plugins/supervisor_test.go`: `defer os.RemoveAll(dir)` registered before `m.Run()` (plus explicit remove after).

2. **`removeOneShotScratch` scratch coverage** (`internal/session/oneshot_reap.go`, lines 130–144)
   - Globs and removes, for each of the three prefixes `datawatch-pi-task-`, `datawatch-pi-status-`, `datawatch-pi-out-`, all files matching `<prefix><session-part>-*` in `os.TempDir()`. Session part derived via `piScratchNamePart`; empty session name is a no-op.

### Landed change commits (all test/cleanup-scoped)

```
ebd183e3 task(1ee80001): Add TestNoNewTmpLeakEntries regression test
2b7af790 task(1ee80001): Extend removeOneShotScratch to cover pi output and status scratch files
49f725d6 task(1ee80001): Convert schedule-store tests to t.TempDir()
54b2dee2 task(1ee80001): Make the plugins supervisor TestMain leak-safe
69f3689b task(1ee80001): Make the three home_isolation TestMains leak-safe
```
