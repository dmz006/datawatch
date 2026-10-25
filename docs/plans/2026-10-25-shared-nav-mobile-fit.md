# Shared navigation mobile fit

Status (2026-10-25): The report-only run is complete for the substitute probe, and the
named checker's own measures are still unverified. `scripts/test-stories/pwa/shared-nav-mobile-fit.mjs`
was absent from the branch when the report-only run was taken (it now exists, see below), so that measurement was taken with
`scripts/test-stories/pwa/shared-nav-mobile-fit-probe.mjs`. That probe ran through
`scripts/e2e-pwa.sh` in local mode against an isolated sandbox daemon on the unmodified
tree: exit 0, 336 occurrences in 26 groups. Its findings are not the checker's findings.
Daemon version, from the Version line in `cmd/datawatch/main.go:119`: 9.11.0. Run record:
`docs/testing/runs/shared-nav-mobile-fit/report-only.txt`.

Spec issue: the spec names a checker. Its findings (the "about 250 nav icon" count and the
"35 px" nav finding) cannot be verified until the original checker is supplied or the
operator confirms the probe is the intended measure.

Update (2026-10-25, after commit `cccbd9f0`): `scripts/test-stories/pwa/shared-nav-mobile-fit.mjs` now exists. It is a gate wrapper that runs the probe in gate mode; it is not the spec's checker. `make test-e2e-shared-nav` ran it on the fixed tree at 13:51 and it PASSED (0 findings at 320, 360 and 390 px, light and dark, `make` exit 0). The same gate on the unmodified tree FAILED with 4 findings. The run record is `docs/testing/runs/shared-nav-mobile-fit/make-run/`. Its first two attempts failed because a long `TMPDIR` stopped Chrome from starting (record in `failed-attempts.log`).

Release numbers: main was 9.15.0 on 2026-10-25, not 9.14.2 as the Phases below first stated. The 9.14.x numbers are superseded: each phase's release is the next patch above main at its merge time.

## Phases

- Phase 1, baseline report-only run on the unmodified tree: In Progress. The substitute probe is done through e2e-pwa.sh (336 occurrences, 26 groups, daemon 9.11.0). The named file now exists as a wrapper around the probe (commit `cccbd9f0`). Its gate ran on 2026-10-25: PASS on the fixed tree, FAIL on the unmodified tree. The spec's own measures are still unverified. Release: none, this phase produces evidence only.
- Shared navigation bar root-cause fix (commits ae73b0ff and a7cc30e4): Done, proven 2026-10-25 on the fixed tree. Gate pass in light and dark at 320, 360 and 390 px (0 findings, was 4), and report-only nav occurrences 44 to 0. The nav no longer scrolls: scrollWidth equals clientWidth at every phone width. Desktop and tablet nav geometry matches nav-baseline.json (456 boxes, zero delta). Evidence: `docs/testing/runs/shared-nav-mobile-fit/post-fix-report.json` and `post-fix-report.txt`. Text zoom was not measured, because the matrix's zoom levels are not defined in the repo.
- Phase 2, fix the one-line shared style findings (header chrome and the toast close button, 156 occurrences in 7 rows): In Progress, not Done. The fix commits raised the header search button and the Observer section toggle to 40 px, but all seven header rows are still flagged at the 44 px probe threshold. Release: the next patch above main at merge time (main was 9.15.0 on 2026-10-25).
- Phase 3, fix the per-tab findings (136 occurrences in 14 rows, deferred to each tab): Planned. Release: the next patch above main after Phase 2, at merge time. The Observer rows here overlap BL572; see Worklist.
- Phase 4, operator docs, testing tracker rows and the sync issue: Done (2026-10-25). How-to `docs/howto/navigation-mobile-fit.md` (listed in `docs/howto/README.md`, with its reason for the LLM-only list in `scripts/check-howto-coverage.sh`); the run in `docs/testing.md`; the section "Shared navigation mobile fit" in `docs/testing-tracker.md`; a CHANGELOG entry under [Unreleased] only. Mobile App Sync: filed `dmz006/datawatch-app#271`, titled "[sync] shared navigation fit at phone widths" (`mobile-parity: datawatch-app#271`). Release: none; the merge helper takes the next patch above main. Review corrections (2026-10-25): the issue body no longer names the working branch; the 40 px stylesheet minimum is described as below the 44 px gate line; scroll width is documented as a recorded observation, not a gate assertion; the node test `internal/server/web/app-shared-nav.test.js` is recorded (2 of 2 pass, run by hand, not wired into the Makefile or CI); the `npm ci` symlink side effect is in the how-to.

