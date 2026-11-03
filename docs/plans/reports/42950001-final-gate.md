# Task 42950001 — final gate & rules-review fix pass

Date: 2026-11-03
Branch: `automaton/auto-fix-rules-review-findings-attempt-1`
Toolchain: Go 1.27.x, Node v24.15.0, linux/amd64 (this worktree)
Scope: task `42950001` — the capacity card usage "resets at" line (zone-preserving), the burn-guard rung on the back-off line, and the PWA card renderer it ships with.

## Results (all re-run 2026-11-03)

| Check | Result | Exit | Detail |
|---|---|---|---|
| go build ./... | PASS | 0 | clean |
| gofmt (touched files) | PASS | 0 | no diffs |
| go vet ./cmd/datawatch/ ./internal/autonomous/ | PASS | 0 | clean |
| go test ./... -count=1 | PASS | 0 | **100 packages in the module; 88 with tests, all pass; 12 have no test files; 16,131 top-level test functions, 0 failures** |
| node --check internal/server/web/app.js | PASS | 0 | syntax OK (46,104 lines) |
| node --test internal/server/web/app-capacity-backoff.test.js | PASS | 0 | 5 tests (functional, not source-string) |
| bash scripts/e2e-pwa.sh capacity-backoff-card | PASS | 0 | `3 probes OK → PASS`; real Chromium at 390px; evidence `scripts/test-stories/evidence/capacity-backoff-card/` (state.json + png) |
| datawatch-app comment (B4) | done | — | commented on the existing burn-guard parity issue (not a duplicate): datawatch-app#332 |

> The earlier version of this file reported "5 Go packages / 7536 tests". Those numbers were wrong. The module has **100 Go packages (88 with tests) and 16,131 tests**, all passing.

## Defect fixed (daemon-timezone contract)

The rate-limit detector path and the in-flight rebuild path in `cmd/datawatch/usage_backoff_wiring.go` stored the back-off reset in the zone the **session** reported it (UTC), so the record — the single source every surface reads — carried a different offset than the burn-guard path and the PWA's "resets at". Both now convert with `.In(time.Local)`, matching the burn-guard path, so every reader (PWA, CLI, comm, MCP) shows the daemon's own time. `budgetTime` in the PWA (app.js) prints the zone the deadline arrived in and never re-bases it.

Regression: `cmd/datawatch/usage_backoff_wiring_test.go::TestRateLimitBackoffHandler_StoresResetInDaemonZone` — **fails before the fix** (stored deadline `…+00:00 (UTC)`), **passes after** (stored deadline in the daemon zone).

## Rules-review findings addressed

| # | Finding | Resolution | Evidence |
|---|---|---|---|
| A1 | CHANGELOG `[Unreleased]` entry missing | Added the "resets at" zone fix, the burn-guard rung line/chip, and the new locale key | `CHANGELOG.md` `## [Unreleased]` |
| A2 | testing-tracker missing the `GET /api/capacity` rung contract | New section "Usage back-off line on the capacity card" (REST/shared-text, daemon-zone, PWA Node, browser story) | `docs/testing-tracker.md` |
| A3 | stale test results in final-gate + wrong package count | Date set to 2026-11-03; counts corrected to 100/88/16,131; evidence table above | this file; `docs/plans/README.md`; `docs/testing.md` |
| A3c | `capacity_card_backoff_rung` — confirm intentional + 4 other bundles | confirmed present in **en/de/es/fr/ja**; marked intentional in the test and story file comments | `internal/server/web/locales/*.json`; `app-capacity-backoff.test.js`; `pwa/capacity-backoff-card.mjs` |
| B (rules-review) | Playwright story added but not running | story written to the modern runner and **run green** (`bash scripts/e2e-pwa.sh capacity-backoff-card` → PASS) | evidence above |
| Test 5 | JS test asserts source, not behavior | **Functional** test added: `app-capacity-backoff.test.js` executes `budgetTime`/`capacityBackoffHtml` (zone, fallback, rung chip, "reset time unknown") | 5 pass |
| B3 | 5 locales for the new key | all five bundles carry the key + translations | `locales/*.json` |
| B4 | datawatch-app issue | commented on **datawatch-app#332** (existing burn-guard parity issue — not a duplicate) | https://github.com/dmz006/datawatch-app/issues/332#issuecomment-6606997099 |

## Commit-message contract (for the harness commit of this fix)

**Subject format** (a `type(scope):` subject — not a `task(42950001): …` subject):

```
fix(capacity): usage "resets at" keeps the deadline's own zone; surface the burn-guard rung
```

**Token checklist — every token below must appear in the final commit message** (with the shown value):

```
rules: AGENT.md
tests: cmd/datawatch/usage_backoff_wiring_test.go::TestRateLimitBackoffHandler_StoresResetInDaemonZone; internal/server/web/app-capacity-backoff.test.js (5)
e2e: scripts/test-stories/pwa/capacity-backoff-card.mjs (green, 2026-11-03)
changelog: CHANGELOG.md [Unreleased]
readme: docs/plans/README.md
backlog: docs/testing-tracker.md (new "Usage back-off line on the capacity card" section)
locale: capacity_card_backoff_rung (en/de/es/fr/ja)
id-check: 42950001
leak-check: no real credentials in fixtures, logs, or reports
node-check: 100 packages, 88 with tests, 0 failures
make-build: ok
ci: n/a (worktree, no PR)
lint: gofmt clean; go vet clean on touched packages
locale-guard: capacity_card_backoff_rung present in all five bundles
api-test: GET /api/capacity rung (TestCapacityBackoff_RungOnBurnGuardHold); daemon-zone (TestRateLimitBackoffHandler_StoresResetInDaemonZone)
comm-test: n/a (no comm-surface change in this fix)
cli-test: n/a (no CLI-surface change in this fix)
mobile-parity: datawatch-app#332
issue: https://github.com/dmz006/datawatch-app/issues/332
```
