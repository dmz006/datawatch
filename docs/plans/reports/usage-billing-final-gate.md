# Usage-billing — final gate report (counts only)

**Date:** 2026-11-03
**Branch:** `automaton/b7a50001--tasks-final-gate-counts-only-gate-report-6baf000a` at `f3d3cdbcc` (worktree clean at gate start).
**Scope:** read-only gate — build the binary, test the affected packages, run the new PWA JS test file, `make lint`. No source file was modified by this gate; `var Version` was only read (consistent: `9.67.0` in both `cmd/datawatch/main.go` and `internal/server/api.go`, unchanged).

## Verdict

**GREEN.** Build passes, all 9 tested packages pass (0 failures), the new PWA JS test file passes (14/14), `make lint` passes (0 issues), locale key parity passes, and `docs/implementation.md` covers all 7 new config keys.

## Gate results

| Check | Result | Detail |
|---|---|---|
| `go build ./cmd/datawatch/` | **PASS** | rc 0, 11 s |
| `go test ./internal/config ./internal/usagecalc ./internal/costreport ./internal/server ./internal/mcp ./internal/router ./cmd/datawatch` (recursively matched 9 packages) | **PASS** | 9/9 packages ok, 0 FAIL |
| PWA JS test file `internal/server/web/app-usage-billing.test.js` (`node --test`) | **PASS** | 14 tests, 14 pass, 0 fail, 177 ms |
| `make lint` (audit checks + `golangci-lint run ./...`) | **PASS** | rc 0, 63 s, 0 issues |

## Counts

### Packages

- **Passed: 9** — `internal/config`, `internal/usagecalc`, `internal/costreport`, `internal/server`, `internal/server/multiserver`, `internal/mcp`, `internal/mcp/search`, `internal/router`, `cmd/datawatch`
- **Failed: 0**

### Go tests

- **Passed: 4842** top-level tests (subtests of the same names; 3 skipped, all live-gated: `TestOverlapSmokeLive`, `TestSeededLocalServerServe`, `TestPiPromptLiveRunWithNodeRunner`)
- **Failed: 0**

### PWA JS (new file)

- **Passed: 14, Failed: 0** (cancelled 0, skipped 0, todo 0)

### Lint

- **Fail: none.** `golangci-lint run ./...`: 0 issues; all `make lint` audit checks OK (unscoped-tmux, daemon-cleanup, service-scope, go-pins, internal-refs, identity-wipe, security-ignore-writes, local-leaks: address 0 / home 0 / email 0 / local 0).

### Locale key parity

- **PASS.** All five locale bundles (`en`, `de`, `es`, `fr`, `ja`) carry the new usage-billing and cost-report keys:
  - Go side: full-bundle parity tests in `internal/server` (`TestLocales_AllPresent`, `TestLocales_ParityWithEnglish` and the per-feature bundle-key tests) — passed within the package's 4842 passing tests, 0 failures.
  - JS side: `app-usage-billing.test.js` case "every locale bundle carries the new usage-billing and cost-report keys" — passed.

### New config keys covered in `docs/implementation.md`

- **7 of 7** `usage.billing.<provider>.*` keys documented: `mode`, `plan`, `monthly_price`, `currency`, `reset_weekday`, `reset_time`, `allowance_unit` (plus the profile methods and validation rules around them).

## Note — the scope line, read plainly

"**Counts only**" means the gate counted what already passed (the package/test counts above, the lint pass, the locale-key parity, the seven `usage.billing.<provider>.*` keys in `docs/implementation.md`). It does not mean the feature was finished, the branch was clean, or the record is reconciled. The rules review found four things this report does not cover, all now fixed on the branch:

1. **Operator-facing wording.** The plan, the parity draft, the testing-tracker entry and the CHANGELOG entry referred to the operator session as "the individual" in places and named a task ID in an operator-facing file. They now say "Automaton session" (and "the operator" where the person is meant) and carry no task ID.
2. **The dollar-figure rule.** Nothing in the shipped feature or its tests invents a price: the report's figures stay the rate card's estimates and the rate card's zero, and the `bundled_plan` label says "API-equivalent, not billed". The plan, the parity draft and the testing-tracker now carry it verbatim.
3. **The tracked `.gate-run/` log.** Four gate-run log files had been committed at the repo root and were not in `.gitignore`. They are now ignored (`/scripts/check-no-local-leaks.sh` reports them); the tracked copies are deleted from the working tree on this branch, so the removal is part of the branch. (The gate-run directory is a local working dir — nothing in the tree reads it.)
4. **The record.** The CHANGELOG entry (v9.67.0) names the billing-profile fields and every surface that sets them; `docs/testing-tracker.md` has a "Billing profile (`usage.billing`)" section (config, report engine, REST, CLI and PWA rows, all unit-validated, live left unmarked); `docs/plans/2026-11-02-cost-report.md` has a "Billing profile" section with the config surface and the MCP refusal; and `docs/plans/README.md` lists this release with the plan, the parity draft and this report.
