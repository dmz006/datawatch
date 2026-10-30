// Package usagecalc is the pure calculation behind the usage calculator: given the
// operator's weekly usage readings, the weekly reset moment and a few settings, Advise
// reports whether the current burn is within or over budget for the week and what each
// choice of lane count would cost.
//
// The package does no I/O and never reads the wall clock. Every moment it uses, including
// "now", comes in through Input. It is a port of UsageModel.advise from the reference
// model in docs/plans/assets/usage-calculator-mockups/variant-pace-gauge.html, with the
// operator decisions of docs/plans/2026-10-25-usage-calculator-decisions.md applied:
// readings that carry a percentage or a spend figure, bought extra credit, a reset moment
// kept as a weekday, a clock time and a time zone name, an explicit zero-burn finish, and
// remaining work that is either typed or estimated from the queue.
//
// The package also keeps a lane history (lane_history.go): a short record of how many
// Claude lanes were running over time. A lane is a distinct holder of a capacity lease on
// a Claude pool, and ClaudeLanes counts them from the leases it is given. LaneHistory asks
// a LaneCounter for the current count, writes a sample whenever the count changes and
// otherwise about once a minute, keeps samples as long as it keeps readings, and saves them
// as lanes.json beside the usage file. Lookup answers how many lanes were running at a
// moment, but only when a sample was taken within the two minutes before it; otherwise it
// says nothing was recorded rather than guess.
//
// Invariants that hold for every input and every policy:
//
//   - The advice never names zero lanes. LanesSuggested is at least 1 for use-all and pace
//     and is the maximum lane count for finish-early.
//   - Switching policy changes only the target, the headline, the notes, the lane
//     suggestion and the finish block of the advice. Every number of the reading is the
//     same under all three policies.
//   - The first note under every policy says the operator chooses the lane count, and the
//     calculator never stops or slows a lane.
//   - No infinite or NaN value appears in the output, so the result always marshals to JSON.
package usagecalc
