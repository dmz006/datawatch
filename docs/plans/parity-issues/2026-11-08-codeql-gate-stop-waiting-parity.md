# [sync] CodeQL alert gate: stop waiting when the rate limit cannot reset inside the wait budget — parity draft (datawatch app)

**Decision 2026-11-09: not filed, no app surface.** The change is to the CodeQL alert gate script only, so the datawatch app has nothing to match. Four near-identical drafts were generated for one change.

**Not filed yet.** This is the tracked draft. The GitHub issue is opened only when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI (no `gh`, no issue
creation on a tracker) and creates nothing on any host.

**Defect this work addresses** (found 2026-11-08 from t-observation): the CodeQL
alert gate, after hitting a GitHub API rate-limit error, kept sleeping out its
remaining wait budget even when the rate limit could not reset inside that budget.
The run burned the whole budget on a retry that could not possibly succeed, and
the eventual failure reads like a code problem when it was only a rate-limit
window. The work makes the gate stop waiting at the moment it can prove the
reset is out of reach, instead of at the end of the budget.

## Affected area

The change lives in the CodeQL alert gate shell tool, `scripts/codeql-alert-gate.sh`.
The new helper `reset_out_of_reach` compares the core reset epoch (learned by the
existing `lookup_rate_reset` helper, which makes one extra call to the rate-limit
endpoint) against the time already waited and the total wait budget, and both
poll loops in the gate now consult it before they sleep: `wait_analyses`, which
waits for the CodeQL analyses to finish, and the `open_alerts` read loop, which
waits for open code-scanning alerts. When the helper says the reset is out of
reach, the loop breaks immediately instead of entering its next sleep. Regression
coverage lives in `scripts/tests/test_codeql_alert_gate.py`, with beyond-budget
and inside-budget reset cases for both loops. The operator guidance describing
the new stop is in [docs/howto/codeql-local-scan.md](../../howto/codeql-local-scan.md),
[docs/howto/automation-repo-ci-watch.md](../../howto/automation-repo-ci-watch.md),
and the CodeQL gate entry in [docs/datawatch-definitions.md](../../datawatch-definitions.md),
with the test coverage noted in [docs/testing.md](../../testing.md). The sibling
drafts [2026-11-08-codeql-gate-rate-limit-stop-parity.md](2026-11-08-codeql-gate-rate-limit-stop-parity.md)
and [2026-11-08-codeql-gate-reset-out-of-reach-parity.md](2026-11-08-codeql-gate-reset-out-of-reach-parity.md)
record the same defect from the single-loop and full-fix-surface angles
respectively; this draft records the parity surface of the stop-waiting change
as one unit of work.

## Current behavior

Before this work, a rate-limited gate always slept out whatever of its wait budget
remained, no matter how far away the reset was, and then failed with an error
that an operator (or a CI watcher) could not distinguish from a code finding.
After this work, in the repo: when a rate-limit error comes back and the learned
reset epoch is later than the remaining budget, the gate stops at once, without
sleeping. It prints a `::error title=Rate limited::` line that names the reset
epoch and the remaining wait budget, says the problem is not in the code, and
tells the operator to rerun the check after the reset time, then exits 1. When
the reset falls inside the remaining budget, the gate behaves exactly as before:
a `::notice title=Rate limited::` line, a wait of at least 1 second and never
longer than the remaining budget, then a retry. A rate-limit error with no known
reset time, and any other kind of API failure, keep their old handling. The gate's
exit status and its log lines are its only observable outputs; no REST, MCP,
config, or stored-state contract changes.

## Expected behavior

A rate-limited gate run whose reset cannot be reached inside its budget should
fail fast with a clear, non-code reason and the reset time, so an operator or a
CI watcher can reschedule the check for after that time rather than wait on a
retry that cannot finish. A rate-limited run whose reset does fall inside the
budget should keep waiting and retrying as before. Any client that surfaces CI or
check results should map this kind of failure to a neutral "retry after reset"
state, distinct from a code finding, and must not loop re-running the check to
try to clear it. Until any such client view exists, the expected behavior is
fully met by the gate's log lines, exit status, and the howto guidance already
in the repo.

## Suggested next step

No client code change is needed for this work itself: the gate is a shell tool run
by CI and local scans, the only surfaces that touch its output are the ones that
already read shell exit codes and log lines, and no datawatch client renders a
CodeQL gate result as of this change. The immediate step is operator-side and is
already done by this work's docs commit: confirm the local-scan and CI-watch
howtos describe the fast-fail `Rate limited` line as "rerun after the reset
time". The one open parity gap this work leaves is forward-looking: if a
check-status or CI-result view is ever planned for a datawatch-app client (PWA,
Android, or iPhone per the parity standard), file that as a `feat(ci):`-prefixed
issue requiring the `Rate limited` error title to map to the neutral "retry
after reset" state rather than a code-failure state, and file nothing before that
point.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | excluded | The gate is a shell script. The change adds no endpoint and changes no datawatch API contract. |
| MCP | excluded | No MCP tool wraps the gate script or exposes its exit status or log lines. |
| CLI | excluded | `codeql-alert-gate.sh` is a repo script run by CI and local scans, not a `datawatch` subcommand; no CLI command is added or changed. |
| comm channel | excluded | The gate neither sends nor receives channel messages. A future channel-surfaced check status should use the same neutral mapping, already covered by the deferred datawatch-app issue above. |
| YAML/config | excluded | No config key is added. The wait budget and poll interval remain environment inputs of the script and are unchanged. |
| PWA | open (optional, deferred) | No PWA view renders gate output today, so this work leaves nothing broken. The one open gap: any future check-status view must map the `Rate limited` title to "retry after reset", not code failure. Tracked by this draft; no issue yet. |
| Android | open (optional, deferred) | No app code lives in this repo. Same deferred neutral mapping as PWA; capability parity would require it only if the view ships. |
| iPhone/iOS | open (optional, deferred) | Same as Android, per the PWA == Android == iPhone parity standard. |
