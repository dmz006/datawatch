Issue: not filed yet (record the issue URL here once it is filed)

# [sync] Dashboard cards fixed and new: work map, lane pulse, six series endpoints, console release detail, search callers, cost report reconciliation, card purposes and locale keys

## Body of the issue

This is the parity issue for the mobile app datawatch-app: datawatch-app must render the dashboard card data listed below, so that its dashboard matches the PWA.

The dashboard cards were reworked so that each card either works or is not shown. Two cards were rebuilt (Network is now the Work map, Multi-EKG is now the Lane pulse), six new series cards were added, and the Live Events, Timeline, 30-Day Activity, CI and release, Smoke Run, Search Usage and Cost report cards now read real data. This records the exact contract the mobile app should render: the new and changed REST endpoints and fields, the card purposes, the rule for hiding a card, and the locale keys. No setting was added, so there is nothing to expose in the app's settings.

### Hiding rule (all cards)

A card that is off, empty or never active is not shown. The PWA does this through its `DASH_CARD_FEATURES` table: each card has an `active()` check, and while it is false the card is left out of the grid and the Add Card picker (with `dashboard.hide_inactive_cards`, on by default, off shows a one-line reason with a link instead). Why: a card with no data tells the operator nothing and reads as broken. The app should apply the same rule per card, using the "has data" test listed with each card below.

### New REST endpoints

All are `GET`, read-only, need the dashboard read capability when called by a federation peer, and answer `405` to other methods.

