# Final Gate Report — Record the skip list from the attempt snapshot (e0cb0003)

Date: 2026-11-01

## Requirement coverage

1. **`SnapshotAttemptWorkWithSkips` added** — `internal/autonomous/attempt_snapshot.go:39`
   `func SnapshotAttemptWorkWithSkips(ctx context.Context, dir, taskKey string, attempt int) (ref string, skipped []gitguard.Skip, err error)`
   Holds the full previous body of `SnapshotAttemptWork`; `skipped` is nil on every
   return before `p.Keep` computes it and returned (non-nil) on every return after:
   post-staging failure, file-count refusal, `write-tree`/`commit-tree`/`update-ref`
   failures, the no-change `tree == headTree` path, and the success path.
   → PASS (0 build errors, 0 test failures)

2. **`SnapshotAttemptWork` is a thin wrapper** — `internal/autonomous/attempt_snapshot.go:30`
   Two-value signature unchanged; calls `SnapshotAttemptWorkWithSkips` and drops the
   skip list. All existing callers (13 call sites in `attempt_snapshot_test.go`,
   `attempt_restore_test.go`, `attempt_snapshot_guard_test.go`) keep compiling.
   → PASS (0, 0)

3. **Interface extended** — `cmd/datawatch/attempt_preserve.go:17`
   `attemptSnapshotStore` now requires
   `RecordTaskSkippedSnapshotPaths(prdID, taskID string, skipped []autonomouspkg.SnapshotSkippedPath) error`.
   The real `*autonomous.Manager` already implements it (`internal/autonomous/manager.go:4343`),
   so `main.go` call sites (`killStalledWorker(..., autonomousMgrRef, ...)`) satisfy it without change.
   → PASS (0, 0)

4. **Fake updated** — `cmd/datawatch/attempt_preserve_test.go:36`
   `fakeStallStore` (the only fake implementing the interface in `cmd/datawatch` test files)
   gained `RecordTaskSkippedSnapshotPaths`, recording into the shared event log
   (`record-skipped:<n>`) and the `skipped` capture slice, with an injectable
   `recordSkippedErr`. `end_attribution_test.go` reuses the same fake and compiles.
   → PASS (0, 0)

5. **`preserveAttemptWork` records the skip list** — `cmd/datawatch/attempt_preserve.go:44`
   Calls `autonomouspkg.SnapshotAttemptWorkWithSkips`; when the skip list is non-empty
   and `store != nil`, records `autonomouspkg.TaskSnapshotSkippedFromGuard(skipped)` via
   the store **before** the error/ref handling, so it runs even when `ref == ""` or the
   snapshot failed after the staging pass. Record failures are logged as
   `recording N staging-guard skip(s) on the task failed: <err>` — count only, no path
   names. No new log line carries a path.
   → PASS (0, 0)

## Commands

- `rtk go build ./...` — PASS (0 errors)
- `rtk go vet ./internal/autonomous/ ./cmd/datawatch/` — PASS (0, 0)
- `rtk go test ./internal/autonomous/ ./cmd/datawatch/` — PASS, 7068 tests, 0 failures

No new tests were required by the task; existing suites cover the wrapper contract
(snapshot success/clean/stash/sanitized-key/ro-index, guard refusal, restore) and the
stall-kill ordering with the extended fake.
