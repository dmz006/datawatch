# Cost report — final gate report (read-only)

**Date:** 2026-11-02
**Branch:** `automaton/090f0001--tasks-run-the-final-gate-for-the-cost-report-af2f000c` at
`87eb36f63` (Merge task af2f000b); worktree clean at gate start.
**Scope:** run the full gate on the feature branch and report every result; no file was changed.

## Verdict

**The gate is NOT green.** `go test ./...` fails, `golangci-lint run ./...` fails, and
`scripts/check-no-local-leaks.sh` fails — the failures trace to the live-smoke task
(af2f000b) having committed its `tmp/live-smoke/` directory (32 files) into the branch, plus
four unchecked `fmt.Fprint*` calls left in the CLI table printer. The Phase Completion
Checklist also has open items (parity issue not filed, `docs/README.md` index missing the
how-to line, stale status lines). The feature itself works: build, Node suite, the internal
ref check, version match and the live smoke evidence all pass.

## Gate results

| Check | Result | Detail |
|---|---|---|
| `go build ./...` | **PASS** | rc 0, 3 s |
| `go test ./...` (full suite) | **FAIL** | single failing package, see below; every other package `ok` |
| `node --check internal/server/web/app.js` | **PASS** | node v24.15.0 |
| `node --test internal/server/web/*.test.js` | **PASS** | tests 2026, pass 2026, fail 0, skipped/todo 0, 2.8 s |
| `golangci-lint run ./...` | **FAIL** | golangci-lint present at `/home/<user>/go/bin/golangci-lint`; 4 issues (`errcheck: 4`), rc 1 |
| `bash scripts/check-no-internal-refs.sh` | **PASS** | "✓ PASS: no internal refs in user-facing surfaces." |
| `bash scripts/check-no-local-leaks.sh` | **FAIL** | 6 "home" findings, all inside the tracked `tmp/live-smoke/` directory; rc 1 |
| Version match (main.go vs api.go) | **PASS** | `cmd/datawatch/main.go:123` and `internal/server/api.go:205` both `var Version = "9.66.0"` |

### `go test ./...` — failing package

- **`github.com/dmz006/datawatch/tests/ci` — FAIL**, 1 failing test:
  - `TestOnlyKnownFilesAreTrackedAtTheRepositoryRoot`
  - Output line: `repo_root_files_test.go:59: files or folders tracked at the repository root that are not on the allowed list (move them to scripts/ or docs/, or add them here on purpose): [tmp/]`
- Cause: commit `8f376e4ff` (task af2f000b, "Run the live smoke test across REST, MCP and CLI") left its smoke harness tracked at the repository root: `git ls-files tmp` shows **32 files** under `tmp/live-smoke/` (data dir, `driver.sh`, `driver.out`).
- All other packages pass (e.g. `internal/server` 70.7 s, `internal/session` 187.1 s, `internal/router` 146.5 s, `cmd/datawatch`, all `internal/*` `ok`; full run 248 s).

### `golangci-lint run ./...` — issues

All 4 are `errcheck` in `cmd/datawatch/cli_cost_report.go`:

- `106:15: Error return value of fmt.Fprintln is not checked` — `fmt.Fprintln(w, "key\t sessions\t tokens_in\t tokens_out\t usd")`
- `108:15: Error return value of fmt.Fprintln is not checked` — `fmt.Fprintln(w, "key\t sessions\t tokens_in\t tokens_out")`
- `116:15: Error return value of fmt.Fprintf is not checked` — `fmt.Fprintf(w, "%s\t %d\t %d\t %d\t %s\n", r.Key, …)`
- `118:15: Error return value of fmt.Fprintf is not checked` — `fmt.Fprintf(w, "%s\t %d\t %d\t %d\n", r.Key, …)`

### `scripts/check-no-local-leaks.sh` — findings

6 "home" findings (everything else: address 0, email 0, local 0), **all in tracked `tmp/live-smoke/` files** produced by the smoke task:

- `tmp/live-smoke/data/last-shutdown.json:7`
- `tmp/live-smoke/data/last-shutdown.json.consumed:7`
- `tmp/live-smoke/driver.out:1`, `:31`, `:58`, `:59`

### Smoke evidence (feature was exercised on a running daemon)

From the tracked `tmp/live-smoke/driver.out` (task af2f000b):

