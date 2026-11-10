package server

import (
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

func TestAutomataEventWatchOffNilSource(t *testing.T) {
	if _, err := AutomataEventsFromRepoCIWatch(nil); err == nil {
		t.Fatal("nil source must be an error, not an empty log")
	}
}

func TestAutomataEventWatchOffModeOff(t *testing.T) {
	src := func() autonomous.RepoCIWatchStatus {
		return autonomous.RepoCIWatchStatus{Mode: config.RepoCIWatchModeOff}
	}
	got, err := AutomataEventsFromRepoCIWatch(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want exactly 1", len(got))
	}
	e := got[0]
	if e.Kind != AutomataEventWatch || e.Severity != AutomataSeverityInfo || !e.Quiet {
		t.Errorf("off event = %+v, want watch kind, info severity, quiet", e)
	}
	if !strings.Contains(e.NextAction, "autonomous.automation.repo_ci_watch.mode") {
		t.Errorf("next_action = %q, want the mode key to turn the watch on", e.NextAction)
	}
	if err := e.Valid(); err != nil {
		t.Errorf("off event invalid: %v", err)
	}
}

func TestAutomataEventWatchOffEmptyModeIsOff(t *testing.T) {
	src := func() autonomous.RepoCIWatchStatus { return autonomous.RepoCIWatchStatus{} }
	got, _ := AutomataEventsFromRepoCIWatch(src)
	if len(got) != 1 || !got[0].Quiet {
		t.Fatalf("empty mode events = %+v, want one quiet off event", got)
	}
}

func TestAutomataEventWatchOffNeverPolled(t *testing.T) {
	src := func() autonomous.RepoCIWatchStatus {
		return autonomous.RepoCIWatchStatus{
			Mode:    config.RepoCIWatchModeOn,
			Records: []*autonomous.RepoCIWatchRecord{{RunID: "1", Status: "failing"}},
		}
	}
	got, err := AutomataEventsFromRepoCIWatch(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("never-polled got %d events, want exactly 1 (not a status)", len(got))
	}
	if got[0].Severity != AutomataSeverityInfo || got[0].Kind != AutomataEventWatch {
		t.Errorf("never-polled event = %+v, want watch info", got[0])
	}
	if err := got[0].Valid(); err != nil {
		t.Errorf("never-polled event invalid: %v", err)
	}
}

func TestAutomataEventWatchOffFailingAndPendingRuns(t *testing.T) {
	polled := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	updated := polled.Add(-time.Minute)
	src := func() autonomous.RepoCIWatchStatus {
		return autonomous.RepoCIWatchStatus{
			Mode:       config.RepoCIWatchModeOn,
			LastPollAt: polled,
			Records: []*autonomous.RepoCIWatchRecord{
				{Repo: "dmz006/datawatch", RefName: "main", RunID: "101", Status: "failing", RunURL: "https://ci/101", UpdatedAt: updated},
				{Repo: "dmz006/datawatch", RefName: "main", RunID: "102", Status: "green", UpdatedAt: updated},
				{Repo: "dmz006/datawatch", RefName: "v1.2.3", RunID: "103", Status: "pending", FirstSeenAt: polled.Add(-time.Hour)},
				nil,
			},
		}
	}
	got, err := AutomataEventsFromRepoCIWatch(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2 (failing and pending only)", len(got))
	}
	for _, e := range got {
		if e.Severity != AutomataSeverityWarn || e.Kind != AutomataEventWatch {
			t.Errorf("run event = %+v, want watch warn", e)
		}
		if err := e.Valid(); err != nil {
			t.Errorf("run event invalid: %v", err)
		}
	}
	if got[0].Detail != "https://ci/101" || !got[0].Time.Equal(updated) {
		t.Errorf("failing event = %+v, want run url detail and updated time", got[0])
	}
	if !got[1].Time.Equal(polled.Add(-time.Hour)) {
		t.Errorf("pending event time = %v, want first seen fallback", got[1].Time)
	}
}

func TestAutomataEventWatchOffOnAllGreen(t *testing.T) {
	src := func() autonomous.RepoCIWatchStatus {
		return autonomous.RepoCIWatchStatus{
			Mode:       config.RepoCIWatchModeShadow,
			LastPollAt: time.Now(),
			Records:    []*autonomous.RepoCIWatchRecord{{RunID: "9", Status: "green"}},
		}
	}
	got, err := AutomataEventsFromRepoCIWatch(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("all-green got %d events, want none", len(got))
	}
}
