# datawatch v5.26.60 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.59 → v5.26.60
**Patch release** (no binaries — operator directive).
**Closed:** PRD-flow Phase 3.A — per-story execution profile + per-story approval (schema, manager methods, REST surface, tests).

## What's new

Phase 3 implementation begins, per design at [`docs/plans/2026-04-27-prd-phase3-per-story-execution.md`](2026-04-27-prd-phase3-per-story-execution.md). v5.26.60 lands sub-patch **3.A**: schema, manager methods, REST surface, unit tests. Sub-patches B (Manager.Run gating + config flag), C (PWA widgets), and D (smoke + howto refresh) follow.

### Schema additions

```go
type PRD struct {
    ...existing
    DecompositionProfile string `json:"decomposition_profile,omitempty"`
    // ProjectProfile re-purposed: now the *default execution* profile.
}

type Story struct {
    ...existing
    ExecutionProfile string     `json:"execution_profile,omitempty"`
    Approved         bool       `json:"approved,omitempty"`
    ApprovedBy       string     `json:"approved_by,omitempty"`
    ApprovedAt       *time.Time `json:"approved_at,omitempty"`
    RejectedReason   string     `json:"rejected_reason,omitempty"`
}

const StoryAwaitingApproval StoryStatus = "awaiting_approval"
```

Migration: existing PRDs have `DecompositionProfile=""` (interpreted as "decomposed against the global config knob at the time"). Existing stories have `Approved=false` (interpreted as "no per-story gate applied"). Backward compatible — Phase 3.B will add the config knob that activates the gate.

### Manager methods (lock-after-approve gate where applicable)

| Method | Purpose | When allowed |
|------|------|------|
| `SetStoryProfile(prd, story, profile, actor)` | Override a single story's execution profile | `needs_review` / `revisions_asked` |
| `ApproveStory(prd, story, actor)` | Mark story approved; transitions `awaiting_approval`→`pending` | PRD `approved` / `active` / `running` |
| `RejectStory(prd, story, actor, reason)` | Block a single story with reason | PRD `approved` / `active` / `running` |

Each appends a corresponding `Decision` to the audit timeline (`set_story_profile`, `approve_story`, `reject_story`).

### REST surface (full configuration parity)

Three new sub-routes under `/api/autonomous/prds/{id}/`:

```
POST .../set_story_profile  { story_id, profile, actor? }
POST .../approve_story      { story_id, actor? }
POST .../reject_story       { story_id, actor?, reason }
```

`AutonomousAPI` interface extended; the test fake `fakeOrchAutonomous` got matching stub methods so existing test suites compile.

### Tests

6 new unit tests in `internal/autonomous/lifecycle_test.go`:

- `TestSetStoryProfile_RewritesAndAudits`
- `TestSetStoryProfile_EmptyClears`
- `TestSetStoryProfile_RefusesAfterApprove`
- `TestApproveStory_TransitionsAndAudits`
- `TestApproveStory_RefusesBeforePRDApprove`
- `TestRejectStory_BlocksAndRequiresReason`

Total: 471 tests passing (was 465; +6 net).

### What's NOT in this sub-patch

- **Manager.Run gating** — runner still treats every approved-PRD's stories as runnable. Phase 3.B adds the `awaiting_approval` skip + the `autonomous.per_story_approval` config flag.
- **PWA widgets** — story rows don't show profile-override / Approve / Reject buttons yet. Phase 3.C lands those.
- **Smoke probe** — `§7l` for the new endpoints + the howto refresh come in Phase 3.D.

Each sub-patch is small, individually committable, and gives the operator a clean rollback point if something is wrong.

## Configuration parity

REST surface added; CLI / MCP / comm-channel reach is implicit through the daemon's existing autonomous-API forwarder. Phase 3.B will add the `autonomous.per_story_approval` config knob with full parity.

## Tests

471 unit tests passing. Smoke unaffected (58/0/1) — Phase 3 doesn't activate without the config flag yet.

## Upgrade path

```bash
git pull
datawatch restart
# No behavior change yet — Phase 3.A landed schema + endpoints
# without flipping any existing flow. Phase 3.B will add the
# config flag that activates per-story approval. Operators on
# v5.26.60 can already POST .../set_story_profile to attach a
# per-story execution profile (the runner doesn't read it yet —
# that lands in 3.B).
```
