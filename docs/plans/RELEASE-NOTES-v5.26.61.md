# datawatch v5.26.61 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.60 → v5.26.61
**Patch release** (no binaries — operator directive).
**Closed:** PRD-flow Phase 3.B — Manager.Run gating + `autonomous.per_story_approval` config flag.

## What's new

Phase 3.A (v5.26.60) shipped the schema + REST surface. v5.26.61 lands sub-patch **3.B**: the actual gating behavior + the config knob that activates it (with full configuration parity).

### `autonomous.per_story_approval` config flag

Default `false` — preserves the existing v5.26.x behavior where PRD approval implicitly approves every story. When `true`:

```
Decompose → needs_review → Approve PRD
              ↓ (per_story_approval=true)
              every Story.Status: pending → awaiting_approval
              ↓
              Runner skips awaiting_approval stories
              ↓ (operator)
              POST .../approve_story per story
              ↓
              Story.Status: awaiting_approval → pending
              Runner picks up pending tasks
```

### Configuration parity

| Channel | How to read / write |
|------|------|
| YAML | `autonomous.per_story_approval: true` |
| REST GET | `GET /api/config` → `autonomous.per_story_approval` |
| REST PUT | `PUT /api/config { "autonomous.per_story_approval": true }` |
| MCP / CLI / comm | Through the `config_set` tool / `datawatch config set` / `configure autonomous.per_story_approval=true` |

### Runner gating in `flattenTasks`

The story-skip rule was added to `flattenTasks` in `internal/autonomous/executor.go`:

```go
for i := range prd.Story {
    st := prd.Story[i].Status
    if st == StoryAwaitingApproval || st == StoryBlocked {
        continue
    }
    for j := range prd.Story[i].Tasks {
        out = append(out, &prd.Story[i].Tasks[j])
    }
}
```

Effect: when the runner re-enters (after `ApproveStory` transitions a story `awaiting_approval` → `pending`), the story's tasks now appear in `flattenTasks`'s output and get topo-sorted into the work queue. Blocked stories are also skipped — they're a terminal state until operator action.

### Approve transition

`Manager.Approve` was extended: when `m.cfg.PerStoryApproval` is true, every fresh story (status `""` or `pending`) gets transitioned to `awaiting_approval` at the moment the operator approves the PRD. Stories already past that state (`in_progress`, `completed`, etc.) are left alone — preserves the invariant that a re-approve doesn't unwind running work.

### What's NOT in this sub-patch

- **PWA per-story Approve / Reject buttons** — Phase 3.C (next).
- **Settings → Autonomous → Per-story approval gate toggle** — Phase 3.C.
- **§7l smoke probe + howto refresh** — Phase 3.D.

## Tests

471 unit tests still passing (Phase 3.A's 6 tests cover the manager methods; the runner gating is exercised by `flattenTasks` indirectly). Phase 3.D will add a smoke section that toggles the flag via REST → approves a PRD → confirms stories land in `awaiting_approval` → calls `ApproveStory` → confirms the runner picks up.

Smoke unaffected by default (config flag stays false unless operator opts in).

## Known follow-ups

Operator's New Session modal ask: *"New session should have same directory or profile and local daemon or cluster profile to start."* Tracked as task #46. Not part of Phase 3 — separate UX consistency patch.

## Upgrade path

```bash
git pull
datawatch restart
# To activate the per-story approval gate:
datawatch config set autonomous.per_story_approval true
# or via Web UI: Settings → General → (the Container Workers
# section is unrelated) — actually the Autonomous knob lives
# under Settings → General → Autonomous; v5.26.x config-section
# layout has it grouped with the other autonomous.* keys.
```
