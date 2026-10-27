# Autonomous triage — the Android / iPhone surface (this repo's daemon side)

**Date:** 2026-10-26
**Version:** 9.17.1 (lands with the next 9.x release on merge)
**Repo:** `dmz006/datawatch-app` (Android + iPhone/iOS)
**Daemon counterpart (this repo):** the triage record, the triage pass and the
alert — `internal/autonomous/triage_record.go`, `triage_pass.go`,
`triage_text.go` — and the read on every daemon surface: REST
(`GET /api/autonomous/triage`, filters `automation_id`, `prd_id`,
`status=failed|blocked`), MCP (`autonomous_triage`), CLI
(`datawatch autonomous triage`), comm channel (`autonomous triage [automation-id]`),
and the PWA triage card in the Automaton detail view.

**Status:** the app-side triage surface is owed to `dmz006/datawatch-app`,
and the owed issue **has been filed** as
[`dmz006/datawatch-app` #284](https://github.com/dmz006/datawatch-app/issues/284)
(2026-10-27). This spec said not to open external issues, but the daemon
repo's standing Mobile-Parity Rule (AGENT.md, Section B4) requires a
datawatch-app issue for any operator-visible PWA change and it takes
precedence; the rule prefers commenting on a relevant existing issue, but
none existed, so a new one was filed with the prefix the rule asks for. The
filed body (the issue body below) stands on its own, and the debt is also
tracked in `docs/testing-tracker.md` (the *Wearable / mobile read* row and
the *Mobile-Parity: filed* block, which repeat the same text).

## What the daemon already gives the app

| Surface | What the app gets |
|---|---|
| REST | `GET /api/autonomous/triage` → `{"records":[…newest first…], "alert_minutes":30}`; optional filters `automation_id`, `prd_id`, `status=failed\|blocked`; a filter that matches nothing lists empty (`"records":[]`), never a 404; a wrong `status` is a 400 that names the two allowed values. |
| Config | `autonomous.automation.triage_alert_minutes` (default `30`; an explicit `0` means the alert is off and `0` is reported in `alert_minutes`; a negative value is refused at load and on every patch surface). |
| Comm channel | the triage alert itself: one per stuck episode, carrying the record's full text (stuck task, task error, episode start, session end reason, the four retry budgets), sent at the bound above. |
| PWA wording | the five locale bundles carry the card's wording (`triage_card_title`, `triage_status_*`, the four budget labels and the empty state); the app should reuse the same en source strings under the Terminology Rule (Automaton / Automata, never "PRD" in UI text). |

## The surface the app owes

One triage card in the app's Automaton detail view, rendering the same record
the PWA card renders: status, the failed or blocked task and its error, when the
Automaton first entered that state, the session end reason, the four retry
budgets, and any notes. The card clears when the record does; a stuck queue is
one read over `GET /api/autonomous/triage`.

## Acceptance (app side)

1. The Automaton detail view shows the card on Android and iPhone for a failed or blocked Automaton — status, failed task, first-seen, alert time, the four retry budgets, the notes — in en and at least one more locale.
2. A record that disappears (the Automaton recovers) clears the card on the next read.
3. `alert_minutes` `0` is displayed as "alert off", not as a zero-minute timer.

## Filed issue body for `dmz006/datawatch-app` [#284](https://github.com/dmz006/datawatch-app/issues/284)

> `feat(automaton-detail): triage card for failed/blocked Automata`
>
> Daemon 9.x ships one triage record per failed or blocked Automaton, readable on
> every surface (REST `GET /api/autonomous/triage`, MCP `autonomous_triage`,
> CLI `datawatch autonomous triage`, comm `autonomous triage`), with one triage
> alert per stuck episode after `autonomous.automation.triage_alert_minutes`
> (default 30; `0` = off). The PWA ships the Automaton-detail triage card in the
> same release. The Android and iPhone detail views need the same card: the
> record renders status, failed or blocked task and error, first-seen, session
> end reason, the four retry budgets, and notes; `alert_minutes` in the response
> body shows the effective alert bound. Filters: `automation_id`, `prd_id`,
> `status=failed|blocked` (a no-match filter lists empty, a wrong status is a
> 400). Wording: reuse the en source strings in the daemon repo's
> `internal/server/web/locales/en.json` triage keys under the Terminology Rule.
> Acceptance: card visible in en and one other locale on both platforms; card
> clears when the record does; `alert_minutes: 0` displayed as off.
>
> Daemon-side details: `<this repo>/docs/plans/2026-10-26-autonomous-triage-read-route.md`
> and `<this repo>/docs/api/mobile-surface.md` (section *Triage records (Automata surface)*).
