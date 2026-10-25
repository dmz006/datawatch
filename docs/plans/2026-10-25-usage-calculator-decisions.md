# BL472 usage and cost calculator: operator decisions

Date started: 2026-10-25. The ten open questions of `docs/plans/2026-10-23-usage-calculator-design.md` are answered one at a time, with the operator's words quoted. The implementation PRD reads this file and does not ask again.

## Decisions

1. **Safety margin for "Pace to the week": 5 points as the default, and a setting.** (Operator, 2026-10-25: "1, 5 points as a setting".) The calculator advises against a target five points under the pace line unless the operator changes the setting.
2. **Hourly session window: no verdict of its own; it stays a secondary line.** (Operator, 2026-10-25: "1, keep it secondary".) The weekly verdict is what sets lane counts. The design leaves room for a session verdict if session readings are ever entered.
3. **Burn per lane: one figure for now, with room for per-model later.** (Operator, 2026-10-25: "1, one figure for now".) The design keeps a place to split the figure by model once a lane record per model exists.
4. **No previous reading within 2 hours: fall back to the week average and say so on the screen.** (Operator, 2026-10-25: "1, fall back and say so".) The screen shows a line that no reading exists in the last 2 hours and the week average is used, so the figure is understood to be smoother than the recent burn.
5. **Readings: typed by hand for now, with a paste path later, and the design must plan for API users and for reading usage from the account.** (Operator, 2026-10-25: "1, but plan for api users and reading from account for some users,  but me but plan for it".) For this operator the reading is typed by hand. The reading source is a pluggable input: typed, pasted usage text, and an account or API source for users who can read usage from the account or an API, each producing the same reading record. Only the typed source is built first; the others are planned for, not built. An API user's usage may be spend and tokens and not a weekly percentage, so the reading record must be able to carry either a percentage or a spend figure.
6. **Remaining work for "Run flat out": both.** (Operator, 2026-10-25: "3".) The calculator computes an estimate from the queued Automata times a cost per task, and the operator can overwrite it by typing a figure. The computed estimate is labelled as an estimate, the typed figure replaces it and is labelled as typed, and the queue's task count is shown beside both. The cost per task is a setting, and the screen must make clear which of the two figures it is using.
7. **Reset moment: a weekday, a local clock time and a time zone name.** (Operator, 2026-10-25: "1, zone name".) For example Sunday 08:00 in the America/New_York zone; the calculator follows the zone's own rules and stays correct across a daylight saving change. A fixed UTC offset is not used.

## Still open (asked in order)

8. Whether a reading belongs to one daemon or to the shared account across federated daemons.
9. How bought extra credit appears next to the weekly allowance.
10. Whether the older 22:08 reading entry should be corrected.
