# BL472 usage and cost calculator: operator decisions

Date started: 2026-10-25. The ten open questions of `docs/plans/2026-10-23-usage-calculator-design.md` are answered one at a time, with the operator's words quoted. The implementation PRD reads this file and does not ask again.

## Decisions

1. **Safety margin for "Pace to the week": 5 points as the default, and a setting.** (Operator, 2026-10-25: "1, 5 points as a setting".) The calculator advises against a target five points under the pace line unless the operator changes the setting.

## Still open (asked in order)

2. Whether the hourly session window gets its own verdict.
3. Whether burn per lane is inferred per model (Haiku and Sonnet separately).
4. What happens when there is no previous reading within 2 hours.
5. Whether readings are typed by hand only or also pasted from the usage screen text.
6. Where the remaining work for "Run flat out" comes from.
7. How the reset moment is stored across a daylight saving change.
8. Whether a reading belongs to one daemon or to the shared account across federated daemons.
9. How bought extra credit appears next to the weekly allowance.
10. Whether the older 22:08 reading entry should be corrected.
