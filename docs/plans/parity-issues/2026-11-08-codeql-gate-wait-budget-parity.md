# [sync] CodeQL alert gate: no client surfaces the fast-fail rate-limit result — parity gap left open (datawatch app)

**Decision 2026-11-09: not filed, no app surface.** The change is to the CodeQL alert gate script only, so the datawatch app has nothing to match. Four near-identical drafts were generated for one change.

**Not filed yet.** This is the tracked draft. The GitHub issue is opened only when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI (no `gh`, no issue
creation on a tracker) and creates nothing on any host.

**Defect this work addresses** (found 2026-11-08): the CodeQL alert gate kept
waiting out its remaining wait budget after a rate-limit error even when the rate
limit could not reset inside that budget, so the run burned the whole budget on
a retry that could not succeed, and the eventual failure read like a code
problem when it was only a rate-limit window.

## Affected area

The fix is in the CodeQL alert gate shell tool (`scripts/codeql-alert-gate.sh`,
the `wait_analyses` loop and the `reset_out_of_reach`/`lookup_rate_reset`
helpers): when the core rate-limit reset epoch is later than the gate's
remaining wait budget, the gate now stops at once instead of sleeping. Regression
coverage lives in `scripts/tests/test_codeql_alert_gate.py` (beyond-budget and
inside-budget cases for the wait-analyses reset path). The operator guidance in
this repo that describes both the old wait and the new stop is
[docs/howto/codeql-local-scan.md](../../howto/codeql-local-scan.md) and
[docs/howto/automation-repo-ci-watch.md](../../howto/automation-repo-ci-watch.md),
with the gate's definition kept in
[docs/datawatch-definitions.md](../../datawatch-definitions.md). The sibling
drafts [2026-11-08-codeql-gate-rate-limit-stop-parity.md](2026-11-08-codeql-gate-rate-limit-stop-parity.md)
and [2026-11-08-codeql-gate-reset-out-of-reach-parity.md](2026-11-08-codeql-gate-reset-out-of-reach-parity.md)
cover the same defect from the `open_alerts` side and the full fix surface;
this draft records only the residual gap that work leaves.

## Current behavior

The gate's only observable outputs are its exit status and its log lines. On a
rate-limit error whose reset is out of reach inside the remaining budget, it now
prints a `::error title=Rate limited::` line that names the reset epoch and the
remaining wait budget, says the problem is not in the code, and tells the
operator to rerun after the reset time, then exits 1 without sleeping. When the
reset falls inside the budget, it behaves as before: a `::notice title=Rate
limited::` line, a wait of at most the remaining budget (at least 1 second),
then a retry. Nothing in datawatch's clients renders this output: no REST or MCP
endpoint exposes the gate's exit status, the CLI does not wrap the script, the
comm channels relay nothing from it, and no PWA, Android, or iPhone surface
shows a CodeQL gate result as of this change.

## Expected behavior

A failed gate run caused by an unreachable rate-limit reset should be readable
by anything that later ingests CI or check results as "failed because of a
rate-limit window; safe to reschedule after the reset time" — distinct from a
code finding. A local operator running the howto flow sees the fast-fail line
and reruns after the reset rather than investigating code. Any future client
view of check results should map the `Rate limited` error title to that neutral
"retry after reset" state and must not loop re-running the check to clear it.
Until such a view exists, the expected behavior is fully met by the repo's own
log lines and the howto guidance.

## Suggested next step

No client issue is required right now: every surface that exists today treats
the gate as an opaque shell script, and the operator-facing howtos already
describe the fast-fail line as "rerun after the reset time". The one open parity
gap is that no datawatch client (PWA today, Android/iOS per the parity standard)
can distinguish a rate-limited gate failure from a code failure if it ever
displays check results. When — and only when — a check-status or CI-result view
is planned for any client, file a `feat(ci):`-prefixed issue on datawatch-app
requiring the `Rate limited` title to map to a neutral "retry after reset"
state rather than a code-failure state, and file no issue before that.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | closed / none | The gate is a shell script; this change adds no endpoint and exposes no field. Nothing to mirror. |
| MCP | closed / none | No MCP tool wraps the gate script or its exit status. |
| CLI | closed / none | The gate runs as a repo script from CI and local scans, not as a `datawatch` subcommand; no command was added or changed. |
| comm channel | closed / none | The gate does not send or receive channel messages. A future channel-surfaced check-status should use the same neutral mapping, covered by the datawatch-app issue above. |
| YAML/config | closed / none | No config key added; the wait budget and interval stay environment inputs of the script and are unchanged. |
| PWA | open (optional, deferred) | No PWA view renders gate output today. The one open gap: any future check-status view must map the `Rate limited` title to "retry after reset", not code failure. Tracked by this draft, no issue yet. |
| Android | open (optional, deferred) | No app code in this repo. Same deferred mapping as PWA; capability parity would require the same neutral state if the view ships. |
| iPhone/iOS | open (optional, deferred) | Same as Android, per the PWA == Android == iOS parity standard. |
