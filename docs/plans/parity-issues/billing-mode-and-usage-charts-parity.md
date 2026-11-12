# [parity] Billing mode setting, unit-separated cost display and the four usage chart cards (datawatch-app follow-up)

**Not filed yet.** — this is the tracked draft; the GitHub issue is opened on the
`dmz006/datawatch-app` repository when the operator files it. Filing the issue does
not change a line of this repo. Draft written 2026-11-12.

Parent release: the daemon release that carries the per-LLM billing mode and
the usage chart cards (the release number is recorded here when the issue is
filed).

**Android and iPhone are tracked here, not changed here.** The change below
landed on the daemon, the CLI, the comm channel, MCP, YAML/config and the web
(PWA) settings and Automata pages. Nothing in `datawatch-app` was touched; this
file is the record the app work follows.

## Affected area

Three related operator surfaces, all driven by one new per-LLM field, `billing_mode`:

1. **The billing mode setting.** Every LLM registry entry now carries a
   `billing_mode` that says how that entry is paid for.
2. **The unit-separated cost display.** The cost report, the cost-over-time
   chart and the combined usage report keep each kind of spend in its own unit
   and never add dollars to points.
3. **Four usage chart cards** on the Automata page, plus the Events entries
   that explain a card that is not drawn.

The operator-facing walk-through is in
[docs/howto/cost-report.md](../../howto/cost-report.md) (the **What api,
allowance and local mean**, **Dollars and points are never added together**,
**The chart cards on the Automata page** and **Billing mode and the usage charts
on every surface** sections) and
[docs/howto/llm-registry.md](../../howto/llm-registry.md) (**Set the billing mode
of an LLM**). The unit labels on the usage page are in
[docs/howto/usage-calculator.md](../../howto/usage-calculator.md) (**Unit labels
on the usage page**).

## What changed in datawatch

### 1. The billing mode setting row and its help

- **Field.** `billing_mode` on each LLM registry entry: `api` (metered, every
  call billed per token), `allowance` (a flat plan allowance that resets on a
  schedule; calls draw on it and nothing is billed per call) or `local` (local
  inference on your own hardware, no per-call charge). An empty value
  resolves, in this order, from the entry's own value, then the provider's
  `usage.billing` profile (`bundled_plan` reads as `allowance`, `api_metered`
  as `api`, `free_local` as `local`), then the kind default (`claude-code` is
  `allowance`; `ollama`, `openwebui` and `shell` are `local`; every other kind
  is `api`).
- **Migration.** When the daemon loads its configuration it writes the
  resolved value onto every entry that has none. A cost report row reads the
  same before and after; an entry that already has a value is never touched;
  a second load changes nothing.
- **Web settings row.** Settings → Compute → LLM edit form, in the **Budget**
  section directly after the free-text **Billing group** field: a **Billing mode** select with the options
  `—` (blank, nothing stored), `api`, `allowance` and `local`, and a **?**
  help link to the cost report how-to whose text is `billing_mode_help`.
  Saving a blank value omits the field so the daemon resolves it again.
- **Every surface.** REST `billing_mode` on `POST /api/llms`,
  `PUT /api/llms/{name}` and `PUT /api/config` (`llms.<name>.billing_mode`),
  returned by `GET /api/llms`, `GET /api/llms/{name}` and `GET /api/config`
  (an unknown value is a `400` naming the allowed values); MCP `llm_add` and
  `llm_update`, read back by `llm_get` and `llm_list`; CLI
  `datawatch llm add|update <name> --billing-mode <mode>` and
  `datawatch config set llms.<name>.billing_mode <mode>`; comm channel
  `llm add|update <name> billing_mode=<mode>` and
  `configure llm.<name>.billing_mode=<mode>`; YAML `billing_mode:` on each
  entry under `llms`.
- **Readers of the mode.** The budget, the burn guard and the usage advice now
  read each LLM's resolved billing mode. `GET /api/usage/advice` (and the MCP
  `usage_advice` tool) carries `unit_labels`, a map of LLM name to a label key:
  `usage_unit_dollars` for `api`, `usage_unit_points` for `allowance`, none for
  `local`. The advice figures themselves are unchanged. The web usage cards do
  not draw `unit_labels` yet; today the field is for REST and MCP clients.

