package server

// Automata event log: repo CI watch state source.
//
// The repo CI watch read view turns into operator events here. When the
// watch is off the source reports one quiet info event that says how to turn
// it on. When the watch has never polled it reports one info event. When it
// is on, each run that is failing or pending gets one warn event. The source
// arrives through a function type, so the adapter is testable without a
// running daemon, and it never changes the watch itself. Operator text says
// Automaton or Automata, never PRD.

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// automataRepoCIWatchStateSource returns the repo CI watch read view. It
// stands in for autonomous.Manager.RepoCIWatchStatus.
type automataRepoCIWatchStateSource func() autonomous.RepoCIWatchStatus

// automataRepoCIWatchOffNextAction tells the operator how to turn the watch on.
const automataRepoCIWatchOffNextAction = "Turn the repo CI watch on: set autonomous.automation.repo_ci_watch.mode to shadow (log only) or on."

// automataRepoCIWatchEvents converts a repo CI watch state into events.
func automataRepoCIWatchEvents(s autonomous.RepoCIWatchStatus) []AutomataEvent {
	if s.Mode == "" || s.Mode == config.RepoCIWatchModeOff {
		return []AutomataEvent{{
			ID:         "repo_ci_watch:off",
			Time:       s.LastPollAt.UTC(),
			Kind:       AutomataEventWatch,
			Summary:    "Repo CI watch is off",
			NextAction: automataRepoCIWatchOffNextAction,
			Quiet:      true,
			Severity:   AutomataSeverityInfo,
		}}
	}
	if s.LastPollAt.IsZero() {
		return []AutomataEvent{{
			ID:         "repo_ci_watch:never_polled",
			Kind:       AutomataEventWatch,
			Summary:    "Repo CI watch has not polled yet",
			NextAction: "Wait for the first repo CI poll; if it never runs, check the daemon log.",
			Severity:   AutomataSeverityInfo,
		}}
	}
	var out []AutomataEvent
	for _, rec := range s.Records {
		if rec == nil || (rec.Status != "failing" && rec.Status != "pending") {
			continue
		}
		at := rec.UpdatedAt
		if at.IsZero() {
			at = rec.FirstSeenAt
		}
		out = append(out, AutomataEvent{
			ID:       fmt.Sprintf("repo_ci_watch:%s:%s:%s", rec.Repo, rec.RunID, rec.Status),
			Time:     at.UTC(),
			Kind:     AutomataEventWatch,
			Summary:  fmt.Sprintf("CI run %s on %s (%s) is %s", rec.RunID, rec.Repo, rec.RefName, rec.Status),
			Detail:   rec.RunURL,
			Severity: AutomataSeverityWarn,
		})
	}
	return out
}

// AutomataEventsFromRepoCIWatch reads the repo CI watch state through src and
// returns its events. A nil src is an error, not an empty log.
func AutomataEventsFromRepoCIWatch(src automataRepoCIWatchStateSource) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataSourceNil
	}
	return automataRepoCIWatchEvents(src()), nil
}
