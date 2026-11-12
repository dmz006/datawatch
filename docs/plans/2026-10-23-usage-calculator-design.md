# Usage and cost calculator: design pass with three mockup variants

**Date:** 2026-10-23

**Version:** current main when written (no release number is recorded in this document)

**Status:** Implemented on this line and live in the running build: the design is in effect per the operator's decisions in `docs/plans/2026-10-25-usage-calculator-decisions.md`, and it ships in the next release above main (the merge step sets the version; the unreleased entry is in CHANGELOG). The parity surface and implementation paths are in `## Parity surface` at the end of this document. Operator-facing guide: `docs/howto/usage-calculator.md`.

**Scope:** design document; the implementation now spans `internal/usagecalc/`, `internal/server/usage_calculator*.go` + `internal/server/usage_readings*.go`, `internal/router/usage_calculator.go`, `internal/mcp/usage_calculator*.go`, `cmd/datawatch/cli_usage.go`, and the usage-calculator sections of `internal/server/web/app.js`

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
- The field order matches the PWA: reading, reset, pace line, burn, projected end, run out, verdict, lanes and policy, then the advice. The mockups add a lane table (lane planner) and points per day (burn ledger) as extra lines, so those two forms are longer; the build settles the final layout.
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

## Variant A: pace gauge

The first card on the page is the verdict: a chip for the recent-burn verdict, the week-average verdict beside it, and the one-line headline from the model. Under it come the model notes, the weekly gauge, the reading fields, the policy card, a chart of the readings since the reset, the input form and the text forms. The page is `variant-pace-gauge.html` in `docs/plans/assets/usage-calculator-mockups/`. Its distinctive part is the gauge: one bar from 0 to 100 percent with the weekly percent filled, a solid tick for the pace line, and dashed and dotted ticks showing where the recent and week-average burns put the week at the reset. A projection past 100 is drawn in the red zone at the right edge.

### Inputs the operator enters

- **Weekly percent.** A whole-number field from 0 to 100, default 72. Each change re-runs the model at once. A value outside the range, or not a number, shows "Enter a number from 0 to 100." and keeps the last valid figures on screen. The reading time does not move with it: the page keeps the reading at Friday 22:08 local.
- **Weekly reset day.** A select from Sunday to Saturday, default Sunday. With the reset time it gives the most recent reset at or before the reading, through the `lastResetFor` function in the `usage-view` script of `variant-pace-gauge.html`.
- **Weekly reset time.** A time field, default 08:00. The page applies it only when the value reads as HH:MM.
- **Session percent (optional).** A number field, empty by default. When it is filled the page shows "Session N% (secondary, not used in the verdict)". No figure reads it.
- **Usage policy.** Three radio buttons, default Pace to the week: Run until used up, Pace to the week, and Run flat out. The arrow keys move between them.
- **Claude lanes running.** A stepper from 0 to 3, default 3. Minus is disabled at 0 and plus at 3. Zero is accepted as the operator's own choice, and the page then shows "Zero lanes is the operator's decision and is never advised."

Four values are fixed in the page script and cannot be changed on the page: the previous reading (67 percent at Friday 17:09), the recent-window lane count (3), the idle baseline (3 points per 24 hours) and the safety margin (5 points). The remaining work for Run flat out is also fixed, at 20 points. The brief expected that figure to be an input on this page; in the mockup it is a constant. Whether the operator enters it is a later build decision.

### What is inferred

Every figure comes from `advise()` in the shared reference model, `.datawatch/verify/usage-calc-reference-model.js`, which the page embeds unchanged. Days are 24 hours.

- **Days since the reset** is the reading time minus the last reset, divided by 24 hours: 5.59. **Days left** is the next reset minus the reading time, floored at 0: 1.41.
- **Pace line** is days since the reset divided by 7, times 100: 79.8 percent. **Headroom** is the pace line minus the percent: 7.8 points. When the percent is above the line, the page says "ahead of the pace line" in place of the headroom.
- **Recent burn** is the change since the previous reading divided by the hours between the two readings, times 24, floored at 0: 24.1 per 24 hours (from Friday 17:09, 4.98 hours before). A gap under 2 hours makes the page use the week average and say so.
- **Week-average burn** is the percent divided by the days since the reset: 12.9 per 24 hours.
- **Projected end at the reset** is the percent plus the burn times the days left, computed once with each burn: 106.0 percent with the recent burn (over) and 90.2 percent with the average (within).
- **Run out** is (100 minus the percent) divided by the burn, in days. If that is at least the days left, the page shows none before the reset. Otherwise it shows the moment, Sunday 02:02, and the hours early, (days left minus days to run out) times 24: about 6 hours. The week-average burn gives no run out.
- **Verdict** is over when the recent projection is above 100 and within otherwise. The average verdict is computed separately and shown beside it, and the two can disagree.
- **Chosen-lanes line** is the lane row for the lanes input. Its burn is the idle baseline plus lanes times the burn per lane, where the burn per lane is max((recent burn minus baseline) divided by 3, 0). For 3 lanes that is 3 plus 3 times 7.0, giving 24.1 and a projected end of 106.0. The divisor 3 is the fixed recent-window count, not a measurement.
- **Gauge and chart geometry.** The gauge track runs from 0 to 100. A projection past 100 is drawn in the red zone and clamped at its edge, labelled "Over, past the edge". The chart plots the readings since the reset from a series fixed in the `usage-view` script (16 readings and the reading shown), with the pace line from 0 at the reset to 100 at the next reset, and dashed projections from the last reading to the reset.

