<!-- Filed 2026-11-09 as https://github.com/dmz006/datawatch-app/issues/410 (release v9.114.0). -->
# [parity] No-retry holds: render `no_retry_reason`, treat the "(no retry)" alert as a hold, and explain why a bare retry re-holds — mobile client parity (datawatch app)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when the
operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Related drafts.**
[2026-11-08-first-attempt-cost-parity.md](2026-11-08-first-attempt-cost-parity.md)
tracks the first-attempt side of the same story — the cheap first worker, the
`autonomous.first_attempt` pair and the attempt-cost settings.
[2026-11-08-retry-resume-cost-parity.md](2026-11-08-retry-resume-cost-parity.md)
tracks the retry-resume side — a retry that continues the previous session
instead of paying another cold start, and the `resumed`/`resume_reason` fields.
This draft tracks the third piece: the failures that no retry can fix at all,
which are held for review at once instead of burning the remaining attempts.

## Affected area

The operator-facing view of a task that is held for review because its last
failure has no route to a fix through another attempt, on the Android and
iPhone clients. This work ("Make a failed task attempt cost as little as
possible" — PRD `fb940001`, the 2026-11-08 measurement that a task is tried
up to three times and each attempt used to re-pay a full cold start) lands on
the daemon, for the story files
`internal/autonomous/no_retry_reason.go`,
`internal/autonomous/no_retry_reason_test.go`,
`internal/autonomous/executor.go`,
`internal/autonomous/failure_escalation_attempts.go`,
`internal/autonomous/failure_escalation_views.go`,
`internal/autonomous/failure_escalation_review.go` and
`internal/autonomous/no_retry_review_test.go` (with the hold wiring in
`internal/autonomous/no_retry_hold.go`):

While the failure-escalation policy is on, the daemon runs a no-retry check on
each verifier rejection, before the attempt is recorded. A failure has no
retry when any of these holds: the verifier failed the task because **no
files changed** and the task writes nothing (read-only, or no planned files —
a re-run of a task that writes nothing has no new work to verify); the task's
named **verification script is missing** from the worktree (the same missing
script fails a re-run the same way); or the **same failure summary — case and
surrounding whitespace apart — has already failed twice** (a third identical
failure means retrying is not fixing anything). When one fires, the failed
attempt is still recorded — carrying the operator-readable reason in its
`no_retry_reason` field — the task is parked in `review_needed` at once, with
a `FailureReason` of source `no-retry-hold` whose detail is the reason, and a
review summary that leads with the reason and then lists the recorded
attempts; **no further attempt is scheduled and the rest of the failure
budget is not spent**. One error-level alert, titled *Automaton task needs
review (no retry)*, names the Automaton and the task, the reason — for
example `verification script … is missing from the worktree` or `this failure
summary has already failed twice; a third identical attempt will not change
the outcome` — the last worker, and the ways to act: change the task so it
can be verified (give it files to write, a verification script, or a
changeable spec), or cancel it. The held-task entry of the status view now
carries the reason as `no_retry_reason` alongside its `last_backend_reason`,
`last_resumed` and `last_resume_reason`, and the hold resolves through the
same five actions as any held task — retry, edit spec and retry, change
worker, narrow scope, cancel, on every surface — with one twist that belongs
to every surface: for a no-retry hold, a retry with nothing changed re-enters
the hold on the same basis, since the cause was never touched; the useful
moves are the ones that change the basis. Every other failure keeps the
ladder, the counts and the retry-limit hold exactly as before, and with
`enabled: false` the check does not run at all.

The operator guidance lives in
[docs/howto/failure-escalation.md](../../howto/failure-escalation.md) (section
"No-retry failures"), the *No-retry hold* entry in
[docs/datawatch-definitions.md](../../datawatch-definitions.md), and the
*Failure escalation and held tasks* section of
[docs/operations.md](../../operations.md).

## Current behavior (what this work delivers on the server and PWA)

On the daemon the shape is real and auditable: a failure that no retry can
fix no longer burns the remaining attempts on an attempt that is already known
to be pointless — the task arrives at the operator's review sooner, with the
reason in place of the burned attempts, and the reason is the point of the
hold: it says what the operator should change, on the attempt record, on the
held-task row, in the review report and in the one alert per hold. Nothing is
new structurally for a client that already handles holds: the state is the
`review_needed` a retry-limit hold already uses (the same badge, the same
`review_needed` status line, the same five actions), the alert is one regular
error-level message on the existing sink, and the resolution goes through the
action path the clients already implement.

