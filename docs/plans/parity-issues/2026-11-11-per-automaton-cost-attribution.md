# Parity issue draft — Per-Automaton cost counts all of its spend (datawatch-app)

**Not filed yet.** This is a draft only. Nothing here was created on a tracker. When the
operator decides to file it, file it on **datawatch-app** as a sync issue
(`[sync] feat(cost): per-Automaton cost counts all spend`) and record the issue URL and number
here. Parent release: the next release above main at merge time.

## Affected area

The per-Automaton cost figures: the tokens, active time, session count and tool-call count
shown for an Automaton on the cost card, the cost report grouped by `automaton`, and any
Automaton detail view in the mobile app that shows spend.

## Current behavior

Before this change an Automaton's cost counted only its worker sessions. Planning (decompose),
rules review, verification, guardrail and retry sessions were not attributed to the Automaton,
so its totals were too low. Deleting or cleaning up a session also removed its spend from the
Automaton's total. The mobile app shows whatever totals the daemon reports, so it currently
shows these under-counted figures. Tool calls were not counted at all.

## What the daemon now does

Each session record carries the Automaton and task it belongs to, stamped at every autonomous
launcher (workers, verifiers, planner and decompose sessions). A new spend ledger, kept in its
own JSON file under the daemon data directory and keyed by session and role, stores tokens in
and out, cache read and creation tokens, tool calls, active seconds, backend and start and end
times. Roles are `worker`, `decompose`, `rules_review`, `verification`, `guardrail` and
`other`. The Claude usage reader counts tool calls from the transcript. The ledger entry stays
after the session is deleted, and entries older than 31 days are pruned; the ledger reports its
oldest retained time so a report can tell "older than the ledger" from "no spend".

Sessions that ran before the upgrade carry no attribution and stay uncounted. New sessions are
counted from the first launch after the upgrade. There is no migration.

## Expected behavior in the app

The app needs no new endpoint or setting for this change; it adds no REST route and no config
key. The app should show the larger, correct totals the daemon already reports, with the
session count and tool-call count beside the tokens and time wherever it shows Automaton spend,
and it should not assume the totals fall when a session is deleted. Where the app shows a cost
card for an Automaton it should not label old Automata as zero spend: spend from before the
upgrade is simply not recorded, so the app should say figures cover activity since the upgrade
and the 31-day retention window, as the cost report body already does with `retention_days`.

## Parity gaps this work leaves

1. **Tool-call and session counts.** The daemon now has both per Automaton. Confirm each is
   exposed in the response the app reads; if a count is only in the ledger and not in a REST
   body, a daemon follow-up must add it before the app can show it. This draft does not claim
   the field exists in a response.
2. **Per-role breakdown.** The ledger knows which role spent what (planning, verification,
   retries). No surface shows that split yet, in the PWA or the app.
3. **Pre-upgrade history.** Old Automata stay under-counted with no marker saying so.

## Suggested next step

Have the operator decide whether to file this. If yes, first check which cost response bodies
carry the session and tool-call counts per Automaton and note any that do not, then file the
datawatch-app issue asking for the counts beside tokens and time and for the "since upgrade"
wording. Leave the per-role breakdown as a separate design question for the operator, since
where it appears in the UI is a placement decision.

## Story files involved

internal/spendledger (doc, ledger and tests), internal/session (store, manager, claude usage,
spend ledger and attribution tests), internal/autonomous (executor, decompose),
cmd/datawatch/main.go and its launcher attribution test.