Open decision, now resolved by the shared nav fix: the 44 bottom-nav horizontal-overflow findings. The fix replaced the scroller with equal flex sizing, so the six buttons fit 320 to 414 px without scrolling, and the 44 findings are gone (scrollWidth equals clientWidth). The operator's earlier question about whether they were by design no longer applies to the phone widths measured here.

## Worklist

Every other-tab finding (not in the bottom nav), one row per distinct selector and check. Viewports: "all 6" means 320x568, 360x740, 375x667, 390x844, 414x896 and 768x1024.

Classification is by placement, checked against `internal/server/web/style.css` for each selector. Evidence for the one-line claim: `.header-search-btn` and `.toast.toast-error-app .toast-error-close` have their own shared rules. `headerAlertPill`, `headerInstallBtn`, `headerHelpLink` and `headerIdentityBtn` have no rule in style.css, so a fix would add a new shared rule. The one-line claim is therefore not verified for those four rows.

| Selector | Check | Size (w x h) | Viewports | Tabs | Count | Classification |
|---|---|---|---|---|---|---|
| span#headerAlertPill.header-alert-pill | tap-target | 34.8x36, 49.9x21 | all 6 | all 6 | 36 | one-line shared style |
| button#headerInstallBtn | tap-target | 26.3x29 | all 6 | all 6 | 36 | one-line shared style |
| button.toast-error-close | tap-target | 28x28 | all 6 | all 6 | 36 | one-line shared style |
| a#headerHelpLink | tap-target | 26.8x29 | all 6 | autonomous, observer, sessions, settings | 24 | one-line shared style |
| button#headerSearchBtn.header-search-btn | tap-target | 35.5x26 | all 6 | autonomous, sessions | 12 | one-line shared style |
| button#headerIdentityBtn.header-identity-btn | tap-target | 38.5x30 | all 6 | autonomous | 6 | one-line shared style |
| button#headerFullscreenBtn | tap-target | 23.9x28 | 768x1024 | all 6 | 6 | one-line shared style |
| button.settings-tab-btn.output-tab | tap-target | 68.7x27 to 89.3x27 | all 6 | settings | 36 | deferred to its tab |
| button.settings-tab-btn.output-tab | horizontal-overflow | 68.7x27, 86x27, 89.3x27 | 320x568 to 414x896 | settings | 11 | deferred to its tab |
| button.dboard-card-rm | tap-target | 12.8x15 | all 6 | dashboard | 20 | deferred to its tab |
| button.output-tab | tap-target | 73.3x40 to 92.5x27 | all 6 | autonomous | 12 | deferred to its tab |
| button.output-tab.active | tap-target | 84.7x40 to 100.7x29 | all 6 | autonomous | 6 | deferred to its tab |
| a (text "Help", no class) | tap-target | 11.1x16 to 16x16 | all 6 | alerts, dashboard | 12 | deferred to its tab |
| button#automationFilterBtn.automata-action-btn | tap-target | 32x28, 40x40 | all 6 | autonomous | 6 | deferred to its tab |
| button#automationHistoryBtn.automata-action-btn | tap-target | 32x28, 40x40 | all 6 | autonomous | 6 | deferred to its tab |
| button#automationNewBtn.btn-primary | tap-target | 141.1x40 | 320x568 to 414x896 | autonomous | 5 | deferred to its tab |
| button#automationEmptyCreateBtn.btn-primary | tap-target | 153.8x28 | 360x740, 390x844, 768x1024 | autonomous | 3 | deferred to its tab |
| button#dashEditBtn | tap-target | 36.4x28 to 46.4x17 | all 6 | dashboard | 6 | deferred to its tab |
| div.settings-section-title.settings-section-toggle | tap-target | 260x35 to 420x35 | all 6 | observer | 6 | deferred to its tab |
| a.docs-link | tap-target | 38x19 | all 6 | observer | 6 | deferred to its tab |
| button (text "History", no class) | tap-target | 49.1x17 | 768x1024 | observer | 1 | deferred to its tab |

