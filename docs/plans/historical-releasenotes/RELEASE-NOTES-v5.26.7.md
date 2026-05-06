# datawatch v5.26.7 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.6 → v5.26.7
**Patch release** (no binaries — operator directive: every release until v6.0 is a patch).
**Closed:** Autonomous-tab Refresh button removed entirely.

## What's new

### Autonomous tab — Refresh button gone

Operator: *"Why is the refresh button still on autonomous page, or should refresh when changes happen, it's the rest of the functionality done for that tab?"*

v5.26.6 hid the manual `↻ Refresh` button when WS was connected and showed a `● auto` badge instead. Operator wanted it gone entirely — the auto-refresh is reliable, the button is just clutter, and the question itself was a sign the hide-when-connected hedge was confusing.

v5.26.7:

- **Refresh button deleted from `renderAutonomousView`.** No "show when WS down" fallback either; the header status dot already surfaces WS-disconnect.
- **Auto badge stays put with WS-state coloring.** Green `● auto` when connected, red `● offline` when not. The operator can see at a glance whether their next mutation will reflect live in the panel.
- SW `CACHE_NAME` bumped → `datawatch-v5-26-7` so installed PWAs drop the v5.26.6 cache on next activate.

## Is the rest of the Autonomous tab done?

Yes. The functional surface as of v5.26.7:

| Feature | Where it landed | Notes |
|---------|-----------------|-------|
| List + status pills + click-to-expand stories | v5.3.0 (BL202 first cut) | Tasks render with spec, LLM badge, spawn/child markers, verdicts |
| New PRD modal | v5.3.0 + v5.5.0 + v5.26.1 | Backend + effort + model; v5.26.1 filters disabled backends + auto-fetches model lists |
| Decompose / Approve / Reject / Request-revision / Run / Cancel | v5.2.0 (BL191 Q1) | Per-status action buttons with confirm where destructive |
| Edit PRD title + spec | v5.19.0 (full CRUD) | Modal (PATCH /api/autonomous/prds/{id}); non-running PRDs only |
| Hard-delete PRD + descendants | v5.19.0 | DELETE ?hard=true; confirms before firing |
| Edit task spec + LLM | v5.5.0 (BL202 second cut) | needs_review / revisions_asked only; ✎ icon button |
| Per-PRD + per-task LLM override | v5.4.0 (BL203) | New "LLM" button on every non-running PRD; live backend list from /api/backends |
| Template instantiate | v5.2.0 (BL191 Q2) | Template flag + Instantiate button + var substitution |
| Decisions log | v5.2.0 (BL191 Q3) | Disclosure on each PRD row |
| Recursive child PRDs | v5.9.0 (BL191 Q4) | Parent badge + depth indicator + Children disclosure with click-to-scroll (v5.26.6) |
| Verdicts at task + story levels | v5.10.0 (BL191 Q5/Q6) | Color-coded badges (pass/warn/block); click-to-expand drill-down panel (v5.26.6) |
| WS auto-refresh on every PRD save | v5.24.0 | `prd_update` broadcast → 250 ms-debounced `loadPRDPanel()` |
| Buttons that actually fire (escHtml inline-onclick fix) | v5.26.3 | Operator-reported v5.26.0 silent no-op |
| Refresh button hidden / removed | v5.26.6 / v5.26.7 | Auto-refresh covers every mutation; button was clutter |

The two BL202 polish items left from the audit (verdict drill-down panel, child-PRD navigation) shipped in v5.26.6. There's nothing else open on the Autonomous tab.

## Configuration parity

No new config knob.

## Tests

1395 still passing. PWA-only cleanup; no Go code changed beyond the version bump.

## Known follow-ups

All operator-driven items closed. v6.0 packaging items unchanged (cumulative release notes, CI for `parent-full` + `agent-goose`, CI security scan automation, GHCR past-minor cleanup run).

## Upgrade path

```bash
git pull          # patch series — no binary update path
# Hard-refresh the PWA tab (Ctrl+Shift+R) once to pick up the new
# CACHE_NAME and the v5.26.7 app.js. The Refresh button will be gone;
# the green `● auto` badge to its right will be the only visible
# indicator that the panel is live-updating.
```
