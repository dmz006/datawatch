<!-- Filed 2026-11-09 as https://github.com/dmz006/datawatch-app/issues/412 (release v9.114.0). -->
# datawatch-app parity issue — failed-task attempt cost (cheap non-final attempts)

**Not filed yet.** The supervisor files this on the `dmz006/datawatch-app` repository with `gh issue create` at merge. The issue title is the heading below ("Title") and the issue body is the text from "## Affected area" through the end of the body. Whoever files it records the issue number in the recording section at the end.

## Title

`[parity] Cheap failed-task attempts: label retry escalations and re-check any per-attempt cost assumptions`

## Parent release

datawatch next patch release (the entry is currently under `[Unreleased]` in `CHANGELOG.md`; daemon version at planning is v9.112.7). The change needs no app-side code action — it is a daemon-side costing change with no new settings — and this is the follow-up issue for the two gaps it leaves open on the client: nothing tells the operator *why* a retry or escalation attempt now costs visibly less, and any app-side budget or estimate logic built on the old "every retry re-pays a full cold start" model now over-states failed-attempt cost.

## Affected area

Automaton task retries and failure-escalation attempts as they show up to a mobile operator, from the operator's point of view. When the datawatch daemon runs an Automaton task and it keeps failing, each non-final attempt (an auto-fix re-run, or an escalation hop to the next worker on the ladder) used to be billed on a full cold start of input. Since this release, a non-final attempt is charged on far less input than the first attempt's cold start: measured 2026-11-08, a task session starts with a median of about 85,000 tokens of context, and a bare `claude -p` start in a task-like folder carries about 64,000 tokens by default versus about 28,000 with MCP servers switched off — the MCP tool definitions are about 36,000 tokens (56%) of every session start, and that is precisely the bulk a non-final attempt no longer re-pays. Concretely for the app: the per-session input tokens, any per-session tokens-in display, and the cost report's figures for these attempts are now a smaller fraction of the first attempt's start, and the gap widens with every additional retry and escalation hop. The attempt order, the `review_needed` hold and its report are exactly as before, a first-try success is unchanged, and there is no new setting to add, read or write.

## Why the app sees a difference it has to explain

Two things on the mobile client are affected, and both are presentation and assumption gaps, not protocol breaks:

1. **A cheap retry looks like a broken one.** The session ledger and the per-session figures an app shows reflect what the daemon charged. Non-final attempts now show far less input than the first attempt of the same task. An operator comparing the three attempts of a failing task on Android or iPhone sees the later attempts with a fraction of the input and nothing telling them that is the intended, designed shape of a cheap retry — the natural read is "the retry got a degraded or truncated context." The daemon already tags these sessions distinctly (the launch-kind vocabulary distinguishes a task's first run from a `kind:auto-fix` re-run carrying the fix context, alongside `role:worker` and the `automaton:` axis), so the app can identify exactly which sessions are the cheap, non-final attempts — the missing piece is the label and the explanation.
2. **Any per-attempt cost assumption is now stale.** Before this release the defensible planning assumption was "each failed attempt and each escalation hop re-pays the full ~85,000-token cold start," so a three-attempt failure cost roughly three full starts' worth of input (up to ~255,000 tokens of input, dominated by the ~36,000 MCP tool definitions re-paid on every one). After it, the first attempt still pays the full start and every retry and hop after it costs a fraction of that. Any app-side logic that estimates, projects, warns, or compares on the premise that "a retry costs the same as a new session" — a budget warning for a retrying Automaton, an expected-cost hint, a per-attempt projection used in a cost or usage display — now over-states the cost of a failing task, and the over-statement grows with each retry and each escalation hop.

## Expected behavior

A mobile operator watching a task fail and retry should be able to see, in the app, that the later attempts are deliberately cheaper — not thinner — so a low input figure on a retry reads as "the daemon did not re-pay the full cold start" rather than "something went wrong." And any app-side budget, estimate, or projection about the cost of a failing or retrying task should reflect the new model: first attempt at the full start, each subsequent retry and escalation hop at a fraction of it.

## Suggested next step

- **Label the cheap attempts** in wherever the app shows attempts of a task (session tiles with a task/launch chip, any per-session tokens display, the cost report grouped per session). The daemon already distinguishes them by launch kind — re-runs carrying the fix context take the `kind:auto-fix` kind, alongside `role:worker` and the `automaton:` axis — so the app can mark them with a short note reading, in effect, "retry/escalation attempt — charged on less input than the first attempt's start," linked to the daemon's howto (`docs/howto/failure-escalation.md`, section "What a non-final attempt costs"). This is a presentation addition, not a protocol change.
- **Re-check any per-attempt cost assumption.** Any budget warning, expected-cost hint, or projection in the Android and iPhone clients that multiplies a failing task's cost by a "one full start per attempt" model now over-states the cost of a failed attempt and should be lowered to the new model (first attempt at a full start, each retry and each escalation hop at a fraction) or removed in favor of the daemon's own cost report, which prices off the actual per-session figures and needs no per-attempt projection.
- **No new settings to wire.** There is no `cost` or `retry` knob to add, read or set: the change is internal to how a non-final attempt is billed, there is nothing to migrate, and the cost report's shape (tokens first, a USD figure only where a rate is configured) is unchanged. This is not a blocker and can be scheduled as a low-priority follow-up.

## Recording

Issue number: (to be recorded by the filer)
File date: 2026-11-08
Release: datawatch next patch release (at planning v9.112.7)
Source work: "Make a failed task attempt cost as little as possible. Measured 2026-11-08: a task is tried up to three times and each attempt [no longer re-pays the full cold start]"
