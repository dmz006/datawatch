# Usage and cost calculator: design pass with three mockup variants

**Date:** 2026-10-23

**Version:** current main when written (no release number is recorded in this document)

**Status:** Design only. No implementation. The operator compares the variants and chooses.

**Scope:** documents and mockup pages under docs/plans/ only

## Purpose and scope

The calculator answers one question on every usage reading: is the current burn within or over budget for the week, and what does each choice of Claude lanes cost against the rest of the week? The subscription quota cannot be queried. Nothing in the codebase reads the remaining allowance, and the only signals are the operator's own readings of the usage screen plus what can be inferred from the time between readings. [docs/plans/2026-10-14-concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md) section 4a records this. The calculator therefore runs on operator-reported readings and inference.

The weekly ceiling is the real constraint. The session window is a rolling window that resets within hours; it is shown as a secondary line and never feeds the verdict. The snapshot log in the same concurrency design, section 4b, records a point where the session meter read 15 percent while the weekly figure read 99 percent, which is why the two numbers are kept apart.

The calculator exists because of a supervisor decision that went wrong. On 2026-10-23 the supervisor held all Claude lanes from 02:40 to 17:09 on its own projection. The concurrency design records that hold as the wrong call. The operator's decision of the same day sets the rule this design follows:

> the usage policy is the operator's choice, never a standing supervisor hold.

> On every usage reading the supervisor advises whether the current burn is within or over budget for the week and lets the operator choose how many lanes to run (for example one, two or three Claude lanes). The supervisor never stops all Claude lanes on its own projection.

Those two sentences are quoted from the operator decision at the end of [docs/plans/2026-10-14-concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md). From them this design takes four rules:

- The usage policy is the operator's choice. Operators differ, so the calculator offers three policies and never picks one for them.
- On every reading the calculator says whether the current burn is within or over budget for the week.
- The operator chooses how many Claude lanes run. The calculator advises a count; it does not set one.
- The supervisor never stops all Claude lanes on its own projection.

**In scope for this pass:**

- Mockups for the PWA, starting with the phone frame and then the tablet and desktop frames, one page per variant.
- Text forms for the CLI and for the comm channel, in the same field order as the PWA.
- The data each variant needs: the inputs the operator enters, the values inferred from them, and where each comes from.
- The three variants shipped side by side on one index page, so the operator can compare them on the same data.
- A recommendation and open questions, written in later parts of this file. This section does not pick a variant.

**Out of scope for this pass:**

- Any production code. Nothing under cmd/, internal/, scripts/, hooks/, channel/, deploy/ or tests/ changes.
- The ceiling and admission mechanics: the executor's admission points and the dispatch gate. These stay in [docs/plans/2026-10-14-concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md) sections 4c and 4d.
- The per-Automation budget policy, which the same concurrency design places in its own later design.

**Deliverables, by path:**

- `docs/plans/assets/usage-calculator-mockups/index.html`: the index page that shows the three variants side by side.
- `docs/plans/assets/usage-calculator-mockups/variant-pace-gauge.html`: variant one.
- `docs/plans/assets/usage-calculator-mockups/variant-lane-planner.html`: variant two.
- `docs/plans/assets/usage-calculator-mockups/variant-burn-ledger.html`: variant three.
- `docs/plans/2026-10-23-usage-calculator-design.md`: this document.
- `.datawatch/verify/usage-calc-reference-model.js`: the reference model. Each mockup page embeds this file unchanged, so the numbers agree everywhere.

Four static mockups were filed earlier as design references. They are JPEG images listed in section 4b-i of [docs/plans/2026-10-14-concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md): `daily-decay-and-weekly-refresh.jpeg`, `weekly-dashboard-sections.jpeg`, `daily-breakdown-dashboard.jpeg` and `3-shift-plan-dashboard.jpeg`. They are illustrations with placeholder numbers. This pass cites them as earlier reference images and does not copy their layouts or numbers.

