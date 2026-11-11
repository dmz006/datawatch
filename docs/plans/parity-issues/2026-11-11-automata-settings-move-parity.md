Issue: pending (filed from the operator host)

Issue URL: pending (filed from the operator host)

**Parity:** The Compose app mirrors the PWA (and the REST/MCP surface behind it). This issue covers the Automata configuration that moved into Settings under Automata (the usage reading entry and extra credit, the merge pipeline settings and the decision record settings), the removal of the Setup and policy tab and the move of the supervisor loop runs to Events, the Decision records card that now hides when it is empty, the usage page rebuilt as a one-sentence verdict plus three fixed-size usage cards, the three dashboard card types and the Resources row, the redesigned lanes card and progress and cost cards, and every new locale key.
**PWA parity rule:** datawatch-app has to reflect the same surface: settings rows, the cards and their states, and the locale keys below.
**Related PWA plan:** `docs/plans/2026-11-06-automata-pages-redesign.md` — the Settings move decision (all configuration lives in Settings) and the usage page decision (three small fixed-size cards with a verdict).

## Body of the issue

The Automata pages were reorganised on the PWA side so that nothing editable is left on them. What the operator reads or acts on stays on its Automata page; configuration lives in Settings:

- **Reading entry and extra credit in Settings.** Settings under Automata has a "Usage reading and extra credit" entry. It uses the same forms and the same endpoints as the old usage page, so lane advice and the burn guard see a saved figure at once. The usage page now links to it ("Save a reading or extra credit in Settings") and no longer holds the forms.
- **Merge pipeline and decision record settings in Settings.** The merge pipeline settings (`autonomous.merge_pipeline.*`, with the per-project overrides) and the decision record settings (`autonomous.decisions.*`) are edited in Settings under Automata, not on the Automata pages.
- **Setup and policy tab removed.** The tab is gone. What it showed that is not configuration, the supervisor loop runs and counts, now appears on Events. Events carries a pointer line, "Automata configuration is in Settings under Automata".
- **Decision records card hidden when empty.** The card on Events shows only when there is at least one decision record.
- **Usage page: verdict plus three fixed-size cards.** One sentence above the cards says whether the week is over budget, within budget or unknown. Below it sit three small cards that keep a fixed size and do not stretch with the page: weekly allowance, pace line and burn per day. Each card states what it is for, its state, why, and what to do next. Explanations sit behind a "?" help control, with a link to the usage calculator guide. With no readings the page shows an empty state that points to Settings.
- **Three dashboard card types and the Resources row.** The same three cards are dashboard card types (`dash_card_usage_allowance`, `dash_card_usage_pace`, `dash_card_usage_burn`) and also appear as a row on the Resources tab.
- **Redesigned lanes and progress and cost cards.** The lanes card shows one row per lane with its node, its state and what it runs; the bar is full when the lane works, half when it waits on a verifier and empty when idle. The progress and cost cards show a title, a value, a unit, a trend word (Rising, Falling, Steady) and a help line; a forecast card shows the days-to-finish range.

The operator's words on these changes (from the design record):

> q7, move to settings all configuration should be in settings - also are all
> configuratable options fully aligned with parity rule? hopefully ??

> if it is an output/log/details then A in housekeeping; if it is configuration
> details then it should be in settings

> A recommendation is good

On the usage page the operator wrote that the usage calculator is "i don't like at all", wanted "smaller graphs that do not expand with the page", and that "the graphs should be dashboards". On progress and cost: "good but again not made for human consumption and could be made to be nicer".

The Compose app must reflect all of it: configuration only in its settings screen, no editable configuration on its Automata screens, the usage verdict and three fixed-size cards, the dashboard card types, the Resources row, and the new lanes, progress and cost cards.

### Settings keys and defaults

