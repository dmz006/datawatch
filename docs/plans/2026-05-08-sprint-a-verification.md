# Sprint A — Automata UX mobile-first overhaul — live verification

**Filed:** 2026-05-08, after operator directive to "follow plans + test against AGENT.md".
**Test method:** local PWA at `https://localhost:8443/` driven via Chrome MCP plugin (browser_batch).
**Daemon:** v6.22.1 running.

## Why this audit

Operator's `Unclassified` block in `docs/plans/README.md` lists ~30 sub-bullets under "changes to automata from recent update". That block was filed **before v6.13.x patches landed**. The patches addressed many of the listed items. This audit verifies the live PWA against each bullet so the operator can confirm closure or surface residuals.

## Verified state per item

| Operator bullet | Status | Evidence (live + source) |
|---|---|---|
| **A1.1** Template strip "2 chars wide × 30 rows tall, unreadable" | ✅ shipped v6.13.1 | Live: `.wizard-template-strip` is single flex row, 49px tall, 530px wide. Source comment: `"Start-from-template strip: single-line flex row that fits any viewport; no more 2-char-wide × 30-row malformed column."` (`app.js:10348`) |
| **A1.2** All large text inputs need mic | ⚠️ partial | Wizard intent + title fields have mic (2 mics verified). **Generic mic-button helper `micButtonHTML(targetId)` exists** (`app.js:3583`) but isn't yet attached to every large input across the app. |
| **A1.3** "What do you want to accomplish" too small + truncates | ✅ shipped v6.13.1 | `<textarea rows="4" class="wizard-textarea">` full width 530px in current viewport. |
| **A1.4** "Inferred / Type" wizard contradiction | ✅ shipped v6.13.1 | Replaced with "Detected: <type> ✏️" pill (tap to expand chip row). Source: `"Inferred header DROPPED — type is now a 'Detected: <type> ✏️' pill below intent that taps to expand the chip row (Q1 option c)."` (`app.js:10351`) |
| **A1.5** Type buttons wrap on mobile | ✅ shipped v6.13.1 | `.wizard-type-chips` uses single-row scroll-x. Comment: `"Type chips: single-row scroll-x on mobile; no wrap."` (`app.js:10353`) |
| **A1.6** Huge padding around workspace/director/etc. | ✅ shipped v6.13.1 | Tightened to 6px from 12px+. Comment: `"Workspace + Execution + Advanced: tightened paddings (6px instead of 12px+)."` Live: `wizard-template-strip` padding: `5px 8px`. |
| **A1.7** Advanced checkbox excessive padding | ✅ shipped v6.13.1 | Live: `.wizard-checkbox-row` is 22px tall with 1px 0 padding, 2px gap between rows. Comment: `"Advanced checkboxes: 2px row gap, no excess margins."` |
| **A1.8** "Skills (available in agent profile)" — what does that mean? | ✅ shipped v6.13.1 | Hint text changed to: `"💡 Configure skills per workspace in Settings → Agents → Project Profiles → Skills."` Operator follow-up question still: **does Project Profile editor actually have a Skills field?** (Answer: yes — see A10 below + `app.js:9136 renderProjectEditorForm`.) |
| **A2** Multi-select error "command isn't known" | ✅ shipped v6.12.4 | `batchAutomataAction` has eligibility map: `run` only on `approved`, `approve` only on `needs_review`, `cancel` on non-history, `archive`/`delete` on history. All wire to specific REST endpoints. Source: `app.js:9770-9810`. **Live test:** selected 1 PRD, batch bar appeared with disabled/enabled buttons matching state. |
| **A3.1** Buttons inconsistent across automata cards | ⚠️ unverified | Need to look at multiple automata in different states; only 1 PRD currently in the list. |
| **A3.2** "..." dropdown should go away | ✅ shipped v6.6.0 | Persistent header toolbar exposes Edit Spec / Settings / Request Revision / Clone to Template / Delete as visible buttons. Source: `app.js:_renderDetailHeader`. |
| **A3.3** Action buttons should have own row at top | ✅ shipped v6.6.0 | Persistent header above tab strip; shipped via BL246. |
| **A3.4** Tabs (overview/stories/decisions/scans) look like buttons | ✅ shipped v6.6.0 | Live: `.output-tab` class with `.active` selected state — real tabs. Source: `app.js:10848-10852`. |
| **A4** Stories tab cards | ✅ shipped v6.6.0 | `_renderDetailStories` calls `renderStory(prd, st)` which renders rich card per BL246. Source: `app.js:10888-10894`. |
| **A5** Decisions tab needs cards w/ detail | ✅ shipped v6.6.0 | `_renderDetailDecisionsTab` renders timeline + expandable detail per row. Source: `app.js:10896+`. |
| **A6** Scan tab needs cards | ✅ shipped v6.6.0 | Scan tab renders results + Run-Scan button. Source: `_renderDetailScan`. |
| **A7** Rules check tab missing | ✅ shipped v6.6.0 | Rules tab IS rendered when `hasRules` is true (line 10846). **Tab only shows when enabled** — matches operator's last-line "scans and rules check should be visible only if they are enabled". |
| **A8.1** Run-scan / rules-check should be top-level actions | ⚠️ partial | Tabs include the run-button inline; **no separate top-level "Run Scan" action button** in persistent header outside the tab strip. Operator's complaint matches: scan running needs to be in the workflow, not buried in a tab. |
| **A8.2** Show scans/rules tabs only if enabled | ✅ shipped v6.6.0 | Confirmed at line 10846 (`hasScan`, `hasRules` filters). |
| **A9** Settings-for-automata UX | ✅ shipped v6.6.0 | Same wizard + tightened-padding patterns reused via `_prdMountModal`. |
| **A10** "Edit project profile: XXX" header redundant | ✅ shipped v6.13.1 | Source comment at `app.js:9130-9135`: `"v6.13.1 — operator: 'when edit is opened the profile should be across the top and not Edit project profile: XXX, we know we're editing'. Strip the prefix; show profile name as the heading."` `const title = isNew ? 'New ' + kind + ' profile' : name;` |