## Shared model

All three variants call one pure function, `UsageModel.advise`, in the reference model file. The variants differ in layout and in which fields come first. The numbers come from the same function, so a given reading shows the same figures in every variant. The model is described below in the same words as the reference file, so a reader can check each formula against the code.

### Inputs the operator enters

| Input | Model field | How it is entered and used |
|---|---|---|
| Weekly percent and the time it was read | `percent`, `readAt` | Entered on every reading. Required. |
| Weekly reset | `lastResetAt`, `nextResetAt` | Entered once as a weekday and time, for example Sunday 08:00 local. The page derives the most recent past reset as `lastResetAt` and the one a week later as `nextResetAt`. |
| Previous reading | `previous.percent`, `previous.readAt` | The reading before this one, from the operator's own log. Optional. Without it the recent burn falls back to the week average. |
| Session percent and its reset time | none in the model | Optional. Shown as a separate secondary line. It does not feed the verdict. |
| Claude lanes running | `lanes` | The number the operator chooses. The model clamps it between 0 and `maxLanes`. |
| Lanes in the recent window | `recentLanes` | How many lanes ran between the previous reading and this one. Defaults to `lanes`. |
| Policy | `policy` | One of `use-all`, `pace` or `finish-early`. Defaults to `use-all`. |
| Remaining work, for Run flat out | `remainingWork` | Points of weekly allowance still needed. Entered directly, or derived from the queued Automata as the sum of each Automata's estimated cost in points. The model takes the number only. Whether to derive it is a later build decision. |
| Safety margin | `safetyPoints` | Default 5 points. Used only by Pace to the week. |

Two settings have defaults and are not normally entered per reading: the idle baseline (`baselinePer24h`, default 3 points per 24 hours) and the maximum lane count (`maxLanes`, default 3). Every input is a number or a timestamp. No free text feeds a calculation.

### What is inferred

Every value below is computed by `advise()` from the inputs. Days are 24 hours. The formulas are the reference model's own.

| Value | Model field | Formula |
|---|---|---|
| Days since the reset | `daysSinceReset` | (`readAt` minus `lastResetAt`) in days |
| Days left | `daysLeft` | (`nextResetAt` minus `readAt`) in days, never below 0 |
| Allowance per 24 hours | `allowancePer24h` | 100 divided by 7, about 14.3 points per day |
| Pace line | `paceLine` | (days since the reset divided by 7) times 100 |
| Headroom | `headroom` | pace line minus percent; negative means ahead of pace |
| Week average burn | `weekAvgBurnPer24h` | percent divided by days since the reset; 0 when days since the reset is not positive |
| Recent burn per 24 hours | `recentBurnPer24h` | (percent minus previous percent) divided by the hours between the two readings, times 24, never below 0. Used only when that gap is at least 2 hours (`MIN_RECENT_GAP_HOURS`). Otherwise the week average is used and `recentFallback` is true. |
| Projected end at the reset | `projectedEndRecent`, `projectedEndAvg` | percent plus burn times days left, once with the recent burn and once with the week average |
| Run out | `runOutAtRecent`, `runOutAtAvg`, `hoursEarlyRecent`, `hoursEarlyAvg` | Days to run out is (100 minus percent) divided by the burn. If the burn is 0 or less, or the days to run out is at least the days left, there is no run out. Otherwise the run-out moment is `readAt` plus those days, and hours early is (days left minus days to run out) times 24. |
| Idle baseline | `baselinePer24h` | Default 3 points per 24 hours |
| Burn per lane | `perLanePer24h` | max((recent burn minus baseline) divided by `recentLanes`, 0); 0 when `recentLanes` is 0. An explicit `perLanePer24h` input overrides it. |
| Projection by lane count | `byLanes` | For each lane count from 0 to `maxLanes`: burn = baseline plus lanes times burn per lane; projected end = percent plus burn times days left; run out and hours early as above, using that burn. |

