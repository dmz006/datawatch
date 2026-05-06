# Release Notes — v6.11.2 (PRD → Automata sweep + BL262 filed)

Released: 2026-05-05
GH release: https://github.com/dmz006/datawatch/releases/tag/v6.11.2

## Summary

Two things ship in v6.11.2:

1. **PRD → Automata user-visible string sweep** — the BL221 (v6.2.0) rename of "PRD" to "Automaton/Automata" was applied to the data model + Automata view + nav, but a long tail of UI strings (toasts, modal titles, button tooltips, settings section headers, locale fallbacks) still said "PRD". Operator caught these on review of v6.11.1.
2. **BL262 filed** — Claude rate-limit prompt format the existing detector misses.

## Changed

### `internal/server/web/app.js` — 19 user-visible strings

| Was | Now |
|---|---|
| Toggle PRD filters | Toggle Automata filters |
| PRD Orchestrator (settings section) | Automata Orchestrator |
| No PRDs match. | No Automata match. |
| Edit PRD `<id>` (modal title) | Edit Automaton `<id>` |
| New PRD / New PRD graph | New Automaton / New Automaton graph |
| parent PRD / root PRD / child PRD (tooltips) | parent / root / child automaton |
| this PRD's row (tooltip) | this automaton's row |
| PRD updated / PRD edit failed / etc. (toasts) | Automaton updated / Automaton edit failed / etc. |
| Failed to load PRD | Failed to load automaton |
| PRD `<id>` not in current filter | Automaton `<id>` not in current filter |
| Delete PRD `<id>` ? + child PRD spawned via SpawnPRD | Delete Automaton ? + child automaton via spawn-automaton |
| Autonomous PRD decomposition (config) | Autonomous Automata decomposition |
| PRD-DAG orchestrator (config) | Automata-DAG orchestrator |
| %d PRD/PRDs (Pipeline card) | %d automaton/automata |
| (inherit PRD default) | (inherit automaton default) |

### Locale bundles — all 5 swept

Bulk pass on de/es/fr/ja using `\bPRD\b → Automaton` / `\bPRDs\b → Automata`. ja.json received a separate non-word-boundary pass since Japanese text concatenates without spaces.

13+ keys per bundle updated. en.json: 13 explicit edits. Other 4 bundles: bulk replacement.

## What didn't change

- Function names (`renderPRDDetailView`, `runPRDScan`, `confirmPRDDelete`).
- DOM IDs / CSS classes (`prd-row`, `prd-task-session`, `prd-header-btn`).
- API paths (`/api/prds/...`).
- Go struct field names (`prd.parent_prd_id`).
- Locale keys (`prd_btn_delete_title` etc.) — only the values changed.
- Code comments mentioning PRD — historical breadcrumbs explaining BL221.

These are technical surface; refactoring would be destabilizing with no operator-visible benefit.

## BL262 — Claude rate-limit prompt detection

Operator-reported prompt that the existing detector missed:

> `You're out of extra usage · resets 11:50am (America/New_York)`

Existing regex set covers "Claude usage limit reached" / "rate limit" / "try again at" but not:
- "out of extra usage" wording
- `· resets <time> (<tz>)` separator
- Named-timezone format (`America/New_York`)

Filed as BL262 in `docs/plans/README.md` with acceptance criteria for adding the pattern + named-timezone parser. Small targeted addition to existing rate-limit detector; no new package, no new surface.

## Mobile parity

[`datawatch-app#59`](https://github.com/dmz006/datawatch-app/issues/59) filed: same PRD → Automaton/Automata user-visible string sweep on the Compose Multiplatform side.

## See also

- CHANGELOG.md `[6.11.2]`
- `docs/plans/README.md` BL262 entry
