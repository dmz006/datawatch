# datawatch v5.26.39 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.38 → v5.26.39
**Patch release** (no binaries — operator directive).
**Closed:** Autonomous-planning howto refreshed for v5.26.30/32/34/36/37 UX (PRD-flow phase 6 partial).

## What's new

### `docs/howto/autonomous-planning.md` updated

PRD-flow phases 1, 2, and the v5.26.36/37 FAB rework all changed the New PRD modal and PRD detail surfaces, but the howto still described the pre-v5.26.30 layout. v5.26.39 brings the howto in sync:

- **Header layout note** — explains the "PRDs" label + filter toggle (⛁) + collapsed filter row + bottom-right (+) FAB introduced in v5.26.36 / v5.26.37.
- **New "Submit a spec" subsection from the PWA** — walks through the unified Profile dropdown (v5.26.30): pick `__dir__` for project-directory mode, or pick a configured project profile to surface the Cluster dropdown (v5.26.34's "Local service instance" first option). Backend/Effort/Model only show in dir mode (profile carries the worker LLM via `image_pair`).
- **CLI example with profile + cluster flags** — concrete `datawatch autonomous prd create --project-profile … --cluster-profile …` invocation.
- **Story-level review + edit subsection (v5.26.32)** — covers the ✎ button on each story, the `POST /api/autonomous/prds/{id}/edit_story` REST endpoint, and the title-only-keeps-description rule. Mentions the parallel ✎ task-edit affordance.
- **Reachability table updated** — the PWA row now points at "Autonomous tab → (+) FAB at bottom-right" instead of the obsolete "Settings → Autonomous → New PRD" path.

### What didn't change

Screenshots — the existing `screenshots/autonomous-landing.png`, `autonomous-prd-expanded.png`, and `autonomous-mobile.png` still show recognizable PRD list / detail content, just with the previous header layout. Recapturing them needs browser-automation tooling and is tracked separately as part of phase 6's full screenshots refresh. The textual notes added in v5.26.39 explain the gap so an operator reading the doc understands the screenshots are pre-v5.26.36.

The `docs/howto/autonomous-review-approve.md` and `docs/howto/profiles.md` walkthroughs already align with current behavior — `profiles.md` was created in v5.26.31 against the v5.26.30 modal shape, and the review/approve flow's headline interactions (Approve / Reject / Request revision buttons) didn't shift.

## Configuration parity

No new config knob — pure docs update.

## Tests

Docs-only change. Go test suite unaffected (still 465 passing). Smoke unaffected (37/0/1).

## Known follow-ups

Remaining Phase 6 work:

- Screenshots recapture (autonomous-landing, autonomous-prd-expanded, autonomous-mobile) once browser-automation tooling is wired.
- Diagrams that show the unified profile path (data-flow + sequence diagrams in `docs/flow/`).

Phase 3 (per-story execution profile + per-story approval gate) and phase 4 (file association) — design needed before implementation. Persistent fixtures from v5.26.33 are the test substrate.

## Upgrade path

```bash
git pull
# No daemon restart needed — docs-only change. Re-render the
# rendered docs in the PWA (Diagrams page or any inline doc link)
# to pick up the refreshed walkthrough.
```