| Endpoint | Query | Notes |
|---|---|---|
| `/api/dashboard/work-map` | none | Graph of active work. Fields: `generated_at`, `trace` (false means placement only, no trace store), `counts` (node counts by kind), `nodes[]`, `edges[]`, `running[]`. `nodes[]`: `id`, `kind` (`project`, `automation`, `automaton`, `task`, `session`), `label`, `status` (`ok`, `running`, `waiting`, `done`, `error`, `rate_limited`, `stalled`), `node`, `lane`, `role`, `parent`, `events`, `errors`, `tasks_omitted` (Automaton nodes, tasks beyond the per-Automaton cap). `edges[]`: `from`, `to`. `running[]`: `id`, `label`, `state`, `node`, `lane`, `role`, `updated_at`. `503` when there is neither a session store nor a trace store. Card has data when `nodes` is non-empty. |
| `/api/dashboard/lane-pulse` | `window` (Go duration, default `6h`, `1m` to `168h`, else `400`) | One series per lane and node. Top level: `generated_at`, `window_from`, `window_to`, `window_seconds`, `recording_from` (omitted when unknown; time before it is never shown as idle), `series[]`, `burn`. `series[]`: `lane`, `node`, `busy_seconds`, `waiting_seconds`, `idle_seconds`, `segments[]` (`state` is `busy`, `waiting` or `idle`, `start`, `end`; busy and waiting may overlap). `burn`: `available`, `reason` (code when unavailable: `not_wired`, `advice_unavailable`, `needs_percent`, `needs_reading`, `needs_reset`), `stale`, `points`, `pace_line`, `ahead_of_pace`, `burn_per_24h`, `week_avg_burn_per_24h`, `readings[]` (`at`, `points`, `pace_line`). Usage is in points of the weekly allowance, never dollars. Card has data when `series` is non-empty or `burn.available` is true. |
| `/api/dashboard/series/lane-utilization` | `window` (default `24h`, `1h` to `168h`) | `generated_at`, `window_from`, `window_to`, `bucket_seconds` (3600), `recording_from`, `lanes[]` with `lane` and `hours[]` (`start`, `busy_fraction` 0 to 1, null before the ledger kept history). Has data when `lanes` is non-empty. |
| `/api/dashboard/series/throughput-forecast` | `days` (default 30, 1 to 365) | `generated_at`, `days`, `direction`, `points[]` oldest first: `date`, `throughput_per_day`, `prds_last_24h` (completions in the 24 hours before the snapshot; the field name is the daemon's existing wire name), `completion_low_days`, `completion_high_days` (nullable), `known_tasks`, `estimated_tasks`. Has data when `points` is non-empty. |
| `/api/dashboard/series/verifier-health` | `window` (default `24h`, `1m` to `168h`) | `generated_at`, `window_from`, `window_to`, `window_seconds`, `recording_from`, `nodes[]`: `node`, `runs`, `waits`, `wait_total_seconds`, `wait_avg_seconds`, `run_total_seconds`, `run_avg_seconds`, `outcomes`, `passes`, `pass_rate` (0 to 1, null when no result was logged, so never shown as 0%). Has data when `nodes` is non-empty. |
| `/api/dashboard/series/release-train` | `days` (default 14, 1 to 90) | `generated_at`, `days`, `window_from`, `tags[]` (`at`, `prd_id` is the id of the Automaton that shipped, `tag`, `ok`), `ci_state` (`failing`, `pending`, `green`, `none`), `ci[]` (`repo`, `ref_kind`, `ref_name`, `workflow`, `status`, `updated_at`), `deploy` (`state` is `ok`, `failed` or `none`, `at`, `prd_id`), `empty`, `empty_reason_key`. |
| `/api/dashboard/series/review-backlog` | `limit` (default 50, 1 to 200) | `generated_at`, `limit`, `plan_count`, `question_count`, `oldest_age_seconds`, `plans[]` (`id`, `title`, `status`, `since`, `age_seconds`), `questions[]` (`question_id`, `prd_id`, `title`, `state`, `since`, `age_seconds`), `empty`, `empty_reason_key`. Oldest first. |
| `/api/dashboard/series/defects-per-day` | `days` (default 14, 1 to 90) | `generated_at`, `days`, `points[]` (`date` UTC day, `found`, `fixed`), `total_found`, `total_fixed`, `empty`, `empty_reason_key`. |
| `/api/autonomous/release-tags` | none | `tags[]` oldest first: `name` (vX.Y.Z), `commit`, `at`, `date`. Feeds the 30-Day Activity release marks. Needs the autonomous read capability; `503` when autonomous mode is off. |

For the three series that carry `empty` and `empty_reason_key` (release train, review backlog, defects per day), `empty` true means the card is not shown; `empty_reason_key` is the locale key of the one line saying why. For the other cards the app uses its own empty test from the table above and the matching `dash_card_off_*_reason` key.

The six series cards are: Lane utilization, Throughput forecast, Verifier health, Release train, Review backlog and Defects per day. They are available in Add Card and are not in the default layout; each shows once its window holds data.

### Changed REST endpoints and fields

`GET /api/dashboard/console` (the CI and release card, section `ci_release`):

| Where | Field | Notes |
|---|---|---|
| `ci_release` | `release` | new object, present when the section is available. Existing section fields are unchanged. |
| `ci_release.release` | `current_version` | the running daemon version |
| `ci_release.release` | `latest_release` | omitted when unknown |
| `ci_release.release` | `release_state` | `up_to_date`, `update_available`, `check_failed` or `not_wired` |
| `ci_release.release` | `watch_mode`, `watch_off`, `watch_link` | the repo CI watch mode, whether it is off, and the anchor of the setting to turn it on. A watch that is off does not hide the card. |
| `ci_release.release` | `ship_push`, `ship_tag`, `ship_ci_watch` | the ship policy for push, tag and CI watch |
| `ci_release.release` | `ship_last_note` | omitted when empty: the newest ship note |
| `ci_release.release` | `failing_runs[]` | at most 5, newest first, empty when the watch is off: `repo`, `workflow`, `run_id`, `status` (`failing` or `hung`), `run_url` (omitted when absent) |

`GET /api/websearch/stats` (the Search Usage card):

| Where | Field | Notes |
|---|---|---|
| top level | `callers[]` | new, largest first: `caller` (`session`, `automaton`, `mcp`, `rest`, `cli`), `total`. Existing fields (`enabled`, `cache_enabled`, `provider_names`, `summary`, `daily_series`) are unchanged. |

`GET /api/cost/report` (the Cost report card):

| Where | Field | Notes |
|---|---|---|
| `totals` | `cache_read_tokens`, `cache_write_tokens`, `local_tokens` | new, alongside `sessions`, `tokens_in`, `tokens_out` |
| `rows[]` | `cache_read_tokens`, `cache_write_tokens`, `local_tokens` | new, same meaning per row |
| `rows[]` | `api_equivalent_usd` | optional, the bundled-plan spend priced at API rates; `usd_estimate`, `billing_mode`, `billing_label` unchanged |
| top level | `reconciliation` | new, omitted when no usage reading exists: `points`, `pace_line`, `ahead_of_pace`, `api_equivalent_usd`, `implied_usd_per_point` (omitted when either side is zero), `days[]` (`date`, `points`, `api_equivalent_usd`) |

Sessions that report only cache tokens now count, so a row can have zero `tokens_in` and `tokens_out`. The USD columns are labelled API-equivalent, not a bill (two existing locale values changed, listed below).

### MCP

- `dashboard_work_map_get` returns the same object as `GET /api/dashboard/work-map`.
- `dashboard_lane_pulse_get` returns the same object as `GET /api/dashboard/lane-pulse` (optional window).
- `dashboard_series_get` takes a series name (`lane-utilization`, `throughput-forecast`, `verifier-health`, `release-train`, `review-backlog`, `defects-per-day`) and returns the same object as `GET /api/dashboard/series/<name>`.
- The existing console, web search statistics and cost report tools return the changed fields above.

### CLI

- `datawatch dashboard work-map`, `datawatch dashboard lane-pulse` and `datawatch dashboard series <name>` print the same data as the endpoints, with the window, days and limit options each endpoint takes.

### Comm channel

- `dashboard work-map`, `dashboard lane-pulse [window]` and `dashboard series <name> [value]` reply with compact lines of the same data; an unknown series name lists the six valid names.

### Card purposes

Every card has a one-line purpose and a help entry (a link to the card reference). The app should show the purpose line under the card title. Keys are `dash_card_purpose_<card>` and `dash_card_help_<card>`; the help text is one shared sentence. The series cards also carry `dash_series_purpose_*`, `dash_series_now_*` and `dash_series_next_*` lines (purpose, current state, next action).

| Key | EN value |
|---|---|
| `dash_card_purpose_console_questions` | Questions the Automata are waiting for you to answer. | same English text as a placeholder for now |
| `dash_card_purpose_console_held` | Automata that are held or blocked, and why they stopped. | same English text as a placeholder for now |
| `dash_card_purpose_console_lanes` | How many lanes are busy, waiting or idle right now. | same English text as a placeholder for now |
| `dash_card_purpose_console_usage` | Your plan usage so far, against the pace line. | same English text as a placeholder for now |
| `dash_card_purpose_console_ci_release` | Release, ship and repo CI state, with the next action. | same English text as a placeholder for now |
| `dash_card_purpose_integration_status` | The latest result from each scheduled job. | same English text as a placeholder for now |
| `dash_card_purpose_tree` | Automata with their stories and tasks, as a live tree. | same English text as a placeholder for now |
| `dash_card_purpose_events` | A live feed of Automata and session events. | same English text as a placeholder for now |
| `dash_card_purpose_sparklines` | Session activity over time, one line per session. | same English text as a placeholder for now |
| `dash_card_purpose_gantt` | Automaton tasks over the last 6 hours, as bars. | same English text as a placeholder for now |
| `dash_card_purpose_heatmap` | Automata completed and shipped each day this month. | same English text as a placeholder for now |
| `dash_card_purpose_guardrails` | Verdicts from the guardrail gates that checked recent work. | same English text as a placeholder for now |
| `dash_card_purpose_ekg` | Busy, waiting and idle time for each lane, plus usage burn. | same English text as a placeholder for now |
| `dash_card_purpose_smoke` | Progress of the release smoke and end-to-end runs. | same English text as a placeholder for now |
| `dash_card_purpose_memory_scope` | How many memories each scope holds. | same English text as a placeholder for now |
| `dash_card_purpose_websearch_usage` | Web searches by provider and by who made them. | same English text as a placeholder for now |
| `dash_card_purpose_work_map` | Projects, Automations, Automata, tasks and sessions as a graph. | same English text as a placeholder for now |
| `dash_card_purpose_lane_utilization` | How much of each lane's time was busy over the window. | same English text as a placeholder for now |
| `dash_card_purpose_throughput_forecast` | How many Automata should finish soon at the current pace. | same English text as a placeholder for now |
| `dash_card_purpose_verifier_health` | How often verifiers pass, fail or have to wait for a node. | same English text as a placeholder for now |
| `dash_card_purpose_release_train` | Tags, CI runs and deploys in the window, in order. | same English text as a placeholder for now |
| `dash_card_purpose_review_backlog` | Plans and held questions waiting for a decision. | same English text as a placeholder for now |
| `dash_card_purpose_defects_per_day` | Defects found and fixed each day. | same English text as a placeholder for now |
| `dash_card_purpose_cost_report` | Token spend grouped by Automaton, Automation, backend or week. | same English text as a placeholder for now |

Help keys, all with the same value ("What this card shows, where its data comes from and why it can be hidden."), plus `dash_card_help_label` ("Help for this card"): `dash_card_help_console_questions`, `dash_card_help_console_held`, `dash_card_help_console_lanes`, `dash_card_help_console_usage`, `dash_card_help_console_ci_release`, `dash_card_help_integration_status`, `dash_card_help_tree`, `dash_card_help_orbital`, `dash_card_help_events`, `dash_card_help_sparklines`, `dash_card_help_gantt`, `dash_card_help_heatmap`, `dash_card_help_guardrails`, `dash_card_help_ekg`, `dash_card_help_smoke`, `dash_card_help_memory_scope`, `dash_card_help_websearch_usage`, `dash_card_help_work_map`, `dash_card_help_lane_utilization`, `dash_card_help_throughput_forecast`, `dash_card_help_verifier_health`, `dash_card_help_release_train`, `dash_card_help_review_backlog`, `dash_card_help_defects_per_day`, `dash_card_help_cost_report`, `dash_card_help_label`.

### New locale keys

Every key below exists in en, de, es, fr and ja in the PWA bundles (`internal/server/web/locales/`); the de, es, fr and ja values are English placeholders until translated. Keys are listed exactly as in the locale files. Placeholders use `%1$s` / `%1$d` positional form.

| Key | EN value | de / es / fr / ja |
|---|---|---|
| `dash_console_ci_latest` | Latest release v%1$s | same English text as a placeholder for now |
| `dash_console_ci_running` | Running v%1$s — no newer release | same English text as a placeholder for now |
| `dash_console_ci_check_failed` | Release check failed | same English text as a placeholder for now |
| `dash_console_ci_not_wired` | Release check not wired in | same English text as a placeholder for now |
| `dash_console_ci_ship` | Ship: push %1$s · tag %2$s · CI watch %3$s | same English text as a placeholder for now |
| `dash_console_ci_ship_note` | Last ship note: %1$s | same English text as a placeholder for now |
| `dash_console_ci_watch_off` | repo CI watch off | same English text as a placeholder for now |
| `dash_console_ci_watch_link` | Next: turn the repo CI watch on in Settings | same English text as a placeholder for now |
| `dash_console_ci_none_failing` | No failing or hung CI runs | same English text as a placeholder for now |
| `dash_console_ci_failing` | %1$s failing or hung CI run(s) | same English text as a placeholder for now |
| `dash_console_ci_run_link` | Open run | same English text as a placeholder for now |
| `dash_work_map` | Work map | same English text as a placeholder for now |
| `dash_lane_pulse` | Lane pulse | same English text as a placeholder for now |
| `dash_lane_pulse_busy` | busy | same English text as a placeholder for now |
| `dash_lane_pulse_waiting` | waiting | same English text as a placeholder for now |
| `dash_lane_pulse_none` | No lane activity in this window. | same English text as a placeholder for now |
| `dash_lane_pulse_usage` | Usage | same English text as a placeholder for now |
| `dash_lane_pulse_points` | points | same English text as a placeholder for now |
| `dash_lane_pulse_pace_line` | pace line | same English text as a placeholder for now |
| `dash_lane_pulse_ahead` | ahead of pace | same English text as a placeholder for now |
| `dash_lane_pulse_on_pace` | on pace | same English text as a placeholder for now |
| `dash_lane_pulse_stale` | stale | same English text as a placeholder for now |
| `dash_card_off_ekg_reason` | No lane has held or waited on a lease in this window yet, and no usage reading is available — this card fills as lanes run. | same English text as a placeholder for now |
| `dash_card_off_ekg_link` | Dashboard card reference | same English text as a placeholder for now |
| `dash_lane_util` | Lane utilization | same English text as a placeholder for now |
| `dash_throughput_forecast` | Throughput forecast | same English text as a placeholder for now |
| `dash_verifier_health` | Verifier health | same English text as a placeholder for now |
| `dash_series_busy_share` | Share of each hour a lane held a lease | same English text as a placeholder for now |
| `dash_series_no_history` | Before the capacity ledger kept history | same English text as a placeholder for now |
| `dash_series_none` | No data in this window yet. | same English text as a placeholder for now |
| `dash_series_release_train_none` | Nothing shipped, watched or deployed in this window yet. | same English text as a placeholder for now |
| `dash_series_review_backlog_none` | No plans or held questions are waiting for a decision. | same English text as a placeholder for now |
| `dash_series_defects_none` | No defects were recorded in this window. | same English text as a placeholder for now |
| `dash_series_date` | Date | same English text as a placeholder for now |
| `dash_series_throughput` | Daily throughput (24 h) | same English text as a placeholder for now |
| `dash_series_range` | Completion (days) | same English text as a placeholder for now |
| `dash_series_node` | Node | same English text as a placeholder for now |
| `dash_series_runs` | Runs | same English text as a placeholder for now |
| `dash_series_waits` | Waits | same English text as a placeholder for now |
| `dash_series_avg_wait` | Avg wait | same English text as a placeholder for now |
| `dash_series_avg_run` | Avg run | same English text as a placeholder for now |
| `dash_series_pass_rate` | Pass rate | same English text as a placeholder for now |
| `dash_card_off_lane_util_reason` | No lane has held a lease in the last 24 hours — this card fills as lanes run. | same English text as a placeholder for now |
| `dash_card_off_forecast_reason` | No saved progress snapshot has a throughput or completion estimate yet — the forecast fills as Automata finish. | same English text as a placeholder for now |
| `dash_card_off_verifier_reason` | No verifier has run or waited on a node in this window — this card fills as verifiers run. | same English text as a placeholder for now |
| `dash_card_off_series_link` | Dashboard card reference | same English text as a placeholder for now |
| `dash_work_map_unavailable` | Work map unavailable | same English text as a placeholder for now |
| `dash_work_map_empty` | No work mapped yet — sessions and Automaton tasks appear here as they run. | same English text as a placeholder for now |
| `dash_work_map_running` | Running now | same English text as a placeholder for now |
| `dash_work_map_no_trace` | placement only (no trace store) | same English text as a placeholder for now |
| `dash_work_map_counts` | nodes | same English text as a placeholder for now |
| `dash_card_off_work_map_reason` | No live sessions or Automaton tasks yet — the map fills as work runs. | same English text as a placeholder for now |
| `dash_card_off_work_map_link` | Dashboard card reference | same English text as a placeholder for now |
| `dash_network_purpose` | Work in progress: projects, Automations, Automata, tasks and sessions. Click a project to expand it, a session to open it. | same English text as a placeholder for now |
| `dash_network_next` | Show the next running session | same English text as a placeholder for now |
| `dash_network_none_running` | nothing running | same English text as a placeholder for now |
| `websearch_callers` | by caller | same English text as a placeholder for now |
| `websearch_caller_session` | session | same English text as a placeholder for now |
| `websearch_caller_automaton` | Automaton task | same English text as a placeholder for now |
| `websearch_caller_mcp` | MCP client | same English text as a placeholder for now |
| `websearch_caller_rest` | REST | same English text as a placeholder for now |
| `websearch_caller_cli` | CLI | same English text as a placeholder for now |
| `dash_card_off_events_reason` | No Automata or session events yet — this card fills as work runs. | same English text as a placeholder for now |
| `dash_card_off_events_link` | Dashboard card reference | same English text as a placeholder for now |
| `dash_card_off_heatmap_reason` | No Automata completed or shipped in the last 30 days — the month fills as work lands. | same English text as a placeholder for now |
| `dash_card_off_heatmap_link` | Dashboard card reference | same English text as a placeholder for now |
| `dash_card_off_ci_release_reason` | No release or ship state reported yet — this card fills after the first release check. | same English text as a placeholder for now |
| `dash_card_off_ci_release_link` | Dashboard card reference | same English text as a placeholder for now |
| `dash_heatmap_tip_completed` | Automata completed to date: %1$d | same English text as a placeholder for now |
| `dash_heatmap_tip_shipped` | Shipped: %1$d | same English text as a placeholder for now |
| `dash_heatmap_tip_release` | Release: %1$s | same English text as a placeholder for now |
| `dash_heatmap_tip_none` | No activity recorded | same English text as a placeholder for now |
| `dash_heatmap_total` | %1$d shipped · %2$d releases · last %3$d days | same English text as a placeholder for now |
| `dash_card_off_gantt_reason` | No Automaton task has run in the last 6 hours — its bar appears here when one starts. | same English text as a placeholder for now |
| `dash_card_off_gantt_link` | Dashboard card reference | same English text as a placeholder for now |
| `dash_card_events_purpose` | Newest Automata and session events. | same English text as a placeholder for now |
| `dash_card_smoke_purpose` | Progress of release smoke and end-to-end test runs. | same English text as a placeholder for now |
| `dash_card_off_cost_report_reason` | No recorded sessions with token usage in this window — the card fills as sessions report usage. | same English text as a placeholder for now |
| `dash_card_off_cost_report_link` | Dashboard card reference | same English text as a placeholder for now |
| `cost_report_usd_api_value` | $%1$s API-equivalent | same English text as a placeholder for now |
| `cost_report_column_cache` | Cache read / write | same English text as a placeholder for now |
| `cost_report_cache_io` | %1$d read · %2$d write | same English text as a placeholder for now |
| `cost_report_column_local` | Local-model tokens | same English text as a placeholder for now |
| `cost_report_totals` | Totals: %1$d in · %2$d out · %3$d cache read · %4$d cache write · %5$d local-model | same English text as a placeholder for now |
| `cost_report_reconcile_title` | Against the usage readings | same English text as a placeholder for now |
| `cost_report_reconcile_points` | %1$s points used against a pace line of %2$s — %3$s | same English text as a placeholder for now |
| `cost_report_reconcile_ahead` | ahead of the pace line | same English text as a placeholder for now |
| `cost_report_reconcile_within` | within the pace line | same English text as a placeholder for now |
| `cost_report_reconcile_implied` | $%1$s API-equivalent over %2$s points is $%3$s API-equivalent per point | same English text as a placeholder for now |
| `cost_report_reconcile_day` | %1$s: %2$s points · $%3$s API-equivalent | same English text as a placeholder for now |
| `lane_pulse_reason_not_wired` | The usage calculator is not wired on this daemon. | same English text as a placeholder for now |
| `lane_pulse_reason_advice_unavailable` | Usage advice could not be read. | same English text as a placeholder for now |
| `lane_pulse_reason_needs_percent` | A weekly percentage is needed before the usage advice can show numbers. | same English text as a placeholder for now |
| `lane_pulse_reason_needs_reading` | Enter a weekly reading to see the usage advice. | same English text as a placeholder for now |
| `lane_pulse_reason_needs_reset` | Set the weekly reset to see the usage advice. | same English text as a placeholder for now |
| `dash_release_train` | Release train | same English text as a placeholder for now |
| `dash_review_backlog` | Review backlog | same English text as a placeholder for now |
| `dash_defects_per_day` | Defects per day | same English text as a placeholder for now |
| `dash_card_off_release_train_reason` | No tags, CI runs or deploy state in this window yet — this card fills after the first release or ship. | same English text as a placeholder for now |
| `dash_card_off_review_backlog_reason` | No plans or held questions are waiting for a decision — this card fills when one waits. | same English text as a placeholder for now |
| `dash_card_off_defects_reason` | No defects were found or fixed in this window — this card fills as defects are recorded. | same English text as a placeholder for now |
| `dash_series_tags` | Tags | same English text as a placeholder for now |
| `dash_series_ci_state` | CI | same English text as a placeholder for now |
| `dash_series_deploy_state` | Deploy | same English text as a placeholder for now |
| `dash_series_ci_runs` | Watched runs | same English text as a placeholder for now |
| `dash_series_repo` | Repository | same English text as a placeholder for now |
| `dash_series_plans` | Plans waiting | same English text as a placeholder for now |
| `dash_series_questions` | Held questions | same English text as a placeholder for now |
| `dash_series_title` | Title | same English text as a placeholder for now |
| `dash_series_age` | Age | same English text as a placeholder for now |
| `dash_series_found` | Found | same English text as a placeholder for now |
| `dash_series_fixed` | Fixed | same English text as a placeholder for now |
| `dash_series_total` | Total | same English text as a placeholder for now |
| `dash_series_status` | Status | same English text as a placeholder for now |
| `dash_series_age_hours` | %1$d h | same English text as a placeholder for now |
| `dash_series_state_failing` | failing | same English text as a placeholder for now |
| `dash_series_state_pending` | pending | same English text as a placeholder for now |
| `dash_series_state_green` | green | same English text as a placeholder for now |
| `dash_series_state_ok` | ok | same English text as a placeholder for now |
| `dash_series_state_failed` | failed | same English text as a placeholder for now |
| `dash_series_state_none` | none | same English text as a placeholder for now |
| `dash_series_purpose_lane_util` | What it is for: how much of each hour every lane spent holding work, over the last 24 hours. | same English text as a placeholder for now |
| `dash_series_now_lane_util` | %1$d lanes in the last 24 hours. | same English text as a placeholder for now |
| `dash_series_next_lane_util` | Next: if a lane stays idle while work waits, check the Automata queue and the lane settings. | same English text as a placeholder for now |
| `dash_series_purpose_forecast` | What it is for: daily throughput of finished Automata and the range of days to completion. | same English text as a placeholder for now |
| `dash_series_now_forecast` | %1$d days of throughput in the window. | same English text as a placeholder for now |
| `dash_series_next_forecast` | Next: if the completion range is wide, add capacity or narrow the Automata in progress. | same English text as a placeholder for now |
| `dash_series_purpose_verifier` | What it is for: how often each verifier node ran or waited, and how often its runs passed. | same English text as a placeholder for now |
| `dash_series_now_verifier` | %1$d nodes in the last 24 hours. | same English text as a placeholder for now |
| `dash_series_next_verifier` | Next: for a node with many waits or a low pass rate, check that node’s connection and verifier settings. | same English text as a placeholder for now |
| `dash_series_purpose_release_train` | What it is for: the tags cut, the CI state and the deploy state of the current release. | same English text as a placeholder for now |
| `dash_series_now_release_train` | %1$d tags and %2$d watched CI runs. | same English text as a placeholder for now |
| `dash_series_next_release_train` | Next: if CI is failing, open the failing run before cutting the next release. | same English text as a placeholder for now |
| `dash_series_purpose_review_backlog` | What it is for: plans and held questions waiting for an operator decision, oldest first. | same English text as a placeholder for now |
| `dash_series_now_review_backlog` | %1$d plans and %2$d held questions are waiting. | same English text as a placeholder for now |
| `dash_series_next_review_backlog` | Next: answer the oldest held question, or decide the oldest waiting plan. | same English text as a placeholder for now |
| `dash_series_purpose_defects` | What it is for: defects found and fixed per day, so you can see whether the backlog is shrinking. | same English text as a placeholder for now |
| `dash_series_now_defects` | %1$d found and %2$d fixed in the last 14 days. | same English text as a placeholder for now |
| `dash_series_next_defects` | Next: if more are found than fixed, triage the open defects into an Automaton. | same English text as a placeholder for now |

### Changed locale values (same key, new wording)

| Key | Old EN value | New EN value |
|---|---|---|
| `cost_report_column_usd` | USD (est.) | API-equivalent USD |
| `cost_report_column_api_equiv` | API-equiv (unbilled) | API-equivalent USD (unbilled) |

Please send translations of the new keys into DE, ES, FR and JA (use the app's own word for Automaton: de Automat, es autómata, fr automate, ja オートマトン); the PWA bundles will then mirror them in place of the placeholders.

### Acceptance (mobile)

- Work map renders the nodes by kind with their status, the edges, and the running list; `trace` false shows the placement-only note; `503` shows "Work map unavailable".
- Lane pulse renders busy, waiting and idle per lane and node, and the burn panel in points against the pace line; an unavailable burn shows the `lane_pulse_reason_*` text for its `reason` code; `stale` is labelled.
- Each of the six series cards renders its data, shows its own empty line from `empty_reason_key` (or the `dash_card_off_*_reason` key) and is not shown while it has no data.
- The CI and release card shows current and latest version, the release state, the ship policy, the failing runs with links, and the "turn the repo CI watch on" link when `watch_off` is true.
- Search Usage shows the per-caller counts; Cost report shows the cache and local-model columns and the reconciliation block when present.
- Every card shows its purpose line; every new string is localized in EN, DE, ES, FR and JA.
- Unknown or absent new fields render exactly as today, so the app stays forward-compatible.

## Filing record

Not filed. The draft is ready to file against the mobile app repository once checked for an open issue about the dashboard cards (search first, comment on an existing issue rather than filing a second). Record the issue URL and date here when filed.
