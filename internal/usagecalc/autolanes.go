package usagecalc

import "fmt"

// Auto-lane decision directions, reported against the cap currently applied.
const (
	AutoLanesUp        = "up"
	AutoLanesDown      = "down"
	AutoLanesUnchanged = "unchanged"
)

// AutoLaneDecision is the outcome of the auto-lane rule for one advice reading: the
// target inflight cap, how it moves against the cap currently applied, and the reason
// that names the reading the target was built from.
type AutoLaneDecision struct {
	// Target is the inflight lane cap to apply. It is at least 1 and never above the
	// operator maximum, so it is never zero.
	Target int `json:"target"`
	// Direction is AutoLanesUp, AutoLanesDown or AutoLanesUnchanged against CurrentCap.
	Direction string `json:"direction"`
	// Reason names the reading (the advice lanes of the active policy) and the rule
	// applied to it.
	Reason string `json:"reason"`
}

// DecideAutoLanes is the pure auto-lane rule applied to one advice reading. It takes the
// lanes suggested by the active policy, the count of open special items, the operator
// maximum and the cap currently applied, and it does no I/O and reads no clock: every
// input is a parameter, so the same inputs always give the same decision.
//
// The rule: target = the advice lanes plus the special items, clamped to between 1 and
// the operator maximum. When the target equals the cap currently applied, the decision
// reports unchanged; otherwise it reports up or down. The target is never zero: a
// reading of zero lanes, or an operator maximum below 1 that would make the clamp range
// empty, both settle at 1 lane.
func DecideAutoLanes(suggestedLanes, specialItems, operatorMaximum, currentCap int) AutoLaneDecision {
	sum := suggestedLanes + specialItems
	hi := operatorMaximum
	if hi < 1 {
		hi = 1
	}
	target := clampInt(sum, 1, hi)

	direction := AutoLanesUnchanged
	switch {
	case target > currentCap:
		direction = AutoLanesUp
	case target < currentCap:
		direction = AutoLanesDown
	}

	reason := fmt.Sprintf("the advice reading is %d lane(s) for the active policy; "+
		"with %d open special item(s) that is %d, clamped to %d within the operator maximum of %d",
		suggestedLanes, specialItems, sum, target, hi)

	return AutoLaneDecision{Target: target, Direction: direction, Reason: reason}
}
