package autonomous

import (
	"testing"
	"time"
)

// TestSupervisorWatchStatsCounts — the numeric view counts the findings
// by standing: open includes the would-alert and alerted ones, resolved
// ones are counted apart and never as open.
func TestSupervisorWatchStatsCounts(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	alertedAt := now.Add(-time.Minute)
	st := SupervisorWatchStatus{
		Mode:       SupervisorWatchModeShadow,
		LastPollAt: now.Add(-90 * time.Second),
		Findings: []*SupervisorWatchFinding{
			{Kind: SupervisorWatchTaskFailed, Subject: "a"},
			{Kind: SupervisorWatchTaskFailed, Subject: "b", WouldAlert: true},
			{Kind: SupervisorWatchCIRed, Subject: "1", AlertedAt: &alertedAt},
			{Kind: SupervisorWatchCIRed, Subject: "2", Resolved: true},
			nil,
		},
		Comparison: SupervisorWatchComparisonView{AgreedDays: 3},
	}
	got := st.Stats(now)
	if got.Mode != SupervisorWatchModeShadow {
		t.Errorf("Mode = %q", got.Mode)
	}
	if got.FindingsOpen != 3 || got.FindingsWouldAlert != 1 || got.FindingsAlerted != 1 || got.FindingsResolved != 1 {
		t.Errorf("counts open/would/alerted/resolved = %d/%d/%d/%d, want 3/1/1/1",
			got.FindingsOpen, got.FindingsWouldAlert, got.FindingsAlerted, got.FindingsResolved)
	}
	if got.LastPollAgeSeconds != 90 {
		t.Errorf("LastPollAgeSeconds = %d, want 90", got.LastPollAgeSeconds)
	}
	if got.AgreedDays != 3 {
		t.Errorf("AgreedDays = %d, want 3", got.AgreedDays)
	}
}

// TestSupervisorWatchStatsNeverPolled — a coordinator that never polled
// reads -1 for the poll age, not a huge age since the zero time.
func TestSupervisorWatchStatsNeverPolled(t *testing.T) {
	got := SupervisorWatchStatus{Mode: SupervisorWatchModeOff}.Stats(time.Now())
	if got.LastPollAgeSeconds != -1 {
		t.Errorf("LastPollAgeSeconds = %d, want -1", got.LastPollAgeSeconds)
	}
	if got.FindingsOpen != 0 || got.FindingsResolved != 0 {
		t.Errorf("no findings: counts = %+v", got)
	}
}

// TestSupervisorWatchAPIStats — the API accessor derives the same view
// from the manager's read view.
func TestSupervisorWatchAPIStats(t *testing.T) {
	m := &Manager{}
	api := &API{M: m}
	got := api.SupervisorWatchStats()
	if got.LastPollAgeSeconds != -1 || got.FindingsOpen != 0 {
		t.Errorf("fresh manager stats = %+v", got)
	}
}
