# datawatch v5.26.69 — release notes

**Date:** 2026-04-28
**Patch.** Closes the remaining audit + plan items in one bundle.

## What's new

Seven items in one patch (operator-asked: *"Finish all plans, finish all audits, then the plans from the audits, finish the tests, finish everything"*):

### 1. `scripts/release-smoke-secure.sh` (#57)

Standalone runner for encryption-mode smoke. Brings up an encrypted daemon at port 18444 in a temp data dir, verifies `/api/health` reports `encrypted=true`, exercises memory save → list round-trip → daemon restart → state persistence. Skips cleanly when `DATAWATCH_SECURE_PASSWORD` env var is missing or `datawatch` isn't in `PATH`. Closes §41 audit gap #5.

### 2. `docs/flow/prd-phase3-phase4-flow.md` (#51)

New mermaid sequence + state-machine diagrams covering the v5.26.60–67 PRD-flow rework:

- Per-story state machine: `pending → awaiting_approval → pending → in_progress → completed`, with reject/resume edges.
- End-to-end sequence including `DecompositionPrompt` files extraction + `ProjectGit.DiffNames` post-session hook + `RecordTaskFilesTouched`.
- File-conflict detection flowchart (PWA-side `_conflictMap` walk).

Added to `internal/server/web/diagrams.html` index so the mermaid renderer picks it up. Closes Phase 6 diagrams refresh.

### 3. Mempalace audit gap table (#52)

`docs/plans/2026-04-27-mempalace-alignment-audit.md` extended with:

- Module-by-module gap table for every file in `MemPalace/mempalace/mempalace/`. 24 modules audited; 12 fully ported (✅), 7 partial (🟡), 5 outright gaps (⏳).
- Refined quick-win shortlist (5 items): `room_detector_local.py` port (#1, ~6h), memory pinning (#2, ~2h), conversation-window stitching (#3, ~3h), `query_sanitizer.py` port (#4, ~2h), `repair.py` self-repair (#5, ~1d).
- Hand-off notes: each lands as own patch with full configuration parity.

The 5 gaps + 7 partials are tracked for v6.1+. Audit itself is complete.

### 4. `docs/howto/autonomous-planning.md` extended (Phase 4 + howto sync)

New "File association (v5.26.64–67, Phase 4)" subsection covers `📝` / `✅` pill semantics, decomposer prompt extension, post-session diff hook, file-edit modal, conflict detection. With REST endpoint shapes and audit decision kinds.

### 5. Autonomous howto screenshots recaptured

Same four shots regenerated against the v5.26.69 PWA so they reflect Phase 3 (story Approve/Reject pills + profile pill + status pill + rejected-reason rendering) + Phase 4 (file pills + ✎ files button + ⚠ conflict markers):

- `autonomous-landing.png` (90KB)
- `autonomous-prd-expanded.png` (110KB)
- `autonomous-mobile.png` (194KB)
- `autonomous-new-prd-modal.png` (56KB)

Pipeline: `bash scripts/howto-seed-fixtures.sh` → `datawatch restart` → `node scripts/howto-shoot.mjs <shot>`.

### 6. v6.0 cumulative release notes — DRAFT

`docs/plans/RELEASE-NOTES-v6.0.0-DRAFT.md` consolidates the v5.0.x → v5.26.69 patch + minor accumulation into a single operator-facing narrative. Covers PRD-flow Phases 1–6, Container Workers F10, memory + KG, operator UX, CI hardening, smoke coverage growth (33/0/2 → 66/0/3), configuration parity rule, mobile companion mirror, migration notes, known follow-ups. Operator finalizes prose / order / level-of-detail at v6.0 cut moment.

### 7. Three new datawatch-app issues for v5.26.60+ PWA changes

Filed under [datawatch-app#10](https://github.com/dmz006/datawatch-app/issues/10) umbrella:

- [#18](https://github.com/dmz006/datawatch-app/issues/18) — Phase 3 per-story approval + execution profile (v5.26.60–62)
- [#19](https://github.com/dmz006/datawatch-app/issues/19) — Phase 4 file association + edit modal (v5.26.64+67)
- [#20](https://github.com/dmz006/datawatch-app/issues/20) — Unified Profile dropdown in New Session modal (v5.26.63)

Each scoped per-feature so the mobile companion can pick them up independently. #10 commented with cross-links.

## Configuration parity

No new config knob (encryption smoke uses existing `--secure` flag).

## Tests

Smoke unaffected (60/0/3 against the dev daemon). Go test suite unaffected (475 passing). `release-smoke-secure.sh` runs cleanly when `DATAWATCH_SECURE_PASSWORD` is set; SKIPs when missing.

## Backlog status — all asked items closed

| Task | Status |
|------|------|
| #47 Phase 4 decomposer prompt | ✅ v5.26.67 |
| #48 Phase 4 post-session diff hook | ✅ v5.26.67 |
| #49 Phase 4 file-conflict detection | ✅ v5.26.67 |
| #50 Phase 4 PWA file-edit modal | ✅ v5.26.67 |
| #51 Phase 6 diagrams refresh | ✅ this patch |
| #52 Mempalace audit gap table | ✅ this patch |
| #53 KG add+query smoke | ✅ v5.26.68 |
| #54 Spatial-dim filter smoke | ✅ v5.26.68 |
| #55 Entity detection smoke | ✅ v5.26.68 |
| #56 Per-backend channel send smoke | ✅ v5.26.68 (partial; outbound deferred per CI portability) |
| #57 Encryption-mode smoke runner | ✅ this patch |
| #58 Stdio MCP smoke | ✅ v5.26.68 (partial; subcommand check) |
| #59 Wake-up L4/L5 smoke | ✅ v5.26.68 (prerequisite check) |
| #60 v6.0 release notes draft | ✅ this patch |

Task list cleared. Remaining v6.0-prep items are operator-prepared (cut moment) or PAT-gated (GHCR cleanup).

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload PWA. Diagrams viewer index now includes the new
# Phase 3+4 flow doc. Howto + screenshots reflect current PWA
# shape. release-smoke-secure.sh available for encryption-mode
# regression coverage.
```
