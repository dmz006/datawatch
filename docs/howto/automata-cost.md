---
docs:
  index: true
  topics: [automata, cost, progress, forecast, ui]
---

# Automata cost: the three metric cards

The **Progress and cost** sub-tab of the Automation view opens with three
small metric cards. Each shows one number, its unit, and a trend arrow
(**Rising**, **Falling** or **Steady**), with a **?** button that explains it.
A card with no data yet stays empty rather than showing zero.

| Card | Number | Unit line | Trend |
|---|---|---|---|
| **Progress** | Tasks finished out of every task in open work, for example `12 / 30` | tasks done of all tasks | Whether the estimated finish date is widening or converging |
| **Cost** | The total, for example `$41.20` | API-equivalent dollars | Up or down against the previous reading |
| **Forecast** | The estimated range, for example `3.5–7` | days to finish (low to high) | The same direction as the progress trend |

## The cost figure is not what you pay

The Cost card shows what the work would cost **at API prices**. On a flat
plan it is **not** what you pay; the plan's price is fixed. Read it as a
measure of how much work was done, and read the allowance cards in
[usage-calculator.md](usage-calculator.md) for how much of the week is left.
The per-provider billing label and point figures are in
[cost-report.md](cost-report.md).

## Where the numbers come from

- Progress and Forecast come from the progress numbers described in
  [automata-progress.md](automata-progress.md): counts by kind, rates per
  active day and the completion range.
- Cost comes from the cost report; see [cost-report.md](cost-report.md).

The cards are read-only and need no settings.
