# Show finished-not-merged on every surface

- **Date:** 2026-11-13
- **Version:** current branch (last set version 9.172.14)
- **Scope:** `internal/autonomous/` (coordinator triage pass, merge-pending pass, supervisor watch tests), `internal/server/web/` (PWA tests, OpenAPI), `internal/server/` (rollup/timeline, already present), `docs/`.
- **Parent work:** `task(e5700001)` — first attempt `7c07e948f` shipped the display surface (rollup/derivation, PWA badge + locales + CSS, glossary entry) with `Verification: not run before this commit`. Companion archive-side work `7ec771931` (merged-branch guard on the archive step, CLI + docs).

## What "finished-not-merged" means

An Automaton is finished when its status is completed, or it has since been archived. It is not merged when its working branch still carries commits that never landed on the base branch. For every such record a derived, read-only `merge_state` field with the single value `finished_not_merged` appears on every read surface. Nothing is written, alerted, or recorded for it — the daemon derives it at view time.

## Phases

### Phase 1 — Display surfaces (already shipped, `7c07e948f` + `7ec771931`)

- Derivation of `merge_state` in the project rollup and timeline (`internal/server/project_rollup.go`, `project_timeline.go`) covering both completed and archived records.
- PWA: one `merge-state-badge` per finished-not-merged Automaton on the Projects card line, the project page (arcs sub-tab) and the Gantt row label, exactly one badge per record; copy in all five locale bundles.
- Glossary entry in `docs/datawatch-definitions.md`.

Checklist:
- [x] Functionality implemented (committed `7c07e948f`).
- [x] Unit tests added (rollup/timeline tests, PWA tests in `app-merge-state.test.js`).
- [x] Live/smoke test — N/A: derivation is a pure projection over existing records and the archive guard's live behavior is covered by the archive-commit's own gate; no persistence change in this phase.
- [x] `go test ./...` (touched packages) and `node --test internal/server/web/app-merge-state.test.js` green — re-verified 2026-11-13 (Phase 5).
- [x] General docs: glossary entry present (this phase); `CHANGELOG.md` N/A — no version bump per task rule; `docs/config-reference.yaml` N/A — no new or changed settings (merge-pending config exists); `docs/operations.md` + `docs/howto/supervisor-loop.md` updated by `7ec771931`; `docs/testing-tracker.md` rows present (archive-guard commit) and extended here; no leaked tracker IDs (gate-checked); `Parity surface` section below.
- [x] Conditional docs: glossary entry shipped with the phase; no new howto — the merge-pending supervisor loop howto already exists.

### Phase 2 — Archived Automata stay in the coordinator merge-ready list

- `internal/autonomous/automation_coordinator.go` — `triagePass`: the no-failed-record clearing branch now treats a finished record as completed **or** archived (`prd.Status == PRDCompleted || prd.Status == PRDArchived`), so an archived Automaton is not left with a stale triage record and is handled in the same merge-ready sweep as a completed one.
- `internal/autonomous/merge_pending_pass.go` — doc comments corrected: the pass and `mergePendingCheck` describe "a finished (completed, or archived) Automaton", which is what `AutomatonMergeState()` (the canonical status check) already accepts.

Checklist:
- [x] Functionality implemented (coordinator branch fix; comments).
- [x] Unit tests: `TestSupervisorWatchWorkArchivedFinishedNotMerged`, `TestMergePendingPass_ArchivedFinishedNotMergedKeepsRecord`, `TestMergePendingPass_ArchivedFinishedNotMergedShadowObserved` (names carry `Finished`/`NotMerged` per task rule).
- [x] Live/smoke test — N/A: pass behavior is fake-git + fixed clock; the alert path itself was live-checked 2026-10-30 (see `docs/plans/2026-10-29-automation-merge-pending-live-check.md`).
- [x] `go test ./internal/autonomous/` full package green (2026-11-13, exit 0); touched-package filtered runs green.
- [x] General docs: `CHANGELOG.md` N/A — no version bump; `docs/config-reference.yaml` N/A — no settings change; `docs/operations.md`/`docs/howto/supervisor-loop.md` already describe the completed+archived behavior from `7ec771931`; `docs/testing-tracker.md` — 3 new rows added today; no tracker IDs in the diff (grep-clean); `Parity surface` section updated.
- [x] Conditional docs: no new operator-facing concept — the behavior already exists and is defined in the glossary.

### Phase 3 — OpenAPI documentation for the read-only field

