# [sync] CodeQL alert gate stops at once when the rate-limit reset is out of reach — defect fix parity (datawatch app)

**Decision 2026-11-09: not filed, no app surface.** The change is to the CodeQL alert gate script only, so the datawatch app has nothing to match. Four near-identical drafts were generated for one change.

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Defect this work addresses** (found 2026-11-08): when the GitHub API rate
limit was hit while the CodeQL alert gate was waiting for analyses, the gate
could not finish inside its wait budget, yet it still waited out the remaining
budget before giving up. The wait was spent on a retry that could not succeed,
and the failure looked like a code problem when it was only a rate-limit
window.

## Affected area

The defect sits in the wait loop of the CodeQL alert gate
(`scripts/codeql-alert-gate.sh`, the `wait_analyses` function and its helper
`lookup_rate_reset`). When a rate-limit error comes back, the gate asks the
rate-limit endpoint for the core reset epoch and waits for it, capped to the
remaining wait budget. The fix adds a check ahead of that wait: when the reset
epoch is later than the remaining budget, the gate stops at once. The
regression coverage lives in `scripts/tests/test_codeql_alert_gate.py`, which
gained beyond-budget and inside-budget wait-analyses reset cases. The operator
guidance that describes the stop lives in
[docs/howto/codeql-local-scan.md](../../howto/codeql-local-scan.md),
[docs/howto/automation-repo-ci-watch.md](../../howto/automation-repo-ci-watch.md),
the CodeQL gate entry in [docs/datawatch-definitions.md](../../datawatch-definitions.md),
and [docs/testing.md](../../testing.md).

## Current behavior (what this work delivers in the repo)

When the reset is out of reach, the gate prints a
`::error title=Rate limited::` line that names the reset epoch and the
remaining wait budget, says the problem is not in the code, and tells the
operator to rerun the check after the reset time. It then exits 1 immediately,
with no sleep. When the reset falls inside the remaining budget, the gate
behaves as before: it prints a `::notice title=Rate limited::` line, waits until
the reset (at least 1 second, never longer than the remaining budget), and
retries. Any other API failure keeps the old behaviour: nothing seen yet, retried
on the normal schedule. The gate's exit status and log lines are the only
observable outputs; no REST, MCP, config, or stored-state contract changes.

## Expected behavior

A rate-limited CodeQL gate run that cannot see the reset inside its budget
fails fast with a clear, non-code reason and the reset time, so an operator or
a CI watcher can reschedule the check for after that time rather than waiting
on a retry that cannot finish. A rate-limited run whose reset falls inside the
budget still waits and retries as it did before. Clients that surface CI or
check results should show such a run as a failed check with the rate-limit
message, not as a code finding, and should not treat it as a reason to block or
re-run work in a loop.

## Suggested next step

No client work is needed for the repo change itself, because the gate is a
shell tool run by CI and local scans, and no datawatch client renders its
output. The next step is operator-side: confirm that the CI-watch and local-scan
howtos describe the fast-fail rate-limit line as "rerun after the reset time",
which the docs commit already does. If a datawatch-app check-status view
later shows CodeQL gate results, that view should map the `Rate limited` error
title to a neutral "retry after reset" state rather than a code-failure state.
File that as a `feat(ci):`-prefixed issue only when such a view is actually
planned; until then this draft records the decision and no issue is required.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | excluded | The gate is a shell script. It does not add or change any REST endpoint, and it does not read or write datawatch's API. |
| MCP | excluded | No MCP tool wraps the gate script, and its exit status is not exposed as an MCP field. |
| CLI | excluded | `codeql-alert-gate.sh` is a repo script run by CI and local scans, not a `datawatch` subcommand. No CLI command is added or changed. |
| comm channel | excluded | The gate is not reachable from the messaging channels, and it sends no channel messages. |
| YAML/config | excluded | No config key is added. The wait budget and interval are environment inputs of the script and are unchanged. |
| PWA | excluded | The PWA does not display CodeQL gate output. Nothing in the PWA changes. |
| Android | excluded | No app code lives in this repo. The only client-facing note is the optional neutral "retry after reset" mapping described under Suggested next step, which is tracked by this draft; it is not required for this change. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same optional mapping is tracked here for the native client. |