### Data the daemon must supply

- **The weekly reading (percent and time).** Does not exist as stored data. The operator types it into the page and the page keeps nothing. The readings exist only as the hand-kept table in `docs/plans/2026-10-14-concurrency-and-budget-design.md`, section 4b. A later build needs a timestamped store of readings that the operator appends to. The subscription quota cannot be read programmatically, as section 4a of that document records.
- **The previous reading.** Also does not exist as stored data. It is the prior row of the same log and feeds the recent burn; without it the page uses the week average. The brief's statement that this page needs nothing beyond the reading and the reset time holds for the pace line, the verdict and the week-average figures. The recent burn and the chart also need the prior rows of the same log. No separate source is needed.
- **Reset weekday and time.** Operator-entered. No subscription reset setting exists. The flags `--budget-weekday`, `--budget-time` and `--budget-zone` in `cmd/datawatch/cli_budget.go` set a weekly anchor, but for a dollar budget window (`inference.BudgetWindow`), not for the subscription week. A build should keep its own setting and not reuse those flags.
- **Session percent.** Operator-entered. No daemon source exists, for the reason given in section 4a of the concurrency design.
- **Claude lanes running.** An operator-chosen number. The daemon keeps no record of how many Claude lanes ran, so the chosen-lanes line projects a typed count rather than a measured one.
- **Per-session cost.** Exists. `internal/session/cost.go` keeps tokens in, tokens out and estimated USD on each session (the `TokensIn`, `TokensOut` and `EstCostUSD` fields), and `SummaryFor` totals them. This page does not use it, because the verdict is a subscription percent and dollar totals do not map onto it.

### How each policy changes the advice

- **Run until used up:** the headline reads "Over budget at the recent burn: 3 lanes run out about 6 hours before the reset. 2 lanes end at 96.1%." The page suggests 2 lanes, the largest count whose projected end is 100 or less, and its notes say running out early is allowed under this policy. Worked result: 2 lanes, ending near 96 percent. The page never advises zero lanes, never holds points back for later in the week, and never slows or stops a lane.
- **Pace to the week:** the default. The headline reads "Over budget for the week at 3 lanes (106% at the reset). 1 lane ends at 86.1%." The target is 95 percent (100 minus the 5-point margin), and 2 lanes at 96.1 percent miss it, so the page suggests 1 lane. Worked result: 1 lane. The page never advises zero lanes and never slows or stops a lane; the operator sets the count.
- **Run flat out:** the page uses the fixed 20 points of remaining work. The headline reads "Within budget: the work finishes in about 22.8 hours with 5.2 points of allowance to spare", and the finish line reads "Work finishes Sat 20:54, 5.2 points to spare." It suggests 3 lanes, every lane, and its notes say all lanes run and the calculator does not slow them. Worked result: within budget with 3 lanes, although the recent burn projects 106 percent at the reset, because this policy checks the work against the allowance rather than the burn. The page never advises zero lanes and never slows or stops a lane.

## Variant B: lane planner

The first card is the same verdict as variant A: the recent-burn chip, the week-average chip beside it, and the headline. The reading fields sit in a collapsible list inside that card. Under it come the notes, the policy card with the lane choice, and then the centre of the page, a table with one row for each lane count from 0 to 3. Each row gives the burn per 24 hours, the projected end at the reset, the run out, the hours early and whether the row fits the policy target. The advised row and the operator's chosen row carry badges. Below the table is a week timeline. The page is `variant-lane-planner.html` in `docs/plans/assets/usage-calculator-mockups/`.

### Inputs the operator enters

- **Weekly percent.** A whole-number field from 0 to 100, default 72. No floor applies.
- **Claude lanes that ran in the recent window.** A whole-number field from 0 to 3, default 3, with the hint "Between the previous reading and this one." It is the only input that stands in for lane history, and it sets the divisor of the burn-per-lane figure.
- **Idle baseline per 24 hours.** A decimal field from 0 to 20 in steps of 0.1, default 3. Its label carries the tag "inferred, you can override", but nothing computes the baseline. The field holds a typed value, and the model default is also 3.
- **Safety margin, points.** A whole-number field from 0 to 50, default 5. Only Pace to the week reads it; its target is 100 minus the margin.
- **Remaining work, points.** A decimal field from 0 to 100 in steps of 0.5, default 20. It is shown and read only when Run flat out is selected; under the other policies it is left out of validation.
- **Claude lanes running.** A stepper from 0 to 3, default 3, labelled "your choice". Zero is accepted, and the page then says it is never advised.
- **Usage policy.** Three radio buttons, default Pace to the week.

Not on this page: the reset day and time, which are fixed in the script as the constants `LAST_RESET` and `NEXT_RESET` (2026-10-18 08:00 and 2026-10-25 08:00 local); the reading time, fixed at Friday 22:08; the previous reading, fixed at 67 percent on Friday 17:09; and any session percent field. No field updates the page until every field is valid. If one is not, the page shows "Enter numbers in the allowed ranges" with the limits.

### What is inferred

Position in the week, the burns, the verdict, the projected end and the run out use the same formulas as variant A, from the shared model above, with the same fixed reading. Four values are added here.

