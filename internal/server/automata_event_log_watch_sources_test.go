package server

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestAutomataEventWatchSourcesFindings(t *testing.T) {
	seen := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	alerted := seen.Add(-time.Minute)
	findings := []*autonomous.SupervisorWatchFinding{
		{
			Kind:        autonomous.SupervisorWatchTaskFailed,
			Subject:     "auto-42",
			Text:        "Task of PRD auto-42 failed",
			FirstSeenAt: seen.Add(-time.Hour),
			LastSeenAt:  seen,
			AlertedAt:   &alerted,
		},
		{
			Kind:        autonomous.SupervisorWatchCIRed,
			Subject:     "run-9",
			Text:        "CI run run-9 is red",
			FirstSeenAt: seen.Add(-2 * time.Hour),
			LastSeenAt:  seen.Add(-time.Hour),
			Resolved:    true,
		},
		{
			// No LastSeenAt: the event falls back to FirstSeenAt.
			Kind:        autonomous.SupervisorWatchMainCheckoutDirty,
			Subject:     "/checkout",
			FirstSeenAt: seen.Add(-3 * time.Hour),
		},
	}

	got, err := AutomataEventsFromSupervisorWatch(func() []*autonomous.SupervisorWatchFinding { return findings })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d events, want 3", len(got))
	}

	// Automaton is set only for the Automaton-subject kind.
	if got[0].Kind != AutomataEventWatch || got[0].Automaton != "auto-42" || got[0].Project != "" || got[0].Automation != "" {
		t.Errorf("task_failed event = %+v, want watch kind with automaton auto-42 only", got[0])
	}
	if got[0].Summary != "Task of Automaton auto-42 failed" {
		t.Errorf("summary = %q, want PRD rewritten to Automaton", got[0].Summary)
	}
	if got[0].Severity != AutomataSeverityWarn {
		t.Errorf("alerted open finding severity = %q, want warn", got[0].Severity)
	}
	if !got[0].Time.Equal(seen) {
		t.Errorf("time = %v, want last seen %v", got[0].Time, seen)
	}

	// Resolved CI finding: quiet, info, and no Automaton (subject is a CI run).
	if got[1].Automaton != "" || !got[1].Quiet || got[1].Severity != AutomataSeverityInfo {
		t.Errorf("resolved ci event = %+v, want quiet info with no automaton", got[1])
	}

	// Checkout finding: time falls back to first seen; nothing invented.
	if !got[2].Time.Equal(seen.Add(-3*time.Hour)) || got[2].Automaton != "" || got[2].Project != "" {
		t.Errorf("fallback event = %+v", got[2])
	}
	if got[2].Summary == "" {
		t.Errorf("empty text must fall back to a kind and subject summary")
	}

	for _, e := range got {
		if err := e.Valid(); err != nil {
			t.Errorf("event %q invalid: %v", e.ID, err)
		}
	}
}

func TestAutomataEventWatchSourcesFindingsSkipsNilAndKeepsIDsUnique(t *testing.T) {
	at := time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)
	findings := []*autonomous.SupervisorWatchFinding{
		nil,
		{Kind: autonomous.SupervisorWatchCIRed, Subject: "a", Text: "red one\nsecond line", LastSeenAt: at},
		{Kind: autonomous.SupervisorWatchCIRed, Subject: "b", Text: "red two", LastSeenAt: at},
	}
	got, err := AutomataEventsFromSupervisorWatch(func() []*autonomous.SupervisorWatchFinding { return findings })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (nil skipped)", len(got))
	}
	if got[0].ID == got[1].ID {
		t.Errorf("ids collide: %q", got[0].ID)
	}
	if strings.ContainsAny(got[0].Summary, "\r\n") || got[0].Summary != "red one second line" {
		t.Errorf("summary = %q, want one line", got[0].Summary)
	}
}

func TestAutomataEventWatchSourcesFindingsEmptyAndNilSource(t *testing.T) {
	got, err := AutomataEventsFromSupervisorWatch(func() []*autonomous.SupervisorWatchFinding { return nil })
	if err != nil || len(got) != 0 {
		t.Fatalf("empty source = %v, %v; want no events, no error", got, err)
	}
	if _, err := AutomataEventsFromSupervisorWatch(nil); err == nil {
		t.Fatal("nil watch source must be an error")
	}
}

