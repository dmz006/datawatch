# datawatch v5.26.64 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.63 → v5.26.64
**Patch release** (no binaries — operator directive).
**Closed:** PRD-flow Phase 4 — file association (schema + manager + REST + tests + PWA pills).

## What's new

Phase 4 lands per design at [`docs/plans/2026-04-27-prd-phase4-file-association.md`](2026-04-27-prd-phase4-file-association.md). Earlier work (v5.26.30/32/34/36/37/46/60/61/62/63) shipped Phases 1, 2, 3, 5, 6, plus the New-Session-modal parity. **Phase 4 closes the design backlog.**

### Schema

```go
type Story struct {
    ...existing
    FilesPlanned []string `json:"files_planned,omitempty"`   // LLM-extracted, operator-editable
}

type Task struct {
    ...existing
    FilesPlanned []string `json:"files_planned,omitempty"`   // LLM-extracted
    FilesTouched []string `json:"files_touched,omitempty"`   // post-spawn from session diff
}
```

50-path cap per list (per the design's 5KB-per-story budget).

### Manager methods

| Method | Purpose | When allowed |
|------|------|------|
| `SetStoryFiles(prd, story, files, actor)` | Operator edits Story.FilesPlanned | `needs_review` / `revisions_asked` |
| `SetTaskFiles(prd, task, files, actor)` | Operator edits Task.FilesPlanned | same |
| `RecordTaskFilesTouched(prd, task, files)` | Daemon-internal hook for post-session diff | post-spawn (no lock gate) |

Each operator-facing method appends `set_story_files` / `set_task_files` audit decisions; the post-spawn hook is silent (its evidence is the `files_touched` field itself).

### REST surface

```
POST /api/autonomous/prds/{prd_id}/set_story_files
  { story_id, files: [...], actor? }

POST /api/autonomous/prds/{prd_id}/set_task_files
  { task_id, files: [...], actor? }
```

Both behind the same lock-after-approve gate the rest of the story/task edit endpoints use. `RecordTaskFilesTouched` is daemon-internal — no REST route (the post-session diff callback fires it directly).

### PWA file pills

Story rows render `📝 [list of paths]` (accent-blue) when `FilesPlanned` is set. Task rows render two distinct rows when populated: `📝 planned` (blue) and `✅ touched` (green). Operator can spot at a glance:

- Which files a story is *planned* to touch (review-time signal).
- Which files the worker *actually* touched after spawn (audit-time signal).
- The diff between the two (planning accuracy heuristic).

Editing the file list goes through the existing `set_story_files` / `set_task_files` endpoints. PWA modal for file editing is a follow-up — currently operators edit via REST (or via a future CSV-style edit modal mirroring the v5.26.8 pattern).

### What this deliberately defers

- **Decomposer prompt change** — the design proposed extending the decompose prompt to ask the LLM for `files: [...]` per story/task. v5.26.64 lands the schema + handlers; the prompt update can ship in a follow-up patch (v5.26.65+) without breaking compatibility with PRDs that have empty `FilesPlanned`.
- **Post-session diff hook wiring** — the schema field + manager method are in place; wiring the actual `git diff --name-only` callback from the session-end path is a follow-up. Manual operators (or smoke probes) can call `RecordTaskFilesTouched` directly for now.
- **Conflict-detection (two stories planning the same file)** — Phase 4.1 follow-up per the design doc.

These let v5.26.64 ship today as a clean Phase 4 milestone without blocking on prompt-engineering / post-session-hook plumbing.

## Configuration parity

REST + MCP + CLI + comm channel reach for both new endpoints (the daemon's existing autonomous-API forwarder routes them automatically).

## Tests

4 new unit tests in `internal/autonomous/lifecycle_test.go`:

- `TestSetStoryFiles_RewritesAndCaps` — happy path + 50-cap enforcement
- `TestSetStoryFiles_RefusesAfterApprove` — lock-after-approve gate
- `TestSetTaskFiles_RewritesAndAudits` — task-level + audit decision
- `TestRecordTaskFilesTouched_PostSpawnNoLock` — daemon-internal hook works post-PRD-approval

Total: 475 unit tests passing (was 471; +4 net). Smoke unaffected (Phase 4 doesn't activate without operator action).

## Phase status — done

| Phase | Status |
|------|------|
| Phase 1 — unified Profile dropdown | ✅ v5.26.30 + .34 |
| Phase 2 — story review/edit | ✅ v5.26.32 |
| Phase 3 — per-story execution profile + approval | ✅ v5.26.60–62 |
| Phase 4 — file association | ✅ v5.26.64 |
| Phase 5 — persistent smoke fixtures | ✅ v5.26.33 |
| Phase 6 — howtos + screenshots + diagrams | 🟡 howto v5.26.39, screenshots v5.26.54; diagram updates pending |

The PRD-flow rework's design backlog is now fully implemented (modulo Phase 6 diagram updates, which are docs-only).

## Known follow-ups

- **#39** wake-up stack L0–L5 smoke probes
- **#41** docs/testing.md ↔ smoke coverage audit
- Phase 4 follow-ups: decomposer prompt update, post-session diff callback wiring, file-conflict detection, PWA file-edit modal

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA — story rows show 📝 file pills when
# FilesPlanned is set; task rows additionally show ✅ touched
# pills post-spawn (once the post-session hook is wired in a
# follow-up patch).
```
