package server

// Automata event log record.
//
// AutomataEvent is one line in the operator-facing event log for Automata,
// Automatons and Automations: a watch observation, a decision, a retry, a
// check, a cleanup, an alert or an error. This file defines the record, the
// merge of several event slices into one newest-first list, and an opaque
// before-cursor for paging through that list. It holds no I/O.
//
// Terminology: "Automaton" is one unit of autonomous work and "Automation" is
// the chain object. Summary and next-action text must never say "PRD".

import (
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// AutomataEventKind names what an event records.
type AutomataEventKind string

const (
	AutomataEventWatch    AutomataEventKind = "watch"
	AutomataEventDecision AutomataEventKind = "decision"
	AutomataEventRetry    AutomataEventKind = "retry"
	AutomataEventCheck    AutomataEventKind = "check"
	AutomataEventCleanup  AutomataEventKind = "cleanup"
	AutomataEventAlert    AutomataEventKind = "alert"
	AutomataEventError    AutomataEventKind = "error"
)

// AutomataEventSeverity ranks how much attention an event needs.
type AutomataEventSeverity string

const (
	AutomataSeverityInfo  AutomataEventSeverity = "info"
	AutomataSeverityWarn  AutomataEventSeverity = "warn"
	AutomataSeverityError AutomataEventSeverity = "error"
)

// AutomataEvent is one entry in the Automata event log.
//
// Time is stored in UTC and serialised as RFC3339. Project, Automaton and
// Automation may each be empty when the event is not tied to that object.
// NextAction is empty when nothing is needed from the operator. Quiet marks
// harmless entries that a UI may fold away by default.
type AutomataEvent struct {
	ID         string                `json:"id"`
	Time       time.Time             `json:"time"`
	Kind       AutomataEventKind     `json:"kind"`
	Project    string                `json:"project,omitempty"`
	Automaton  string                `json:"automaton,omitempty"`
	Automation string                `json:"automation,omitempty"`
	Summary    string                `json:"summary"`
	Detail     string                `json:"detail,omitempty"`
	Reason     string                `json:"reason,omitempty"`
	NextAction string                `json:"next_action,omitempty"`
	Quiet      bool                  `json:"quiet,omitempty"`
	Severity   AutomataEventSeverity `json:"severity"`
}

// prdWordPattern matches the internal name that must not reach operator text.
var prdWordPattern = regexp.MustCompile(`\bPRDs?\b`)

// Valid reports whether the event has a stable id, a known kind and severity,
// a one-line summary, and operator text that uses Automaton/Automata wording.
func (e AutomataEvent) Valid() error {
	if strings.TrimSpace(e.ID) == "" {
		return errors.New("automata event: id is empty")
	}
	switch e.Kind {
	case AutomataEventWatch, AutomataEventDecision, AutomataEventRetry,
		AutomataEventCheck, AutomataEventCleanup, AutomataEventAlert, AutomataEventError:
	default:
		return fmt.Errorf("automata event %q: unknown kind %q", e.ID, e.Kind)
	}
	switch e.Severity {
	case AutomataSeverityInfo, AutomataSeverityWarn, AutomataSeverityError:
	default:
		return fmt.Errorf("automata event %q: unknown severity %q", e.ID, e.Severity)
	}
	if strings.ContainsAny(e.Summary, "\r\n") {
		return fmt.Errorf("automata event %q: summary must be one line", e.ID)
	}
	if prdWordPattern.MatchString(e.Summary) || prdWordPattern.MatchString(e.NextAction) {
		return fmt.Errorf("automata event %q: summary and next_action must say Automaton or Automata, not PRD", e.ID)
	}
	return nil
}

// AutomataEventCursor marks a position in the newest-first event list. A
// caller pages backwards by passing the cursor of the last event it received.
type AutomataEventCursor struct {
	Time time.Time
	ID   string
}

// Encode returns an opaque, URL-safe token for the cursor.
func (c AutomataEventCursor) Encode() string {
	raw := c.Time.UTC().Format(time.RFC3339Nano) + "|" + c.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeAutomataEventCursor parses a token produced by Encode. An empty token
// returns a zero cursor and no error, which means "from the newest event".
func DecodeAutomataEventCursor(token string) (AutomataEventCursor, error) {
	if token == "" {
		return AutomataEventCursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return AutomataEventCursor{}, fmt.Errorf("automata event cursor: %w", err)
	}
	ts, id, ok := strings.Cut(string(b), "|")
	if !ok || id == "" {
		return AutomataEventCursor{}, errors.New("automata event cursor: malformed token")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return AutomataEventCursor{}, fmt.Errorf("automata event cursor: %w", err)
	}
	return AutomataEventCursor{Time: t.UTC(), ID: id}, nil
}

// IsZero reports whether the cursor is the "from the newest event" start.
func (c AutomataEventCursor) IsZero() bool {
	return c.Time.IsZero() && c.ID == ""
}

// MergeAutomataEvents concatenates several event slices into one list sorted
// newest first. Events with the same time are ordered by id ascending, so the
// result is deterministic. Inputs are not modified. Nil or empty input yields
// an empty, non-nil slice.
func MergeAutomataEvents(lists ...[]AutomataEvent) []AutomataEvent {
	total := 0
	for _, l := range lists {
		total += len(l)
	}
	out := make([]AutomataEvent, 0, total)
	for _, l := range lists {
		out = append(out, l...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return automataEventBefore(out[i], out[j])
	})
	return out
}

// AutomataEventsBefore returns up to limit events that come strictly after the
// cursor in a list already sorted by MergeAutomataEvents. A zero cursor starts
// at the newest event. A limit of zero or less returns every remaining event.
func AutomataEventsBefore(sorted []AutomataEvent, cursor AutomataEventCursor, limit int) []AutomataEvent {
	start := 0
	if !cursor.IsZero() {
		start = len(sorted)
		for i, e := range sorted {
			if automataEventAfterCursor(e, cursor) {
				start = i
				break
			}
		}
	}
	end := len(sorted)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	out := make([]AutomataEvent, end-start)
	copy(out, sorted[start:end])
	return out
}

// automataEventBefore orders newer events first, then lower id first.
func automataEventBefore(a, b AutomataEvent) bool {
	if !a.Time.Equal(b.Time) {
		return a.Time.After(b.Time)
	}
	return a.ID < b.ID
}

// automataEventAfterCursor reports whether e sorts after the cursor position.
func automataEventAfterCursor(e AutomataEvent, c AutomataEventCursor) bool {
	if !e.Time.Equal(c.Time) {
		return e.Time.Before(c.Time)
	}
	return e.ID > c.ID
}