The moved settings keep their existing keys and defaults; only where they are edited changed. Every surface (REST `config_set`, CLI `config set`, comm's `configure` and the PWA settings panel) reaches the same validation.

| Key group | Where it is edited now | Notes |
| --- | --- | --- |
| `usage.calculator.*` (policy, extra credit, `safety_points`, `max_lanes`, `auto_lanes`, `auto_lanes_max`, reset weekday/time/zone) | Settings under Automata, "Usage reading and extra credit" and the usage policy rows | Reading entry and extra credit post to the same endpoints as before; defaults unchanged. |
| `autonomous.merge_pipeline.*` | Settings under Automata | Per-project overrides are edited in Settings with a project dropdown; defaults unchanged. |
| `autonomous.decisions.*` | Settings under Automata | Defaults unchanged (`autonomous.decisions.enabled` stays on). |

No new setting keys were added by this change.

### Locale keys

Every locale file (`en`, `de`, `es`, `fr`, `ja`) gets the same new keys. English reference below, read from the diff of the five locale bundles against the branch base; `de`, `es`, `fr`, `ja` carry translations of the same set. The app should carry the same strings, translated the same way it translates neighbouring rows.

| Key | English |
| --- | --- |
| `automata_events_config_in_settings` | Automata configuration is in Settings under Automata |
| `settings_usage_entry_title` | Usage reading and extra credit |
| `settings_usage_entry_intro` | Save a usage reading or the extra credit figure here. These are the same forms and the same endpoints as the usage page, so Automata lane advice and the burn guard see the result at once. |
| `settings_usage_entry_saved` | Saved. The advice now uses this figure. |
| `settings_usage_entry_help` | Usage reading and extra credit help |
| `usage_page_help_label` | What is this? |
| `usage_page_help_docs` | Read the usage calculator guide |
| `usage_page_verdict_over` | Over budget: at the recent burn you will use about %2$s% of the allowance by the reset (%1$s% used now). |
| `usage_page_verdict_within` | Within budget: you have used %1$s% and the burn leads to about %2$s% by the reset. |
| `usage_page_verdict_unknown` | No verdict yet: save another reading. |
| `usage_page_empty_body` | No readings yet. Save this week's weekly percentage in Settings under Usage reading and extra credit. |
| `usage_page_settings_link` | Save a reading or extra credit in Settings |
| `usage_page_allowance_title` | Weekly allowance |
| `usage_page_allowance_help` | How many points of this week's allowance are used, out of the ceiling. The ceiling is 100 plus any extra credit you bought. |
| `usage_page_allowance_state` | %1$s of %2$s points used |
| `usage_page_allowance_why` | Ceiling %1$s (100 plus %2$s extra points) |
| `usage_page_allowance_next_over` | Run fewer lanes or wait for the reset. |
| `usage_page_allowance_next_ok` | Nothing to do. |
| `usage_page_pace_title` | Pace line |
| `usage_page_pace_help` | Where your usage stands against the straight line that would use the whole allowance exactly at the reset. |
| `usage_page_pace_state_ahead` | %1$s% used, pace line %2$s%: ahead of the line |
| `usage_page_pace_state_behind` | %1$s% used, pace line %2$s%: %3$s points of headroom |
| `usage_page_pace_why` | %1$s days since the reset, %2$s days left |
| `usage_page_pace_next_ahead` | Slow down: you are ahead of the line. |
| `usage_page_pace_next_ok` | Keep the current lanes. |
| `usage_page_burn_title` | Burn per day |
| `usage_page_burn_help` | How many points of the allowance are used each day, with one bar for each day since the reset. |
| `usage_page_burn_state` | %1$s points per day (recent) |
| `usage_page_burn_why` | Week average %1$s points per day |
| `usage_page_burn_next_over` | Try %1$s lane(s) to stay inside the allowance. |
| `usage_page_burn_next_ok` | Keep the current lanes. |
| `dash_card_usage_allowance` | Weekly allowance |
| `dash_card_usage_pace` | Pace line |
| `dash_card_usage_burn` | Burn per day |
| `lane_fill_node` | Node: %1$s |
| `lane_fill_node_none` | Node: not reported |
| `lane_fill_help_text` | One row per lane: its node, its state and what it runs. The bar is full when the lane works, half when it waits on a verifier and empty when idle. |
| `metric_trend_up` | Rising |
| `metric_trend_down` | Falling |
| `metric_trend_flat` | Steady |
| `metric_progress_title` | Progress |
| `metric_progress_unit` | tasks done of all tasks |
| `metric_progress_help` | Tasks finished out of every task in open work. The trend shows whether the estimated finish date is widening or converging. |
| `metric_cost_title` | Cost |
| `metric_cost_unit` | API-equivalent dollars |
| `metric_cost_help` | What the work would cost at API prices. It is not what you pay on a flat plan. |
| `metric_forecast_title` | Forecast |
| `metric_forecast_unit` | days to finish (low to high) |
| `metric_forecast_help` | The estimated range of days until open work is done. |

The `settings.self_config_recent_unavailable` key was only re-punctuated in the bundles and is not new.

## Acceptance

- The app's settings screen has a Usage reading and extra credit entry under Automata, the merge pipeline settings and the decision record settings; the Automata screens hold no editable configuration.
- The Setup and policy screen is gone; supervisor loop runs and counts show on Events, and Events points to Settings for configuration.
- The Decision records card is hidden when there are no records.
- The usage screen shows the verdict sentence and the three fixed-size cards (weekly allowance, pace line, burn per day) with state, why and next step, a help control, and an empty state that links to Settings.
- The three usage cards are available as dashboard card types and as a row on the Resources screen.
- The lanes card and the progress, cost and forecast cards match the redesign, including the trend words.
- The app carries every locale key above for every locale it ships.

## Filing record

Not filed yet. The operator files it from the operator host, because a task cannot call the hosting CLI:

```
gh issue create --repo dmz006/datawatch-app \
  --title "Parity: Automata settings move and the usage dashboards" \
  --body-file docs/plans/parity-issues/2026-11-11-automata-settings-move-parity.md
```