- `GET /api/cost/report` HTTP 200 for `group_by=automaton|automation|backend|week`, with `retention_days=31` and `retention_days PRESENT=yes` on the bare call.
- `group_by=quarterly` (invalid) → HTTP 400 (expected 400).
- `PUT /api/config cost_report.default_group_by=backend` → HTTP 200, and `GET /api/config` reads back `cost_report.default_group_by=backend` while the other four keys hold the defaults (`default_window_days 7`, `include_peer_spend false`, `show_usd_estimates true`, `week_start monday`).
- MCP `tools/call cost_report` → driver rc 0.
- CLI `datawatch cost report --format json` → rc 0 (the `--json` misspelling correctly returned rc 1).

## Phase Completion Checklist (AGENT.md) vs `docs/plans/2026-11-02-cost-report.md`

Items that are **not checked and not explicitly marked N/A with a reason**:

1. **`go test ./...` … both green** — not green: `tests/ci TestOnlyKnownFilesAreTrackedAtTheRepositoryRoot` fails because of the tracked `tmp/` from the smoke task. Every other gate item in this report was run from the same tree.
2. **Mobile-Parity Rule** — `docs/plans/parity-issues/cost-report.md` still opens with "Not filed yet" and carries no datawatch-app issue URL or number, even though commit `dc2fb9f03` (task af2f000a) is titled "… and file the datawatch-app parity issue". The rule requires the issue to be filed *before the merge*; as of this gate it is open.
3. **General documentation checklist — doc index** — `docs/howto/cost-report.md` exists and is linked from `docs/operations.md:672` and `docs/setup.md:584`, but **`docs/README.md` has no `howto/cost-report.md` index row** (grep for "cost" in `docs/README.md` returns nothing; its how-to table stops before the cost-report row, although it does carry the other recent how-tos). The local `docs/howto/README.md` table also has no row for it, though that table is not exhaustive (e.g. `burn-guard` is absent too). `CHANGELOG.md` ([Unreleased] → Added) and `docs/testing-tracker.md` ("Cost report" section) are present and consistent.
4. **Plan status line current** — two stale lines:
   - The header's **Version** line says "none on the branch", but task af2f000a has since bumped both `Version` vars to `9.66.0`.
   - **Status** still reads "Validated: left unmarked until the smoke task exercises the feature on a running daemon" — the smoke task (af2f000b) *has* exercised it (evidence above), yet no Validated mark has been recorded (the same "left unmarked" note remains in the `docs/testing-tracker.md` Cost report rows).
5. **Phase checklists present in the plan** — AGENT.md requires "Every phase's own checklist must include the Phase Completion Checklist below". `docs/plans/2026-11-02-cost-report.md` has a Phases *table* (all "Done") but no per-phase checklist section at all, let alone the checklist with checked/N/A items. Not checked, not marked N/A.

Items that pass or are covered:

- **Functionality + unit tests** — implemented and committed across af2f0001→af2f000b; suite green apart from the root-files test above.
- **Live/smoke test against a running daemon** — done (REST, MCP, CLI evidence in `tmp/live-smoke/driver.out`).
- **Conditional docs** — `docs/howto/cost-report.md` plus the glossary entry in `docs/datawatch-definitions.md`, and the `cost_report` sections in `docs/operations.md` (line 672) and `docs/setup.md` (line 584) are present.
- **Localization Rule** — covered: the full Node suite (2026 tests) passes, including the locale-coverage tests for the `cost_report_*` / `settings_cost_report_*` keys in all five bundles.
- **Version bumped in both places** — `9.66.0` in both files, they match.
- **No leaked tracker IDs / Automaton wording** — `scripts/check-no-internal-refs.sh` passes.

## How to go green (informational — nothing was changed per the read-only scope)

1. Untrack (and remove or relocate) the `tmp/live-smoke/` directory — that single change clears the `tests/ci` failure and all 6 local-leak findings.
2. Check the four `fmt.Fprint*` error returns in `cmd/datawatch/cli_cost_report.go:106,108,116,118`.
3. File the datawatch-app parity issue and record its URL/number in `docs/plans/parity-issues/cost-report.md` before merge.
4. Add the `howto/cost-report.md` row to the `docs/README.md` how-to table (and, optionally, `docs/howto/README.md`).
5. Update the plan: Version line (`9.66.0`), record the Validated mark with the smoke evidence, and give each phase the AGENT.md Phase Completion Checklist with each item checked or N/A-reasoned.
