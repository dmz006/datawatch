# datawatch v5.26.53 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.52 → v5.26.53
**Patch release** (no binaries — operator directive).
**Closed:** Three design plan docs (Phase 3 + Phase 4 + Mempalace audit) and 7 mobile-companion issues.

## What's new

Operator directive (this turn): *"Work on everything except 6.0 release. Make sure to follow rules, make app issues, no hard code configuration, all settings through all channels and all other rules. Continue until we are ready to do final tests."*

v5.26.53 closes the remaining design-track items from the backlog so that v6.0 prep has concrete plans for every open piece.

### 1. PRD-flow Phase 3 design — `docs/plans/2026-04-27-prd-phase3-per-story-execution.md`

Per-story execution profile + per-story approval gate. Splits the existing `PRD.ProjectProfile` into:

- `PRD.DecompositionProfile` — the LLM that turns the spec into a plan.
- `PRD.ProjectProfile` (re-purposed) — the *default* execution profile.
- `Story.ExecutionProfile` — per-story override.
- `Story.Approved` + `StoryStatus = "awaiting_approval"` — per-story gate.

Adds 4 REST endpoints: `POST .../stories/{sid}/approve`, `POST .../stories/{sid}/reject`, `PUT .../stories/{sid}/profile`, extended `PUT .../profiles` with `decomposition_profile`.

PWA: extends New PRD modal with a "Decomposition profile" dropdown; story rows get profile-override widget + Approve/Reject buttons. Settings → Autonomous gets a "Per-story approval gate" toggle (default OFF preserves current behavior).

### 2. PRD-flow Phase 4 design — `docs/plans/2026-04-27-prd-phase4-file-association.md`

File association across PRD/story/task. Two sources:

- **`FilesPlanned`** — extracted by the decomposer LLM (prompt asks for `files: [...]` per story/task).
- **`FilesTouched`** — populated by the existing post-session diff hook (`git diff --name-only` against the worker's project_dir).

PWA renders a 📝 / ✅ pill row on each story/task. Conflict highlight when two pending stories plan the same file. Editable through the existing story-edit modal.

### 3. Mempalace alignment audit plan — `docs/plans/2026-04-27-mempalace-alignment-audit.md`

Operator clarification: *"i meant against mempalace. … look at expanding to full spatial memory like mempalace has."*

Doc establishes the audit *frame* — current state matrix (L0-L5 + spatial dims + BL97/98/99 ports all ✓), three audit steps (pull upstream / enumerate / fill gap table), provisional quick-win shortlist (auto-tagging, pinning, conversation-window stitching). The audit itself runs against current upstream and produces a follow-up doc.

### 4. datawatch-app issues — 7 child issues filed under #10

- [#11](https://github.com/dmz006/datawatch-app/issues/11) — Unified Profile dropdown in New PRD (PWA v5.26.30/34/46)
- [#12](https://github.com/dmz006/datawatch-app/issues/12) — Story-level review + edit (PWA v5.26.32)
- [#13](https://github.com/dmz006/datawatch-app/issues/13) — PRD panel FAB + filter toggle (PWA v5.26.36/37/46)
- [#14](https://github.com/dmz006/datawatch-app/issues/14) — Directory picker + mkdir-while-browsing (PWA v5.26.46)
- [#15](https://github.com/dmz006/datawatch-app/issues/15) — Response capture filter (PWA v5.26.31/51)
- [#16](https://github.com/dmz006/datawatch-app/issues/16) — Input Required banner refresh (PWA v5.26.44/45/49)
- [#17](https://github.com/dmz006/datawatch-app/issues/17) — `/diagrams.html` viewer fixes (PWA v5.26.50/51)

Each is scoped per-feature so the mobile companion can pick them up independently. #10 stays as the umbrella tracker; commented with cross-links.

## Configuration parity

All proposed REST endpoints in Phase 3 and Phase 4 explicitly require MCP + CLI + comm-channel parity per the project rule (no REST-only landings).

## Tests

No code changes — design + docs + issues. Smoke unaffected (51/0/1). Go test suite unaffected (465 passing).

## Known follow-ups (remaining for v6.0 prep)

| Item | Status |
|------|------|
| F10 agent lifecycle smoke probe | needs `agents.enabled` fixture |
| Wake-up stack L0–L5 smoke probes | needs spawned-agent fixture |
| Stdio-mode MCP tools smoke | needs MCP client wrapper |
| Phase 6 — screenshots refresh | needs browser automation |
| GHCR past-minor cleanup | needs PAT |
| v6.0 cumulative release notes | operator-prepared at cut |

The first three are gated on agent infrastructure being wired in CI. The rest are operator-side or implementation tasks now backed by design docs.

## Upgrade path

```bash
git pull
# No daemon restart needed — docs + plans only. Read the new
# design docs at docs/plans/2026-04-27-prd-phase3-*.md /
# 2026-04-27-prd-phase4-*.md / 2026-04-27-mempalace-alignment-audit.md
# before starting implementation work on those phases.
```
