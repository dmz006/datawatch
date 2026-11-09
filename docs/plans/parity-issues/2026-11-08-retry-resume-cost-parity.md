<!-- Filed 2026-11-09 as https://github.com/dmz006/datawatch-app/issues/411 (release v9.114.0). -->
# [parity] Retry resume: label a resumed retry, render the resume-outcome fields, and re-check per-attempt cost assumptions — mobile client parity (datawatch app)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened when
the operator files it. Filing the issue does not change a line of this repo.
This draft is a file write only: it never runs any hosting CLI and creates
nothing on any host.

**Related draft.**
[2026-11-08-first-attempt-cost-parity.md](2026-11-08-first-attempt-cost-parity.md)
tracks the sibling piece of the same story — the first-attempt worker, the
`autonomous.first_attempt` pair, and the attempt-cost settings as a group.
This draft tracks the resume-side of it: a retry that continues the previous
session instead of paying another full cold start, and the fields and
behaviour that tells the operator what happened.

## Affected area

The operator-facing view of a retried Automaton task attempt on the Android
and iPhone clients — specifically the retry-resume half of the "Make a failed
task attempt cost as little as possible" work (PRD `fb940001`, the
2026-11-08 measurement that a task is tried up to three times and each
attempt used to re-pay a full cold start). What that work adds on the daemon
side, for the story files
`internal/autonomous/retry_resume_decision.go`,
`internal/autonomous/retry_resume_flow.go` (via `executor.go`),
`internal/autonomous/failure_escalation_attempts.go`,
`internal/autonomous/failure_escalation_views.go` and their tests:

First, a behavior. When a task's attempt fails **verification** (stalls,
start failures and infra failures never set one up) and the worker's session
is still open, the daemon may resume that session for the next attempt
instead of starting a fresh one. It does so only when every check passes —
the attempt's backend supports resume (today `claude-code` only),
`autonomous.retry_resume_max_context_tokens` is above 0, and the session's
context is at or under that cap (an unmeasurable size passes when the other
checks do). A resumed attempt spawns nothing: it keeps the failed attempt's
worktree and receives one line typed into the open session — a marker naming
it as an Automaton retry resumed with its context, plus the same failure
summary a fresh retry's prompt would carry, flattened and cut at 4,000
characters. A missing send path or a failed send falls back to a fresh
session. A resumed retry that still fails counts as a failed attempt exactly
as before: `max_failures`, the escalation ladder, the `review_needed` hold
and its alert are unchanged; only the cost of the pass changes — the summary
line and the continuation instead of another ~85,000-token cold start.

Second, two record fields, on every surface that shows attempts. Each
attempt record after the first carries `resumed` (true only when the attempt
continued the previous session) and `resume_reason`, with a closed
operator-readable vocabulary: `resumed previous session`, or one of the
refusals `session ended; starting a fresh attempt`, `backend cannot resume;
starting a fresh attempt`, `resume disabled by setting; starting a fresh
attempt`, `context above the limit; starting a fresh attempt`, or one of the
send fallbacks `no send-input hook; starting a fresh attempt` and `send into
session failed; starting a fresh attempt`. A held task's review entry carries
the same two for its last attempt, as `last_resumed` / `last_resume_reason`,
next to the existing `last_backend_reason` and the worker's choice reason.

Third, one setting that doubles as the on/off switch:
`autonomous.retry_resume_max_context_tokens` (default 150,000 tokens, a whole
number from 0 to 2,000,000; 0 turns resume off and forces every retry onto a
fresh session — the pre-release behaviour). It is validated by the same
shared validator (`ValidateAttemptCostSettings`) across every surface that
accepts the attempt-cost keys, and names the key on refusal.

The operator guidance lives in
[docs/howto/failure-escalation.md](../../howto/failure-escalation.md)
(section "Retry resume: continuing the previous session"), the *Retry resume
(attempt cost)* entry in
[docs/datawatch-definitions.md](../../datawatch-definitions.md), and the
*Failure escalation and held tasks* section of
[docs/operations.md](../../operations.md).

## Current behavior (what this work delivers on the server and PWA)

On the daemon the resume shape is real, decision-auditable and backward
safe: a verifier-failed `claude-code` attempt whose session is still open is
resumed by default (150,000-token cap), the resume line and the fresh-retry
prompt carry the same failure summary, and every retry attempt — resumed or
not — records whether it resumed and the exact reason, in the closed
vocabulary above. When the operator sets the cap to 0, every retry starts a
fresh session exactly as it did before, so the setting is also the
rollback. The fields ride the existing read surfaces as new optional keys:
the attempt records and the held-task rows on the autonomous status and
review endpoints now carry `resumed`/`resume_reason` and
`last_resumed`/`last_resume_reason`, omitted or empty for attempts recorded
before this release. There is no new endpoint, no protocol change and no
write path beyond the one config key, and the failure flow — attempt order,
hold, report, alert, the five operator actions on every surface — is exactly
as before.

