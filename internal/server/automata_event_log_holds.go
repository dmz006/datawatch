package server

// Expired-hold entries in the Automata event log.
//
// When an operator hold's look-again time passes, the autonomous manager
// marks the hold expired and raises one alert. The event log shows the same
// state: exactly one alert event per expired hold, derived from the stored
// hold record. A hold without a look-again time never expires, and a hold
// that was released or given a new look-again time (the expiry mark cleared)
// shows no entry.

import (
	"fmt"
	"time"
)

// automataHoldsSource returns the stored Automata (as raw values) whose hold
// state the adapter reads. A nil source means the daemon does not expose the
// hold state, so the adapter is skipped.
type automataHoldsSource func() []any

// AutomataEventsFromHolds reads the held Automata through src and returns one
// alert event per expired hold: warn severity, a stable id built from the
// Automaton id and the ExpiredAt time, the project and Automaton fields
// filled, and a summary that says the Automaton is ready to review. Holds
// that have not expired, and released or extended holds (ExpiredAt cleared),
// produce no entry. A nil src is an error, not an empty log.
func AutomataEventsFromHolds(src automataHoldsSource) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataSourceNil
	}
	var out []AutomataEvent
	for _, raw := range src() {
		p, err := toPRD(raw)
		if err != nil || p == nil {
			continue
		}
		if p.Hold == nil || p.Hold.ExpiredAt == nil {
			continue
		}
		name := p.Title
		if name == "" {
			name = p.ID
		}
		expiredAt := p.Hold.ExpiredAt.UTC()
		out = append(out, AutomataEvent{
			ID:         fmt.Sprintf("alert:hold_expired:%s:%s", p.ID, expiredAt.Format(time.RFC3339Nano)),
			Time:       expiredAt,
			Kind:       AutomataEventAlert,
			Project:    p.ProjectDir,
			Automaton:  name,
			Automation: p.AutomationID,
			Summary:    "Hold expired: " + name + " is ready to review",
			Detail:     "hold reason: " + p.Hold.Reason,
			NextAction: "Release the hold, or set a new hold-until time",
			Severity:   AutomataSeverityWarn,
		})
	}
	return out, nil
}
