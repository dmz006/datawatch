# [sync] CodeQL alert gate stops at once when the rate-limit reset is out of reach — parity draft (datawatch app)

**Decision 2026-11-09: not filed, no app surface.** The change is to the CodeQL alert gate script only, so the datawatch app has nothing to match. Four near-identical drafts were generated for one change.

**Not filed yet.** This is the tracked draft. The GitHub issue is opened only when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Defect this work addresses** (found 2026-11-08): when the GitHub core rate limit
was hit while the CodeQL alert gate was reading analyses or open alerts, and the
reset time fell later than the gate's remaining wait budget, the gate still slept
through its budget before giving up. That sleep could never end in a successful
retry, and the final failure looked like a code problem when it was only a
rate-limit window.

## Affected area

The defect sits in the rate-limit branches of the CodeQL alert gate
(`scripts/codeql-alert-gate.sh`). Two loops are involved: `wait_analyses`, which
polls for analyses to finish, and `open_alerts`, which reads open code-scanning
alerts. Both call the helper `reset_out_of_reach`, which compares the core reset
epoch from `lookup_rate_reset` with the time already waited and the total wait
budget. The regression coverage lives in `scripts/tests/test_codeql_alert_gate.py`.
It has beyond-budget and inside-budget cases for both loops, including
`test_wait_analyses_reset_beyond_budget_stops_at_once` and
`test_open_alerts_reset_beyond_budget_stops_at_once`. The operator guidance that
describes the stop is in [docs/howto/codeql-local-scan.md](../../howto/codeql-local-scan.md),
[docs/howto/automation-repo-ci-watch.md](../../howto/automation-repo-ci-watch.md),
the CodeQL gate entry in [docs/datawatch-definitions.md](../../datawatch-definitions.md),
and [docs/testing.md](../../testing.md).

## Current behavior (what this work delivers in the repo)

When the reset epoch is later than the remaining budget, both loops stop at once,
without sleeping. They print a `::error title=Rate limited::` line. That line
names the reset epoch and the remaining budget, says the problem is not in the
code, and tells the operator to rerun the check after the reset time. The gate
then exits 1. When the reset falls inside the remaining budget, the gate behaves
as before: it prints a `::notice title=Rate limited::` line, waits until the
reset (at least 1 second and never longer than the remaining budget), and
retries. A rate-limit error with no reset time known still waits out the budget,
as before. Any other API failure keeps its old handling. The exit status and log
lines are the only observable outputs. No REST, MCP, config, or stored-state
contract changes.

## Expected behavior

A rate-limited CodeQL gate run whose reset cannot be reached inside its budget
fails fast with a clear, non-code reason and the reset time. An operator or a CI
watcher can then reschedule the check for after that time instead of waiting on a
retry that cannot finish. A rate-limited run whose reset falls inside the budget
still waits and retries as it did before. A client that surfaces CI or check
results should show such a run as a rate-limit failure with the message above,
not as a code finding, and should not re-run the check in a loop to clear it.

## Suggested next step

No client code change is needed for the repo change itself. The gate is a shell
tool run by CI and local scans, and no datawatch client renders its output. The
next step is operator-side: confirm that the CI-watch and local-scan howtos tell
the reader that the fast-fail `Rate limited` line means "rerun after the reset
time". If a datawatch-app check-status view later shows CodeQL gate results, that
view should map the `Rate limited` error title to a neutral "retry after reset"
state rather than to a code-failure state. File that as a `feat(ci):`-prefixed
issue only when such a view is actually planned. Until then this draft records
the decision and no issue is required.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | excluded | The gate is a shell script. It does not add or change any REST endpoint, and it does not read or write datawatch's API. |
| MCP | excluded | No MCP tool wraps the gate script, and its exit status is not exposed as an MCP field. |
| CLI | excluded | `codeql-alert-gate.sh` is a repo script run by CI and local scans, not a `datawatch` subcommand. No CLI command is added or changed. |
| comm channel | excluded | The gate is not reachable from the messaging channels, and it sends no channel messages. |
| YAML/config | excluded | No config key is added. The wait budget and interval remain environment inputs of the script and are unchanged. |
| PWA | excluded | The PWA does not display CodeQL gate output. Nothing in the PWA changes. |
| Android | excluded | No app code lives in this repo. The only client-facing note is the optional neutral "retry after reset" mapping in Suggested next step, which this draft tracks; it is not required for this change. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same optional mapping is tracked here for the native client. |
