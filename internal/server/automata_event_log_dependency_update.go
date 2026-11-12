package server

// Automata event log: weekly dependency update source.
//
// Each finished weekly run becomes one event naming the run and its one-line
// summary when it updated something or needs attention. A run whose outcome
// is nothing to update writes no row, because a status that is none is not
// shown. The source arrives through a function type, so the adapter is
// testable without a running daemon. Operator text says Automaton, never PRD.

import (
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// automataDependencyUpdateRun is one finished weekly run as the log sees it.
type automataDependencyUpdateRun struct {
	Title  string
	At     time.Time
	Report autonomous.DependencyUpdateReport
}

// automataDependencyUpdateSource returns the finished weekly runs.
type automataDependencyUpdateSource func() []automataDependencyUpdateRun

// automataDependencyUpdateEvents converts runs into events.
func automataDependencyUpdateEvents(runs []automataDependencyUpdateRun) []AutomataEvent {
	var out []AutomataEvent
	for _, r := range runs {
		ev := AutomataEvent{
			ID:      fmt.Sprintf("dependency_update:%s:%d", r.Title, r.At.UTC().Unix()),
			Time:    r.At.UTC(),
			Kind:    AutomataEventCheck,
			Summary: fmt.Sprintf("Weekly dependency update %s: %s", r.Title, r.Report.Summary),
		}
		switch r.Report.Outcome {
		case autonomous.DependencyUpdateUpdated:
			ev.Severity = AutomataSeverityInfo
		case autonomous.DependencyUpdateNeedsAttention:
			ev.Severity = AutomataSeverityWarn
			ev.NextAction = "Open the Automaton and review what was dropped or what is waiting for your decision."
		default:
			continue // nothing to update: no row
		}
		out = append(out, ev)
	}
	return out
}

// AutomataEventsFromDependencyUpdate reads the weekly runs through src and
// returns their events. A nil src is an error, not an empty log.
func AutomataEventsFromDependencyUpdate(src automataDependencyUpdateSource) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataSourceNil
	}
	return automataDependencyUpdateEvents(src()), nil
}
