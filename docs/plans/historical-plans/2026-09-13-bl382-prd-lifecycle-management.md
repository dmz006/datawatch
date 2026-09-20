# BL382 — PRD Lifecycle Management: cancel_story / cancel_task / requeue_task

**Date:** 2026-09-13  
**Version at planning time:** v8.26.1  
**Target release:** v8.28.0 (minor; BL381 ships as v8.27.0 first)  
**Plan author:** dmz006  
**GitHub issues:** GH#149 (cancel_story), GH#150 (cancel_task), GH#151 (requeue_task), GH#152 (umbrella + component parity)  
**Related app issues:** datawatch-app#171 (edit_task UI — server already has endpoint), datawatch-app#172 (approve note — server already accepts `note` param)

---

## Problem

PRD operators have no way to skip a single story or task that is no longer needed without cancelling the entire PRD. `reset_task` (v8.23.0) resets *failed/blocked* tasks back to pending but cannot requeue a successfully completed task. There is also no component-level parity: no PWA affordances, no MCP tools, and no comms channel events for these operations.

---

## Scope

### In scope
- 3 new server endpoints (or 2 new + 1 extended)
- MCP tools for all 3 operations
- Comms channel events (story_cancelled, task_cancelled, task_requeued)
- PWA affordances: cancel story, cancel task, requeue task
- Locale strings × 5 (en/de/es/fr/ja) for new PWA actions
- Audit log emission for all 3 operations
- Federation behavior documented; graceful 422 fallback
- Observability: cancel/requeue counters in SystemStats
- Testing tracker sections (B1)
- release-smoke.sh extended (B12)
- docs/testing-tracker.md updated
- GitHub issues updated as each phase lands

### Out of scope
- datawatch-app#171 edit_task UI — server endpoint already exists; app-side work only
- datawatch-app#172 approve note — server `Approve(id, actor, note string)` already accepts note; app-side work only
- BL29 Android app implementation — gated on this sprint; app team picks up after server endpoints land
- BL30 Android Auto lifecycle parity — gated on BL29

---

## Architecture

### Reuse decision
Extends the existing `AutonomousAPI` interface pattern used by `Approve`, `Reject`, `Cancel`, `ResetTask`, and `ApproveStory`/`RejectStory`. Each operation:
1. Validates state preconditions, returns 409 with description on invalid transition
2. Writes a `Decision` row (same as approve/reject/cancel do today)
3. Stops/interrupts any running session for the targeted story/task
4. Returns the updated `PrdDto` so callers get full state in one round-trip

### requeue_task: Option A (extend reset_task)
Extend `POST /api/autonomous/prds/{id}/reset_task` to accept an optional `force: true` flag. Without `force`, existing behaviour is preserved (only `failed`/`blocked` → `pending`). With `force: true`, any terminal state including `complete` and `cancelled` is reset to `pending`.

Rationale: avoids a new endpoint, simpler MCP surface, matches the issue's own Option A preference. The `ResetTask` function signature is extended to `ResetTask(prdID, taskID, actor string, force bool)` — all existing callers pass `force: false`.

---

## Files to change

| File | Change |
|------|--------|
| `internal/server/api.go` | Add `CancelStory`, `CancelTask` to `AutonomousAPI` interface; extend `ResetTask` signature with `force bool`; add HTTP handlers; add MCP tool registrations |
| `internal/autonomous/manager.go` | Implement `CancelStory`, `CancelTask`; extend `ResetTask` for force; comms event dispatch |
| `internal/autonomous/models.go` | No struct changes expected (story/task already have `status` field) |
| `internal/server/orchestrator_enrich_test.go` | Add `CancelStory`, `CancelTask` stubs to `fakeOrchAutonomous`; update `ResetTask` stub signature |
| `internal/server/web/app.js` | PWA: cancel story menu item, cancel task menu item, requeue task button, confirmation dialogs |
| `internal/server/web/locales/en.json` | New locale keys (all 5 locales updated) |
| `internal/server/web/locales/de.json` | Translated keys |
| `internal/server/web/locales/es.json` | Translated keys |
| `internal/server/web/locales/fr.json` | Translated keys |
| `internal/server/web/locales/ja.json` | Translated keys |
| `internal/server/v5280_locales_test.go` | Add new high-visibility keys to `mustHave` slice |
| `internal/autonomous/bl382_cancel_test.go` | New: unit tests for CancelStory, CancelTask, ResetTask force |
| `docs/testing-tracker.md` | New sections for all 3 endpoints |
| `scripts/release-smoke.sh` | New sections §N (cancel_story), §N+1 (cancel_task), §N+2 (requeue_task) |
| `docs/parity-status.md` | Update PWA/Android/MCP parity rows |

