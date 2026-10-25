# Shared navigation mobile fit

Status (2026-10-25): The baseline is partly done. The named checker `scripts/test-stories/pwa/shared-nav-mobile-fit.mjs` is absent from branch `automaton/b54f0001`, and the Observer checker it depends on exists only on `main` (9.14.2) and another branch. Reading those is outside this run's scope, so the checker was not run. Instead, a substitute probe, `scripts/test-stories/pwa/shared-nav-mobile-fit-probe.mjs`, ran report-only against an isolated sandbox daemon on the unmodified tree. Its results are in `docs/testing/runs/shared-nav-mobile-fit/report-only.txt` and `report-only.json`. The substitute's findings are not the checker's findings. Daemon version, read from `cmd/datawatch/main.go:119`: 9.11.0.

## Phases

- Phase 1, baseline report-only run on the unmodified tree: In Progress. Substitute probe done (334 occurrences, 26 groups). Named checker run still needed. Release: none, this phase produces evidence only.
- Phase 2, fix the one-line shared style findings (header chrome and the toast close button, 156 occurrences in 7 rows): Planned. Release: 9.14.3, the next patch above main 9.14.2 (version from the earlier attempt's record of main; not re-verified in this run).
- Phase 3, per-tab findings (134 occurrences in 14 rows, deferred to each tab): Planned. Release: each tab's own patch release, not this plan.

## Worklist

Every finding outside the bottom navigation bar, one row per distinct selector and check. Classification by placement: header chrome and the toast close button (present on all six tabs) are `one-line shared style`; everything else sits in a single tab's content and is `deferred to its tab`. This is not checked against `style.css`, so no row is a verified one-line fix. Viewports: "all 6" means 320x568, 360x740, 375x667, 390x844, 414x896, 768x1024.

| Selector | Check | Viewports | Tabs | Count | Classification |
|---|---|---|---|---|---|
| span#headerAlertPill.header-alert-pill | tap-target | all 6 | all 6 | 36 | one-line shared style |
| button#headerInstallBtn | tap-target | all 6 | all 6 | 36 | one-line shared style |
| button.toast-error-close | tap-target | all 6 | all 6 | 36 | one-line shared style |
| a#headerHelpLink | tap-target | all 6 | autonomous, observer, sessions, settings | 24 | one-line shared style |
| button#headerSearchBtn.header-search-btn | tap-target | all 6 | autonomous, sessions | 12 | one-line shared style |
| button#headerIdentityBtn.header-identity-btn | tap-target | all 6 | autonomous | 6 | one-line shared style |
| button#headerFullscreenBtn | tap-target | 768x1024 | all 6 | 6 | one-line shared style |
| button.settings-tab-btn.output-tab | tap-target | all 6 | settings | 36 | deferred to its tab |
| button.settings-tab-btn.output-tab | horizontal-overflow | 320x568 to 414x896 | settings | 11 | deferred to its tab |
| button.dboard-card-rm | tap-target | all 6 | dashboard | 20 | deferred to its tab |
| button.output-tab | tap-target | all 6 | autonomous | 12 | deferred to its tab |
| button.output-tab.active | tap-target | all 6 | autonomous | 6 | deferred to its tab |
| a (text "Help", no class) | tap-target | all 6 | alerts, dashboard | 12 | deferred to its tab |
| button#automationFilterBtn.automata-action-btn | tap-target | all 6 | autonomous | 6 | deferred to its tab |
| button#automationHistoryBtn.automata-action-btn | tap-target | all 6 | autonomous | 6 | deferred to its tab |
| button#automationNewBtn.btn-primary | tap-target | 320x568 to 414x896 | autonomous | 5 | deferred to its tab |
| button#automationEmptyCreateBtn.btn-primary | tap-target | 768x1024 | autonomous | 1 | deferred to its tab |
| div.settings-section-title.settings-section-toggle | tap-target | all 6 | observer | 6 | deferred to its tab |
| a.docs-link | tap-target | all 6 | observer | 6 | deferred to its tab |
| button (text "History", no class) | tap-target | 768x1024 | observer | 1 | deferred to its tab |
| button#dashEditBtn | tap-target | all 6 | dashboard | 6 | deferred to its tab |

Navigation findings, summarized by count and selector (not listed row by row). All 44 are `horizontal-overflow`. None is a tap-target finding, and none sits inside a nav icon.

- button.nav-btn[data-view=settings]: 25 (320x568 to 414x896)
- button#navBtnDashboard.nav-btn[data-view=dashboard]: 8 (320x568, 360x740)
- button.nav-btn[data-view=sessions]: 7 (320x568 to 414x896)
- button#navBtnDashboard.nav-btn.active[data-view=dashboard]: 2 (320x568, 360x740)
- button#navBtnAutonomous.nav-btn[data-view=autonomous]: 2 (320x568, 360x740)

The brief cites roughly 250 navigation-icon findings. The substitute found none inside nav icons, so that figure is unverified until the named checker runs.

**35 px tap-target height finding.** In the substitute run, the only 35 px-tall flagged element is `div.settings-section-title.settings-section-toggle`, the "System Statistics" section toggle in the Observer tab (260x35 to 420x35 across the six viewports). It is a tab-content section toggle. It is not a nav button and not a badge inside a nav icon. The checker's own 35 px finding was not reproduced by name, so this identification is a candidate only.

## Parity surface

| Surface | Decision | Reason |
|---|---|---|
| PWA | Include | The findings are in the PWA shared navigation and header (`app.js`, `style.css`), which is the fix target. |
| REST | Exclude | Layout-only change to PWA markup and CSS. No endpoint or response shape changes. |
| MCP | Exclude | No tool surface change. |
| CLI | Exclude | No command or flag change. |
| Comm channel | Exclude | The navigation sends no commands, so no channel routing change. |
| YAML/config | Exclude | No new config keys. The sandbox config was generated from `testdata/datawatch.yaml` with the substitutions `e2e-pwa.sh` uses; the probe reads no config. |
| Android | Exclude (provisional) | The worktree has no Android source. Android browser viewports are covered by the PWA row. |
| iPhone | Exclude (provisional) | The worktree has no iOS source. iPhone Safari viewports are covered by the PWA row. |
