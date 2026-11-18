# 2026-11-12 04:35 — priority stall + backlog findings (s1-t2) — COMPLETE

Done:

1. `internal/autonomous/supervisor_watch_record.go` — added
   `SupervisorWatchPriorityStall = "priority_stall"` and
   `SupervisorWatchBacklog = "backlog"` to the kind set (14 → 16 kinds).
   `supervisor_watch_record_test.go` expectation updated (16 entries).

2. `internal/autonomous/supervisor_watch_work.go` — added the two real checks:
   - `CheckPriorityStall(now, cfg)`: one finding per waiting Automaton whose
     resolved priority is at or above `cfg.EffectivePriorityFloor()` and whose
     waiting age (whole minutes from creation, waiting set = draft, planning,
     needs_review, approved, revisions_asked) is strictly past
     `cfg.EffectiveStallThresholdMinutes()`. Report-only, kind+subject dedupe,
     reads only PRDs + config.
   - `CheckBacklog(now)`: the single consolidated `backlog` finding, subject
     fixed "backlog". Entries = waiting set + (failed|blocked with at least
     one task not completed/cancelled); sorted priority desc → created asc
     (missing = oldest) → id; capped at 16 entries with a "+k more" tail;
     `id(status)` or `id(status, n left)`. Empty queue raises nothing.

3. `internal/autonomous/supervisor_watch_text.go` — added
   `SupervisorWatchBacklogSubject = "backlog"`, the two kinds in
   `SupervisorWatchText` (single non-empty operator lines naming "Automaton/
   Automata", no tracker ids), and the `SupervisorWatchPriorityStallText` /
   `SupervisorWatchBacklogEntryText` / `SupervisorWatchBacklogText` helpers.

4. `internal/autonomous/supervisor_watch_pass.go` — pass runs
   `CheckPriorityStall` then `CheckBacklog` after `CheckWorktreeSilent`
   (before `CheckModelResident`).

5. Tests: `supervisor_watch_work_test.go` + `supervisor_watch_pass_test.go`
   gains — priority stall (below floor / past / inside threshold, floor 0,
   non-waiting, dedupe), backlog (order, tiebreaks, 16+tail cap, empty,
   failed-with-work-left included, left-0 exclusion, waiting left-0 text,
   dedupe), pass wiring (real CheckPriorityStall end-to-end with s1-t1 config;
   both new kinds recorded and not already reported).

6. `scripts/supervisor-watch.py` — `ts_of()` helper; `PRIORITY_FLOOR=80`,
   `STALL_THRESHOLD_MIN=30`, waiting statuses, cap 16. Replaced the old
   three-way backlog_failed/blocked/review split with the consolidated
   `backlog` (same sort and cap as the daemon) and a `priority_stall` list;
   `map_finding` maps both line shapes (`PRIORITY STALL (\S+):…` →
   kind priority_stall + prd id; `BACKLOG ` → kind backlog, subject "backlog").
   Removed the draft-HOLD-marker heuristic from backlog inclusion (daemon has
   no such filter; waiting set is status-based) so the two agree.

Docs:
- `docs/howto/automation-supervisor-watch.md`: sixteen checks, two new finding
  table rows, config table rows for the s1-t1 keys (`stall_threshold_minutes`
  default 30, `priority_floor` default 80), working-model "other fifteen".
- `docs/operations.md`: kinds list + sentence updated, two config rows added.
- `docs/datawatch-definitions.md`: Supervisor watch entry counts + kinds list.
- Regenerated `internal/server/web/assets/docs-bm25-index.json` (tracked in
  the older layout; gitignored here) and the `make docs-index` gitignored
  copy — 6448 chunks / 323 paths, identical on both.

Verification:
- `go build ./...` ok; `go vet ./internal/autonomous/` clean.
- `rtk go test ./internal/autonomous/ ./internal/docsindex/...` — 8674 passed,
  0 failed. `TestSupervisorWatchCompare*` (daemon vs script) 8 passed.
  `rtk go test -run SupervisorWatch` — 197 passed.
- `python3 -m py_compile scripts/supervisor-watch.py` ok.
- `docsindex` package tests — 30 passed (index regeneration is valid).