The burn per lane is inferred, not measured. It treats the recent burn above the idle baseline as the work of the lanes that ran in that window. A window that mixes several kinds of work (verifiers, merges, supervision ticks, decomposes) spreads them into the same figure. The readings in the concurrency design show earlier per-task coefficients overcounting by several points, so the lane figure has to be recalibrated at each new reading. The calculator says so in its notes rather than presenting the figure as exact.

### The within or over verdict

- The verdict is `over` when `projectedEndRecent` is above 100, and `within` otherwise. It uses the recent burn because the recent burn describes what the lanes are doing now.
- `verdictAvg` applies the same rule to the week average and is shown beside the main verdict. The two can disagree. At the 22:08 reading of 2026-10-23 the recent burn projects 106.0 percent (over) and the week average projects 90.2 percent (within). Both are shown, and each is labelled.
- `aheadOfPace` is a separate flag. It is true when the percent is above the pace line. It describes position in the week, not the budget. A reading can be ahead of pace and still within budget, and a reading behind pace can still be over budget if the recent burn is high.

### Fields shown on every reading

Each variant shows these fields for every reading, in this order:

1. The percent and the time it was read.
2. Days since the reset.
3. The pace line, with the headroom beside it.
4. Burn per 24 hours, recent and week average. The page says so when the recent burn fell back to the week average.
5. Projected end at the reset, for both burns.
6. The time the allowance would run out, or none before the reset, with the hours early.
7. The verdict at the recent burn, with the week-average verdict beside it.
8. The lanes running and the policy chosen.
9. The policy headline: the one-line advice from `advise()`.

The session line, when entered, appears separately below these and is labelled secondary.

### The model function

`UsageModel.advise(input)` is a pure function. It reads no window, document, storage or clock; the reading time comes in as `readAt`. `UsageModel.formatClock(iso, offsetHours)` formats a moment as a short weekday and time, such as `Sun 02:02`, at a fixed offset from UTC. A later build can port both functions unchanged, because nothing in them depends on the page.

The inputs used are `readAt`, `percent`, `lastResetAt`, `nextResetAt`, `previous` (with `percent` and `readAt`), `lanes`, `recentLanes`, `maxLanes` (default 3), `baselinePer24h` (default 3), `perLanePer24h`, `safetyPoints` (default 5), `policy` (default `use-all`) and `remainingWork`.

The outputs are `daysSinceReset`, `daysLeft`, `allowancePer24h`, `paceLine`, `headroom`, `recentBurnPer24h`, `weekAvgBurnPer24h`, `recentFallback`, `projectedEndRecent`, `projectedEndAvg`, `runOutAtRecent`, `runOutAtAvg`, `hoursEarlyRecent`, `hoursEarlyAvg`, `verdict`, `verdictAvg`, `aheadOfPace`, `perLanePer24h`, `baselinePer24h`, `byLanes` (each row with `lanes`, `burnPer24h`, `projectedEnd`, `runOutAt` and `hoursEarly`), `chosen` (the row for the lanes input) and `advice`. The `advice` object holds `policy`, `lanesSuggested`, `headline`, `notes` and `finish`. `finish` is null unless the policy is `finish-early` and `remainingWork` is a number. When present it holds `hours`, `at`, `endPercent`, `sparePoints` and `beforeAllowance`.

Each mockup page embeds `.datawatch/verify/usage-calc-reference-model.js` unchanged inside a script element with the id `usage-model`. A change to the function is therefore a change to all three pages at once, and the checker runs against the same embedded copy.

### Text forms

The CLI and the comm channel get text forms of the same reading. Rules that apply to both:

- CLI lines are at most 80 columns. Comm-channel lines are at most 50 columns, because the channel wraps narrower than a terminal.
- The field order matches the PWA: reading, reset, pace line, burn, projected end, run out, verdict, lanes and policy, then the advice.
- The comm-channel form ends with the reply the operator can send to choose a policy or a lane count.
- The commands shown below are examples. The final command names are not decided here.

