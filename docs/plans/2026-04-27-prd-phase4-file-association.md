# PRD-flow Phase 4 — file association

**Status:** Design (no implementation yet).
**Owner:** —
**Tracks under:** Operator's PRD-flow rework, originally requested 2026-04-27.
**Author:** generated 2026-04-27 in v5.26.53 cycle.
**Source:** [`docs/plans/2026-04-27-v6-prep-backlog.md`](2026-04-27-v6-prep-backlog.md) § PRD-flow rework.

---

## Operator intent

> *"make sure that the details from the prd are associated with files in the folder and stories and tasks to be approved."*

Today the PRD/story/task records are operator-readable plans, but they don't track *which files* in the workspace each one touches. When a worker session finishes a task, the post-session git diff is recorded as `Session.DiffSummary` (BL10) — but only as a shortstat (files-changed, +ins/-del). The actual file list isn't surfaced anywhere structured.

What the operator wants:

- For each story/task: a list of files it expects to touch (or has touched).
- Visible in the PWA detail view: "story X touches `internal/foo.go` + `docs/howto/foo.md`."
- Useful for review-before-approve, post-spawn audit, and conflict detection ("two stories want to write the same file").

## Two sources of file refs

| Source | When | Cost | Confidence |
|------|------|------|------|
| **LLM-extracted (decompose-time)** | During decomposition; the decomposer proposes files alongside title/description | One LLM round; risk of hallucinated paths | Medium — useful for review |
| **Post-hoc from session diff (run-time)** | After each task's session ends; `git diff --name-only main...HEAD` against `Session.ProjectDir` | Free (already running git for `DiffSummary`) | High — exact ground truth |

Recommendation: **both**, recorded separately.

```go
type Story struct {
    // existing
    ...
    FilesPlanned []string `json:"files_planned,omitempty"` // LLM-extracted
}

type Task struct {
    // existing
    ...
    FilesPlanned []string `json:"files_planned,omitempty"` // LLM-extracted
    FilesTouched []string `json:"files_touched,omitempty"` // post-hoc from diff
}
```

`FilesPlanned` is extracted by the decomposer prompt — already used to disambiguate stories during planning (the decomposer produces structured JSON; it's a small prompt change to ask for `files: [...]` per story/task).

`FilesTouched` is populated by the same hook that computes `DiffSummary` today (`internal/session.PostSessionCommit` walks the worker's project_dir).

## REST surface

Read endpoints (no new — existing GETs surface the new fields):

```
GET /api/autonomous/prds/{id}
  → PRD.Story[].FilesPlanned
  → PRD.Story[].Tasks[].FilesPlanned
  → PRD.Story[].Tasks[].FilesTouched   (after spawn completion)
```

Write endpoints:

```
PUT /api/autonomous/prds/{prd_id}/stories/{story_id}/files
  Body: { files: [...] }
  Effect: Story.FilesPlanned = files. Lock-after-approve gate.
  
PUT /api/autonomous/prds/{prd_id}/tasks/{task_id}/files
  (analogous)
```

The post-hoc `FilesTouched` is daemon-populated; no operator-facing write.

Configuration parity rule — REST + MCP + CLI + comm channel CRUD, same as every other field.

## PWA changes

Story / task detail rows render a small "Files" pill row when `FilesPlanned || FilesTouched` is non-empty:

```
[ Story 1: Add cache layer ]
  📝 internal/cache/store.go  internal/cache/store_test.go  docs/cache.md   ← FilesPlanned
  ✅ internal/cache/store.go  internal/cache/store_test.go                  ← FilesTouched (after run)
```

Conflict highlight: if two pending stories list the same file in `FilesPlanned`, the second one shows a ⚠ marker with a tooltip listing the conflicting story id.

Editable as part of the existing story-edit modal (Phase 2 v5.26.32 added title + description; Phase 4 adds a free-form file-list field — comma- or newline-separated — saved through `PUT .../files`).

## Manager / executor wiring

### Decomposer prompt change

Update `internal/autonomous/decomposer.go` (or wherever the decompose prompt lives) — add a `files` array to the JSON schema requested from the LLM:

```json
{
  "stories": [{
    "title": "...",
    "description": "...",
    "files": ["internal/cache/store.go", "..."],
    "tasks": [{
      "title": "...",
      "spec": "...",
      "files": ["..."]
    }]
  }]
}
```

Defensive: if the LLM omits `files`, leave `FilesPlanned` empty — the post-hoc `FilesTouched` still populates after the run.

### Post-session diff hook

`PostSessionCommit` (in `internal/session/`) already runs `git diff` for the shortstat. Extend to also capture `git diff --name-only` and write it back to `Task.FilesTouched` via a callback the autonomous manager registers with the session manager.

Cleanup invariant: when a session is killed before completion, `FilesTouched` is left empty (the diff captures only against successful runs).

## Tests

Unit tests:
- Decomposer fake returns a JSON with `files` arrays → manager populates `FilesPlanned` correctly.
- Post-session callback fires → `FilesTouched` set on the right task.
- Conflict detection helper: two stories sharing a planned file → flag returned.

Smoke (script):
- Decompose a contrived spec ("Touch file X and file Y") → verify FilesPlanned non-empty in the PRD response.
- (Skipped in CI without a worker spawn — FilesTouched needs a real session run.)

## Documentation

- New section in `docs/howto/autonomous-planning.md`: "Reviewing planned + touched files."
- `docs/api/autonomous.md`: new fields in the PRD response shape.

## Risks / open questions

1. **LLM file-path hallucination.** The decomposer may emit paths that don't exist. Mitigation: client-side display-only validation (greyed-out for non-existent paths). Don't fail the decompose on invalid paths — operator can edit them out via the story-edit modal.

2. **Large file lists.** Some stories legitimately touch dozens of files. Cap at 50 per story; truncate in display with "+ N more".

3. **Storage growth.** PRD records grow with file lists. Keep the lists path-only (no content snippets); 50-path × 100-byte avg = 5 KB per story max. Acceptable.

4. **Sensitive paths.** A planned file like `secrets/.env` showing in the PWA could leak intent. Mitigation: re-use the existing path-redaction filter from the audit log (`docs/operations.md`).

## Backlog hand-off

Implementation order:
1. Schema additions (`Story.FilesPlanned`, `Task.FilesPlanned`, `Task.FilesTouched`) + REST surface — one PR.
2. Decomposer prompt update — separate PR (LLM change isolated).
3. Post-session diff hook → autonomous callback wiring — separate PR.
4. PWA file-pill rendering + edit affordance — separate PR.
5. Howto + diagram updates — separate PR.

Smoke tests added incrementally per the per-release-smoke rule.

Phase 4.1 follow-ups (not in scope here):
- File-level conflict detection during decompose (refuse plans where two stories overlap).
- Cross-PRD file conflict detection (a new PRD planning to touch a file already pending in another).
