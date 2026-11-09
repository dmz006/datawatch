# CodeQL gate rate-limit early stop — docs audit (re-run)

**Date:** 2026-11-09. **Scope:** the docs for "Make the CodeQL alert gate stop
waiting when the rate limit cannot reset inside its wait budget" (found
2026-11-08) — the howto, operations, glossary and test entries that must
describe the post-change behavior.

This audit was run on a re-run of the docs-set step for the story. The base
already carries the story's docs commits (the docs-set and how-to-text runs
recorded in the git log), so this pass re-verified every affected entry
against the current `scripts/codeql-alert-gate.sh` behavior instead of
re-editing prose that is already correct.

## What the docs now say (verified against the script)

- **`docs/howto/codeql-local-scan.md`**, section "Red check from a rate
  limit": a `Rate limited` error line on the analyses step (`wait-analyses`)
  or the alerts step (`open-alerts`) means the gate could not ride the limit
  out — either the reset was later than the remaining budget, in which case
  the gate stopped at once without waiting, or the budget was spent while the
  API kept limiting it (`core reset epoch: unknown` when none was learned).
  A `Rate limited` notice is the still-in-budget wait-and-retry; a `No
  analysis for` line is the real missing-analysis timeout. Exit codes
  unchanged: clean 0, usage 2, rate-limit that the gate cannot ride out 1.
  Matches the script's three lines exactly: the out-of-reach
  `::error title=Rate limited::` (naming the reset epoch and the remaining
  budget, no sleep), the spent-budget `::error title=Rate limited::`
  (`core reset epoch: <epoch or unknown>`), and the in-budget
  `::notice title=Rate limited::`.
- **`docs/howto/automation-repo-ci-watch.md`**, the CodeQL red-check
  paragraph under the classification section: a gate red that is only a rate
  limit is recorded like any other failing `security` run — record-only, no
  rerun; the job log line (notice vs error) tells the two reds apart, and
  the error case says to wait until the named reset time and start the check
  again. Consistent with the gate's output.
- **`docs/datawatch-definitions.md`**, glossary entry "Rate-limit reset out
  of reach": the gate does not sleep toward a reset it cannot reach; it
  prints the `Rate limited` error line and exits 1 at once; a reset inside
  the budget is waited for and retried; a limit with no reset time is
  retried on the normal interval until the budget runs out; `open-alerts`
  never reports a zero-alert result from it. The link to
  `howto/codeql-local-scan.md#red-check-from-a-rate-limit` resolves to the
  section heading.
- **`docs/operations.md`**, the Repo CI watch section: `CodeQL` remains in
  the default `security_workflows` list (record-only, never rerun), which is
  the disposition of a rate-limit-red CodeQL run. No operations entry
  contradicts the early-stop behavior.
- **`docs/testing.md`**, "CodeQL alert gate: rate-limit waits (alert-gate
  job)": lists the stop-at-once cases for both steps (reset beyond the
  remaining budget: no sleep recorded; reset inside the budget: one sleep)
  alongside the older wait-and-retry cases; expected count thirteen.
- **`CHANGELOG.md`** `[Unreleased]`: states the gate exits 1 immediately
  with the `::error title=Rate limited::` line, the "How it shows up" line
  covers the `core reset epoch: unknown` form, and the migration note says
  to wait until the named reset time and rerun the check.

## Evidence (run this pass)

| Check | Result |
|-------|--------|
| `bash scripts/verify-codeql-gate-docs.sh` | all checks `ok`, exit 0 |
| `python3 -I scripts/tests/test_codeql_alert_gate.py` | 13/13 pass, exit 0 |
| `python3 scripts/tests/test_codeql_alert_gate.py` via `make test` line | same entry point; both steps' out-of-reach and inside-budget cases covered by `test_wait_analyses_reset_beyond_budget_stops_at_once`, `test_wait_analyses_reset_inside_budget_sleeps_once_then_succeeds`, `test_open_alerts_reset_beyond_budget_stops_at_once`, `test_open_alerts_reset_inside_budget_sleeps_once_then_reads` |
| Link audit | `docs/datawatch-definitions.md` → `howto/codeql-local-scan.md#red-check-from-a-rate-limit` resolves; both howtos indexed in `docs/README.md` and `docs/howto/README.md`; parity issue draft links resolve under `docs/plans/parity-issues/` |

## Operator review flags (still open)

1. **The parity issue draft is still unfiled.**
   `docs/plans/parity-issues/2026-11-08-codeql-gate-rate-limit-stop-parity.md`
   is the tracked draft marked "Not filed yet."; only the operator files it.
   Filing it does not change a line of the repo.
2. **The two open items in the trigger-narrowing plan** (recorded in
   `docs/howto/codeql-local-scan.md` under "What it is and why"): whether a
   dependabot dependency bump should keep starting a gate run, and whether
   the "Open high/critical alerts" job is a required branch check (if it is,
   a documents-only pull request that now starts no run would wait for a
   check that never appears).
3. **Client-side mapping, only if such a view is planned:** the parity draft
   says a future check-status view should map the `Rate limited` error title
   to a neutral "retry after reset" state rather than a code-failure state.
   No client code in this repo renders the gate's output, so nothing is
   pending in-repo; the draft holds the decision.
4. **The alert #768 gate confirmation** in `docs/operations.md` (confirm the
   "Open high/critical alerts" gate run is green on main before the changelog
   entry is promoted) is a separate story's review item and stays open there
   — a rate-limit-red from this change is a rerun-after-reset, not a failure
   of that confirmation.

## Bottom line

No doc edit was needed in this pass: every affected howto, operations,
glossary and test entry already describes the post-change behavior and
matches the script and its 13 test cases. The flags above are the operator's
remaining work.
