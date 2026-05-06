# datawatch v5.26.31 — release notes

**Date:** 2026-04-27
**Spans:** v5.26.30 → v5.26.31
**Patch release** (no binaries — operator directive).
**Closed:** Response capture filter regression (third pass) + README cleanup + mempalace audit backlog item.

## What's new

### Response capture: pure-noise tails now filter to empty

Operator: *"Last response is now only garbage and not any text from the last set of responses."*

The v5.26.23 filter overcorrected from v5.26.15: where v5.26.15 was too aggressive (killed prose framed in TUI borders), v5.26.23 was too charitable. Concrete failures captured from a live `claude-code` session that was still thinking when the operator inspected `/api/sessions/response`:

```
✻                                2
✢                      1
                       2
     (ctrl+b ctrl+b (twice) to run in background)
* Perambulating… (11m 42s · ↓ 22.2k tokens · almost done thinking with high effort)
──────────────────────────────────────── datawatch claude ──
  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt
✶                      3
```

Why this passed v5.26.23:

| Line | Reason it leaked |
|------|------|
| `* Perambulating… (11m 42s · ↓ 22.2k tokens …)` | hasWord3 matched "Perambulating"; anchored-footer didn't match (line starts with `*`) |
| `──── datawatch claude ──` | hasWord3 matched "datawatch claude"; isPureBoxDrawing requires 100% box-drawing chars |
| `⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt` | hasWord3 matched; `⏵⏵` prefix defeated the anchored-footer match |
| `✻                                2` | single spinner + digit — multi-spinner check needs ≥2 spinners; pure-glyph switch can't match because of the digit |
| `                       2` | bare digit + whitespace — no rule covered it |

v5.26.31 adds four new structural detectors and broadens the noise-pattern check:

- **`isLabeledBorder`** — line is ≥60% box-drawing chars among non-space runes AND has ≥6 box-drawing runes total. Catches `──── label ──` style section dividers regardless of label text.
- **`hasEmbeddedStatusTimer`** — line contains `·` and a digit immediately followed by `s`/`m`/`h` plus space/end. Catches `(11m 42s · ↓ tokens · thinking)` inside a longer line where `isPureStatusTimer` (which requires the WHOLE line be parenthesized) doesn't match.
- **`isSpinnerCounter`** — line has ≥1 spinner glyph + digits + whitespace + <3 letters. Catches `✻ 2` / `✢ 1`.
- **`isPureDigitLine`** — line is only digits + whitespace. Catches the bare counters.

And the noise-pattern list (`esc to interrupt`, `bypass permissions`, `ctrl+b ctrl+b`, `to run in background`, etc.) now applies to ALL lines, not just no-word lines. Trade-off: real prose like *"the doc says press esc to interrupt"* CAN now be filtered. Operator's volume of TUI-noise complaints dwarfs the false-positive cost.

### README cleanup

Operator: *"in main readme, don't need 'highlights since' or 'what's new' sections. the latest release summary is great and then just make sure the features and details throughout the document include all features at a high level"*

Removed:

- **`### Highlights since v4.0.0`** (5 bullets, version-tagged)
- **`### What's new since v3.0`** (10 thematic paragraphs)

The "Current release: vX.Y.Z" line at the top stays. Features that lived only in the removed sections (cross-cluster observer federation, slim distroless agent containers, `datawatch-app` mobile companion) folded into "What it does" so coverage is preserved at a high level without the version-bracketed framing.

### Mempalace alignment audit — backlog item added

Operator clarification: when asking about spatial memory layers, the comparison was against **mempalace**, not the internal L0–L5 stack.

Added to `docs/plans/2026-04-27-v6-prep-backlog.md`: full audit task that pulls current mempalace main, diffs against the BL97/98/99 baseline, enumerates every feature we haven't ported, maps each to existing datawatch concept / planned BL / true gap, and produces a prioritised plan doc + 1–3 quick-win shortlist for v6.1. Audit-only — implementation is downstream BLs.

## Configuration parity

No new config knob.

## Tests

2 new tests in `internal/session/response_filter_test.go`:

| Test | Verifies |
|------|----------|
| `TestStripResponseNoise_DropsSession_eac4_OperatorReport` | Verbatim operator-captured 8-line noise tail filters to empty |
| `TestStripResponseNoise_KeepsRealProseAroundNoise` | Real prose mixed with noise lines preserves the prose, drops the noise |

`TestStripResponseNoise_FooterAnchoringIsPositional` retired (the trade-off it enshrined is no longer the chosen behavior); replaced by the simpler `TestStripResponseNoise_BareFooterDrops`.

Total: 462 passing across `internal/session`, `internal/server`, `internal/autonomous` (was 460 in v5.26.30; +2 net).

## Known follow-ups

Same as v5.26.30 — see `docs/plans/2026-04-27-v6-prep-backlog.md`. New entries this release:

- Mempalace alignment audit + spatial memory expansion plan (operator-asked).

Phase 2–6 of the New PRD modal rework (story-level approve/edit, per-story execution profile, file association, persistent test cluster + smoke profile, docs refresh) still tracked.

## Upgrade path

```bash
git pull
datawatch restart
# Hard-reload the PWA on next visit (SW cache bumped to
# datawatch-v5-26-31). The `📄 Response` viewer on a still-thinking
# claude session should now show empty rather than a wall of TUI
# noise; once the answer comes back the prose appears normally.
```
