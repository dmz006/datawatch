package server

// Automata event log: infrastructure retry and check sources.
//
// Two adapters turn records the daemon already keeps into AutomataEvent
// values for the operator event log: infrastructure retries and holds (kind
// retry), taken from the infra retry read behind GET /api/autonomous/infra-retries,
// and recorded check findings (kind check), taken from the checks read behind
// GET /api/autonomous/checks. The source data arrives through small function
// types, so the adapters are testable without a running daemon and nothing
// here calls the handlers. Project, Automaton and Automation are set only where
// the source record names them; they are never invented. Operator text says
// Automaton or Automata, never PRD.

import (
	"errors"
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// automataInfraRetryTasksSource returns up to limit tasks with infrastructure
// retries or held for an infrastructure reason, most recently retried first.
// It stands in for the infra retry status read.
type automataInfraRetryTasksSource func(limit int) []autonomous.InfraRetryTask

// automataChecksFindingsSource returns the recorded check findings, newest
// first. It stands in for the findings list of the checks status read.
type automataChecksFindingsSource func() []autonomous.StatusFinding

// automataRetryTime is the time of the latest retry decision for a task, or the
// zero time when the task has no recorded retry (a hold with no history).
func automataRetryTime(t autonomous.InfraRetryTask) time.Time {
	var at time.Time
	for _, r := range t.Retries {
		if r.At.After(at) {
			at = r.At
		}
	}
	return at
}

// automataLatestRetry returns the retry entry with the highest number, and
// whether there is one.
func automataLatestRetry(t autonomous.InfraRetryTask) (autonomous.InfraRetryEntry, bool) {
	var latest autonomous.InfraRetryEntry
	found := false
	for _, r := range t.Retries {
		if !found || r.Number > latest.Number {
			latest = r
			found = true
		}
	}
	return latest, found
}

// automataRetryEvents converts infrastructure retry tasks into retry events.
// A held task is warn and says what the operator must do; a retried task is
// info and says when its next retry is due. A task with no retries and no hold
// is skipped, because it has nothing to record.
func automataRetryEvents(tasks []autonomous.InfraRetryTask) []AutomataEvent {
	out := make([]AutomataEvent, 0, len(tasks))
	for _, t := range tasks {
		latest, hasRetry := automataLatestRetry(t)
		if !hasRetry && !t.Held {
			continue
		}
		ev := AutomataEvent{
			ID:        fmt.Sprintf("retry:%s:%s:%d", t.AutomatonID, t.TaskID, automataRetryTime(t).UnixNano()),
			Time:      automataRetryTime(t).UTC(),
			Kind:      AutomataEventRetry,
			Automaton: t.AutomatonID,
			Severity:  AutomataSeverityInfo,
		}
		if hasRetry {
			ev.Summary = fmt.Sprintf("Task %s of Automaton %s retried after %s failure, attempt %d of %d",
				t.TaskID, t.AutomatonID, latest.Class, latest.Number, t.MaxTries)
			ev.Reason = latest.Reason
		} else {
			ev.Summary = fmt.Sprintf("Task %s of Automaton %s held with no infrastructure retries", t.TaskID, t.AutomatonID)
		}
		if t.Held {
			ev.Severity = AutomataSeverityWarn
			ev.Summary = fmt.Sprintf("Task %s of Automaton %s held after %d of %d infrastructure retries",
				t.TaskID, t.AutomatonID, t.RetryCount, t.MaxTries)
			if t.HeldCause != "" {
				ev.Reason = t.HeldCause
			}
			if t.QuestionID != "" {
				ev.NextAction = "Answer the open question to resume or stop the held task."
			} else {
				ev.NextAction = "Review the held task and decide whether to retry it."
			}
		} else if hasRetry && !latest.NextAt.IsZero() {
			ev.NextAction = fmt.Sprintf("Retry %d runs at %s.", latest.Number, latest.NextAt.UTC().Format(time.RFC3339))
		}
		ev.Summary = automataOperatorText(ev.Summary)
		ev.Reason = automataOperatorText(ev.Reason)
		ev.NextAction = automataOperatorText(ev.NextAction)
		out = append(out, ev)
	}
	return out
}

// automataCheckEvents converts recorded check findings into check events. The
// finding's time is its recorded time. The source names the check that wrote
// it and is kept as the reason. An empty finding text falls back to the source.
func automataCheckEvents(findings []autonomous.StatusFinding) []AutomataEvent {
	out := make([]AutomataEvent, 0, len(findings))
	for _, f := range findings {
		summary := automataOperatorText(f.Text)
		if summary == "" {
			summary = automataOperatorText(fmt.Sprintf("Check %s recorded a finding", f.Source))
		}
		out = append(out, AutomataEvent{
			ID:        fmt.Sprintf("check:%s:%s:%d", f.Source, f.PRDID, f.At.UnixNano()),
			Time:      f.At.UTC(),
			Kind:      AutomataEventCheck,
			Automaton: f.PRDID,
			Summary:   summary,
			Reason:    automataOperatorText(f.Source),
			Severity:  AutomataSeverityInfo,
		})
	}
	return out
}

// errAutomataRetryCheckSourceNil is returned when an adapter is given no source.
var errAutomataRetryCheckSourceNil = errors.New("automata event retry/check source: source is nil")

// AutomataEventsFromInfraRetries reads up to limit retry tasks through src and
// returns them as retry events. A nil src is an error, not an empty log. A
// limit below 1 is an error, because the read needs a positive count.
func AutomataEventsFromInfraRetries(src automataInfraRetryTasksSource, limit int) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataRetryCheckSourceNil
	}
	if limit <= 0 {
		return nil, fmt.Errorf("automata event retry source: limit must be positive, got %d", limit)
	}
	return automataRetryEvents(src(limit)), nil
}

// AutomataEventsFromChecks reads the recorded check findings through src and
// returns them as check events. A nil src is an error.
func AutomataEventsFromChecks(src automataChecksFindingsSource) ([]AutomataEvent, error) {
	if src == nil {
		return nil, errAutomataRetryCheckSourceNil
	}
	return automataCheckEvents(src()), nil
}