func TestAutomataEventWatchSourcesDecisions(t *testing.T) {
	at := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)
	later := at.Add(5 * time.Minute)
	decisions := []autonomous.LaneFillDecision{
		{
			At: at, LaneID: "gpu-1", LaneType: autonomous.LaneTypeGPU,
			Action: autonomous.LaneFillActionStartPRD, PRDID: "auto-7", Title: "Add cache",
			Reason: "lane idle with eligible work", Executed: true, LastSeenAt: later, RepeatCount: 2,
		},
		{
			At: at, LaneID: "claude-1", LaneType: autonomous.LaneTypeClaude,
			Action: autonomous.LaneFillActionBlocked, Blocker: "PRD needs a dependency",
		},
		{
			At: at.Add(time.Minute), LaneID: "claude-2", LaneType: autonomous.LaneTypeClaude,
			Action: autonomous.LaneFillActionDecompose, PRDID: "auto-8", Title: "Ship notes",
			Error: "decompose failed",
		},
		{
			At: at, LaneID: "gpu-2", LaneType: autonomous.LaneTypeGPU,
			Action: autonomous.LaneFillActionIdle,
		},
	}

	var askedLimit int
	got, err := AutomataEventsFromLaneFillDecisions(func(limit int) []autonomous.LaneFillDecision {
		askedLimit = limit
		return decisions
	}, 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if askedLimit != 25 {
		t.Errorf("source asked for limit %d, want 25", askedLimit)
	}
	if len(got) != 4 {
		t.Fatalf("got %d events, want 4", len(got))
	}

	start := got[0]
	if start.Kind != AutomataEventDecision || start.Automaton != "auto-7" || start.Project != "" || start.Automation != "" {
		t.Errorf("start event = %+v, want decision with automaton auto-7 only", start)
	}
	if !start.Time.Equal(later) {
		t.Errorf("repeated decision time = %v, want last seen %v", start.Time, later)
	}
	if start.Summary != `Lane gpu-1 (gpu): start Automaton "Add cache"` {
		t.Errorf("start summary = %q", start.Summary)
	}
	if start.Severity != AutomataSeverityInfo || start.Quiet {
		t.Errorf("start severity/quiet = %q/%v", start.Severity, start.Quiet)
	}

	blocked := got[1]
	if blocked.Severity != AutomataSeverityWarn || blocked.Automaton != "" || blocked.Detail != "Automaton needs a dependency" {
		t.Errorf("blocked event = %+v", blocked)
	}

	failed := got[2]
	if failed.Severity != AutomataSeverityError || failed.Detail != "decompose failed" || failed.Automaton != "auto-8" {
		t.Errorf("error event = %+v", failed)
	}

	idle := got[3]
	if !idle.Quiet || idle.Automaton != "" || idle.Summary != "Lane gpu-2 (gpu): idle" {
		t.Errorf("idle event = %+v", idle)
	}

	for _, e := range got {
		if err := e.Valid(); err != nil {
			t.Errorf("event %q invalid: %v", e.ID, err)
		}
	}
}

func TestAutomataEventWatchSourcesDecisionsGuards(t *testing.T) {
	if _, err := AutomataEventsFromLaneFillDecisions(nil, 10); err == nil {
		t.Fatal("nil decision source must be an error")
	}
	if _, err := AutomataEventsFromLaneFillDecisions(func(int) []autonomous.LaneFillDecision { return nil }, 0); err == nil {
		t.Fatal("zero limit must be an error")
	}
	got, err := AutomataEventsFromLaneFillDecisions(func(int) []autonomous.LaneFillDecision { return nil }, 5)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty decisions = %v, %v", got, err)
	}
}

func TestAutomataEventWatchSourcesMergeWithLog(t *testing.T) {
	at := time.Date(2026, 11, 9, 13, 0, 0, 0, time.UTC)
	watch, err := AutomataEventsFromSupervisorWatch(func() []*autonomous.SupervisorWatchFinding {
		return []*autonomous.SupervisorWatchFinding{{Kind: autonomous.SupervisorWatchCIRed, Subject: "r", Text: "red", LastSeenAt: at}}
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := AutomataEventsFromLaneFillDecisions(func(int) []autonomous.LaneFillDecision {
		return []autonomous.LaneFillDecision{{At: at.Add(time.Minute), LaneID: "g", Action: autonomous.LaneFillActionIdle}}
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	merged := MergeAutomataEvents(watch, dec)
	if len(merged) != 2 || merged[0].Kind != AutomataEventDecision || merged[1].Kind != AutomataEventWatch {
		t.Fatalf("merged order wrong: %+v", merged)
	}
}