Estimated: 14 files. Plan doc required per AGENT.md § Planning Rules (3+ files).

---

## Phase breakdown

### Phase 1 — Server API + tests

**Goal:** All 3 operations available via REST. Tests pass. `AutonomousAPI` interface satisfied.

**Steps:**
1. Add to `AutonomousAPI` interface in `api.go`:
   ```go
   CancelStory(prdID, storyID, actor, reason string) (any, error)
   CancelTask(prdID, taskID, actor, reason string) (any, error)
   ```
2. Update `ResetTask` signature: `ResetTask(prdID, taskID, actor string, force bool) (any, error)` — update interface + all call sites.
3. Add `fakeOrchAutonomous` stubs in `orchestrator_enrich_test.go`:
   ```go
   func (f *fakeOrchAutonomous) CancelStory(string, string, string, string) (any, error) { return nil, nil }
   func (f *fakeOrchAutonomous) CancelTask(string, string, string, string) (any, error) { return nil, nil }
   func (f *fakeOrchAutonomous) ResetTask(string, string, string, bool) (any, error) { return nil, nil }
   ```
4. Implement in `manager.go`:
   - `CancelStory`: load PRD, find story by ID, validate not already terminal, mark `cancelled`, stop any `in_progress` tasks via session kill, write Decision, return updated PrdDto.
   - `CancelTask`: load PRD, find task by ID, validate not already terminal, mark `cancelled`, if session is running kill it (same pattern as PRD Cancel), write Decision, return updated PrdDto.
   - `ResetTask` force path: if `force=true`, allow reset from `complete` or `cancelled` in addition to existing `failed`/`blocked`.
5. Wire HTTP handlers in `api.go`:
   - `POST /api/autonomous/prds/{id}/cancel_story` → body `{story_id, actor?, reason?}`
   - `POST /api/autonomous/prds/{id}/cancel_task` → body `{task_id, actor?, reason?}`
   - `POST /api/autonomous/prds/{id}/reset_task` — extend to accept `force` bool in body
6. State precondition errors: return HTTP 409 with `{"error": "story already in terminal state: <status>"}`.
7. Write `internal/autonomous/bl382_cancel_test.go`:
   - `TestBL382_CancelStory_PendingStory_MarkedCancelled`
   - `TestBL382_CancelStory_InProgressStory_SessionKilled`
   - `TestBL382_CancelStory_AlreadyCancelled_Returns409`
   - `TestBL382_CancelTask_PendingTask_MarkedCancelled`
   - `TestBL382_CancelTask_InProgressTask_SessionKilled`
   - `TestBL382_CancelTask_AlreadyCancelled_Returns409`
   - `TestBL382_ResetTask_Force_CompletedTask_ResetsToPending`
   - `TestBL382_ResetTask_NoForce_CompletedTask_Returns409`
   - `TestBL382_ResetTask_Force_CancelledTask_ResetsToPending`

**AGENT.md checklist triggers (Phase 1):**
- B1: New endpoint contracts → testing-tracker.md (Phase 6)
- B9: Reuse-and-expand audit: extends `Decision` row pattern from `approve`/`reject`/`cancel`; extends `ResetTask` primitive
- B10: Skills-awareness check — no new session/PRD/agent path; N/A
- B16: New audit events → JSON-lines round-trip test + CEF (Phase 5)

**Update GH issues when Phase 1 merges:**
- Close GH#149 with comment: "Implemented in v8.28.0 — `POST /api/autonomous/prds/{id}/cancel_story`"
- Close GH#150 with comment: "Implemented in v8.28.0 — `POST /api/autonomous/prds/{id}/cancel_task`"
- Close GH#151 with comment: "Implemented in v8.28.0 — `reset_task` extended with `force: true` to requeue completed tasks"

---

### Phase 2 — Comms channel events

**Goal:** story_cancelled, task_cancelled, task_requeued events emitted via comms channel.

**Steps:**
1. In `manager.go`, after each successful operation call the existing comms event dispatcher (same pattern as the `needs_review`, `approved`, `cancelled` events):
   - `story_cancelled`: "Story `<story.Title>` skipped in `<prd.Name>` — `<reason if set>`"
   - `task_cancelled`: "Task `<task.Title>` cancelled in story `<story.Title>` (`<prd.Name>`)"
   - `task_requeued`: "Task `<task.Title>` re-queued in story `<story.Title>` (`<prd.Name>`)"