### 2. The unit-separated cost display

- **Cost report rows** (`GET /api/cost/report`) carry `billing_mode`
  (`bundled_plan`, `api_metered`, `free_local` or `mixed`) and `billing_label`.
  Dollars and points are never added together: a row that spans billing units
  reads `mixed`, has no `usd_estimate`, and carries `unit_totals`, one entry
  per unit in the order `dollars`, `points`, `none`. Each entry has `unit`,
  `billing_mode`, `sessions`, `tokens_in`, `tokens_out`, `active_seconds` and
  the figure for its unit (`dollars`, or `points` and `percent`).
  `totals_by_unit` repeats the per-unit totals across all rows. A bundled-plan
  row carries `api_equivalent_usd`, `estimated_usage_points` and
  `share_of_monthly_price` instead of a `usd_estimate`.
- **Cost over time** draws one chart per unit; each axis is labelled with its
  unit and billing mode (`cost_charts_unit_dollars` / `cost_charts_unit_points`
  with `cost_charts_mode_api_metered`, `cost_charts_mode_bundled_plan` or
  `cost_charts_mode_free_local`). A `local` entry has tokens only.
- **Combined usage report** lists money per unit on its own line, never as one
  sum.
- **No figure is invented.** A money figure is omitted where no rate or
  `monthly_price` is set, and all money is omitted when
  `cost_report.show_usd_estimates` is off; the chart then draws tokens and says
  so (`cost_charts_money_off`).

### 3. The four chart cards

The Automata page shows the combined usage report as four cards in the cost
report area of its **Progress and cost** tab. A row of controls above them sets
the grouping (Project, Automation or Automaton), the window (7 days to 8
weeks) and whether the first chart shows tokens or money. Every card carries a
one-line purpose, a state (`Showing`, `No data` or `Off`), a reason, a next
action and a **?** that opens the cost report how-to.

| Card | Reads | What it draws |
|------|-------|---------------|
| **Cost over time** | `GET /api/cost/series` | A line per group: tokens, or money (one chart per billing unit) |
| **Tool usage** | `GET /api/cost/tool-usage` | A line per group: tool call counts. Counts only, never tool names (`cost_charts_tool_names_note`) |
| **Retries and failure categories** | `GET /api/cost/failures` | Failed attempts per week stacked by category (`infra`, `content`, `uncategorized`), and a table of attempts, tasks, retries and top category per group |
| **Combined usage report** | The three routes above, joined | One table: tokens, money (per unit), tool calls, failed attempts and retries per group |

- **Route parameters.** `group_by` and `window` on all three. The series and
  tool-usage routes also take `bucket` (`hour` or `day`; hour up to a 48-hour
  window, day beyond) and group by `automaton`, `project` or `model`; the
  failures route groups by `project`, `automation` or `automaton` and takes
  weekly windows (`30d`, `12w`). A bad value is a `400`; no manager is a
  `503`. Full contracts: `docs/api/cost.md` and the OpenAPI file.
- **Limits.** At most six lines and sixty points per chart line, at most eight
  rows per table; the card says when more groups exist
  (`cost_charts_more_groups`, `cost_charts_more_rows`). Cost over time and tool
  usage are not recorded per Automation, so under that grouping only the
  failures chart and the combined table are drawn
  (`cost_charts_reason_not_grouped`).
- **A card that cannot be drawn.** A card whose data is empty, or whose view
  the daemon cannot supply, is not drawn. Its reason and next action appear as
  an entry at the top of the **Events** tab instead
  (`cost_charts_event_summary`, `cost_charts_event_detail`), and the page says
  how many cards are hidden (`cost_charts_hidden_note`).
- **Other surfaces for the same report.** CLI
  `datawatch cost charts [--group-by <g>] [--window <d>] [--json]`, comm channel
  `cost charts [group_by=<g>] [window=<d>]`, MCP `cost_charts` (read-only). The
  default window comes from `cost_report.default_window_days`; the report has
  no setting of its own.