- `internal/server/web/openapi.yaml` — list, single-record and `PRDRecord` descriptions document `merge_state`: derived, read-only, single value `finished_not_merged`, present for completed **or** since-archived records, no record/alert/write for it.

Checklist:
- [x] Functionality implemented (spec text).
- [x] Unit tests — N/A: YAML documentation change; existing surface tests in `internal/server`, `internal/mcp`, `cmd/datawatch` (filtered `NotMerged|Finished` runs, 13 pass) re-confirm the field still lands as before.
- [x] Live/smoke test — N/A: spec-only change; the endpoints were live-checked 2026-10-30.
- [x] Tests green per Phase 5; spec parses (`yaml.safe_load` ok).
- [x] General docs: no code paths, no settings, no strings, no tracker IDs in the diff.
- [x] Conditional docs: N/A — the glossary entry already covers the concept.

### Phase 4 — PWA rendering tests for the two new surfaces exercised

- `internal/server/web/app-merge-state.test.js` — exact-badge-count tests: the project page (arcs view) renders exactly one `merge-state-badge` for a finished-not-merged Automaton and none otherwise; the Gantt row label renders exactly one badge when `merge_state` is set and none for a record without it (badge count across the whole render is asserted, not just presence).

Checklist:
- [x] Functionality implemented (tests only; rendering code already shipped in Phase 1).
- [x] Unit tests green: 10 pass / 0 fail, 2026-11-13.
- [x] Live/smoke test — N/A: rendering-function tests with a vm sandbox and stubbed globals; no DOM.
- [x] All other PWA test files in the touched area green (`app-projects-layout`, `app-projects-gantt-focus`: 19 pass / 0 fail).
- [x] General docs: N/A — no new strings in this phase (badge copy shipped in Phase 1); no settings; no tracker IDs.
- [x] Conditional docs: N/A.

### Phase 5 — Verification gate (the missing step of the first attempt)

- `gofmt` clean on `internal/autonomous/`; `go vet` clean across `internal/autonomous`, `internal/server`, `internal/router`, `internal/mcp`, `cmd/datawatch`.
- Filtered test runs (`-run 'NotMerged|Finished'`) green in `internal/server`, `internal/router`, `internal/mcp`, `cmd/datawatch` (13 pass).
- Full `go test ./internal/autonomous/` (count=1) exit 0.
- `app-merge-state.test.js` 10/10, neighboring layout/gantt PWA suites green; `node --check app.js` clean.
- Diff is free of tracker IDs outside `docs/plans`; no version bump; no changelog entry (task rule).
- Gate script `.datawatch/verify/finished-not-merged-surfaces.sh` was replicated step by step — its `_common.sh` helper does not exist in this worktree, so the same checks were run directly.

Checklist:
- [x] All gate steps above executed 2026-11-13 with passing results.
- [x] Live/smoke test — N/A for this phase (verification of already-shipped behavior; the merge-pending live check of 2026-10-30 remains the live reference).
- [x] Docs: tracker rows added; general-docs checklist covered by the phases they belong to.

## Parity surface

| Surface | Touched | Notes |
|---|---|---|
| REST | Yes | `merge_state` derivation on list/single endpoints (Phase 1); behavior and shape unchanged by Phases 2–5, now documented in `internal/server/web/openapi.yaml` (Phase 3). |
| MCP | No new change | `get automaton` already surfaces the same field; filtered `NotMerged|Finished` MCP tests re-pass (Phase 5). No new tool or arg. |
| CLI | No new change | `datawatch autonomous` already prints the finished-not-merged note (shipped with `7ec771931`); filtered `cmd/datawatch` tests re-pass. |
| Comm channel | No new change | No new chat verb; the supervisor's `automaton_not_merged` finding (existing) now also covers archived records (Phase 2). Existing channel surfaces it as before. |
| YAML/config | No change | No new or renamed setting; `merge_pending_mode` and `merge_pending_alert_minutes` semantics unchanged. |
| PWA | Yes (tests only this round) | Badge on card line, project page and Gantt row (Phase 1); exact-count rendering tests (Phase 4). |
| Android | Excluded — mobile rendering of the finished-not-merged badge is a datawatch-app capability to mirror from the same REST field; no API contract change, no new string added in this round. Tracked via the parent PRD's app-side surface, not in this repo. |
| iPhone/iOS | Excluded — same reason as Android: no API contract change in this round; the native client renders the same `merge_state` field when it mirrors the PWA card. Tracked outside this repo. |

Shipped with (planned): current branch, no version bump per task rule.