2. Events respect existing channel routing rules (per-project profile, per-PRD overrides).
3. Add unit tests: `TestBL382_CancelStory_EmitsChannelEvent`, `TestBL382_CancelTask_EmitsChannelEvent`, `TestBL382_RequeueTask_EmitsChannelEvent`.

**AGENT.md checklist triggers (Phase 2):**
- B8: New operator-facing behaviour → feature docs (Phase 6): document channel events in all 5 access-method rows

---

### Phase 3 — MCP tools

**Goal:** `autonomous_prd_cancel_story`, `autonomous_prd_cancel_task`, `autonomous_prd_requeue_task` available in MCP.

**Steps:**
1. In `internal/mcp/server.go` (or wherever `autonomous_prd_*` tools are registered), add 3 new tool registrations following the `autonomous_prd_approve` / `autonomous_prd_reset_task` pattern.
2. Each tool delegates to the same `AutonomousAPI` method added in Phase 1.
3. Document in `docs/mcp.md` — add to the Autonomous PRDs tool table.

**AGENT.md checklist triggers (Phase 3):**
- B7: New feature → observability MCP tool added (counts as the MCP surface component)
- B8: MCP tool doc in `docs/mcp.md`

**Update GH issues when Phase 3 merges:**
- Comment on GH#152: "MCP tools `autonomous_prd_cancel_story`, `autonomous_prd_cancel_task`, `autonomous_prd_requeue_task` added in v8.28.0"

---

### Phase 4 — PWA affordances + locales

**Goal:** Cancel story, cancel task, requeue task accessible from the PWA story/task management views.

**Steps:**
1. Cancel story — add to the ⋯ more menu on story rows:
   - Visible when story status is `pending`, `in_progress`, or `awaiting_approval`
   - Triggers a confirmation dialog: "Skip story: `<title>`? (optional reason)" with Cancel / Confirm buttons
   - On confirm: `POST /api/autonomous/prds/{prdId}/cancel_story` → refresh PRD detail
2. Cancel task — add to the ⋯ more menu on task rows:
   - Visible when task status is `pending`, `in_progress`, or `complete`
   - Confirmation dialog: "Cancel task: `<title>`? (optional reason)"
   - On confirm: `POST .../cancel_task` → refresh
3. Requeue task — add ↺ Re-run inline button on task rows:
   - Visible when task status is `complete` or `cancelled`
   - No confirmation dialog (low-risk, already terminal)
   - On click: `POST .../reset_task` with `{task_id, force: true}` → refresh
4. Locale keys to add (en.json):
   - `autonomous.cancelStory` = "Skip story"
   - `autonomous.cancelTask` = "Cancel task"
   - `autonomous.requeueTask` = "Re-run task"
   - `autonomous.cancelStoryConfirm` = "Skip this story? The rest of the PRD will continue."
   - `autonomous.cancelTaskConfirm` = "Cancel this task? The parent story will continue with remaining tasks."
   - `autonomous.reasonOptional` = "Reason (optional)"
5. Add the 5 new high-visibility action keys (`cancelStory`, `cancelTask`, `requeueTask`) to `mustHave` in `internal/server/v5280_locales_test.go`.
6. Translate all 5 keys into de/es/fr/ja.

**AGENT.md checklist triggers (Phase 4):**
- B2: New user-facing strings → locales × 5
- B3: High-visibility locale keys → `TestLocales_CommonNavKeysPresent` mustHave slice updated
- B4: Operator-visible PWA changes → comment on datawatch-app#171 and datawatch-app#172 noting server-side ready; file new datawatch-app issue for PWA action parity if app team needs tracking

**Update GH issues when Phase 4 merges:**
- Comment on GH#152: "PWA cancel story, cancel task, and requeue task affordances added in v8.28.0"

---

### Phase 5 — Audit logging

**Goal:** `cancel_story`, `cancel_task`, `requeue_task` emit structured audit events.

**Steps:**
1. Emit JSON-lines audit events in the same pattern as existing PRD lifecycle audit events:
   - `{"event": "autonomous.story_cancelled", "prd_id": "...", "story_id": "...", "actor": "...", "reason": "...", "ts": "..."}`
   - `{"event": "autonomous.task_cancelled", "prd_id": "...", "task_id": "...", "actor": "...", "reason": "...", "ts": "..."}`
   - `{"event": "autonomous.task_requeued", "prd_id": "...", "task_id": "...", "actor": "...", "ts": "..."}`