## Parity surface

| Surface | Billing mode | Usage charts | Status |
|---------|--------------|--------------|--------|
| REST | `billing_mode` on the LLM routes and config; row fields on `GET /api/cost/report` | `GET /api/cost/series`, `/tool-usage`, `/failures` | Changed |
| MCP | `llm_add`, `llm_update`, `llm_get`, `llm_list`; row fields on `cost_report` | `cost_charts` (read-only) | Changed |
| CLI | `--billing-mode`, `config set`, the `totals by unit` block of `cost report` | `datawatch cost charts` | Changed |
| Comm channel | `llm add\|update … billing_mode=`, `configure llm.<name>.billing_mode=` | `cost charts` | Changed |
| YAML / config | `billing_mode:` on each `llms` entry | No setting of its own | Changed (charts have no setting) |
| PWA | Billing mode select and help on the LLM edit form | Four chart cards on the Automata page, reasons in Events | Changed |
| Android | Not changed here; tracked in this issue | Not changed here; tracked in this issue | Tracked |
| iPhone | Not changed here; tracked in this issue | Not changed here; tracked in this issue | Tracked |

## Current behavior

- The app talks to the same daemon and can already receive `billing_mode` on
  LLM entries and the new row fields on the cost report, but none of it is
  reachable from the app's screens.
- **Settings.** The app's LLM entry editor has no **Billing mode** control, no
  help link for it, and a blank-or-unknown value is not handled. An operator
  on a phone cannot see or change how an entry is paid for.
- **Cost display.** The app's cost report view has no `mixed` row, no
  `unit_totals` and no per-unit totals, so a row that spans dollars and points
  cannot be read there as the daemon states it.
- **Chart cards.** The app has none of the four chart cards, the grouping,
  window and tokens-or-money controls, or the Events entries that explain a card
  that is not drawn.
- **Text.** The 73 locale keys listed below exist in the five web bundles
  (en, de, es, fr, ja) and in none of the app's string resources.

## Expected behavior

- The app's LLM entry editor gets a **Billing mode** control with the options
  blank, `api`, `allowance` and `local`, the same help text (`billing_mode_help`)
  and a link to the cost report how-to. A blank selection stores nothing and the
  daemon resolves the mode; an unknown value is shown with the daemon's `400`
  message.
- The app's cost report view reads `billing_mode`, `billing_label`,
  `unit_totals` and `totals_by_unit` and draws each unit on its own line. It
  must never add dollars to points, never render an `api_metered` row as a
  bundled-plan row (or the reverse), and never invent a figure the daemon did not
  return: an omitted money figure stays omitted and the card says why.
- The app draws the four chart cards (cost over time, tool usage, retries and
  failure categories, combined usage report) with the same controls, limits,
  states, reasons and next actions, and the same per-unit axes and labels.
  Idiomatic delivery per platform is fine; capability parity is the requirement.
- A card that cannot be drawn surfaces its reason and next action in the app's
  Events view, as the web does.
- The app's string resources carry the same 73 keys in all five locales, with
  identical English text and the translations read from the web bundles.
- The usage page may read `unit_labels` from `GET /api/usage/advice` and label
  its figures `$ (dollars)` or `pts (points)` by LLM; the web cards do not yet,
  so this is optional until they do.

## New locale keys (73, identical in all five bundles)

Source: `internal/server/web/locales/{en,de,es,fr,ja}.json`. The same 73 keys
are present in each of the five bundles with the same names (checked key-for-key
against `en.json`); the English text is shown here and the de, es, fr and ja
values are read from the bundles at the same key. Placeholders `%1$s`, `%2$s`,
`%3$s` are positional and keep their order in every locale.

### Billing mode field (5)

| Key | English text |
|-----|--------------|
| `billing_mode_label` | Billing mode |
| `billing_mode_option_api` | api |
| `billing_mode_option_allowance` | allowance |
| `billing_mode_option_local` | local |
| `billing_mode_help` | api: each call is billed per token. allowance: calls draw on a flat plan allowance. local: runs on your own hardware at no per-call charge. The cost report groups spend by this word. |