- **Burn per lane** is max((recent burn minus idle baseline) divided by the lanes in the recent window, 0). With the reading, (24.08 minus 3) divided by 3 is 7.0 per 24 hours, which the page prints in a note under the inputs. If the window count is 0 the figure is 0.
- **What-if rows.** For each lane count l from 0 to 3, the burn is the baseline plus l times the burn per lane, the projected end is the percent plus the burn times the days left, and the run out and hours early come from that burn. For the reading the rows are 3.0 (76.2 percent), 10.0 (86.1 percent), 17.1 (96.1 percent) and 24.1 (106.0 percent, Sunday 02:02, 6.0 hours early). A row fits when its projected end is within the target: 100 under Run until used up and 95 under Pace to the week. Under Run flat out there is no target, and every row reads "running".
- **Advice badge.** The lane count the model suggests for the policy, as in variant A. It is never 0.
- **Finish line for Run flat out.** Days to finish are the remaining work divided by (3 times the burn per lane). The end percent is the percent plus the remaining work plus the baseline times those days. Spare points are 100 minus the end percent, and the finish time is the reading time plus the days. For the reading, 20 divided by 21.1 is 0.95 days, Saturday 20:54, with 5.2 points to spare.
- **Timeline.** The readings from the fixed series and the reading shown, placed by hours since the Sunday reset. Each lane line starts at the reading and runs to the reset, or to its run-out point: 3 lanes ends at Sunday 02:02. Ticks mark every 24 hours.

The shared model section records a known gap: when the burn per lane is 0, the finish time is infinite. The page's finish line then says "No finish time: no lane is burning above the idle baseline", while the headline above it still shows the model's own wording, which can read "-Infinity points short".

### Data the daemon must supply

- **Lanes that ran in the recent window.** Does not exist. The Go code has no lane concept and no lane history: a search of `internal/` and `cmd/` finds no record of how many Claude lanes ran over time. A build needs a per-lane record of starts and stops with times, so the window count can be read from the daemon instead of typed.
- **Burn per lane from lane history.** Does not exist. It needs that lane record and readings at the edges of the window. Today it is one inferred figure divided by a typed count. Section 4b of `docs/plans/2026-10-14-concurrency-and-budget-design.md` records that earlier per-task coefficients overcounted by several points, so the figure has to be recalibrated at each reading.
- **Idle baseline.** Does not exist as a measurement. The 3 points per 24 hours is the model default, `baselinePer24h`. A build would measure idle draw from readings taken while no lane was running.
- **Safety margin.** An operator setting with default 5. No stored value exists anywhere in the daemon.
- **Remaining work.** Operator-typed. The nearest existing field is `EstimatedCostUSD` on Automaton members, in `internal/autonomous/automation.go` lines 144 to 146: an operator-declared expected cost in dollars, valid only on members of the Automaton kind. It is in dollars, not points, and nothing in the calculator reads it. Deriving the remaining work from queued Automata needs a dollars-to-points rule and a planning estimate that the code does not hold.
- **Weekly reading series and previous reading.** As in variant A, no stored record exists.
- **Session percent.** Not used on this page.

### How each policy changes the advice

- **Run until used up:** advice 2 lanes. The table marks 0, 1 and 2 as fitting and 3 as not fitting, with the run out at Sunday 02:02. The headline reads "Over budget at the recent burn: 3 lanes run out about 6 hours before the reset. 2 lanes end at 96.1%." Worked result: 2 lanes, ending near 96 percent. The 0-lane row is labelled "never advised" and "operator's decision". The page never advises zero lanes and never slows or stops a lane.
- **Pace to the week:** target 95 percent, advice 1 lane at 86.1 percent. The 2-lane row, at 96.1 percent, is marked does not fit. Worked result: 1 lane, the largest count that fits the target. The page never suggests a count that projects past the target when a smaller count fits, never advises zero lanes, and never slows or stops a lane.
- **Run flat out:** there is no target, so every row reads "running", and the advice is all 3 lanes. The headline reads "Within budget: the work finishes in about 22.8 hours with 5.2 points of allowance to spare", and the finish line reads "Work of 20 points at 3 lanes finishes Sat 20:54, 5.2 points to spare." Worked result: within budget at 3 lanes, while the recent burn stays over at 106 percent, because the work check and the burn check answer different questions. The page never reports a finish the numbers do not support, never advises zero lanes, and never slows or stops a lane.

## Variant C: burn ledger

The first card is the verdict and the reading: the recent-burn chip, the week-average chip, the headline and the reading fields, with the weekly percent typed into its field. The distinctive part comes next, a bar chart titled "Points used each day since the reset", with one stacked bar for each day from the Sunday reset to the reading, each bar split by kind of work. Under the chart come the cost-per-Automaton table, an example queue, example rows for other consumers, the policy and lane card, and the text forms. The page is `variant-burn-ledger.html` in `docs/plans/assets/usage-calculator-mockups/`.

### Inputs the operator enters

- **Weekly percent.** A whole-number field, default 72. Its minimum is 67, the 17:09 reading, and a lower value shows "Enter a number from 67 to 100." This is the only typed reading on the page.
- **Usage policy.** Three radio buttons, default Pace to the week.
- **Claude lanes running.** A stepper from 0 to 3, default 3, with the same never-advised note at 0.