2. Add JSON-lines round-trip test + CEF format test per AGENT.md B16 rule.
3. Add to `bl382_cancel_test.go`: `TestBL382_AuditEvent_CancelStory`, `TestBL382_AuditEvent_CancelTask`, `TestBL382_AuditEvent_RequeueTask`.

---

### Phase 6 — Observability, docs, testing-tracker, smoke

**Goal:** All AGENT.md B-rule checklist items satisfied before release cut.

**Steps:**

#### Testing tracker (B1)
Add to `docs/testing-tracker.md`:

**Section: "Autonomous PRD Lifecycle — cancel_story / cancel_task / requeue_task"**

| Interface / Endpoint | Tested | Validated | Test Conditions | Notes |
|---|---|---|---|---|
| `POST /api/autonomous/prds/{id}/cancel_story` — marks story cancelled, stops in-progress tasks | Yes | No | `bl382_cancel_test.go::TestBL382_CancelStory_*` | Live test: run PRD, cancel a pending story, confirm PRD continues |
| `POST /api/autonomous/prds/{id}/cancel_task` — marks task cancelled, interrupts session | Yes | No | `bl382_cancel_test.go::TestBL382_CancelTask_*` | Live test: cancel in-progress task, confirm session killed, story continues |
| `POST /api/autonomous/prds/{id}/reset_task` with `force: true` — requeues complete/cancelled task | Yes | No | `bl382_cancel_test.go::TestBL382_ResetTask_Force_*` | Live test: complete a task, requeue it, confirm it re-runs |
| State precondition 409 — terminal-state guard | Yes | No | `TestBL382_Cancel{Story,Task}_AlreadyCancelled_Returns409` | All terminal-state paths covered |
| Comms channel events — story_cancelled, task_cancelled, task_requeued | Yes | No | `TestBL382_*_EmitsChannelEvent` | Live test: cancel story/task, confirm channel message received |
| MCP `autonomous_prd_cancel_story` | No | No | — | Requires running daemon with MCP connected |
| MCP `autonomous_prd_cancel_task` | No | No | — | — |
| MCP `autonomous_prd_requeue_task` | No | No | — | — |
| PWA cancel story button → confirmation → POST → PRD refresh | No | No | — | E2E: open PRD with running story, click ⋯ → Skip story, confirm |
| PWA cancel task button → confirmation → POST → story refresh | No | No | — | — |
| PWA requeue task button → POST with force:true → task back to pending | No | No | — | — |
| Audit events — JSON-lines round-trip + CEF format | Yes | No | `TestBL382_AuditEvent_*` | Per B16 rule |

#### Release smoke (B12)
Add 3 sections to `scripts/release-smoke.sh`:
- `§ cancel_story`: create PRD, decompose, cancel a story, assert story.status == cancelled, assert PRD still running, cleanup
- `§ cancel_task`: create PRD, decompose, cancel a task, assert task.status == cancelled, assert story continues, cleanup
- `§ requeue_task`: create PRD, run to completion, call reset_task force=true on a completed task, assert task.status == pending, cleanup

#### Observability (B7)
- Add `CancelledStories int64`, `CancelledTasks int64`, `RequeuedTasks int64` counters to `SystemStats` in `internal/server/api.go`
- Increment in manager on each successful operation
- Expose in `/api/stats` response and the existing Monitor card (autonomous section)

#### Feature docs (B8) — all 5 access-method rows
Update `docs/setup.md` or `docs/operations.md` (whichever covers autonomous PRD operations) with a table:

| Access method | How to cancel a story |
|---|---|
| REST | `POST /api/autonomous/prds/{id}/cancel_story` `{"story_id":"...", "reason":"..."}` |
| MCP | `autonomous_prd_cancel_story(id, story_id, reason?)` |
| PWA | PRD detail → story row ⋯ menu → Skip story |
| CLI | `datawatch autonomous cancel-story <prd_id> <story_id>` (if CLI subcommand added; else N/A) |
| Comm channel | Not applicable (operator-initiated only; channel receives events, does not send them) |

Same pattern for cancel_task and requeue_task.

