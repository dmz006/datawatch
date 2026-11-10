package server

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestAutomataEventMaintenanceSourcesSafeToDelete(t *testing.T) {
	computed := "2026-11-09T12:00:00Z"
	rep := autonomous.SafeDeleteReport{
		ComputedAt: computed,
		Safe: []autonomous.SafeDeleteEntry{
			{Branch: "automaton/auto-1", Worktree: "/wt/auto-1", Reason: "merged to main", Commits: 3},
			{Branch: "automaton/auto-2", Reason: "no worktree", Commits: 0, DirtyCheckSkipped: true},
		},
		Held: []autonomous.SafeDeleteHeld{
			{Branch: "automaton/auto-3", Worktree: "/wt/auto-3", State: "pending", Dirty: true, Note: "PRD still open"},
			{Branch: "automaton/auto-4", State: "needs_look"},
		},
	}

	got, err := AutomataEventsFromSafeToDeleteReport(func() (autonomous.SafeDeleteReport, error) { return rep, nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d events, want 4 (2 safe + 2 held)", len(got))
	}
	want := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	for _, e := range got {
		if e.Kind != AutomataEventCleanup {
			t.Errorf("event %q kind = %q, want cleanup", e.ID, e.Kind)
		}
		if !e.Time.Equal(want) {
			t.Errorf("event %q time = %v, want %v", e.ID, e.Time, want)
		}
		if e.Automaton != "" || e.Project != "" || e.Automation != "" {
			t.Errorf("event %q invented an owner: %+v", e.ID, e)
		}
		if err := e.Valid(); err != nil {
			t.Errorf("event %q invalid: %v", e.ID, err)
		}
	}

	if got[0].Summary != "Branch automaton/auto-1 is safe to delete" || got[0].Severity != AutomataSeverityInfo {
		t.Errorf("safe event = %+v", got[0])
	}
	if !strings.Contains(got[0].Detail, "3 commits") || !strings.Contains(got[0].Detail, "/wt/auto-1") {
		t.Errorf("safe detail = %q, want commits and worktree", got[0].Detail)
	}
	if !strings.Contains(got[1].Detail, "uncommitted-changes check skipped") {
		t.Errorf("dirty-skipped detail = %q", got[1].Detail)
	}
	if got[2].Severity != AutomataSeverityWarn || got[2].NextAction == "" {
		t.Errorf("dirty held event = %+v, want warn with next action", got[2])
	}
	if strings.Contains(got[2].Reason, "PRD") || !strings.Contains(got[2].Reason, "Automaton") {
		t.Errorf("held note = %q, want PRD rewritten to Automaton", got[2].Reason)
	}
	if got[3].Severity != AutomataSeverityInfo || got[3].NextAction != "" {
		t.Errorf("clean held event = %+v, want info without next action", got[3])
	}

	ids := map[string]bool{}
	for _, e := range got {
		if ids[e.ID] {
			t.Errorf("duplicate id %q", e.ID)
		}
		ids[e.ID] = true
	}
}

func TestAutomataEventMaintenanceSourcesSafeToDeleteEmpty(t *testing.T) {
	got, err := AutomataEventsFromSafeToDeleteReport(func() (autonomous.SafeDeleteReport, error) {
		return autonomous.SafeDeleteReport{ComputedAt: "2026-11-09T12:00:00Z"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d events for an empty report, want 0", len(got))
	}
}

func TestAutomataEventMaintenanceSourcesSafeToDeleteErrors(t *testing.T) {
	if _, err := AutomataEventsFromSafeToDeleteReport(nil); err == nil {
		t.Error("nil source must be an error")
	}
	boom := errors.New("no base repo")
	if _, err := AutomataEventsFromSafeToDeleteReport(func() (autonomous.SafeDeleteReport, error) {
		return autonomous.SafeDeleteReport{}, boom
	}); !errors.Is(err, boom) {
		t.Errorf("source error not wrapped: %v", err)
	}
	if _, err := AutomataEventsFromSafeToDeleteReport(func() (autonomous.SafeDeleteReport, error) {
		return autonomous.SafeDeleteReport{ComputedAt: "yesterday"}, nil
	}); err == nil {
		t.Error("bad computed_at must be an error")
	}
}

func TestAutomataEventMaintenanceSourcesCleanupStatusHealthy(t *testing.T) {
	last := time.Date(2026, 11, 9, 11, 0, 0, 0, time.UTC)
	pct := 42
	st := autonomous.CleanupStatus{
		Worktrees:        autonomous.CleanupPassStatus{Mode: "on", LastRun: &last, Removed: 2, WouldRemove: 1, Skipped: 1, BytesFreed: 1024, OwnerRemoved: 2},
		Temp:             autonomous.CleanupPassStatus{Mode: "report", LastRun: &last, WouldRemove: 5, NetRemoved: 0},
		WorktreeDisk:     &autonomous.CleanupDiskCheck{Path: "/wt", Percent: &pct},
		TempDisk:         &autonomous.CleanupDiskCheck{Path: "/tmp/dw", Percent: nil},
		DiskAlertPercent: 90,
	}
	got, err := AutomataEventsFromCleanupStatus(func() autonomous.CleanupStatus { return st })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want exactly 1", len(got))
	}
	e := got[0]
	if err := e.Valid(); err != nil {
		t.Fatalf("event invalid: %v", err)
	}
	if e.Kind != AutomataEventCleanup || e.Severity != AutomataSeverityInfo {
		t.Errorf("event = %+v, want cleanup info", e)
	}
	if !e.Time.Equal(last) {
		t.Errorf("time = %v, want newest pass run %v", e.Time, last)
	}
	for _, want := range []string{"worktrees: mode on", "removed 2", "temp: mode report", "worktree disk /wt at 42%", "temp disk /tmp/dw unreadable"} {
		if !strings.Contains(e.Detail, want) {
			t.Errorf("detail %q missing %q", e.Detail, want)
		}
	}
	if e.NextAction != "" || e.Reason != "" {
		t.Errorf("healthy status has next_action %q / reason %q", e.NextAction, e.Reason)
	}
}

func TestAutomataEventMaintenanceSourcesCleanupStatusDiskAlert(t *testing.T) {
	alertAt := time.Date(2026, 11, 9, 13, 0, 0, 0, time.UTC)
	pct := 97
	st := autonomous.CleanupStatus{
		WorktreeDisk:    &autonomous.CleanupDiskCheck{Path: "/wt", Percent: &pct, Alert: true},
		DiskAlert:       true,
		LastDiskAlertAt: &alertAt,
	}
	got, err := AutomataEventsFromCleanupStatus(func() autonomous.CleanupStatus { return st })
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d events, err %v", len(got), err)
	}
	e := got[0]
	if e.Severity != AutomataSeverityWarn || e.NextAction == "" {
		t.Errorf("disk alert event = %+v, want warn with next action", e)
	}
	if !e.Time.Equal(alertAt) {
		t.Errorf("time = %v, want last alert %v", e.Time, alertAt)
	}
	if !strings.Contains(e.Detail, "(alert)") || !strings.Contains(e.Detail, "temp disk not configured") {
		t.Errorf("detail = %q", e.Detail)
	}
}

func TestAutomataEventMaintenanceSourcesCleanupStatusFailure(t *testing.T) {
	st := autonomous.CleanupStatus{
		Temp: autonomous.CleanupPassStatus{Mode: "on", Failed: 1, LastError: "remove /x: permission denied"},
	}
	got, err := AutomataEventsFromCleanupStatus(func() autonomous.CleanupStatus { return st })
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d events, err %v", len(got), err)
	}
	e := got[0]
	if e.Severity != AutomataSeverityError {
		t.Errorf("severity = %q, want error", e.Severity)
	}
	if !e.Time.IsZero() {
		t.Errorf("time = %v, want zero when no run is recorded", e.Time)
	}
	if !strings.Contains(e.Reason, "permission denied") {
		t.Errorf("reason = %q", e.Reason)
	}
	if err := e.Valid(); err != nil {
		t.Errorf("event invalid: %v", err)
	}
}

func TestAutomataEventMaintenanceSourcesCleanupStatusNilSource(t *testing.T) {
	if _, err := AutomataEventsFromCleanupStatus(nil); err == nil {
		t.Error("nil source must be an error")
	}
}