Totals: header rows 7 (156 occurrences, Phase 2). Tab rows 14 (136 occurrences, Phase 3). Bottom-nav rows are summarized below, not listed.

Worklist outcome, post-fix report-only pass (2026-10-25, fixed tree a7cc30e4; `docs/testing/runs/shared-nav-mobile-fit/post-fix/report-only/`):

- Total occurrences went from 336 in 26 groups to 292 in 21 groups. The 44 bottom-nav occurrences are gone. All other-tab occurrences remain (292).
- No other-tab row was added or removed, and every row's count is unchanged. Only sizes moved, in three rows: `button#headerSearchBtn.header-search-btn` 35.5x26 to 40x40, the Observer `div.settings-section-title.settings-section-toggle` from 35 px to 40 px tall, and one `span#headerAlertPill.header-alert-pill` occurrence from 34.8 to 34.2 px wide. Those three are still flagged at 44 px.
- The worklist therefore differs from the pre-fix list only in size, not in membership. The one-line-style claim holds for the header search button and the Observer section toggle, not for the other five header rows: `headerAlertPill`, `headerInstallBtn`, `headerHelpLink`, `headerIdentityBtn` and the toast close button still have no fix. Phase 2 stays open until those rows clear 44 px or the operator accepts 40 px as the bar.
- After the fix, 233 of 281 other-tab tap-target occurrences are under 40 px (smallest 11.1 px). These are the Phase 3 rows and are unchanged.
- The 11 remaining other-tab horizontal-overflow occurrences (`button.settings-tab-btn.output-tab` on settings) are unchanged and deferred to Phase 3.

Observer rows (`div.settings-section-title.settings-section-toggle`, `a.docs-link`, the "History" button; 13 occurrences) are included because the spec asks for every other-tab finding. BL592 scopes the Observer tab to BL572, so the operator should decide whether these stay in this worklist.

### Navigation findings (summary, not listed row by row)

All 44 bottom-nav occurrences are `horizontal-overflow`. None is a tap-target finding, and none is inside a `.nav-icon`. Nav buttons are 59 px tall.

- button.nav-btn[data-view=settings]: 25 (320x568 to 414x896)
- button#navBtnDashboard.nav-btn[data-view=dashboard]: 8 (320x568, 360x740)
- button.nav-btn[data-view=sessions]: 7 (320x568 to 414x896)
- button#navBtnDashboard.nav-btn.active[data-view=dashboard]: 2 (320x568, 360x740)
- button#navBtnAutonomous.nav-btn[data-view=autonomous]: 2 (320x568, 360x740)

The brief cites roughly 250 navigation-icon findings. The probe found none inside a nav icon. That is not proof of absence: the probe does not select plain badge spans (`#alertBadge` in the Alerts nav icon has no `onclick`, and `#peerStaleBadge` is hidden at zero). The "250" figure is unverified until the named checker runs.

### The 35 px tap-target height finding

The only 35 px-tall flagged element in the probe is `div.settings-section-title.settings-section-toggle`, the "System Statistics" section toggle in the Observer tab (260x35 to 420x35 across the six viewports). It is a tab-content section toggle. It is neither a nav button nor a badge inside a nav icon. The named checker's own 35 px finding may name a different element; that cannot be checked without it.

## Parity surface

| Surface | Decision | Reason |
|---|---|---|
| PWA | Include | The findings are in the PWA shared navigation and header (`internal/server/web/app.js`, `style.css`, `index.html`). This is the fix target. |
| REST | Exclude | Layout-only change to PWA markup and CSS. No endpoint or response shape changes. |
| MCP | Exclude | No MCP tool surface change. |
| CLI | Exclude | No command or flag change. |
| Comm channel | Exclude | The navigation sends no commands, so no channel routing change. |
| YAML/config | Exclude | No new config keys. The sandbox config comes from `testdata/datawatch.yaml` through the e2e sed, and the probe reads no config. |
| Android | Exclude | The repo has no Android source (no android directory, no Kotlin files). Android browser viewports are covered by the PWA row. |
| iPhone | Exclude | The repo has no iOS source (no ios directory, no Swift files). iPhone Safari viewports are covered by the PWA row. |
