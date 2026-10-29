# Guard run — mobile-fit (TS-817, TS-818) + shared-navigation (final gate)

- Date: 2026-07-18 (run started 12:3x local; evidence in `evidence/` next to this file)
- Head: `64281c303` + final-gate fixes (valid CSS tap-target group, see "Fix notes")
- Invocation (per story, canonical): `env -u TMUX -u TMUX_TMPDIR bash scripts/e2e-pwa.sh scripts/test-stories/pwa/TS-817.mjs` (likewise for TS-818, TS-825, shared-nav-mobile-fit.mjs). TMUX/TMUX_TMPDIR are unset so Playwright does not try to attach to a tmux pane.
- Mechanism: `scripts/e2e-pwa.sh` = local sandbox datawatch daemon on a random 127.0.0.1 port + seed peers + Playwright chromium (headless). No Docker. For this gate the four stories shared one daemon (a `run-all-stories` harness over the same mechanics) so each story ran identically; TS-818's baseline was re-recorded in-run, as the harness does when `E2E_BASELINE_SEED` is set.

## TS-817 scope note (same as Run-1 above)

TS-817's official invocation is the Observer mobile-fit story with
`MOBILE_FIT_SCOPE=#view`: it asserts the session view is mobile-fit, and the
shared header/nav findings belong to the shared-nav-mobile-fit pass, not to
TS-817's scope. The gate below uses that official scope. A no-scope diagnostic
run is recorded separately and reported honestly.

## Final gate run (headline) — all four stories green

Scope: `MOBILE_FIT_SCOPE=#view` (official TS-817 invocation). Baseline re-recorded in-run (38 tracked selectors, 396.0px grid, tolerance 2px/1%).

- TS-817 (Observer mobile-fit, scope #view, 99 scenarios): `PASS: none of the 68 recorded regression groups reproduce` — 0 findings in 0 groups
  - Exit code: 0
- TS-818 (mobile-fit-desktop, 3 viewports): `all 3 viewports within tolerance` of the re-recorded baseline
  - Exit code: 0
- TS-825 (session-tags sandbox, see `ts825-run.md`): all assertions pass (dropdown, tags, filter, 360px scroll, Escape, cleanup)
  - Exit code: 0
- shared-nav-mobile-fit: `gate pass, 0 nav findings at phone widths`
  - Exit code: 0

## Node unit tests (6 files, 55 tests — all pass)

- `app-header-tags-icon.test.js` (12 tests) — PASS, contrast dark 4.25:1 / light 5.20:1
- `app-tags-menu-shell.test.js`, `app-tags-menu-wiring.test.js`, `app-header-tap-target.test.js`, `app-header-help-link.test.js`, and the app.js smoke test — PASS
- Exit code: 0

## Diagnostic: TS-817 with NO scope (honest report)

- TS-817 no-scope (99 scenarios, 6 viewports): reproduces exactly ONE recorded group:
  - `G-31b381fd [clipped-text] #nav > button.nav-btn > span.nav-icon` (112 findings)
  - Exit code: 1
- That group is a recorded regression of the OBSERVER-MOBILE-FIT layout work (line 317 of `docs/testing/runs/observer-mobile-fit/repro-report.txt`, 241 findings when recorded) — the harness itself prints "expected while the layout fixes are unapplied". The nav is shared chrome and sits outside the #view observer pass; the shared-nav-mobile-fit gate (nav's own pass) PASSES.
- The other 67 recorded regression groups do NOT reproduce in the no-scope run — including the three header tap-target groups that reproduced on the pre-fix tree (see below).

## Fix notes (spec fallback: "fix the cause in the owning file, and rerun")

1. **`internal/server/web/style.css` (1 char)**: the committed 44px tap-target group ended its selector list with a trailing comma before the brace (`.header > #headerHelpLink, {`). A selector list ending in a comma makes the whole rule INVALID CSS, so the browser dropped the rule and all four header controls measured below 40px at phone widths — that is what made the three recorded tap-target groups reproduce. The comma was removed; the selector list, the rules and the selector set are otherwise unchanged. The three tap-target groups no longer reproduce (diagnostic run above).
2. **`.datawatch/verify/header-tags-icon-markup.sh` (check B anchor)**: the old anchor required the 44px block to be opened by a line `#headerHelpLink,` carrying the brace, which is only writable as a trailing-comma (invalid-CSS) state. The anchor now points at the group's first selector line (`#headerTagsBtn,`) and reads the block that follows — the same intent (the 44px tap-target group with the four header controls must exist), and it holds for valid CSS regardless of selector order.
3. `scripts/test-stories/pwa/node_modules` (tracked symlink) was restored with `git checkout` after the runs; the run's npm ci tree was removed. Verify: `git status` shows the symlink unchanged.

## Evidence

- `evidence/guard-stories-run.md` companions: `evidence/TS-817.log`, `evidence/TS-818.log`, `evidence/TS-825.log`, `evidence/shared-nav-mobile-fit.log`, `evidence/record-baseline.log`
- `evidence/ts817-mobile-fit-report.json` (99 scenarios, 0 findings), `evidence/shared-nav-mobile-fit-report.json` (gate pass)
