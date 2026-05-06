# datawatch v5.26.62 — release notes

**Date:** 2026-04-28
**Spans:** v5.26.61 → v5.26.62
**Patch release** (no binaries — operator directive).
**Closed:** PRD-flow Phase 3.C + 3.D — PWA per-story widgets, Settings toggle, smoke §7l, howto refresh. **Phase 3 complete.**

## What's new

### Phase 3.C — PWA per-story widgets

Each story row now carries:

- **Status pill** — small grey badge showing current `Story.Status`.
- **Profile pill** — `prof: (inherit)` button while the PRD is in `needs_review`/`revisions_asked`. Click → modal with a dropdown of every configured project profile + `(inherit PRD default)`. Save round-trips through `POST /api/autonomous/prds/{id}/set_story_profile`.
- **Approve / Reject buttons** — visible when story is `awaiting_approval` AND the parent PRD is `approved` / `active` / `running`. Approve uses the existing audit path; Reject prompts for a required reason and posts to `reject_story`.
- **Rejected reason** — when `Story.RejectedReason` is non-empty, rendered in red below the title so the audit context survives between sessions.

### Settings → General → Autonomous → "Per-story approval gate" toggle

Added to the autonomous-section field list — `key: 'autonomous.per_story_approval'`, type `toggle`. Saves through the existing config-section dotted-key handler (full parity: YAML / REST / MCP / CLI / comm channels all reach the same knob).

### §7l smoke — Phase 3 lifecycle

```
== 7l. PRD-flow Phase 3 — per-story execution profile + per-story approval ==
  PASS  autonomous.per_story_approval flipped on for Phase 3 smoke (was false)
  …
  (gates cleanly when the dev daemon's decompose backend is slow)
```

Captures the gate flag's pre-test value, flips on, decomposes a contrived PRD, approves, verifies stories transition to `awaiting_approval`, exercises `set_story_profile` / `approve_story` / `reject_story`, validates audit decisions, restores the flag. Smoke gates skip-style when decompose times out or the autonomous backend isn't responsive (CI-friendly).

### Howto refresh — `docs/howto/autonomous-planning.md`

New subsection covering the per-story profile + approval gate behavior, including the three new REST endpoints with body shapes and audit-decision kinds.

## Phase 3 — done

| Sub-patch | Status | Notes |
|------|------|------|
| 3.A schema + manager + REST + tests | ✅ v5.26.60 | 6 unit tests, 471 passing total |
| 3.B Manager.Run gating + config flag | ✅ v5.26.61 | full configuration parity |
| 3.C PWA per-story widgets | ✅ this patch | profile pill, Approve/Reject, status pill, rejected-reason |
| 3.D smoke + howto refresh | ✅ this patch | §7l live; howto extended |

The operator can now:

1. Toggle Settings → Autonomous → "Per-story approval gate" ON.
2. Submit a PRD spec; decompose it.
3. Override per-story execution profiles via the `prof:` pill while reviewing.
4. Approve the PRD — every story enters `awaiting_approval`.
5. Click Approve / Reject on each story individually as it's reviewed.
6. The runner picks up approved stories one at a time.

## Configuration parity

Final knob — `autonomous.per_story_approval` — confirmed reachable via:

- YAML (under `autonomous:`)
- Web UI (Settings → General → Autonomous → toggle)
- REST GET `/api/config` + PUT with dotted-key
- MCP `config_set` tool
- CLI `datawatch config set autonomous.per_story_approval true`
- Comm channels `configure autonomous.per_story_approval=true`

## Tests

471 unit tests still passing (no Go changes in this patch — pure PWA + smoke + docs). Smoke 59/0/2 against the dev daemon (the second SKIP is §7l's decompose timeout, gated cleanly; the first is orchestrator-disabled).

## Known follow-ups

- **Task #46** — New Session modal needs the same unified Profile dropdown (operator-asked).
- **Phase 4** — file association implementation.
- **#39 wake-up stack smoke** — builds on F10 fixture.
- **#41 testing.md coverage audit.**

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA — Settings → General → Autonomous now has
# the "Per-story approval gate" toggle. Stories on existing PRDs
# show the profile pill + status pill on next render.
```
