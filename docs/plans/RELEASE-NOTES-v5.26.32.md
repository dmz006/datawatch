# datawatch v5.26.32 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.31 → v5.26.32
**Patch release** (no binaries — operator directive).
**Closed:** Story-level review + edit (phase 2 of operator's PRD-flow rework).

## What's new

### Stories now have an edit affordance

Operator: *"i don't see a story review or approval or story edit option."*

Tasks have always had an inline edit button (✎) when the parent PRD is in `needs_review` or `revisions_asked`. Stories did not — operators could see the title and verdicts but couldn't fix decomposition mistakes at the story level (rename, expand the description) without rejecting the entire PRD and re-decomposing. v5.26.32 closes that gap.

What changed:

- **Story description now renders in the PRD detail view.** Decomposition often produces a non-empty `Description` per story (the LLM's expanded plan); previously the field was stored but never shown. The PWA now displays it in muted text below the title, with `white-space: pre-wrap` so multi-line descriptions read naturally.
- **Edit (✎) button on every story** when the PRD is in `needs_review` / `revisions_asked`. Click → modal with Title (single-line) + Description (textarea + mic input). Save round-trips through the new endpoint and re-renders the panel.
- **`POST /api/autonomous/prds/{id}/edit_story`** — new REST endpoint mirroring `/edit_task`. Body: `{story_id, new_title?, new_description?, actor?}`. At least one of `new_title` / `new_description` must be non-empty; empty fields preserve the existing value (so a title-only edit doesn't clobber the description).
- **Audit timeline entry** — `kind: edit_story` decision recorded with the actor + character counts, so the PRD's decision log shows exactly when each story was tweaked and by whom.
- **Lock after approve** — same gate as task edit. Once the PRD is `approved` / `running` / further along, the endpoint returns `400` ("status is locked"). The PWA stops rendering the edit button at the same point.

### What this is NOT

This is **review + edit**, not story-level approval. The PRD-level approve/reject/request-revision workflow stays the gate. Per-story approval would change the run model (Manager.Run currently runs all stories of an approved PRD) and is tracked as a future phase: `docs/plans/2026-04-27-v6-prep-backlog.md` § PRD-flow phase 3 (per-story execution profile + per-story approval).

## Configuration parity

Endpoint reachable via REST + MCP (through the existing autonomous API surface) + comm channels. CLI parity is implicit through the daemon's REST forwarder.

## Tests

3 new unit tests in `internal/autonomous/lifecycle_test.go`:

| Test | Verifies |
|------|----------|
| `TestEditStory_RewritesAndAudits` | Story title + description rewrite round-trips; `kind: edit_story` decision is appended |
| `TestEditStory_TitleOnlyKeepsDescription` | Empty `new_description` does NOT clobber an existing description (operator-friendly default) |
| `TestEditStory_RefusesAfterApprove` | Endpoint returns an error once the PRD is `approved`; matches the lock-after-approve invariant |

Total: 465 passing across `internal/session`, `internal/server`, `internal/autonomous` (was 462; +3 net).

## Known follow-ups

Phase 3 of the PRD-flow rework: per-story execution profile + per-story approval gate. PRD gets a `decomposition_profile` (used to GENERATE) + a default `execution_profile` (used to run); per-story override allows different profiles for different stories. Tracked in `docs/plans/2026-04-27-v6-prep-backlog.md`.

Phase 4 (file association), phase 5 (persistent test cluster + smoke profile), phase 6 (docs/screenshots/diagrams refresh) unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA on next visit (SW cache bumped to
# datawatch-v5-26-32). The PRD detail view now shows story
# descriptions inline + an ✎ button on every story while the PRD
# is awaiting review.
```
