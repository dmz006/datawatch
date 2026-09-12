# Test Plan — v8.25.0

**Version**: v8.25.0  
**Sprint**: Automata UI improvements — bug fixes, spec expand, status graphs, action bar  
**Stories**: TS-671–TS-679 (9 stories: 2 automated unit, 2 automated unit via existing tests, 5 PWA conflict:pwa)  
**Go unit tests added**: 0 (UI-only change; existing TS-700–TS-703 in autonomous_story_status_test.go cover API contract)

---

## Scope

BL373 delivered operator-visible improvements to the Automata section of the PWA.
No new API endpoints. No new config fields. No backend changes.

### Bug fixes

**BUG-A — task click in list card collapses "Stories & tasks" panel**  
Root cause: `renderStory(prd, st)` in `renderAutomataCard` called `_prdToggleTask` →
`loadAutomataPanel` → full list re-render, collapsing all `<details>` elements.  
Fix: replaced with `renderDetailStoriesTree(prd)` (CSS toggle, no reload).

**BUG-B — expand button in Stories tab does nothing on first click**  
Root cause: `_prdToggleTask` called `_refreshAutomataOrPRD` → `loadAutomataPanel`
(list panel, not visible in detail view). DOM not updated.  
Fix: `_prdToggleTask` now detects `_automataDetailId` and calls
`renderPRDDetailView(_automataDetailId)` directly.

### New features

**Spec expand** — Initial spec in detail header expandable beyond 280 chars.
"show full" / "collapse" inline toggle.

**Status graphs** — Progress card shown while `status === running` or `decomposing`:
- ✓/✗ decompose indicator, story count, task count
- Per-story progress bars (X/Y tasks, %)
- Async CPU%/RSS from `/api/observer/envelopes`

**Cancel button visibility** — Cancel always shown in toolbar for non-cancelled automata,
including while running (previously hidden when `status === running`).

**View Sessions** — "→ View Sessions" item added to ⋯ Edit dropdown.

---

## Stories

| Story | Description | How to run |
|-------|-------------|-----------|
| TS-671 | List card task click — panel stays open | PWA: Chrome CDP, `pwa_cdp.py` |
| TS-672 | Detail Stories tab — expand button works first click | PWA: Chrome CDP, `pwa_cdp.py` |
| TS-673 | Spec expand show full / collapse | PWA: Chrome CDP, `pwa_cdp.py` |
| TS-674 | API stories field + status shape (regression guard) | Unit: `go test ./internal/server/` |
| TS-675 | Status graphs visible when running | PWA: Chrome CDP, requires conflict:llm |
| TS-676 | Status graphs compute row from observer envelope | API + PWA |
| TS-677 | Cancel button visible when running | PWA: Chrome CDP, requires conflict:llm |
| TS-678 | ⋯ Edit dropdown contains "View Sessions" | PWA: Chrome CDP |
| TS-679 | Locale guard — 8 new keys in all 5 bundles | Unit: `go test ./internal/server/` |

---

## Pre-conditions

- `autonomous.enabled: true` in daemon config (already set for smoke daemon)
- For TS-675/TS-677: LLM backend configured + running automaton (conflict:llm)
- For TS-676: observer peer registered with at least one active envelope

## Automated validation already in place

- `v5280_locales_test.go` `mustHave` — guards all 8 new keys across all 5 bundles (TS-679)
- `autonomous_story_status_test.go` TS-700–TS-703 — guards `stories` field shape and story status values (TS-674)
- `node --check internal/server/web/app.js` — passes (syntax)
- `go test ./... ` — 2506 tests pass

## Operator validation required (PWA stories)

TS-671, TS-672, TS-673, TS-675, TS-677, TS-678 require a browser session.
Run via `scripts/run-tests.sh` with `TEST_FEATURE=automata` or manually:

```bash
# Open PWA, navigate to Automata tab
# TS-671: expand Stories & tasks on a list card, click a task → panel stays open
# TS-672: open automaton detail → Stories tab → click ▶ → expands immediately
# TS-673: create automaton with >280 char spec → detail header shows "show full"
# TS-675: while running → header shows status graphs card
# TS-677: while running → Cancel button visible in header toolbar
# TS-678: ⋯ Edit dropdown contains "→ View Sessions"
```