## Net findings

**Already shipped (operator-visible items closed in v6.6.0–v6.13.14 patch chain):** 16 of 19 enumerated bullets.

**Genuinely outstanding:**
1. **A1.2 generic mic across every large text input** — the helper exists (`micButtonHTML`) but isn't attached app-wide. Needs an audit of every large `<textarea>` / multi-line `<input>` in the PWA.
2. **A3.1 button consistency across automata cards** — needs a multi-state test (draft / needs_review / running / completed / archived). Currently only 1 PRD present in test environment to verify.
3. **A8.1 top-level "Run Scan" / "Run Rules" action button** in detail-view persistent header — currently the run controls live inside the Scan/Rules tabs.

## Recommendations to operator (binary-question form)

**Q1 — Sprint A scope:** Should I close the Unclassified bullet block en masse and file the 3 genuinely-outstanding items (A1.2 + A3.1 + A8.1) as fresh BLs (BL292, BL293, BL294)?
- **(a)** Yes — close the Unclassified block, file 3 fresh BLs, queue them for v6.23.0.
- **(b)** No — walk every bullet with you live in browser before closing anything.
- **(c)** Different ordering — pick one bullet to fix now and defer the rest.

**Q2 — Sprint A vs Sprint B/C ordering:** The 3 actually-outstanding A items are smaller than I thought. Sprint B (Council Mode polish) is also small. Sprint C (BL289 fallback tests) is small.
- **(a)** Recommended: do all three sprints sequentially in v6.23.0 (10–15 commits, all clean fixes).
- **(b)** Just A — finish Sprint A items in v6.23.0; defer B+C.
- **(c)** Just B+C — close two BLs cleanly; leave A1.2/A3.1/A8.1 as fresh BLs for next cycle.

**Q3 — Sprint A live verification gap:** A3.1 (button consistency) requires multiple PRDs in different states. Only one draft PRD exists right now.
- **(a)** Spawn smoke PRDs in a few states to verify (smoke-* names, cleaned up after — never touches a95f).
- **(b)** Defer A3.1 until next time you see the inconsistency live; close on operator-confirmed evidence then.

Awaiting your call on Q1 / Q2 / Q3.