The CLI form, shown for the 22:08 reading (72 percent):

```
usage: 72% read Fri 22:08, reset Sun 08:00 (1.41 days left)
pace line 79.8%, headroom 7.8, days since reset 5.59
burn 24.1 per 24h recent, 12.9 per 24h week average
projected end 106.0% recent (over), 90.2% week average (within)
runs out Sun 02:02, 6 hours before the reset (recent burn)
lanes 3 running, policy Run until used up
advice: 2 lanes end at 96.1%; 3 lanes run out early
example command: usage lanes 2 (example, not final)
```

The comm-channel form for the same reading:

```
Weekly 72% (read Fri 22:08)
Reset Sun 08:00, 1.41 days left
Pace line 79.8%, headroom 7.8
Burn 24.1 per 24h recent, 12.9 average
Ends at reset: 106.0% recent, 90.2% average
Over budget at the recent burn
Runs out Sun 02:02, about 6 h early
Lanes 3, policy Run until used up
Advice: 2 lanes end at 96.1%
Reply: usage lanes 2 (example, not final)
```

## The three usage policies

The policy is the operator's choice. It changes only the target the advice aims at, the headline, the notes and the finish block. It never changes the numbers of the reading. The ids and labels are fixed: `use-all` is labelled Run until used up, `pace` is labelled Pace to the week, and `finish-early` is labelled Run flat out. Each variant page carries all three as controls with `data-policy` set to those ids.

| id | label | intent | what the advice targets | what the advice says when over | what it never does |
|---|---|---|---|---|---|
| `use-all` | Run until used up | Spend the whole weekly allowance. Unused points are lost at the reset. | The largest lane count whose projected end at the recent burn is at most 100 percent. | "Over budget at the recent burn: 3 lanes run out about 6 hours before the reset. 2 lanes end at 96.1%." The notes say running out early is allowed under this policy. | Never names zero lanes. Never holds points back for later in the week. |
| `pace` | Pace to the week | Last until the reset with a margin, so the week does not run out before its end. | The largest lane count whose projected end is at most 100 minus the safety margin, 95 by default. | "Over budget for the week at 3 lanes (106% at the reset). 1 lane ends at 86.1%." If even one lane misses the target, the notes say the allowance is the limit. | Never suggests a count that projects past the target when a smaller count fits. Never names zero lanes. |
| `finish-early` | Run flat out | Finish the remaining work before the allowance does. | All lanes, always. The check is whether the remaining work finishes before the allowance does. | "Over budget: the allowance runs out before the work finishes (N points short). Consider pacing or running until used up." | Never slows or stops a lane. Never names zero lanes. Never reports a finish that the numbers do not support. |

**How `lanesSuggested` is chosen.** For `use-all`, the model loops over lane counts 1 to `maxLanes` and keeps the largest whose projected end at the recent burn is at most 100. If none fits, it suggests 1. For `pace` it does the same with the limit set to 100 minus `safetyPoints`. For `finish-early` it always suggests `maxLanes`, and the policy's work is in the finish check:

- Time to finish, in days = `remainingWork` divided by (`maxLanes` times the burn per lane).
- End percent = percent plus `remainingWork` plus `baselinePer24h` times the time to finish.
- Spare points = 100 minus the end percent.
- The work finishes before the allowance when the spare points are at least 0 and the time to finish is within the days left (`beforeAllowance`).

If `remainingWork` is not entered, the headline asks for it and `finish` is null.

**Known gap in the reference model.** When the burn per lane is 0, the time to finish is infinite and the headline reads "-Infinity points short". This happens when a reading adds nothing since the previous one, or when the recent burn does not rise above the idle baseline. This pass confirmed it by running the model with the previous reading at the same 72 percent: the headline reads that way and `finish.at` is null. This design does not change the reference model, which sits under `.datawatch/verify` and is not edited by this pass. A later build should show that no lane is burning above the baseline instead of an infinite figure.

