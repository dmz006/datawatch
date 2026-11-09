# Boot-resume test migration audit — task af1e0001/8f99001b

- **Date**: 2026-11-09
- **Task**: "Update the existing boot-resume tests that fail only because a planning
  Automaton is no longer reset to draft by default."
- **Verdict**: **No test change was made, by design — no test matching the task's own
  failure description exists in this branch.** The assertions the task expects to migrate
  were already migrated by two earlier tasks of the same PRD, which merged before this
  branch point.

## Why the task's precondition is stale

The task spec's search pattern — tests combining `PRDPlanning` with `PRDDraft` or
`killOrphanDecomposeSessions` — matches these files (all in `internal/autonomous` except
one):

| File | Status |
|---|---|
| `planning_reattach_test.go` | Already asserts the **new** re-attach behaviour (see below) |
| `planning_boot_outcomes_test.go` | Rewritten for the new `IngestOutput` / `RestartPlanning` outcomes |
| `orphan_decompose_test.go` | Sets `PlanningRestartedOnce` / `PlanningRestartPolicy = "draft"` to pin the legacy draft-reset contrast |
| `planning_reattach_manager_test.go` | Manager-level coverage of the re-attach path |
| `decompose_persist_failure_test.go`, `decompose_streaming_coverage_test.go`, `automation_coordinator_test.go`, `lifecycle_emit_prd_test.go`, `tag_settings_statuses_test.go`, `progress_engine_test.go` | All pass unchanged |
| `internal/server/notify_event_kinds_pwa_test.go` | Passes unchanged; unrelated event-kind check |

The migration was done by:

- `3c4732a1c` "Wire boot-resume to re-attach a reader to a live planner session"
  — created `planning_reattach.go` + `planning_reattach_test.go`.
- `6b35398ba` "Ingest a finished plan and restart planning once with a recorded
  decision" — updated `orphan_decompose_test.go` to use `PlanningRestartPolicy` and
  `PlanningRestartedOnce`, and rewrote `planning_boot_outcomes_test.go` for the
  `plan_recovered` / `planning_restarted` / restart-fallback outcomes.

Both commits are in this branch's history (merged well before this task was handed out).
The affected test family now reads for example:

- `TestBootResume_PlanningReattachLiveSessionStaysPlanning`
- `TestBootResume_PlanningReattachKillsOnlyUnownedPlannerSessions`
- `TestBootResume_PlanningReattachDraftPolicyResetsToDraft` (legacy draft-reset kept
  only as an explicit `PlanningRestartPolicy = "draft"` case)
- `TestBootResume_PlanningPRDContrastStillResetsToDraft`
- `TestBootResume_PlanningRestartOnce*` (five once-only-restart tests)

So the "old behaviour is still asserted" option and the "assert the new re-attach
behaviour" option in the task spec are both already present — neither edit is possible
without either weakening or duplicating a passing assertion, which the task's own
boundary ("Do not weaken any other assertion and do not change tests that pass
unchanged") forbids.

## Fresh verification performed for this task

Run from this worktree root, 2026-11-09 10:43 (local), clean tree (`git status`: nothing
to commit), branch `automaton/af1e0001--tasks-update-the-existing-boot-resume-tests-th-8f99001b`
at `dc15098e0`:

- `go test ./internal/autonomous -run 'BootResume|Boot' -count=1` →
  **84 tests PASS, 0 FAIL, 0 SKIP** (`ok ... 0.221s`).
- `go test ./internal/autonomous ./internal/server ./internal/session ./internal/config
  ./internal/router ./internal/mcp -run 'BootResume|Boot' -count=1` →
  autonomous + server `ok`, the other four packages report "no tests to run", no
  failures anywhere.
- Repo-wide grep for the task's search keywords confirms the 11 files in the table
  above are the complete set; none fail, none assert the old default-draft behaviour
  unconditionally.

## Conclusion

The specification is not wrong in intent, but its precondition — that failing tests of
the described kind exist in the working tree — no longer holds because sibling PRD
tasks already completed the migration this task was written to perform. Per the task's
own boundary clause, a test that passes unchanged is not to be changed, so the
correct and only compliant outcome is zero test-source edits. This report is the
audit record required to keep that no-op defensible.