The gap this leaves is on the Android and iPhone clients. A held task's row
and its failed attempt record now carry a `no_retry_reason` field the client
either renders or drops, and if it drops it the operator loses exactly the
sentence that tells them what to do — "give it files to write, a verification
script, or a changeable spec" — on the one hold where the reason, not the
failure count, is the review question. And the alert for this hold arrives
with a title the clients have not seen before, *Automaton task needs review
(no retry)*: a client that buckets or deduplicates alerts by title, or that
special-cases the known hold alert, needs to read the new title as the same
hold, with the same actions. Finally, the retry action on a no-retry hold has
a cost the other holds do not have: with nothing changed it re-enters the hold
on the same basis, and a mobile operator offered a plain *Retry* button with
no context is set up to press it and be held again the same way.

## Expected behavior

Once parity is met, a client on either platform can do what the PWA can. It
renders `no_retry_reason` on the held-task row and on the failed attempt
record the way it renders the other optional fields of the same payloads —
an absent or empty reason reads as absent, never as an error, so attempts
recorded before this release and holds reached by other means (retry-limit,
infrastructure retries run out) still display unchanged. The client treats an
alert titled *Automaton task needs review (no retry)* as a review hold: same
severity handling, same five actions, and the alert body's *Ways to act* line
is the guidance to show alongside *Retry*. And when the client presents *Retry*
on a hold whose reason is present, it presents it with the basis note — retry
only changes the outcome once the basis has changed: files to write, a
verification script in the worktree, or a changed spec — so the operator
does not expect a bare re-run to succeed on a hold that was raised precisely
because a re-run will not.

## Suggested next step

File one `feat(autonomous):`-prefixed issue in datawatch-app covering the
three items above — the `no_retry_reason` pass-through on held-task rows and
attempt records, the *Automaton task needs review (no retry)* alert title as
a hold on the existing action path, and the basis note on *Retry* for holds
with a reason — referencing this draft and the failure-escalation howto's
*No-retry failures* section as the source of truth for the wording. Where the
app already renders held-task rows and attempt records generically from their
payloads, the acceptance criteria are a pass-through test: one no-retry hold
with a `no_retry_reason` and one retry-limit hold without (the field absent
renders nothing, no error), one failed attempt carrying the reason and the
two attempts before it not, one alert with the new title surfacing the five
actions, and — if the app shows the outcome of a client-initiated retry — a
retry with nothing changed re-entering the hold on the same basis, with the
same reason shown. No REST or MCP contract the clients already parse changes
shape, and there is no setting to add: the check rides on the
pre-existing failure-escalation policy. Capability parity is required on both
platforms; implementation parity is not.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | Read-side only: the attempt records and the held-task entries on the existing autonomous status, review and attempt endpoints now carry `no_retry_reason` (and the `FailureReason` of no-retry holds carries source `no-retry-hold`). No new endpoint; the hold state and the five resolution actions are exactly the ones a retry-limit hold already exposes. |
| MCP | included | The autonomous status and resolve tools return the REST bodies, so the new field and the hold ride along unchanged; the resolve path the tools expose is the same one a client retry uses. |
| CLI | included | The operator reads the reason from the `review_needed` line of `datawatch autonomous status` and resolves through the existing autonomous hold actions; write-side decisions run through the same shared path as every other surface. |
| comm channel | included | Same as CLI: the status wording and the hold actions are shared, so the reason and the actions reach comm-channel operators unchanged; nothing comm-specific is new. |
| YAML/config | excluded | No new key: the no-retry check is gated on the pre-existing failure-escalation policy (`enabled: false` turns the check off and restores retry-then-fail). A client that can already surface that policy does not need a new setting for this story. |
| PWA | included | Reference surface: the held-task row and the attempt records render `no_retry_reason` from their payloads, and the operator guide (howto, definitions, operations) is written for it. |
| Android | excluded | No app code lives in this repo. Capability parity is required and is tracked by this draft: the `no_retry_reason` pass-through, the "(no retry)" alert as a hold, and the *Retry* basis note — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same three items are tracked here for the native client. |

## Recording

Issue number: (to be recorded by the filer)
File date: (to be recorded by the filer)
Release: datawatch next patch release (the entry is under `[Unreleased]` in `CHANGELOG.md`)
Source work: "Make a failed task attempt cost as little as possible" — PRD `fb940001`, story files `internal/autonomous/no_retry_reason.go`, `internal/autonomous/no_retry_reason_test.go`, `internal/autonomous/executor.go`, `internal/autonomous/failure_escalation_attempts.go`, `internal/autonomous/failure_escalation_views.go`, `internal/autonomous/failure_escalation_review.go` and `internal/autonomous/no_retry_review_test.go`.
