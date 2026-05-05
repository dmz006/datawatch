# Release Notes — v6.9.0 (BL258 — Algorithm Mode 7-phase harness)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.9.0
Smoke: 99/0/6

## Summary

BL258 — operator-driven 7-phase Algorithm Mode harness shipped with full 7-surface parity. Closes the BL257-BL260 PAI parity arc's H1-priority gap (per `docs/plans/2026-05-02-pai-comparison-analysis.md` §2 + Recommendation H1).

PAI's Algorithm is the structured-thinking framework: Observe → Orient → Decide → Act → Measure → Learn → Improve. Datawatch's existing Guided Mode (BL221) was a 5-phase PRD-only subset; this BL ships the strict superset as a generic per-session state machine.

For v6.9.0, advance is operator-driven (REST/MCP/CLI/comm/PWA `advance` button). LLM auto-detection of phase boundaries from session output is a follow-up enhancement.

## Added

### `internal/algorithm` package

- `Phase` enum: 7 canonical phases.
- `State` struct: `session_id`, `current`, `history []PhaseOutput`, `started_at`, `updated_at`, `aborted`.
- `Tracker` (in-memory, concurrent-safe): `Start`, `Get`, `All`, `Advance`, `Edit`, `Abort`, `Reset`.
- 12 unit tests pass.

### REST surface

- `GET /api/algorithm` — list every session in Algorithm Mode (returns sessions + canonical phases).
- `POST /api/algorithm/{id}/start` — register session at Observe (idempotent).
- `GET /api/algorithm/{id}` — read state.
- `POST /api/algorithm/{id}/advance` — close current phase by recording its output, advance to next.
- `POST /api/algorithm/{id}/edit` — replace last recorded phase output.
- `POST /api/algorithm/{id}/abort` — terminate mid-flight.
- `DELETE /api/algorithm/{id}` — reset.
- All write paths emit audit entries (`algorithm_start`/`algorithm_advance`/`algorithm_edit`/`algorithm_abort`/`algorithm_reset`).

### MCP tools (7)

- `algorithm_list`, `algorithm_get`, `algorithm_start`, `algorithm_advance`, `algorithm_edit`, `algorithm_abort`, `algorithm_reset`. All proxy to REST.

### CLI

- `datawatch algorithm list`
- `datawatch algorithm get <session-id>`
- `datawatch algorithm start <session-id>`
- `datawatch algorithm advance <session-id> --output "..."`
- `datawatch algorithm edit <session-id> --output "..."`
- `datawatch algorithm abort <session-id>`
- `datawatch algorithm reset <session-id>`

### Comm verb

- `algorithm` (list)
- `algorithm <verb> <session-id> [output...]` for start/get/advance/edit/abort/reset.

### PWA

- Settings → Agents → Algorithm Mode card.
- Per-session row with 7-step phase strip (Obs/Ori/Dec/Act/Mea/Lea/Imp), color-coded by status (current = accent, done = success, future = muted).
- Output input field + Advance / Edit / Abort / Reset buttons per row.
- Auto-loads on Settings tab open.

### Locale

- 14 new keys × 5 bundles (`algorithm_section_title`, `algorithm_intro`, `algorithm_loading`, `algorithm_empty`, `algorithm_history_count`, `algorithm_output_ph`, `algorithm_btn_advance`, `algorithm_btn_edit`, `algorithm_btn_abort`, `algorithm_btn_reset`, `algorithm_aborted`, `algorithm_edit_empty`, `algorithm_confirm_abort`, `algorithm_confirm_reset`).

### Smoke

- New step "14. v6.9.0 BL258 — Algorithm Mode 7-phase per-session harness" — start → state check (observe) → advance → state check (orient) → cleanup.

## Backward compatibility

- No breaking REST/MCP/CLI/comm changes.
- New endpoints; nothing removed or renamed.
- BL221's PRD-only Guided Mode unchanged. Algorithm Mode is the strict superset for non-PRD sessions and is opt-in per session.

## What didn't change

- No new go-mod dependencies.
- No persisted-state schema (in-memory tracker; future work can add disk persistence).

## Mobile parity

[`datawatch-app#54`](https://github.com/dmz006/datawatch-app/issues/54) updated with shipped scope (phase strip rendering + per-session row UI).

## Sequence reminder

- BL257 ✅ closed (v6.8.0 + v6.8.1).
- BL258 ✅ closed (v6.9.0 — this release).
- Next: BL259 P1 — Evals Framework (v6.10.0).
- Then: BL259 P2 — migrate scan to evals (v6.10.1).
- Then: BL260 — Council Mode (v6.11.0).

See `docs/plans/2026-05-05-bl257-260-pai-parity-plan.md`.

## See also

- CHANGELOG.md `[6.9.0]`
- `docs/plan-attribution.md` (BL258 row updated to ✅ v6.9.0)