The page has no queue input, no cost-per-Automaton override and no remaining-work field. The cost coefficients are constants in the `usage-view` script of `variant-burn-ledger.html`, as `COEF`: 0.4 points per full Automaton, 0.15 per decompose or merge session, and an idle baseline of 3. The planning column of the cost table is typed into the HTML. The example queue is a static paragraph. The remaining work for Run flat out is fixed at 20 points in the script, and the page says so. The split by kind of work is five fixed shares in the script. Not on the page: the reset dates, the reading time, the previous reading, the recent-window lane count (3), the safety margin (5) and any session percent. So the operator cannot enter the queue, the cost overrides or the remaining work on this page, which is not what the brief expected.

### What is inferred

Position, burns, verdict, projected end and run out use the same formulas as variant A. The page adds three inferred figures.

- **Points used each day.** The reset counts as 0. The page reads the weekly percent at each 08:00 boundary (24, 48, 72, 96 and 120 hours after the reset) and at the reading's own time (134.1 hours), by straight-line interpolation between the readings in the fixed series plus the reading shown. Each day's total is the difference between consecutive values:

| Day (from 08:00) | Points |
|---|---|
| Sun 18 | 8.7 |
| Mon 19 | 8.2 |
| Tue 20 | 9.8 |
| Wed 21 | 15.3 |
| Thu 22 | 19.3 |
| Fri 23 (partial, to 22:08) | 10.7 |

The six days total 72.0, which is the reading. The readings are between 1.17 and 22 hours apart, so a straight line can move use from one day to the next, and the page calls the totals estimates.

- **Split by kind of work.** Each day's total is divided by five fixed shares: build workers 58 percent, decompose 14, verify 12, supervision 9 and merges 7. The chart's note says the split is illustrative and that no real attribution exists yet. The shares are not derived from session records.
- **Measured column of the cost table.** The same lane table as variant B: 3.0, 10.0, 17.1 and 24.1 per 24 hours for 0 to 3 lanes.
- **Planning column.** Typed values 3.0, 3.7, 4.4 and 5.1, equal to the 3-point baseline plus lanes times 0.7. The 0.7 is 0.4 for one Automaton plus two 0.15 sessions, one decompose and one merge, per lane per day. The coefficients come from the 2026-10-23 readings recorded in `docs/plans/2026-10-14-concurrency-and-budget-design.md`, under the readings of 02:40 and 17:09. The measured figure per lane is 7.0 against a planning figure of 0.7, about ten times as large. The page says the two columns do not agree and that the planning column is not a forecast.
- **Example queue arithmetic.** Six Automata at 0.4 is 2.4 points and six decompose sessions at 0.15 is 0.9, a total of 3.3. This is example text, labelled as not real data, and the Run flat out finish does not use it; that finish uses the 20 points.

### Data the daemon must supply

- **The reading series and the previous reading.** As in variant A, no stored record exists. The day totals are a rollup of that series and need the same store.
- **Kind of work per session.** Partly exists. `internal/session/cost.go` keeps tokens in, tokens out and estimated USD per session. The `Session` struct in `internal/session/store.go` has no kind-of-work field. The nearest record is the `Decision` type in `internal/autonomous/models.go` (around lines 50 to 70), the per-Automaton audit log of model calls. It carries a kind (decompose, run, verify, approve and others), the backend, the model, a per-call cost in USD and an actor. It links to a session only for guardrail judges (`JudgeSessionID`), it is keyed by Automaton rather than by session, and it records no wall-clock time. A split from session records therefore needs a kind on each session or a join from decision entries to sessions, plus a rule that converts dollars to points.
- **Cost per Automaton overrides.** Do not exist. The only per-Automaton estimate is `EstimatedCostUSD` in `internal/autonomous/automation.go` (lines 144 to 146), in dollars and valid only on Automaton members. A build needs override storage and the dollars-to-points rule.
- **Queued Automata with points.** The queued Automata exist in the autonomous package, but no points estimate is held for them. Each queued item needs one.
- **Other consumers.** Section 3 of `docs/plans/2026-10-18-model-host-requirements.md` lists what each consumer must report: name and kind, model, node, tokens in and out where known, wall-clock seconds, and whether it caused an eviction. None of this exists. There is no consumer registry and no per-consumer record; `internal/session/cost.go` counts per session only. The one eviction signal is in `internal/pinkeeper/pinkeeper.go`, which raises a system alert when an eviction forces a re-pin of the pinned model, and it does not name the consumer that caused it. The example rows on this page show calls per day, which section 3 does not list. A build should follow section 3 and add calls only if the operator asks for them.

### How each policy changes the advice

- **Run until used up:** advice 2 lanes. The headline reads "Over budget at the recent burn: 3 lanes run out about 6 hours before the reset. 2 lanes end at 96.1%." The chart and the cost table do not change with the policy; only the advice does. Worked result: 2 lanes, ending near 96 percent. The page never advises zero lanes, never holds points back for later in the week, and never slows or stops a lane.
- **Pace to the week:** advice 1 lane, since 2 lanes at 96.1 percent misses the 95-percent target. The headline reads "Over budget for the week at 3 lanes (106% at the reset). 1 lane ends at 86.1%." Worked result: 1 lane. The page never advises zero lanes and never slows or stops a lane.
- **Run flat out:** the finish line reads "Work finishes Sat 20:54, 5.2 points to spare" on the fixed 20 points, with 3 lanes advised and every lane running. The headline reads "Within budget: the work finishes in about 22.8 hours with 5.2 points of allowance to spare", while the recent burn still projects over at 106 percent. The example queue of 3.3 points is not part of this figure, and the page says the operator decides which of the two figures to enter. The page never reports a finish the numbers do not support, never advises zero lanes, and never slows or stops a lane.

