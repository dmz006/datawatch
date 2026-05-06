# datawatch v5.26.23 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.22 → v5.26.23
**Patch release** (no binaries — operator directive).
**Closed:** Response capture filter regression — prose-in-borders preserved, pure decoration still dropped.

## What's new

### Response viewer no longer eats real prose

Operator: *"Last response now has only the animated stuff and not real response data. Ex 50 ✶ 1 ✽ 2 ✢ 3"*

v5.26.15's filter cleared spinners + status timers + footer hints from the captured response, but it was too aggressive on box-drawing characters. The filter checked `strings.Contains(s, "│  ")` and similar — which matched lines like `│  Here is your answer  │`, killing the real prose claude / opencode framed inside a TUI border.

Net effect: when claude wrapped its answer in a box, the prose got filtered, leaving only the multi-spinner progress lines (which DIDN'T match any v5.26.15 pattern and snuck through).

v5.26.23 inverts the strategy:

| Rule | What it does |
|------|------|
| **`hasWord3` gate** | Any line containing a run of 3+ ASCII letters is presumed prose and kept by default. Border decoration around it doesn't matter. |
| **`isPureBoxDrawing`** | Lines that are 100% box-drawing chars + whitespace (e.g. `╭────────╮`, `│       │`, `├─────┤`) still get dropped. |
| **`isPureStatusTimer`** | Parenthesized timer fragments (`(7s · timeout 1m)`, `(5s)`, `(123ms)`) get dropped BEFORE the prose gate, since words like "timeout" / "elapsed" / "remaining" pass `hasWord3` but are noise. |
| **Anchored footer matching** | Bare `esc to interrupt` at line start drops; prose like *"the doc says press esc to interrupt"* (mid-sentence mention) is kept. |
| **Multi-spinner pollution** | New heuristic drops lines with **2 or more** spinner glyphs (e.g. `Ex50 ✶            1 ✽            2 ✢`). Real prose almost never has more than one spinner. |

3 new unit tests cover the regression matrix:

- `PreservesProseInBoxBorder` — claude-style boxed answer survives.
- `DropsMultiSpinnerLine` — wide-pane progress noise drops, prose around it stays.
- `FooterAnchoringIsPositional` — bare footer drops, prose-mentioning-footer stays.

The existing 6 tests still pass (single-spinner glyphs, status timers, ANSI strip, blank-collapse, empty input, all-noise input).

## Configuration parity

No new config knob.

## Tests

- **9 of 9 response-filter tests passing** (was 6/6 in v5.26.15).
- 1413 + 9 = 1422 Go unit tests passing total.
- Smoke unaffected.

## Known follow-ups

- BL113 token broker integration (v5.26.24+) — replace long-lived `DATAWATCH_GIT_TOKEN` Pod env with per-spawn ephemeral tokens minted by the parent.
- Per-session workspace reaper (clones persist after session ends).
- v6.0 packaging items unchanged.

## Upgrade path

```bash
git pull
datawatch restart
# Click 📄 Response on a session whose answer comes back framed in
# a TUI border — the prose now shows up; only the border lines and
# spinner pollution get filtered.
```