### Usage unit labels (2)

| Key | English text |
|-----|--------------|
| `usage_unit_dollars` | $ (dollars) |
| `usage_unit_points` | pts (points) |

### Chart cards: heading, titles, purposes (11)

| Key | English text |
|-----|--------------|
| `cost_charts_heading` | Cost report charts |
| `cost_charts_help_tip` | Open the cost report howto — what each chart shows and how to read it |
| `cost_charts_help_label` | Help |
| `cost_charts_title_cost` | Cost over time |
| `cost_charts_title_tools` | Tool usage |
| `cost_charts_title_failures` | Retries and failure categories |
| `cost_charts_title_report` | Combined usage report |
| `cost_charts_purpose_cost` | Tokens, or money in one unit at a time, over time for each group. |
| `cost_charts_purpose_tools` | How many tool calls each group made over time. Counts only, never tool names. |
| `cost_charts_purpose_failures` | Failed attempts per week by category, and the automatic retries each group used. |
| `cost_charts_purpose_report` | One table joining tokens, money, tool calls, failed attempts and retries for each group. |

### Chart cards: controls and units (21)

| Key | English text |
|-----|--------------|
| `cost_charts_label_state` | State |
| `cost_charts_label_reason` | Reason |
| `cost_charts_label_next` | Next action |
| `cost_charts_label_group` | Group by |
| `cost_charts_label_window` | Window |
| `cost_charts_label_metric` | Show |
| `cost_charts_refresh` | Refresh |
| `cost_charts_loading` | Loading the cost charts… |
| `cost_charts_group_project` | Project |
| `cost_charts_group_automation` | Automation |
| `cost_charts_group_automaton` | Automaton |
| `cost_charts_metric_tokens` | Tokens |
| `cost_charts_metric_money` | Money |
| `cost_charts_unit_tokens` | tokens |
| `cost_charts_unit_dollars` | $ (dollars) |
| `cost_charts_unit_points` | pts (points) |
| `cost_charts_unit_tool_calls` | tool calls |
| `cost_charts_unit_attempts` | failed attempts |
| `cost_charts_mode_bundled_plan` | bundled plan, API-equivalent, not billed |
| `cost_charts_mode_api_metered` | API metered, billed per use |
| `cost_charts_mode_free_local` | local, no charge |

### Chart cards: state, reason, next action (17)

| Key | English text |
|-----|--------------|
| `cost_charts_state_showing` | Showing |
| `cost_charts_state_empty` | No data |
| `cost_charts_state_off` | Off |
| `cost_charts_reason_showing` | %1$s groups, window %2$s. |
| `cost_charts_reason_report` | %1$s groups, window %2$s, joined from the three charts. |
| `cost_charts_reason_report_partial` | %1$s groups, window %2$s. Cost and tool usage are not recorded per Automation, so those columns are blank. |
| `cost_charts_reason_empty_cost` | No spend was recorded in this window. |
| `cost_charts_reason_empty_tools` | No tool calls were recorded in this window. |
| `cost_charts_reason_empty_failures` | No failed, stalled or cancelled attempts in this window. |
| `cost_charts_reason_empty_report` | None of the three charts has data for this grouping and window. |
| `cost_charts_reason_not_grouped` | This view is not recorded per Automation, only per Project or Automaton. |
| `cost_charts_reason_unavailable` | The daemon could not supply this view. %1$s |
| `cost_charts_next_widen` | Widen the window. |
| `cost_charts_next_group` | Group by Project or Automaton. |
| `cost_charts_next_retry` | Check that the daemon is running, then press Refresh. |
| `cost_charts_next_showing` | Change the grouping or window to look closer. |
| `cost_charts_next_money` | Set rates or the monthly price in Settings, or turn USD estimates on. |

### Chart cards: notes, table columns, events (17)