## Shipping variants side by side behind a setting

This section answers one question: how could the three variants ship together, so the operator can run them for a while and compare them on the same readings, without a rewrite of the calculation? The answer is one shared core, with each variant as a view-only renderer over its result. Adding or removing a variant then touches a renderer, its switcher label and one setting value, and nothing that computes. This pass changes no code. The build that follows owes the surfaces listed in item 5 below.

### 1. The shared core

The calculation is one pure function, `UsageModel.advise`, defined in `.datawatch/verify/usage-calc-reference-model.js` and described in the shared model section above. It reads the readings, the lane history and the policy, and it returns one advice result. Every surface reads that result: the three PWA renderers, the CLI, the comm channel and MCP. A renderer reads fields and draws them. It never computes a burn, a projection, a verdict or a lane count, and it never reads a reading or the policy from anywhere else. Because of that, adding or removing a variant never touches the calculation, the stored readings or the policy code.

The proposed read surfaces for that result are:

- **REST:** `GET /api/usage/advice`, with optional `variant` and `policy` query values. The `variant` value chooses the renderer for one call and never changes a number. The `policy` value overrides the setting for one call and changes only the advice, as the policy section above requires.
- **MCP:** a `usage_advice` tool that returns the same body, following the proxy pattern of `internal/mcp/budget.go`, where `budget_status` proxies `GET /api/budget`.
- **CLI:** `datawatch usage advice`, in a new file beside `cmd/datawatch/cli_budget.go`, which already formats the `GET /api/budget` body with a shared formatter.
- **Comm channel:** `usage advice`, parsed in `internal/router/commands.go` the way `parseBudget` and `parseUsageBackoff` parse their tails.

The CLI and the comm channel render the same text form from the same body, in the field order given under "Text forms" above: reading, reset, pace line, burn, projected end, run out, verdict, lanes and policy, then the advice. A field added to the result therefore appears in both text forms at the same place.

### 2. The setting

Two proposed settings sit under a new `usage:` block in `docs/config-reference.yaml`, named in the same dotted style as `autonomous.usage_backoff` and `autonomous.backoff_max_wait_minutes`:

| Key | Values | Default |
|---|---|---|
| `usage.calculator.variant` | `pace-gauge`, `lane-planner`, `burn-ledger`, `all` | `all`, while the operator compares |
| `usage.calculator.policy` | `use-all` (Run until used up), `pace` (Pace to the week), `finish-early` (Run flat out) | `use-all`, the model default |

Variant and policy are independent. The variant chooses how the result is drawn, and the policy chooses which target the advice aims at. Any variant can show any policy, and changing one never changes the other. With `all`, the PWA shows a switcher on the same reading and the same policy, so the operator can switch renderers and see the same numbers move nowhere. Switching the policy changes only the headline, the notes, the lane suggestion and the finish block.

The CLI and the comm channel take the variant as an argument for one call and do not save it: `datawatch usage advice --variant lane-planner` and `usage advice variant=lane-planner`. In this proposal the text lines are the same whichever variant is named, because the text form has one field order. The argument is still validated against the four values, so a later variant with its own text form can use the same command. Whether text forms should differ is an open question below.

The five access methods, as the Feature Documentation rule in AGENT.md asks, for the variant setting:

| Method | How |
|---|---|
| **YAML** | `usage.calculator.variant` and `usage.calculator.policy` in `~/.datawatch/config.yaml`, documented in `docs/config-reference.yaml` |
| **CLI** | `datawatch config set usage.calculator.variant burn-ledger`, or `datawatch usage advice --variant pace-gauge` for one call |
| **Web UI** | Settings, General Configuration, a Usage calculator card (proposed) |
| **REST API** | `PUT /api/config` with `{"usage.calculator.variant": "burn-ledger"}`; `GET /api/usage/advice?variant=pace-gauge` for one call |
| **Comm channel** | `configure usage.calculator.variant=burn-ledger`; `usage advice variant=lane-planner` for one call |

### 3. Fair comparison

- **One store, one reading.** Readings and lane history are stored once, in a single series that every variant reads from the daemon. No renderer keeps its own copy. Every variant therefore shows the same reading, the same lane table and the same day totals at the same moment. The mockups fix that series inside each page's script (the `usage-view` script in each variant page). A build removes that and reads the series from the daemon instead.
- **At least one full weekly cycle.** The reset is weekly, Sunday 08:00 local in this design. Running all variants for seven days, from one reset to the next, lets the operator see the reset itself: the pace line starting again at zero, the day totals in the burn ledger starting again, and the verdict clearing. A comparison made inside one day shows only one slice of the week and would favour whichever variant reads best at that moment.
- **An optional counter, off by default.** When the operator turns it on, the PWA counts which variant was opened and whether the advice was used. The counter is kept in the browser's local storage, never sent to the daemon, and shown back to the operator on request so they can look back over the comparison. It is a per-viewer convenience. The daemon never depends on it, and CLI and comm calls are not counted.
- **Retiring a variant.** Remove its renderer and its value from the allowed list of `usage.calculator.variant`. No data migration is needed, because no variant owns data. A saved setting that names a retired value falls back to `all`, and the daemon logs one line saying so.