**Invariants that every variant keeps:**

- The advice never names zero lanes. `lanesSuggested` is at least 1 for `use-all` and `pace`, and it is `maxLanes` for `finish-early`.
- The calculator never stops or slows a lane. Every note says the operator chooses the count, and the advice is advice only.
- The operator chooses the lane count. The lanes input is the operator's own number, and the calculator only reports the row for it in `chosen`.
- Switching policy changes only the target, the headline, the notes and the finish block. The numbers of the reading (percent, projections, burn, run out, verdicts and the lane table) are the same for every policy.

## Readings used by every mockup

The table below is the real operator-reported series since the weekly reset of Sunday 2026-10-18 08:00 local. Every mockup uses exactly these rows, so the three variants can be compared on the same data. The next reset is Sunday 2026-10-25 08:00 local. Readings from the earlier week are not used. The 4 percent snapshot of 2026-10-18 14:30 is the second row. The longer snapshot log stays in [docs/plans/2026-10-14-concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md) section 4b.

Hours since the reset are counted from Sunday 2026-10-18 08:00 local. Points since the previous reading are the change in the weekly percent.

| time (local) | weekly percent | hours since the reset | points since previous reading |
|---|---|---|---|
| 10-18 10:20 | 2% | 2.33 | none, first row |
| 10-18 14:30 | 4% | 6.50 | +2 |
| 10-18 17:00 | 5% | 9.00 | +1 |
| 10-18 21:05 | 7% | 13.08 | +2 |
| 10-19 09:55 | 9% | 25.92 | +2 |
| 10-19 17:50 | 13% | 33.83 | +4 |
| 10-19 20:15 | 14% | 36.25 | +1 |
| 10-20 04:31 | 16% | 44.52 | +2 |
| 10-20 16:40 | 19% | 56.67 | +3 |
| 10-21 14:40 | 30% | 78.67 | +11 |
| 10-21 18:30 | 33% | 82.50 | +3 |
| 10-21 19:40 | 34% | 83.67 | +1 |
| 10-22 12:36 | 45% | 100.60 | +11 |
| 10-22 20:44 | 55% | 108.73 | +10 |
| 10-23 02:40 | 58% | 114.67 | +3 |
| 10-23 17:09 | 67% | 129.15 | +9 |
| 10-23 22:08 | 72% | 134.13 | +5 |

Two rows use the week-average fallback for the recent burn. The first row has no previous reading. The 10-21 19:40 reading is 1.17 hours after the 18:30 reading, which is below the 2-hour minimum gap, so its recent burn is the week average. The gaps between the other readings run from 2.42 hours to 22 hours, so their recent burn comes from the previous reading.

## Worked example

This section takes the 22:08 reading of Friday 2026-10-23 (72 percent) through `advise()` from start to finish. Every number is what the reference model returns for these inputs. Results are rounded to one decimal place. The same inputs produce the figures in the three mockups.

### Model input

- Reading: `readAt` 2026-10-23 22:08 local (UTC-4), `percent` 72.
- Previous reading: 67 percent at 17:09 the same day. The gap is 4.98 hours, which is at least 2 hours, so the recent burn is used.
- Last reset: Sunday 2026-10-18 08:00 local. Next reset: Sunday 2026-10-25 08:00 local.
- Lanes: `lanes` 3, `recentLanes` 3 (three lanes ran in the recent window), `maxLanes` 3.
- Settings: `baselinePer24h` 3, `safetyPoints` 5, `remainingWork` 20 points.

### Results