The gap this leaves is on the Android and iPhone clients. A mobile operator
watching a failing task sees a retry whose input is the failure summary plus
a continuation rather than another full cold start, with nothing in the
client saying that a resumed retry is deliberately priced lower — the natural
read of attempt 2 costing a fraction of attempt 1 is a degraded or truncated
session. The attempt record's `resumed` and `resume_reason` keys are new to
the payload the client parses: a client that drops unknown fields loses
exactly the sentence that distinguishes "the daemon continued the worker
after a verified failure" from "the daemon could not, here is why," which is
the difference a held-task operator needs when deciding among retry, change
worker and cancel. And the one knob that turns the whole feature on and off
— `autonomous.retry_resume_max_context_tokens`, default 150,000, range
0–2,000,000, 0 = fresh sessions every time — has no home in the clients'
configuration surfaces: an operator who wants to preview the old
behaviour, or cap how much prior context a retry may carry, is limited to
editing the daemon's `config.yaml` by hand.

## Expected behavior

Once parity is met, a client on either platform can do what the PWA can, for
the resume side of the story. It renders `resumed` and `resume_reason` on an
attempt record, and `last_resumed`/`last_resume_reason` on a held task's
review entry, the way it renders the other fields of the same payloads — an
absent key or an empty reason reads as "no retry decision was recorded",
never as an error, so pre-existing attempts and held tasks still display.
A resumed attempt is presented as the intended, cheaper cost shape — "retry
resumed the previous session with the failure summary" — not as a thin or
failed session, and a fresh attempt shows the refusal reason that explains
why it was not resumed. The client's autonomous settings surface exposes
`autonomous.retry_resume_max_context_tokens` with the same default and range
the daemon validates (0–2,000,000, 0 = resume off), so a change made from
the phone is accepted or refused by the daemon exactly as one made by hand,
naming the key on refusal. And any app-side budget, estimate or projection
about what a failing task's retries will cost is corrected to the new model:
the first attempt is a full cold start, a resumed retry is the summary line
plus the continuation, and a fresh retry is what the cap or a refusal made
it.

## Suggested next step

File one `feat(autonomous):`-prefixed issue in datawatch-app alongside the
one this sibling draft tracks, covering three items — the
`resumed`/`resume_reason` and `last_resumed`/`last_resume_reason` pass-through
with the "resumed retries are deliberately cheaper" presentation note, the
`autonomous.retry_resume_max_context_tokens` setting (default 150,000,
range 0–2,000,000, 0 = off), and the re-check of any per-attempt cost
assumption in the app built on "every retry re-pays a full cold start."
Reference this draft and the sibling one, with the howto's "Retry resume:
continuing the previous session" section as the source of truth for the
reason vocabulary, defaults and range. Where the app already renders attempt
records and held-task rows generically from their payloads, the acceptance
criteria are pass-through tests: a task with a verifier-failed attempt whose
retry resumed (reason `resumed previous session`), a task with a fresh retry
on each of the five refusal vocabulary cases, a held task row carrying
`last_resumed`/`last_resume_reason` and one without, and a round-trip of
`retry_resume_max_context_tokens` including one intentionally out-of-range
value and one at 0 to confirm the validator's message — naming the key — is
shown rather than swallowed, on both Android and iPhone. The settings
round-trip needs the daemon-side write surface for the key on the client's
usual configuration channel first — if the daemon accepts it only from
`config.yaml` today, say so in the issue and let that land before the client
UI. Capability parity is required on both platforms; implementation parity
is not. This is a display-and-settings story, not an integration story: no
REST or MCP contract the clients already parse changes shape, and the
failure flow (attempt order, hold, report, alert, the five actions) is
unchanged on every surface.

## Parity surface

| Surface | Decision | Why |
|---------|----------|-----|
| REST | included | Read-side only: attempt records and the `review_needed` / held-task rows on the existing autonomous status and review endpoints now carry `resumed`/`resume_reason` and `last_resumed`/`last_resume_reason`. No new endpoint; the resume setting is daemon-wide config, not per-PRD state. |
| MCP | included | `autonomous_status` returns the REST status body, so the new fields ride along unchanged; nothing to add on the tool side. |
| CLI | included | `datawatch autonomous status` already lists each held task with `last_resumed`/`last_resume_reason`; the operator reads the effective cap from the existing config surfaces. |
| comm channel | included | The comm channel's `autonomous status` carries the same held-task rows; `configure autonomous.<key>` acceptance for the resume key runs through the shared validator, which names the key on refusal. |
| YAML/config | included | The `autonomous` block's `retry_resume_max_context_tokens` (default 150,000, range 0–2,000,000, 0 = fresh sessions every time) is read live by the executor on the next retry; it is both the cap and the on/off switch. |
| PWA | included | Reference surface: the operator guide (howto, definitions, operations) is written for it; attempt records and held-task rows carry the new keys in the payloads it renders. |
| Android | excluded | No app code lives in this repo. Capability parity is required and is tracked by this draft: resume-outcome pass-through, the resume setting, and the cheap-resumed-retry presentation — implementation parity is not. |
| iPhone/iOS | excluded | Same as Android: no code in this repo, and the same three items are tracked here for the native client. |

## Recording

Issue number: (to be recorded by the filer)
File date: (to be recorded by the filer)
Release: datawatch next patch release (the entry is under `[Unreleased]` in `CHANGELOG.md`)
Source work: "Make a failed task attempt cost as little as possible" — PRD `fb940001`, story files `internal/autonomous/retry_resume_decision.go`, `internal/autonomous/retry_resume_decision_test.go`, `internal/autonomous/executor.go`, `internal/autonomous/retry_resume_flow_test.go`, `internal/autonomous/failure_escalation_attempts.go`, `internal/autonomous/failure_escalation_views.go` and `internal/autonomous/attempt_resume_record_test.go`.
