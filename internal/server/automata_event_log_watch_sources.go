package server

// Automata event log: watch and lane-fill decision sources.
//
// Two adapters turn records the daemon already keeps into AutomataEvent
// values for the operator event log: supervisor watch findings (kind watch)
// and lane fill recent decisions (kind decision). The source data arrives
// through small function types, so the adapters are testable without a
// running daemon and nothing here reads the server's handlers. Project,
// Automaton and Automation are set only where the source record names them;
// they are never invented. Operator text says Automaton or Automata, never
// PRD.

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// automataWatchFindingsSource returns the stored supervisor watch findings,
// open and resolved alike. It stands in for the supervisor watch read view.
type automataWatchFindingsSource func() []*autonomous.SupervisorWatchFinding

// automataLaneDecisionsSource returns up to limit recent lane fill decisions,
// newest first. It stands in for the lane fill status decision history.
type automataLaneDecisionsSource func(limit int) []autonomous.LaneFillDecision

// automataWatchAutomatonKinds lists the finding kinds whose Subject is an
// Automaton id. The other kinds name a node, a lane, a CI run or the main
// checkout, so they never set the Automaton field.
var automataWatchAutomatonKinds = map[autonomous.SupervisorWatchKind]bool{
	autonomous.SupervisorWatchTaskFailed:                true,
	autonomous.SupervisorWatchTaskReviewNeeded:          true,
	autonomous.SupervisorWatchAutomatonNotMerged:        true,
	autonomous.SupervisorWatchWorktreeNoGomod:           true,
	autonomous.SupervisorWatchWorktreeNotRegistered:     true,
	autonomous.SupervisorWatchWorktreeSilent:            true,
	autonomous.SupervisorWatchVerifyScriptNotExecutable: true,
}

// automataPRDWord matches the internal name, which operator text must not use.
var automataPRDWord = regexp.MustCompile(`\bPRD(s?)\b`)

// automataOperatorText makes text one line and replaces the internal PRD name
// with Automaton or Automata, so the event passes AutomataEvent.Valid.
func automataOperatorText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return automataPRDWord.ReplaceAllStringFunc(s, func(m string) string {
		if strings.HasSuffix(m, "s") {
			return "Automata"
		}
		return "Automaton"
	})
}

// automataWatchEvents converts supervisor watch findings into watch events.
// A finding's time is its last sighting, or its first when that is unset.
// Open findings are warn when an alert was raised and info otherwise; a
// resolved finding is info and quiet.
func automataWatchEvents(findings []*autonomous.SupervisorWatchFinding) []AutomataEvent {
	out := make([]AutomataEvent, 0, len(findings))
	for _, f := range findings {
		if f == nil {
			continue
		}
		at := f.LastSeenAt
		if at.IsZero() {
			at = f.FirstSeenAt
		}
		summary := automataOperatorText(f.Text)
		if summary == "" {
			summary = automataOperatorText(fmt.Sprintf("%s on %s", f.Kind, f.Subject))
		}
		ev := AutomataEvent{
			ID:       fmt.Sprintf("watch:%s:%s:%d", f.Kind, f.Subject, at.UnixNano()),
			Time:     at.UTC(),
			Kind:     AutomataEventWatch,
			Summary:  summary,
			Severity: AutomataSeverityInfo,
			Quiet:    f.Resolved,
		}
		if automataWatchAutomatonKinds[f.Kind] && f.Subject != "" {
			ev.Automaton = f.Subject
		}
		if !f.Resolved && f.AlertedAt != nil {
			ev.Severity = AutomataSeverityWarn
		}
		out = append(out, ev)
	}
	return out
}

// automataDecisionActionText names each lane fill action in operator words.
var automataDecisionActionText = map[autonomous.LaneFillAction]string{
	autonomous.LaneFillActionStartPRD:       "start Automaton",
	autonomous.LaneFillActionReportMerge:    "report merge for Automaton",
	autonomous.LaneFillActionDecompose:      "decompose Automaton",
	autonomous.LaneFillActionDecomposeAhead: "decompose ahead for Automaton",
	autonomous.LaneFillActionBlocked:        "blocked",
	autonomous.LaneFillActionIdle:           "idle",
}

// automataDecisionEvents converts lane fill decisions into decision events.
// The event time is the last sighting of a repeated decision, or its first
// decision when no sighting is recorded. A decision whose Automaton id is
// empty keeps the Automaton field empty. An executed-with-error decision is
// error severity; a blocked decision is warn; an idle decision is quiet.
func automataDecisionEvents(decisions []autonomous.LaneFillDecision) []AutomataEvent {
	out := make([]AutomataEvent, 0, len(decisions))
	for _, d := range decisions {
		at := d.LastSeenAt
		if at.IsZero() {
			at = d.At
		}
		action := string(d.Action)
		if text, ok := automataDecisionActionText[d.Action]; ok {
			action = text
		}
		summary := fmt.Sprintf("Lane %s (%s): %s", d.LaneID, d.LaneType, action)
		if d.Title != "" && d.Action != autonomous.LaneFillActionBlocked && d.Action != autonomous.LaneFillActionIdle {
			summary += fmt.Sprintf(" %q", d.Title)
		}
		ev := AutomataEvent{
			ID:        fmt.Sprintf("decision:%s:%s:%s:%d", d.LaneID, d.Action, d.PRDID, at.UnixNano()),
			Time:      at.UTC(),
			Kind:      AutomataEventDecision,
			Automaton: d.PRDID,
			Summary:   automataOperatorText(summary),
			Reason:    automataOperatorText(d.Reason),
			Detail:    automataOperatorText(d.Blocker),
			Severity:  AutomataSeverityInfo,
			Quiet:     d.Action == autonomous.LaneFillActionIdle,
		}
		switch {
		case d.Error != "":
			ev.Severity = AutomataSeverityError
			ev.Detail = automataOperatorText(d.Error)
		case d.Action == autonomous.LaneFillActionBlocked:
			ev.Severity = AutomataSeverityWarn
		}
		out = append(out, ev)
	}
	return out
}

// errAutomataSourceNil is returned when an adapter is given no source.
var errAutomataSourceNil = errors.New("automata event watch source: source is nil")

// AutomataEventsFromSupervisorWatch reads the findings through src and returns
// them as watch events. A nil src is an error, not an empty log.
func AutomataEventsFromSupervisorWatch(src automataWatchFindingsSource) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataSourceNil
	}
	return automataWatchEvents(src()), nil
}

// AutomataEventsFromLaneFillDecisions reads up to limit recent decisions
// through src and returns them as decision events. A nil src is an error.
func AutomataEventsFromLaneFillDecisions(src automataLaneDecisionsSource, limit int) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataSourceNil
	}
	if limit <= 0 {
		return nil, fmt.Errorf("automata event decision source: limit must be positive, got %d", limit)
	}
	return automataDecisionEvents(src(limit)), nil
}