| value | result | how it is computed |
|---|---|---|
| Days since the reset | 5.59 | 134.13 hours divided by 24 |
| Days left | 1.41 | (Sunday 08:00 minus Friday 22:08, 33.87 hours) divided by 24 |
| Pace line | 79.8% | 5.59 divided by 7, times 100, is 79.84 unrounded |
| Headroom | 7.8 points | 79.84 minus 72 |
| Allowance per 24 hours | 14.3 | 100 divided by 7 |
| Recent burn per 24 hours | 24.1 | (72 minus 67) divided by 4.98 hours, times 24, is 24.08 |
| Week average burn per 24 hours | 12.9 | 72 divided by 5.59 is 12.88 |
| Projected end at reset, recent burn | 106.0% | 72 plus 24.08 times 1.41 is 105.98: over |
| Projected end at reset, week average | 90.2% | 72 plus 12.88 times 1.41 is 90.18: within |
| Run out, recent burn | Sunday 02:02 local | (100 minus 72) divided by 24.08 is 1.16 days after the reading |
| Hours early, recent burn | 5.96, about 6 hours | (1.41 minus 1.16) times 24 |
| Run out, week average | none before the reset | (100 minus 72) divided by 12.88 is 2.17 days, more than the 1.41 days left |
| Ahead of pace | no | 72 is below the pace line of 79.8 |

Hand arithmetic on the rounded 5.59 days gives a pace line of 79.9 and headroom of 7.9. The model works from the timestamps, so it shows 79.8 and 7.8, and the mockups show the model's figures.

Burn per lane is 7.0 per 24 hours, from (24.08 minus 3) divided by 3. The lane table for this reading:

| lanes | burn per 24 hours | projected end at reset | run out |
|---|---|---|---|
| 0 | 3.0 | 76.2% | none |
| 1 | 10.0 | 86.1% | none |
| 2 | 17.1 | 96.1% | none |
| 3 | 24.1 | 106.0% | Sunday 02:02, 5.96 hours early |

The verdict at the recent burn is over, because 3 lanes project to 106.0 percent. The week-average verdict is within, at 90.2 percent. The page shows both.

### Advice for each policy

| policy | lanes suggested | headline the model returns |
|---|---|---|
| Run until used up | 2 | Over budget at the recent burn: 3 lanes run out about 6 hours before the reset. 2 lanes end at 96.1%. |
| Pace to the week | 1 | Over budget for the week at 3 lanes (106% at the reset). 1 lane ends at 86.1%. |
| Run flat out | 3 | Within budget: the work finishes in about 22.8 hours with 5.2 points of allowance to spare. |

- Run until used up suggests 2 lanes because 2 lanes project to 96.1 percent, under 100, while 3 lanes project to 106.0.
- Pace to the week suggests 1 lane because 2 lanes at 96.1 percent is above the 95 percent target, while 1 lane at 86.1 percent is under it.
- Run flat out suggests 3 lanes. The 20 points of work at 3 lanes take 0.95 days, or 22.8 hours, which finishes at Saturday 20:54. The end percent is 72 plus 20 plus 3 times 0.95, which is 94.8, leaving 5.2 points (about 5) to spare. The 0.95 days is within the 1.41 days left, so the headline says within budget. This policy checks the work against the allowance, not the burn, so it can read within budget while the recent burn projects over.

The notes for Run flat out say that all lanes run and the calculator does not slow them down under this policy. The first note on every policy says the operator chooses the count.

**Note on the earlier recorded reading.** The entry for this 22:08 reading in [docs/plans/2026-10-14-concurrency-and-budget-design.md](2026-10-14-concurrency-and-budget-design.md), under the heading "Inflight caps set to match the lane model", gives 1.39 days to the reset, 105 percent at the recent burn, and a run out around Sunday 05:00. Recomputing from the timestamps with the reference model gives 1.41 days, 106.0 percent (105.98) and a run out at Sunday 02:02, about 6 hours before the reset. The earlier entry's figures do not match the timestamps, and this pass uses the model's figures. That older document is not edited here; this note is for the operator to decide whether the log should be corrected.
