# Shared navigation mobile fit

Status (2026-10-25): blocked before the report-only run. `scripts/test-stories/pwa/shared-nav-mobile-fit.mjs` does not exist on this branch or on any ref. The Observer mobile-fit checker it depends on exists on `main` (9.14.2) only. The run record is in `docs/testing/runs/shared-nav-mobile-fit/report-only.txt`. Daemon version recorded from `cmd/datawatch/main.go:119`: 9.11.0.

## Phases

- Phase 1, baseline report-only run on the unmodified tree: Blocked (checker absent from this branch). Release: none, this phase produces evidence only.
- Phase 2, fix the one-line shared style findings in `app.js` and `style.css`: Planned. Release: 9.14.3 (next patch above main 9.14.2).
- Phase 3, per-tab findings: Planned. Release: each tab's own release, not this plan.

## Worklist

No rows yet. The report does not exist, so no other-tab finding is known.

| Selector | Check | Viewports | Count | Classification |
|---|---|---|---|---|
| (none, no report) | | | | |

- Navigation icon findings (about 250): not counted. They cannot be grouped by selector until a report exists.
- 35 px tap-target height finding: the producing element cannot be named from the report, so it cannot yet be classified as a nav button or as a badge inside a nav icon.

To fill this section, bring the checker onto this branch (see `report-only.txt`), run the report-only mode, and rebuild the table from `report-only.json`.

## Parity surface

| Surface | Decision | Reason |
|---|---|---|
| PWA | Include | The findings are in the PWA shared navigation (`app.js`, `style.css`), which is the fix target. |
| REST | Exclude | Layout-only change to PWA markup and CSS. No endpoint or response shape changes. |
| MCP | Exclude | No tool surface change. |
| CLI | Exclude | No command or flag change. |
| Comm channel | Exclude | The navigation sends no commands, so no channel routing change. |
| YAML/config | Exclude | No new config keys. The checker reads `testdata/datawatch.yaml` only as a template. |
| Android | Exclude (provisional) | This repo has no Android source. Android browser viewports are covered by the PWA row. Confirm against the report's viewport list. |
| iPhone | Exclude (provisional) | This repo has no iOS source. iPhone Safari viewports are covered by the PWA row. Confirm against the report's viewport list. |
