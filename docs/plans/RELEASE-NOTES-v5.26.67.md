# datawatch v5.26.67 — release notes

**Date:** 2026-04-28
**Patch release** (no binaries — operator directive).
**Closed:** Phase 4 follow-ups — decomposer prompt + post-session diff callback + file-conflict detection + PWA file-edit modal.

## What's new

Per-task implementation of the four Phase 4 follow-ups documented in v5.26.64's "deferred" section. Bundled into one patch since each is small.

### 1. Decomposer prompt extension (#47)

`internal/autonomous/decompose.go` — `DecompositionPrompt` now asks the LLM to emit `files: [...]` per story and per task. Empty array is valid when paths are unknown ahead of time. Existing PRDs in stores are unaffected (empty `FilesPlanned` stays empty).

JSON tag for `Story.FilesPlanned` and `Task.FilesPlanned` renamed `files_planned` → `files` so the LLM's natural emission shape matches the stored PRD JSON shape and the REST `set_story_files` body — single name across the wire.

### 2. Post-session diff callback (#48)

`internal/session/git.go` — new `ProjectGit.DiffNames()` runs `git diff --name-only HEAD~1..HEAD` and returns up to 50 paths.

`cmd/datawatch/main.go` — declares `var autonomousFilesHook` early, wires it inside the autonomous-Manager init block. Hook fires from the existing `SetOnSessionEnd` callback when a session reaches `complete`. If `Manager.FindTaskBySessionID(sess.FullID)` returns a (prd_id, task_id) pair, runs `DiffNames()` against the worker's `ProjectDir` and calls `RecordTaskFilesTouched`. No-op for sessions that aren't autonomous spawns.

### 3. File-conflict detection (#49)

`renderPRDRow` walks the PRD's stories and builds a `file → first-story-id` map. Stories that plan a file already claimed by an earlier (still-pending) story get a `_conflictSet` annotation. `renderStory` reads the conflict set and prepends a ⚠ marker on conflicting files, with a tooltip naming the colliding story id.

Completed/blocked/failed stories don't participate (they're not in flight).

### 4. PWA file-edit modal (#50)

Two new modals:

- `openPRDEditStoryFilesModal(prdID, storyID, currentFiles)` — textarea (one path per line) + mic input + 50-cap. Save POSTs to `set_story_files`.
- `openPRDEditTaskFilesModal(prdID, taskID, currentFiles)` — same shape for tasks; POSTs to `set_task_files`.

Story rows render a `✎ files` button next to the file pill row when the PRD is in `needs_review`/`revisions_asked`. Task rows render a small `✎` next to their file pill. Both behind the existing lock-after-approve gate.

## Configuration parity

No new config knob.

## Tests

Build clean. Smoke unaffected (60/0/3). Go test suite unaffected (475 passing).

## Phase 4 — fully implemented

| Sub-item | Status |
|------|------|
| Schema + REST + tests | ✅ v5.26.64 |
| Decomposer prompt extension | ✅ this patch |
| Post-session diff callback wiring | ✅ this patch |
| File-conflict detection | ✅ this patch |
| PWA file-edit modal | ✅ this patch |

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA. Decompose a new PRD — the LLM should emit
# files arrays per story/task; PWA renders 📝 file pills with
# ⚠ markers on conflicts; ✎ files button opens the edit modal.
# Worker session diff fires automatically post-spawn; ✅ touched
# files appear on each task's row.
```
