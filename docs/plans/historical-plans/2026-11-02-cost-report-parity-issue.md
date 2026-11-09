# datawatch-app parity issue — cost report (READY TO FILE, NOT FILED)

This is a ready-to-file draft for a sync issue on **datawatch-app**. It covers the cost
report shipped in datawatch main: the REST read, the `cost_report` settings keys, the help
anchor and the English locale strings, plus the requested DE, ES, FR and JA translations.

Do not run `gh` from here and leave the issue URL/number blank — the supervisor files it at
the merge and records the URL back into this file.

---

## Title

`feat(parity): cost report for the Android and iOS clients`

## Body

The datawatch daemon now serves a token-first cost report: for a window and a grouping it
counts the sessions and shows tokens in / tokens out first, and a USD estimate where — and
only where — configured cost rates cover every backend in the group (an unpriced group shows
tokens and `rate_configured: false`, never a guessed dollar figure). The body also carries
`retention_days` (the 31-day ledger the figures rest on) and a `lifetime_fallback` flag.
Groupings: `automaton` (default), `automation`, `backend`, `week`. The Android and iOS
clients need the read, the settings and the strings.

### New REST route

`GET /api/cost/report` — the report over the daemon's configured defaults, with optional
query parameters:

- `group_by` — one of `automaton`, `automation`, `backend`, `week`; an absent parameter
  falls back to the `cost_report.default_group_by` setting.
- `since` / `until` — RFC3339 or `YYYY-MM-DD`; an absent `until` is now, an absent `since`
  is `until` minus the `cost_report.default_window_days` setting.

Each response row carries `group`, `sessions`, `tokens_in`, `tokens_out`, `usd_estimate`
(absent when there is none) and `rate_configured`. Render tokens first, the USD figure only
where the body carried one — never format or invent a number the daemon did not price — and
show the retention line and the lifetime-fallback note from the body. Tolerate 400/404/503
(or a daemon without the route) by hiding the card or showing a non-fatal error state.

### `cost_report` settings keys

Five keys under `cost_report.*`, read and written through `GET` / `PUT /api/config`:

| Key | Type / vocabulary | Default |
|---|---|---|
| `cost_report.default_group_by` | `automaton` · `automation` · `backend` · `week` | `automaton` |
| `cost_report.default_window_days` | whole number ≥ 1 | `7` |
| `cost_report.show_usd_estimates` | boolean | `true` |
| `cost_report.week_start` | `monday` · `sunday` | `monday` |
| `cost_report.include_peer_spend` | boolean | `false` |

One invalid value rejects the whole PUT and stores nothing — surface the daemon's message
rather than retrying.

### Help anchor

The PWA help link targets the `#cost-report` glossary entry in
`docs/datawatch-definitions.md` (full href `#docs/datawatch-definitions.md#cost-report`).
Link the app's cost report help button to the same entry; the entry documents the report,
the card controls, the settings group and the settings keys.

### English locale keys to add

Copy these keys from `internal/server/web/locales/en.json` in the datawatch repository,
with the same names (card, states, row and settings strings):

- `dash_cost_report`
- `cost_report_card_title`
- `cost_report_help_link`
- `cost_report_refresh`
- `cost_report_group_by`
- `cost_report_window_days`
- `cost_report_group_default`
- `cost_report_group_automaton`
- `cost_report_group_automation`
- `cost_report_group_backend`
- `cost_report_group_week`
- `cost_report_column_group`
- `cost_report_column_sessions`
- `cost_report_column_tokens`
- `cost_report_column_usd`
- `cost_report_tokens_io`
- `cost_report_usd_value`
- `cost_report_usd_none`
- `cost_report_key_unknown`
- `cost_report_retention_window`
- `cost_report_lifetime_fallback`
- `cost_report_empty`
- `cost_report_loading`
- `cost_report_error`
- `cost_report_rows_not_shown`
- `settings_cost_report`
- `settings_cost_report_default_group_by`
- `settings_cost_report_default_window_days`
- `settings_cost_report_show_usd_estimates`
- `settings_cost_report_week_start`
- `settings_cost_report_include_peer_spend`

### Translations requested

Please add DE, ES, FR and JA translations of the 31 English strings above for each client.
The datawatch PWA shipped the same key set in all five bundles (`en`, `de`, `es`, `fr`,
`ja`), so the PWA bundles in `internal/server/web/locales/` in the datawatch repository are
a suitable reference for the German, Spanish, French and Japanese wording.
