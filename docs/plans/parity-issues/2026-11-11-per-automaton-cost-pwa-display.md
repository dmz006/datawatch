# Parity issue draft — Per-Automaton cost display and unknown spend (datawatch-app)

**Not filed yet.** This is a draft only. Nothing here was created on a tracker. When the
operator decides to file it, file it on **datawatch-app** as a sync issue
(`[sync] feat(cost): show full per-Automaton cost and mark unknown spend`) and record the
issue URL and number here. It complements
`2026-11-11-per-automaton-cost-attribution.md`, which covers the daemon-side attribution; this
draft covers the display side that the PWA now has and the mobile app does not.

## Affected area

Where spend is shown for an Automaton: the Overview of the Automaton detail view, and the cost
report table grouped by `automaton` (and the other groupings). Also the cost section of the
merge report and the change record, and the cost-report howto and API/MCP mapping docs that
describe these figures.

## Current behavior

The PWA now shows an Automaton's spend on the detail Overview and the cost report table carries
extra columns: cache read and created tokens, tool calls, span and active time, next to
sessions and tokens. Any figure the daemon did not report is shown as "unknown" rather than as
zero, and the report's window note and unknown-spend note are shown beneath the table. The
labels are in all five locales (en, de, es, fr, ja).

The mobile app has none of this. It shows only the older columns, treats a missing figure as
zero, shows no tool-call count and no span or active time, and does not show the window or
unknown-spend notes. An Automaton with unrecorded spend therefore reads as free.

## Expected behavior

The app should match the PWA. Wherever it shows Automaton spend it should show sessions,
tokens, cache read and created, tool calls, span and active time. A figure that is absent from
the response should read "unknown", not zero or a dash. When the response says spend is
unknown, the app should show the accompanying note, and it should show the report's window note
so a reader can tell "older than the retained window" from "no spend". Strings come from the
same locale keys as the PWA, in all five languages.

## Parity gaps this work leaves

1. **Detail columns and Overview cost.** The app lacks the cache, tool-call, span and
   active-time figures on both the Overview and the report.
2. **Unknown versus zero.** The app has no unknown state for a missing figure.
3. **Notes.** The window note and unknown-spend note are not shown.
4. **Response fields.** Before the app relies on them, confirm which response bodies carry
   `tool_calls`, `span_seconds`, `active_seconds`, `cache_read`, `cache_creation`,
   `spend_unknown` and the notes. This draft does not claim any field is missing or present in a
   body the app reads; check the daemon response and note any gap for a daemon follow-up.
5. **Per-role breakdown.** Still shown nowhere, in the PWA or the app. This is a placement
   decision for the operator, not part of this issue.

## Suggested next step

Have the operator decide whether to file. If yes, first check the cost-report and
`autonomous_prd_get` responses for the fields in gap 4 and note any that are missing, then file
the datawatch-app issue asking for the extra figures, the "unknown" wording and the notes, with
a pointer to the PWA locale keys. Leave the per-role breakdown as a separate design question.

## Story files involved

internal/server/web/app.js, the five locale files (en, de, es, fr, ja), the web tests
app-automation-detail.test.js and cost-report-view.test.js, internal/changerecord/cost.go and
its test, internal/genartifacts/merge_report.go and its test, docs/howto/cost-report.md and
docs/api-mcp-mapping.md.
