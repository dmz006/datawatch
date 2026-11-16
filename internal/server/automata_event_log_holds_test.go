package server

// Expired-hold entries in the Automata event log.
//
// Test coverage:
//
//   - TestHoldExpiredEventRowAppearsOnce — an expired hold shows exactly
//     one alert row with the expected fields; held-not-expired and released
//     Automata show none;
//   - TestHoldEventRowGoneAfterRelease — the row disappears once the hold
//     is released (the hold record is cleared).

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// holdLogFixture is one PRD in a shape the adapter can read.
func holdLogPRD(status autonomous.PRDStatus, hold *autonomous.HoldState) *autonomous.PRD {
	return &autonomous.PRD{
		ID:           "prd-hold-1",
		Title:        "alpha",
		ProjectDir:   "/proj",
		AutomationID: "auto-1",
		Status:       status,
		Hold:         hold,
	}
}

func TestHoldExpiredEventRowAppearsOnce(t *testing.T) {
	until := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	expired := time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC)

	// One expired hold, one held-not-expired hold and one released
	// Automaton (no hold record) in the same pool.
	src := func() []any {
		return []any{
			holdLogPRD(autonomous.PRDHeld, &autonomous.HoldState{Reason: "wait for the release", Until: &until, ExpiredAt: &expired}),
			holdLogPRD(autonomous.PRDHeld, &autonomous.HoldState{Reason: "wait for the release", Until: &until}),
			holdLogPRD(autonomous.PRDBlocked, nil),
		}
	}

	evs, err := AutomataEventsFromHolds(src)
	if err != nil {
		t.Fatalf("AutomataEventsFromHolds: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %d, want exactly 1: %+v", len(evs), evs)
	}
	ev := evs[0]
	if err := ev.Valid(); err != nil {
		t.Fatalf("event is invalid: %v", err)
	}
	if ev.Kind != AutomataEventAlert {
		t.Errorf("Kind = %q, want alert", ev.Kind)
	}
	if ev.Severity != AutomataSeverityWarn {
		t.Errorf("Severity = %q, want warn", ev.Severity)
	}
	if !strings.Contains(ev.ID, "prd-hold-1") {
		t.Errorf("ID = %q, want it built from the Automaton id", ev.ID)
	}
	if !ev.Time.Equal(expired) {
		t.Errorf("Time = %v, want the ExpiredAt time %v", ev.Time, expired)
	}
	if ev.Project != "/proj" {
		t.Errorf("Project = %q, want /proj", ev.Project)
	}
	if ev.Automaton != "alpha" {
		t.Errorf("Automaton = %q, want the title alpha", ev.Automaton)
	}
	if ev.Automation != "auto-1" {
		t.Errorf("Automation = %q, want auto-1", ev.Automation)
	}
	if want := "Hold expired: alpha is ready to review"; ev.Summary != want {
		t.Errorf("Summary = %q, want %q", ev.Summary, want)
	}
	if !strings.Contains(ev.Detail, "wait for the release") {
		t.Errorf("Detail = %q, want it to carry the hold reason", ev.Detail)
	}
	if ev.NextAction != "Release the hold, or set a new hold-until time" {
		t.Errorf("NextAction = %q, want the release-or-extend hint", ev.NextAction)
	}
}

func TestHoldEventRowGoneAfterRelease(t *testing.T) {
	until := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	expired := time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC)

	p := holdLogPRD(autonomous.PRDHeld, &autonomous.HoldState{Reason: "wait for the release", Until: &until, ExpiredAt: &expired})
	src := func() []any { return []any{p} }

	if evs, err := AutomataEventsFromHolds(src); err != nil || len(evs) != 1 {
		t.Fatalf("before release: events = %d, err = %v, want 1", len(evs), err)
	}

	// The operator releases the hold: the record is cleared.
	p.Hold = nil
	p.Status = autonomous.PRDBlocked

	if evs, err := AutomataEventsFromHolds(src); err != nil || len(evs) != 0 {
		t.Fatalf("after release: events = %d, err = %v, want none", len(evs), err)
	}
}
