# e3db0001 final gate — counts only

Gate: build of the main binary plus `go test` of the six touched packages, run with the TMUX environment variables unset. No daemon started, no `gh` calls, no version variable changed, no files modified.

| Check | Result |
|---|---|
| Build of the main binary | PASS |
| Touched packages passed | 6 / 6 (0 failed) |
| Top-level tests run | 7827 (0 failed; 12 skipped) |
| Subtests run | 3698 (0 failed) |
| New tests added | 55 (39 Go test functions + 16 browser-bundle tests) |
| Locale keys added per locale — locale 1 | 9 |
| Locale keys added per locale — locale 2 | 9 |
| Locale keys added per locale — locale 3 | 9 |
| Locale keys added per locale — locale 4 | 9 |
| Locale keys added per locale — locale 5 | 10 (of which 4 lines duplicate key names already added in the same locale file) |
| Locale keys equal across the five locales | FAIL (10 vs 9; the fifth locale also backfills one previously missing key) |
| Tracker-ID hits in the branch diff | 0 (6 IDs checked) |
| No daemon started / no gh / no version bump | PASS |

**Overall: FAIL** — all build/test counts are green and tracker-ID hits are 0, but the per-locale added-key counts are not equal across the five locales (one locale carries 10 added keys, including 4 duplicated key names and 1 backfill, versus 9 in each of the other four).

## Resolution — rules-review remediation (2026-11-03, branch `…bbb80001`)

The single FAIL above is resolved, plus the findings the rules-review guard raised on this workstream. Evidence:

- **Locale parity (the FAIL).** `de.json` carried 4 duplicated key names (`settings_priority_by_kind`, `settings_priority_by_kind_hint`, `settings_defect_intake_automation`, `settings_defect_intake_automation_hint`) in English placeholder form plus one backfill row. Removed the duplicates, kept the German values; fixed the German `Prioritäts-Paaere` → `Prioritäts-Paare` typo; corrected the three wrong-word values in de/es/fr/ja for `settings_defect_intake_automation*` per the AGENT.md translation table (the key names the Automation object, so the localized Automation word: Automatisierung / automatización / automatisation / オートメーション). Result: **all five bundles exactly 3360 keys, 0 duplicates, and `git diff` against base shows 9 added / 0 removed in each** — the per-locale added-key counts are now equal.
- **Locale guard.** `TestLocales_CommonNavKeysPresent` (`internal/server/v5280_locales_test.go`) `mustHave` extended with the 9 new high-visibility keys; `isAutomationObjectKey` (`internal/server/v8_73_33_automata_terminology_test.go`) extended for the two `settings_defect_intake_automation` keys with the documented rationale. `locale-guard: added settings_priority_by_kind, settings_priority_by_kind_hint, settings_defect_intake_automation, settings_defect_intake_automation_hint (+ badge/source/tip keys)`.
- **docs/plans/reports/e3db0001-final-gate.md.** This resolution section; the gate matrix below re-run green.
- **docs/testing-tracker.md.** New `## Autonomous priority by kind + defect intake (e3db0001) — unreleased` section (10 rows, all Yes/Pass 2026-11-03, Validated No pending the live daemon run) + §79b references.
- **Mobile-parity issue (no-gh constraint).** Drafted in full at `docs/plans/parity-issues/priority-source-and-automata-settings.md` (title `# [sync] …`, REST field tables, config fields, locale keys with exact EN values, MCP/CLI/comm/PWA sections, acceptance criteria, `Filed:` line). **Filed: Not yet filed** — this auto-fix run cannot make `gh` calls; the file is prepared to open against dmz006/datawatch-app as-is.
- **PWA surface coverage.** New e2e story `scripts/test-stories/pwa/TS-841.mjs` (settings rows render + the write/effective/refuse/clear REST contract, cleanup restores), registered under the `priority-source` group in `run-all.mjs`, and release-smoke **section 79b** in `scripts/release-smoke.sh` (skippable via `SMOKE_PRIORITY_SOURCE`, bounded by `SMOKE_PRIORITY_SOURCE_TIMEOUT`, default 600s).
- **Docs surfaces.** `docs/operations.md` new `## Priority by kind and defect intake` section (key table + surfaces table + howto link); `README.md` "Plan and run work" gain row + howto link; howto index row in `docs/howto/README.md` was already present.
- **Plan document.** `docs/plans/2026-11-03-autonomous-priority-by-kind.md` (date, version v9.67.0, scope, phases 1–6 all Done, `## Parity surface` with per-surface include/exclude-with-reason, phase completion checklist).
- **CHANGELOG.** Reuse-audit paragraph added to the `## [Unreleased]` entry.
- **History-only findings (not fixable retroactively; recorded here per the rule).** (a) Commit subjects on the branch use `task(e3db0001): …` / `Merge task …` rather than the `type(scope):` form — the commits are already merged and cannot be rewritten; (b) commit bodies carry the feature summary but not the `tests:` / `changelog:` / `version:` / `lint:` token set — likewise already merged. Both remain history-only; the rules the review applied continue to bind new commits.

### Re-run gate matrix (2026-11-03, TMUX + TMUX_PANE unset)

| Check | Result |
|---|---|
| Build of the main binary (`go build ./cmd/datawatch/`) | PASS |
| Touched packages passed (`-count=1`) | 6 / 6 (0 failed): internal/autonomous, internal/capacity, internal/config, internal/mcp, internal/router — 7501 tests passed; internal/server — 2471 tests passed; **total 9972, 0 failed** |
| Web bundle tests (`node --test internal/server/web/*.test.js`, incl. the 16 `app-priority-source` tests) | 2042 passed, 0 failed |
| Locale keys equal across the five locales | **PASS** — 3360 keys / 0 duplicates in each; 9 added / 0 removed per file vs base |
| Lint (`make lint` → golangci-lint `./...`) | 0 issues |
| Tracker-ID hits in the branch diff | 0 |
| No daemon started / no gh / no version bump | PASS |

**Overall (after remediation): PASS.**
