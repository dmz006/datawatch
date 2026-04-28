# PRD-flow Phase 3 — per-story execution profile + per-story approval

**Status:** Design (no implementation yet).
**Owner:** —
**Tracks under:** Operator's PRD-flow rework, originally requested 2026-04-27. Phase 1, 2, 5 shipped; phase 3 + 4 are design-first.
**Author:** generated 2026-04-27 in v5.26.53 cycle.
**Source:** [`docs/plans/2026-04-27-v6-prep-backlog.md`](2026-04-27-v6-prep-backlog.md) § PRD-flow rework.

---

## Operator intent

> *"the prd should be able to be generated from one profile and should be able to have default and configurable other profiles that can be used to execute on the stories built."*

Two distinct profile *roles* exist for a single PRD today; we conflate them under one field (`PRD.ProjectProfile`):

1. **Decomposition** — the LLM that turns a free-form spec into a tree of stories and tasks. Operator-controlled today via `autonomous.decomposition_backend` config knob (global) or implicitly via the worker LLM picked from the project profile's `image_pair`.
2. **Execution** — the worker LLM (and its compose'd cluster context) that runs each task. Today: same project profile drives this for every story.

The operator wants:

- A PRD knows which profile generated its plan (audit / explainability).
- A PRD has a default execution profile (used for every story unless overridden).
- Each story can override the default execution profile (e.g. story 1 runs on `agent-claude` cluster, story 2 runs on `agent-opencode` cluster).
- Per-story approval gate (operator approves stories one at a time before each runs).

## Why now

Phase 1 (unified Profile dropdown, v5.26.30) and Phase 2 (story review + edit, v5.26.32) made profiles selectable per-PRD and stories editable. Phase 3 is the natural next step: stretch the profile concept down a level (per-story) AND insert an approval gate per story.

Phase 5 (persistent smoke fixtures, v5.26.33) gives us `datawatch-smoke` + `smoke-testing` to validate against without polluting operator state.

## Proposed model changes

### `PRD` struct additions

```go
type PRD struct {
    // existing fields unchanged
    ...

    // Phase 3 (v5.26.x).
    DecompositionProfile string `json:"decomposition_profile,omitempty"`
    // ProjectProfile (existing) → re-purposed: the *default* execution profile.
    // For backward compatibility we keep the field name; new operators set it
    // through the same New PRD modal Profile dropdown.
}
```

Migration: existing PRDs have `ProjectProfile` set (or empty for `__dir__` mode). `DecompositionProfile` is empty for them — interpreted as "decomposed against whatever the global `autonomous.decomposition_backend` was at the time." No backfill needed.

### `Story` struct additions

```go
type Story struct {
    // existing fields unchanged
    ...

    // Phase 3.
    ExecutionProfile string  `json:"execution_profile,omitempty"`  // per-story override
    Approved         bool    `json:"approved,omitempty"`           // per-story approval gate
    ApprovedBy       string  `json:"approved_by,omitempty"`
    ApprovedAt       *time.Time `json:"approved_at,omitempty"`
}
```

### `StoryStatus` extension

Today: `pending`, `in_progress`, `completed`, `blocked`, `failed`.
Add: `awaiting_approval` (between `pending` and `in_progress`).

State machine:

```
pending  ─decompose→  awaiting_approval  ─approve→  in_progress  ─done→ completed
                              │
                              └── reject → blocked (operator review)
```

When PRD-level approval is given, ALL stories transition to `awaiting_approval`. The runner picks up `awaiting_approval` stories that have `Approved: true` and runs them; story-level approval can come incrementally.

Operator default: PRD-level approval implies per-story approval (`approve_all_stories: true` flag — preserves current behavior for operators who don't want the per-story gate). Set via Settings → Autonomous → "Per-story approval gate" toggle.

## REST surface

New endpoints:

```
POST /api/autonomous/prds/{prd_id}/stories/{story_id}/approve
  Body: { actor?: string, note?: string }
  Effect: Story.Approved = true; appends decision kind "approve_story".

POST /api/autonomous/prds/{prd_id}/stories/{story_id}/reject
  Body: { actor?: string, reason: string }
  Effect: Story.Status = "blocked"; appends decision kind "reject_story".

PUT /api/autonomous/prds/{prd_id}/stories/{story_id}/profile
  Body: { execution_profile: string, actor?: string }
  Effect: Story.ExecutionProfile = <name>. Validated against profile store.

PUT /api/autonomous/prds/{prd_id}/profiles
  (existing — extended) Now also accepts `decomposition_profile` field.
```

Configuration parity rule applies — every endpoint mirrored through MCP, CLI, comm channels.

## PWA changes

New PRD modal:

- **Decomposition profile** dropdown — same shape as the existing Profile dropdown, prepended with the `__inherit__` sentinel ("(use autonomous.decomposition_backend default)").
- The existing Profile dropdown stays — labelled "Default execution profile" in the new layout.

Story detail view:

- Each story carries a small profile-override widget when the PRD is in `needs_review` / `revisions_asked`. Dropdown of project profiles + "(inherit PRD default)".
- When PRD is in `running` state and per-story approval is on, each story shows Approve / Reject buttons next to its title.

Settings → Autonomous:

- New toggle: "Per-story approval gate" (default OFF — preserves current behavior).
- Affects: when a PRD is approved, are stories auto-approved (current) or do they enter `awaiting_approval` (new)?

## Manager.Run impact

Today: `Manager.Run(prdID)` topo-sorts stories, kicks off in dependency order.

After Phase 3:
- Skips stories with `Status == "awaiting_approval"`.
- Schedules a poll-loop watching for story-approval transitions; resumes stories as they approve.
- A rejected story sets PRD status to `blocked` (current behavior already does this via guardrail blocks).

Lock invariants:
- Story-edit endpoints (`edit_story`, `set_story_profile`, `approve_story`) refuse on `Status == "in_progress"` or later. Match the existing `EditTaskSpec` lock-after-approve gate.

## Spawn path

When the executor spawns the worker for a story:

```python
# Pseudocode
profile = story.execution_profile or prd.project_profile
cluster = prd.cluster_profile  # cluster stays PRD-level for now (one cluster per PRD)
spawn(profile, cluster, task_spec)
```

Per-story cluster is out of scope for Phase 3 (would multiply the spawn-matrix by per-cluster auth/network state). Tracked as a Phase 3.1 follow-up if operator wants it.

## Tests

Unit tests:
- `Manager.SetStoryProfile` — happy path + lock-after-approve refusal.
- `Manager.ApproveStory` — appends decision; transitions status.
- `Manager.RejectStory` — sets blocked; appends reason.
- `Manager.Run` — skips `awaiting_approval`; resumes on approve.

Smoke (script):
- §7c-equivalent: create PRD → decompose → set per-story profile via PUT → verify persisted → approve story 1, reject story 2 → spawn round-trip on story 1.

Functional (release-smoke against running daemon): cover the full lifecycle exactly once per release per the per-release smoke testing rule.

## Documentation

- Update `docs/howto/autonomous-planning.md` (already partially refreshed v5.26.39): add a "Per-story execution profile" subsection.
- Update `docs/howto/autonomous-review-approve.md`: explain the per-story approval gate.
- Update `docs/profiles.md`: clarify decomposition vs execution roles.
- Refresh diagrams: `docs/flow/orchestrator-flow.md` to show the per-story branch.

## Backlog hand-off

When implementation starts:
1. Land model + REST + audit (one PR).
2. Land PWA per-story widgets (separate PR — easier to roll back if UX is wrong).
3. Land Manager.Run gating (separate PR — runs against the new state machine).
4. Update howtos + diagrams (separate PR).
5. Smoke tests added incrementally with each.

Decision deferred to operator: should `Per-story approval gate` default to ON or OFF for v6.0?
