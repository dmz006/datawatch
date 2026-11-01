package requestwatch

import "fmt"

// Plain-text explanation of each state, one line each.
const (
	// ExplainGenerating says what the generating state means.
	ExplainGenerating = "The node shows the model working on a request."
	// ExplainIdle says what the idle state means.
	ExplainIdle = "The node is reachable but shows no sign of the model working on the request."
	// ExplainUnreachable says what the unreachable state means.
	ExplainUnreachable = "The node could not be read at the last poll."
	// ExplainUnknown says what the unknown state means.
	ExplainUnknown = "No reading has been taken yet."
)

// Explain returns the one-line explanation of a state.
func Explain(s State) string {
	switch s {
	case StateGenerating:
		return ExplainGenerating
	case StateIdle:
		return ExplainIdle
	case StateUnreachable:
		return ExplainUnreachable
	default:
		return ExplainUnknown
	}
}

// durationText renders a no-progress age as whole minutes, floored, with
// "1 minute" singular and "less than a minute" under 60 seconds.
func durationText(seconds int64) string {
	m := seconds / 60
	switch {
	case m < 1:
		return "less than a minute"
	case m == 1:
		return "1 minute"
	default:
		return fmt.Sprintf("%d minutes", m)
	}
}

// Summary renders the plain-text summary of an observation.
//
//	generating:  "waiting on a model request: generating"
//	idle:        "waiting on a model request: no progress for N minutes"
//	unreachable: "waiting on a model request: node unreachable, no progress for N minutes"
//
// An unknown state, before the first sample, reads as waiting with no
// reading yet.
func Summary(o Observation) string {
	const prefix = "waiting on a model request: "
	switch o.State {
	case StateGenerating:
		return prefix + "generating"
	case StateIdle:
		return prefix + "no progress for " + durationText(o.NoProgressSeconds)
	case StateUnreachable:
		return prefix + "node unreachable, no progress for " + durationText(o.NoProgressSeconds)
	default:
		return prefix + "no reading yet"
	}
}