### 4. What every variant keeps identical

A comparison of layouts is fair only if the numbers and the words do not change between renderers. Every variant keeps these identical:

- **The model.** `UsageModel.advise` and its formulas, unchanged in every variant.
- **The verdict wording.** "Within budget" and "Over budget", decided by the same rule: the recent burn gives the main verdict and the week-average burn gives the verdict beside it.
- **The field names and their order**, as given in item 1.
- **The three policy labels**, Run until used up, Pace to the week and Run flat out, with the ids `use-all`, `pace` and `finish-early`.
- **The zero-lanes rule.** The advice never names zero lanes. `lanesSuggested` is at least 1 for `use-all` and `pace`, and it is `maxLanes` for `finish-early`. A zero that the operator sets is shown as the operator's own choice, never as advice.
- **The operator decides the count.** No renderer or surface stops, slows or reorders a lane.

A build should test this directly: feed one fixed result body to every renderer and compare the field values. If a renderer shows a different number for the same reading, it is no longer a layout comparison.

### 5. Surfaces and parity a later build owes

The build follows the Configuration Accessibility Rule and the Mobile-Parity Rule in AGENT.md. It owes these, and this design pass edits none of them:

- **Config reference:** a `usage:` block in `docs/config-reference.yaml` with both keys, annotated as `autonomous.usage_backoff` is annotated.
- **Config handling:** the GET and PUT handling in `internal/server/api.go`, and the General Configuration fields list `GENERAL_CONFIG_FIELDS` in `internal/server/web/app.js`, so the settings card can edit both keys.
- **Locale keys:** the same key names in all five bundles, `internal/server/web/locales/en.json`, `de.json`, `es.json`, `fr.json` and `ja.json`, for example `settings_usage_calculator_variant`, mirroring `settings_usage_backoff`.
- **OpenAPI entries:** `GET /api/usage/advice` and the two config keys, in `docs/api/openapi.yaml` and in the served copy `internal/server/web/openapi.yaml`.
- **MCP tool:** `usage_advice`, beside `internal/mcp/budget.go`.
- **CLI command:** `datawatch usage advice`, beside `cmd/datawatch/cli_budget.go`.
- **Comm channel command:** `usage advice`, in `internal/router/commands.go`.
- **Docs and howto pages:** a section in `docs/operations.md` and `docs/setup.md` with the five-method table, and a howto page that `docs/howto/automata-capacity.md` links to, since that page already covers the back-off and the budget.
- **Mobile tracking issue:** one datawatch-app issue for Android and iPhone, filed once the operator has chosen a variant — filed 2026-10-25: scope [app#286](https://github.com/dmz006/datawatch-app/issues/286) and locale keys [app#287](https://github.com/dmz006/datawatch-app/issues/287); the client contract is `docs/plans/2026-10-25-usage-calculator-mobile-contract.md`. The Mobile-Parity Rule requires it for any operator-visible change. This pass files none and does not choose.

### 6. Federation: open question for the operator

Section 4e of `docs/plans/2026-10-14-concurrency-and-budget-design.md` says the subscription meter is account-scoped, not daemon-scoped. A weekly reading describes the account, not the daemon that stores it. If two federated daemons share a `billing_group`, a reading entered on one would describe both, and the lane history would have to be read across both. If the reading stays one daemon's record, each daemon shows only its own lanes against the account's percent, which is the undercount section 4e warns about.

This design does not decide it. The open question is whether a reading belongs to one daemon or to the shared account that peers confirm. The answer decides where the series is stored, and whether the REST read needs a federation capability guard, which the Federation-Parity Rule in AGENT.md requires for autonomous read cases.

### Open questions for the operator

- Does a reading belong to one daemon or to the shared account (item 6)?
- Should the text forms differ per variant, or stay one form with the variant argument only validated (item 2)?
- Is `all` the right default while the operator compares, and is the local counter worth its browser storage?

## Recommendation for the operator

The choice of variant is the operator's choice. This design pass does not pick a variant, and no variant is picked here. The three pages are `docs/plans/assets/usage-calculator-mockups/variant-pace-gauge.html`, `variant-lane-planner.html` and `variant-burn-ledger.html`, linked from `docs/plans/assets/usage-calculator-mockups/index.html`. The order of the variants on that index is not a ranking. What follows is a process to run, a neutral comparison the operator can judge, and four invariants that hold whatever the operator chooses.

**Process, not a winner.** Run it in three steps:

1. Build the shared core first. Port `UsageModel.advise` from `.datawatch/verify/usage-calc-reference-model.js` into the daemon as the only calculation, and expose its result through the REST, MCP, CLI and comm-channel surfaces described in "Shipping variants side by side behind a setting". Settle the zero-burn case in that core before any renderer is built, using the known gap in the reference model, described under "The three usage policies" above, so that no renderer invents its own wording.
2. Ship the three renderers together behind `usage.calculator.variant`, with `all` as the default while the operator compares. Each renderer reads the same result body, so the numbers cannot differ between them.
3. Run all three for at least one full weekly cycle, from the Sunday 08:00 reset of 2026-10-25 to the one on 2026-11-01, on the same readings. Then compare them, and let the operator set the variant or keep `all`.

**Neutral comparison.** The table sets the three variants against the criteria the operator asked for. Each cell describes what the page does today, as the variant sections above and `docs/plans/assets/usage-calculator-mockups/index.html` describe it. No cell is a score.

| Criterion | Pace gauge | Lane planner | Burn ledger |
|---|---|---|---|
| Glanceability on a phone | One bar shows the weekly percent, the pace line and both projections at the reset. The verdict is the first card. | The verdict comes first, then a table of six columns with one row for each lane count. It is the densest block on a phone. | The verdict and reading fields come first, then a bar chart of the days since the reset. The lane stepper sits after the chart, the cost table and the other-consumer rows. |
| Help choosing the number of lanes | The chosen count, the advised count and one line for three lanes. There is no table of counts. | One row for each count from 0 to 3, with burn, projected end, run out and a fit mark against the target. | A measured column and a planning column for each count. The two disagree (3.0 to 24.1 measured, 3.0 to 5.1 planned), and the page says the planning column is not a forecast. |
| Run until used up | The advice and the headline change, against 100 percent. | The fit marks change, against 100 percent. | The advice and the headline change. The chart and the cost table do not. |
| Pace to the week | The advice and the headline change, against the 95 percent target. | The target line and the fit marks show why a count is advised. | The advice and the headline change. |
| Run flat out | A finish line on the fixed 20 points, with all lanes advised. | A finish line, and every row reads "running". | A finish line on the fixed 20 points, beside the 3.3-point example queue, which the page says does not yet agree with it. |
| Data that must exist before build | The stored readings and the previous reading, the reset setting and, optionally, the session percent. | Everything the pace gauge needs, plus a per-lane start and stop record, a measured idle baseline, the safety margin setting and a remaining-work input. | Everything the lane planner needs, plus a per-day rollup, a kind of work on each session, cost overrides, a points estimate for each queued Automaton and a consumer registry. |
| Build effort | Smallest: one gauge, one chart, one reading form and the text forms. | Medium: a lane table, a timeline, four more inputs and a lane record. | Largest: per-day sums, attribution, and the cost and consumer sections, each needing data the code does not hold. |

**Where each variant fits best.** These are situations, not a ranking.

- The pace gauge fits an operator who wants the position in the week at a glance and changes the lane count rarely. Its single bar in `docs/plans/assets/usage-calculator-mockups/variant-pace-gauge.html` answers how far through the week the account is.
- The lane planner fits an operator who changes the lane count often and wants every count compared on one screen, with the fit mark against the policy target.
- The burn ledger fits an operator who wants to see where the week's points went day by day, and who will wait for real attribution data before trusting the split in `docs/plans/assets/usage-calculator-mockups/variant-burn-ledger.html`.

**Invariants that hold whatever the choice.** Every variant keeps these, as "Variant" and "Shared model" in `docs/plans/2026-10-23-usage-calculator-design.md` require:

- The advice never names zero lanes. Zero appears only as the operator's own choice, as `variant-lane-planner.html` shows with the row marked "never advised".
- The operator chooses the lane count and the policy. No renderer, endpoint or background check sets either.
- The verdict wording is the same everywhere: "Over budget" or "Within budget", decided at the recent burn, with the week-average verdict beside it. The lane planner's finish line already says "No finish time" where its headline keeps the model's own words, so the core must decide that wording once.
- The supervisor never stops all Claude lanes on its own projection. This is the operator decision recorded in `docs/plans/2026-10-14-concurrency-and-budget-design.md`, and the calculator must not reopen it.

## Open questions

1. Should the safety margin for Pace to the week stay at 5 points? It sets the target, and at the 22:08 reading a 3-point margin would advise 2 lanes instead of 1, per the worked example in `docs/plans/2026-10-23-usage-calculator-design.md`.
2. Should the hourly session window get its own verdict? Today it is a secondary line only, so a verdict would need a session series that no reading table in this design holds.
3. Should the burn per lane be inferred per model, Haiku and Sonnet separately, instead of one number? One figure averages lanes that may burn at different rates, and per-model figures need a lane record per model that does not exist yet.
4. What should happen when there is no previous reading within 2 hours? The model falls back to the week average, which hides the recent burn, and the readings table has a 1.17-hour gap at 10-21 19:40, so the fallback is not rare.
5. Are readings typed by hand only, or also pasted from the usage screen text? The mockups only type them; a paste path would need the screen's text layout, which the repository does not record.
6. Where does the remaining work for Run flat out come from: typed in, or the queued Automata times a cost? The worked example uses a fixed 20 points, the burn ledger's example queue prices at 3.3, and no dollars-to-points rule exists in `internal/autonomous/automation.go`.
7. How is the reset moment stored across a daylight saving change? `UsageModel.formatClock` takes a fixed UTC offset, so a stored offset would move the reset by an hour on the day the clocks change and show a wrong run-out time.
8. Does a reading belong to one daemon, or to the shared account across federated daemons? The answer decides where the series is stored and whether the REST read needs a federation capability guard, as section 6 of "Shipping variants side by side behind a setting" describes.
9. How should bought extra credit, as on 2026-10-17, appear next to the weekly allowance? A top-up can take the account past 100 percent, but the verdict and the run-out figures assume a ceiling at 100 and the model has no field for extra credit.
10. Should the older reading entry for the 22:08 reading be corrected? The entry under "Inflight caps set to match the lane model" in `docs/plans/2026-10-14-concurrency-and-budget-design.md` gives 1.39 days, 105 percent and a run out around 05:00, while the timestamps give 1.41 days, 106.0 percent and 02:02, as the worked example notes.

## Parity surface

The ten open questions above were settled in `docs/plans/2026-10-25-usage-calculator-decisions.md`, and the result is a single daemon-computed model every access surface renders — the same numbers from all of them, by construction and by test (`internal/usagecalc` parity tests, the 21 node parity tests over the shared `usage-calculator-fixture.js`, and the browser story TS-819):

| Access method | What it shows and does | Where |
|---|---|---|
| YAML / config on all five surfaces | the ten `usage.calculator.*` keys — `variant`, `policy`, `safety_points`, `cost_per_task_points`, `baseline_per_24h`, `max_lanes`, `reset_weekday`, `reset_time`, `reset_zone`, `keeper` — each PUT-able individually or together; an invalid key rejects the whole patch, none applies | `internal/usagecalc` defaults + `internal/server/usage_calculator_config.go` (apply-then-confirm, the budget-block pattern) |
| CLI | `datawatch usage advice [--variant V] [--policy P] [--lanes N] [--json]` (per-call overrides, validated locally, never stored), `usage reading add --percent \\| --spend-usd [flags]`, `usage reading list`, `usage reading delete <id>`, `usage extra-credit <pts>`, `usage remaining-work <pts\\|clear>` | `cmd/datawatch/cli_usage.go` (thin proxy of the daemon, `cli_usage_test.go`) |
| Web UI (PWA) | the usage-calculator card on the Automation sub-tab: verdict chip (policy + verdict + the one burn figure), the three-variant switcher (`all` shows every layout over the same reading), the last-copy marker and week-average notice, zero-lanes as operator choice; the settings card with exactly the ten keys, saving a field issues `PUT /api/config` with the dotted key | the usage-calculator sections of `internal/server/web/app.js` |
| REST API | `GET /api/usage/advice` (optional per-call `variant`/`policy`/`lanes`), `GET`/`POST /api/usage/readings`, `DELETE /api/usage/readings/<id>`, `PUT /api/usage/extra-credit`, `PUT`/`GET /api/usage/remaining-work`; the `usage.read` / `usage.write` capabilities gate the matching verbs; a JSON-lines audit event records every mutation | `internal/server/usage_calculator_advice.go`, `usage_readings.go`, `usage_calculator_config.go`, `usage_calculator.go` |
| Comm channel | `usage`, `usage advice [variant=V] [policy=P] [lanes=N]`, `usage reading <percent> [lanes=N] [session=N]`, `usage extra <pts>`, `usage work <pts\\|clear>` — the handler proxies the daemon REST loopback and the reply is the shared 50-column text rendering, an invalid form replies with the grammar and sends nothing | `internal/router/usage_calculator.go` + `internal/router/commands.go` |
| MCP tools | `usage_advice`, `usage_readings`, `usage_reading_add`, `usage_reading_delete`, `usage_extra_credit_set`, `usage_remaining_work_set` (plus `config_set` for the variant key) — proxies of the same REST surface, read tools capped at `usage.read`, write tools at `usage.write` | `internal/mcp/usage_calculator*.go` + `internal/federation/mcp_tool_caps.go` |
| Android / iPhone (native clients) | **Pending — issue filed:** the calculator has no native screens yet; the datawatch-app issues are [app#286](https://github.com/dmz006/datawatch-app/issues/286) (scope, filed 2026-10-25) and [app#287](https://github.com/dmz006/datawatch-app/issues/287) (locale keys), both mirrored by the contract in `docs/plans/2026-10-25-usage-calculator-mobile-contract.md` — `❌ pending` in the Account usage calculator row of `docs/parity-status.md` | `docs/parity-status.md` |

The two planned-not-built reading sources — the operator pasting a usage text and daemon-side account or API sources — are also deliberately out of this release: the reading model stays manual and typed for now, and the source gate is defined and tested so those sources can be added on top without touching the model (decision 5).

Shared state, per the decisions: the readings series and extra credit live on the keeper daemon (`data/usage/series.json`, `internal/usagecalc/store.go` + `shared.go`), peers read with `usage.read` and mark a last copy as stale, writes forward to the keeper; remaining work is daemon-local. The calculator keeps its advice-only bar — it advises, it never stops or slows a lane.

Coverage: the `## Usage and cost calculator — unreleased` section of `docs/testing-tracker.md` (plus the MCP and comm-channel detail sections below it), live evidence in release smoke section 82 (record run `smoke-20261028T031253-2859890`, pass), and the operator-facing how-to `docs/howto/usage-calculator.md`.

## Addendum 2026-10-30: the typed lanes input is replaced by the ledger stamp

This change replaces the typed lanes input on a reading with a stamp the daemon writes from the capacity ledger. A reading no longer takes a lane count from the operator; the keeper of the series records the Claude lanes running when the reading is stored, together with a `lanes_source` (`live`, `recorded`, `current`, `unavailable`, or `typed` for a reading stored before the stamp). A `lanes` value sent on a reading by an older caller is accepted and ignored on REST, MCP, the CLI and the comm channel. The advice reports the lanes running now (`lanes_running_now`), the stamped lanes and the what-if lanes apart, and the `lanes` parameter on the advice is the what-if: set for that call only, never saved. The earlier text above is left as it was.
