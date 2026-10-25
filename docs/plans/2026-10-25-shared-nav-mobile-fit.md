# Shared navigation mobile fit

Status (2026-10-25): The report-only run is complete for the substitute probe, and the
named checker remains unrun. `scripts/test-stories/pwa/shared-nav-mobile-fit.mjs` does not
exist on branch `automaton/b54f0001` or on any git ref, so the measurement was taken with
`scripts/test-stories/pwa/shared-nav-mobile-fit-probe.mjs`. That probe ran through
`scripts/e2e-pwa.sh` in local mode against an isolated sandbox daemon on the unmodified
tree: exit 0, 336 occurrences in 26 groups. Its findings are not the checker's findings.
Daemon version, from the Version line in `cmd/datawatch/main.go:119`: 9.11.0. Run record:
`docs/testing/runs/shared-nav-mobile-fit/report-only.txt`.

Spec issue: the spec names a checker that is not on any ref. Its findings (the "about 250
nav icon" count and the "35 px" nav finding) cannot be verified until the file is
supplied or the operator confirms the probe is the intended measure.

## Phases

- Phase 1, baseline report-only run on the unmodified tree: In Progress. The substitute probe is done through e2e-pwa.sh (336 occurrences, 26 groups, daemon 9.11.0). The named checker run is still outstanding because the file is absent. Release: none, this phase produces evidence only.
- Phase 2, fix the one-line shared style findings (header chrome and the toast close button, 156 occurrences in 7 rows): Planned. Release: 9.14.3, the next patch above main 9.14.2.
- Phase 3, fix the per-tab findings (136 occurrences in 14 rows, deferred to each tab): Planned. Release: 9.14.4, the next patch above main after Phase 2 (9.14.3). The Observer rows here overlap BL572; see Worklist.

Open decision, not assigned to a phase: the 44 bottom-nav horizontal-overflow findings. `.nav` is a deliberate horizontal scroller (`overflow-x: auto`, scroll-snap) on narrow widths, so they may be by design. The probe flags them against viewport width. The operator decides whether they are defects before any nav change.

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