| Key | English text |
|-----|--------------|
| `cost_charts_money_off` | Money is not shown: no rate or monthly price is set, or USD estimates are off. Tokens are drawn instead. |
| `cost_charts_more_groups` | Showing the top %1$s of %2$s groups. |
| `cost_charts_more_rows` | Showing %1$s of %2$s rows. |
| `cost_charts_tool_names_note` | Only counts are recorded, not the names of the tools. |
| `cost_charts_hidden_note` | %1$s charts have no data and are not shown. The reasons are in Events. |
| `cost_charts_chart_aria` | Chart: %1$s |
| `cost_charts_col_group` | Group |
| `cost_charts_col_tokens` | Tokens |
| `cost_charts_col_money` | Money |
| `cost_charts_col_tool_calls` | Tool calls |
| `cost_charts_col_attempts` | Failed attempts |
| `cost_charts_col_tasks` | Tasks |
| `cost_charts_col_retries` | Retries |
| `cost_charts_col_top_category` | Top category |
| `cost_charts_total` | Total |
| `cost_charts_event_summary` | %1$s is not shown |
| `cost_charts_event_detail` | Grouped by %1$s, window %2$s. State: %3$s. |

## Suggested next step

1. Add the 73 keys above to the Android and iOS string resources in all five
   locales (en, de, es, fr, ja), copying the translations from the web bundles.
2. Add the **Billing mode** control, its help text and its help link to the LLM
   entry editor on Android and iOS, storing nothing for a blank value.
3. Read `billing_mode`, `billing_label`, `unit_totals` and `totals_by_unit` in
   the cost report view and draw each billing unit separately, with the `mixed`
   row and the assumed-default label read verbatim.
4. Build the four chart cards (cost over time, tool usage, retries and failure
   categories, combined usage report) with the grouping, window and
   tokens-or-money controls, the six-line, sixty-point and eight-row limits, the
   state, reason and next action on each, and per-unit money axes.
5. Show the reason and next action of any card that cannot be drawn as an entry
   in the app's Events view, with a hidden-cards note on the page.
6. Optionally label the usage page figures from `unit_labels`.
7. Add `docs/howto/cost-report.md` and `docs/howto/llm-registry.md` to the app's
   documentation link list and wire them as the help targets for the new
   controls.

## Acceptance (app)

- On both Android and iPhone an operator can set an LLM entry's billing mode to
  `api`, `allowance` or `local`, or leave it blank, and the saved value reads
  back on the next load.
- A cost report row that spans billing units reads `mixed` with one line per
  unit and no combined money figure.
- The four chart cards draw with the same grouping, window and metric choices
  as the web, and a card with no data or no daemon answer is replaced by an
  Events entry carrying its reason and next action.
- Every label, help text, reason and next action reads from the 73 locale keys,
  in all five locales, with no hard-coded English.

## Filing record

- Filed: — (pending; issue number / URL once opened)

## Operator review before filing

Checked against the code on 2026-11-12: the 73 locale keys and their English
text match the five bundles key for key; the route, CLI, comm and MCP names,
limits and error codes match the code. These still need the operator:

1. **File the issue.** This draft is not filed. Open it on `dmz006/datawatch-app`,
   then fill the release number under *Parent release* and the *Filing record*
   above, and link the issue from the parity table in `docs/parity-status.md` if
   that table tracks it.
2. **`unit_labels` is not drawn on the web yet.** The usage cards do not read it,
   so the optional last expected-behavior item has no web reference to copy.
   Decide whether the web cards get it before the apps do.
3. **Two stale links outside this story.** `docs/howto/v7-compute-migration.md`
   still links `compute-mode.md`, which does not exist (the page is
   `compute-nodes.md`). The same bad links in `docs/howto/llm-registry.md` were
   fixed here; that file was not in scope.
4. **Tracker IDs in `docs/cursor-mcp.md`.** The tools table carries older ids
   (for example on the exit hook and result store rows). That file is not a
   surface the internal-refs check covers, so they were left alone.
5. **Howto-coverage check fails on two other howtos.** `scripts/check-howto-coverage.sh`
   reports `docs/howto/automata-events.md` and `docs/howto/merge-shadow.md` as
   having no `exec_steps:` and not being on the LLM-only list. Neither is part
   of this change; the release smoke will block until they are given steps or
   listed.