#### Federation docs (GH#152 acceptance criterion)
Add a note to `docs/operations.md` (or federation docs):
> **Partial story/task cancellation and federation**: When a PRD is running tasks across multiple nodes via federation, `cancel_story` and `cancel_task` operate on the local store and kill the local session reference. If the story/task is executing on a remote node, the remote node's session is killed via the existing session-kill propagation path (`kill_session` federation call). If that call fails (remote node unreachable), the endpoint returns HTTP 422 with `{"error": "federation: could not propagate cancellation to remote node: <name>"}` — the local state is NOT updated in that case to preserve consistency.

#### Parity status
Update `docs/parity-status.md` parity table: add rows for cancel_story, cancel_task, requeue_task in PWA ✅ / Android TBD (BL29) / MCP ✅ / comms ✅ columns.

---

### Phase 7 — Reuse + skills audit (B9, B10)

**Reuse-and-expand audit (B9):**
> CHANGELOG entry: "Reuse audit — `CancelStory`/`CancelTask` extend the existing Decision-row pattern from `approve`/`reject`/`cancel`. `requeue_task` extends the existing `reset_task` primitive with a `force` flag rather than duplicating the reset path."

**Skills-awareness check (B10):**
No new session/PRD/agent/comm-channel path created — these are state transitions on existing entities. Skills hook N/A.

---

### Phase 8 — Release prep (AGENT.md Section A + C)

1. Version bump: `v8.27.0` → `v8.28.0` in **both** `cmd/datawatch/main.go` and `internal/server/api.go` simultaneously.
2. CHANGELOG: add `## v8.28.0 — feat(autonomous): PRD lifecycle management — cancel_story, cancel_task, requeue_task; PWA + MCP + comms parity (BL382)`.
3. README: update badge and "Current release" section to v8.28.0.
4. Smoke run on sandbox daemon: `./scripts/release-smoke.sh`. Record result as `smoke: N sections, P passed, 0 failed`.
5. `make build` check: `go build -o datawatch ./cmd/datawatch/`.
6. All CI workflows green after tag push.
7. Update GH#152 umbrella issue: close with comment linking to release.

**Commit message tokens required:**
```
rules: checked
tests: bl382_cancel_test.go (N tests), v5280_locales_test.go mustHave updated
version: v8.26.1 → v8.28.0
changelog: v8.28.0 added
readme: badge + release section updated
backlog: BL382 closed
id-check: no internal IDs in user-facing docs
leak-check: server.token not in logs
node-check: N/A
make-build: passed
ci: tag pushed, workflows green
tracker: Autonomous PRD Lifecycle — cancel_story/cancel_task/requeue_task
locales: autonomous.cancelStory, autonomous.cancelTask, autonomous.requeueTask
locale-guard: added cancelStory, cancelTask, requeueTask
mobile-parity: datawatch-app#152 (comment posted), datawatch-app BL29 notified
config-parity: N/A (no new config fields)
observability: CancelledStories/CancelledTasks/RequeuedTasks counters in SystemStats
access-docs: added (5 access-method rows for cancel_story/cancel_task/requeue_task)
reuse-audit: Decision-row pattern (approve/reject/cancel); reset_task force extension
skills-hook: N/A
smoke-cleanup: extended for cancel_story, cancel_task, requeue_task sections
smoke-extended: cancel_story, cancel_task, requeue_task
audit-tests: added (JSON-lines round-trip + CEF for story_cancelled/task_cancelled/task_requeued)
```

---

## GH issue update schedule

| When | Action |
|------|--------|
| Phase 1 merges | Close GH#149, GH#150, GH#151 with commit/version reference |
| Phase 3 merges | Comment on GH#152: MCP tools added |
| Phase 4 merges | Comment on GH#152: PWA affordances added; comment on datawatch-app#171 + #172 noting server-side parity complete |
| v8.28.0 tag pushed | Close GH#152 umbrella |
| v8.28.0 CI green | Comment on datawatch-app BL29 tracking thread: server endpoints landed, app-side work unblocked |

---

## Status

| Phase | Status |
|-------|--------|
| Phase 1 — Server API + tests | ⬜ Not started |
| Phase 2 — Comms events | ⬜ Not started |
| Phase 3 — MCP tools | ⬜ Not started |
| Phase 4 — PWA + locales | ⬜ Not started |
| Phase 5 — Audit logging | ⬜ Not started |
| Phase 6 — Observability / docs / smoke | ⬜ Not started |
| Phase 7 — Reuse + skills audit | ⬜ Not started |
| Phase 8 — Release prep | ⬜ Not started |
