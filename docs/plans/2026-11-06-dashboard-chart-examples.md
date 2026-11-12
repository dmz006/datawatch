# Dashboard and chart examples from another project (2026-11-06)

Operator, quoted: "Ideas from another project, don't have to replicate but add examples for the dashboard and graphs and
charts we will have". Five photographs of a work-tracking observatory from another team were supplied. They are not
copied into the repository (they are photographs of a screen with other people's names and internal addresses); this
file records the ideas, in our words, and how each maps to datawatch. Nothing here has to be replicated. The Automata
that build the charts read this file with the design record.

## What the examples show

1. **Token-usage forecast.** A 12-month projection drawn as the measured line, a dashed forecast line and a shaded
   95% range, with three figures under the chart (next 12 months total, average per month, peak month). A banner marks
   it "provisional": the method (trend plus seasonality smoothing) is extrapolating a short history, the figures are
   directional and not a committed budget, and usage outside the tracked pipeline is not counted. Buttons for a link
   and a download.
2. **Consumption.** A date range with a one-click reset to the last 7 days; four headline tiles (total tokens, total
   spend, average cost per invocation, peak context window); spend by model as a donut with the values in the legend;
   spend by person as horizontal bars; tokens per phase as paired bars (input and output); spend per team as bars; and a
   phase economics table (phase, agent, input, output). One panel is labelled "mock data" where the data is not real.
3. **Feature velocity.** Toggles for by-team or all teams and for day, week, month or quarter, with a date range;
   tiles (features completed, peak period, average per period); completions per team as bars; completions over time
   stacked by team; and the average duration of each pipeline stage per team as a stacked horizontal bar, each stage its
   own colour. SVG export on each chart.
4. **Live pipeline board.** Columns for planning, building, testing, done and blocked with a count on each; a card per
   item with its id, team and a stage chip. A left menu groups the views: Monitor (overview, features, limbo, pipeline,
   anomalies, workflow runs, velocity), Tokenomics (consumption, forecast, budget), Organize (teams, users, services)
   and Docs. The top bar has a search, a release filter, a team filter and live, local and syncing status chips.

## How each idea maps to datawatch

| Example | datawatch equivalent | Built by |
|---|---|---|
| Team or pod | Project | Projects tab `d3680001`, counts `dae00001` |
| Feature | Automaton, grouped under its Automation (arc) | counts `dae00001`, tags `b7910001` |
| Pipeline stage | Task kind and phase: plan, build, verify, review, merge | Gantt `ef660001`, billing and stats `29190001` |
| Person or user | Backend, model and node (and the operator for questions) | `29190001`, ledger `24400001` |
| Release filter | datawatch release (v9.104.1 and so on) | `dae00001`, events tab |
| Tokenomics menu | Usage page cards: tokens, spend, allowance and forecast | Settings and cards `9f8f0001`, `29190001` |
| Anomalies | Stuck, hung, silent, idle-lane and failed-gate findings (today in the supervisor watch) | Events tab, dashboards `edc10001` |
| Limbo | Unlanded work and unclassified records | Housekeeping list, counts `dae00001` |

## Chart and card ideas to include (examples, in priority order)

1. **Headline tiles** for every page: total Automata, tasks done, tokens or points used, cost (unit by billing mode),
   average per Automaton, peak context. One date range with a reset to the last 7 days and quick granularity buttons
   (day, week, month, quarter) shared by every card on the page.
2. **Completions over time**, stacked by project, with a peak-period tile and an average-per-period tile
   (Automata and tasks finished per day or week; the data exists in the progress history).
3. **Duration by stage per project**: the average time of plan, build, verify, review and merge as one stacked bar
   per project, so the slow stage is visible. Includes queue wait.
4. **Consumption**: donut of spend or tokens by model and by backend; horizontal bars by project; paired input and
   output bars per stage; a phase economics table (stage, backend, input, output, time). Dollars for `api` entries,
   points for `allowance` entries, never added together.
5. **Forecast**: tokens or points over time as the measured line, a dashed projection and a shaded range, with the
   next-period total, average and peak, and a provisional banner that states the method and what is not counted. The
   example's seasonal method needs a long history; with a few weeks of data use a trend line with a wide band first
   and switch method when there are at least eight weeks, saying which method is in use.
6. **Live pipeline board** (Planning, Building, Verifying, Merging, Done, Blocked) with a count per column and a card
   per Automaton showing project, stage chip and the reason when blocked; this is the Triage and arc view.
7. **Lane utilization over time** and the **lane fill** decisions (busy, idle, waiting) beside the board.
8. **Anomalies** list: the supervisor watch findings (hung worker, silent session, idle lane, stuck merge) as a card
   with the time seen and the action taken.
9. **Honest labels**: a "provisional" banner on any forecast, a "mock data" label where a panel shows sample data,
   and a line saying what is not counted (for example work done outside the tracked Automata pipeline).
10. **Export**: SVG download for each chart, a copy-link button for the current filter, and a CSV of the table behind it.
11. **Navigation**: group the Automata views the way the example groups them (Monitor, Tokenomics, Organize), with the
    search, release, project and status chips in one top bar.

Every card says in one line what it is for, stays bounded in size on a wide screen, and reads in light and dark themes.
