package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestNewReleaseModeChangeDecision_FillsEveryField(t *testing.T) {
	at := time.Date(2026, 11, 9, 14, 30, 0, 0, time.UTC)
	change := ReleaseModeChange{
		Size:    "minor",
		OldMode: "manual",
		NewMode: "auto",
		Actor:   "supervisor",
		Surface: "rest",
	}
	evidence := ReleaseReadiness{
		Size:            "minor",
		Records:         7,
		Conflicts:       1,
		PWAChanges:      2,
		BlockingDefects: 1,
		Required:        ReleaseReadinessBar{Records: 10, Conflicts: 2, PWAChanges: 2},
		Missing:         []string{"needs 3 more records", "needs 1 more conflict"},
		Ready:           false,
	}

	got := NewReleaseModeChangeDecision(change, evidence, at)

	if !strings.HasPrefix(got.ID, decisionIDPrefix) || len(got.ID) != len(decisionIDPrefix)+decisionIDHexLen {
		t.Errorf("ID = %q; want %q prefix with %d hex characters", got.ID, decisionIDPrefix, decisionIDHexLen)
	}
	if want := "Change minor release approval mode from manual to auto"; got.Title != want {
		t.Errorf("Title = %q; want %q", got.Title, want)
	}
	wantSituation := "Release readiness for minor: 7 plan-equal-or-better records (bar 10), 1 conflict records (bar 2), 2 PWA-change records (bar 2), 1 blocking defects. Status: not ready. Missing: needs 3 more records; needs 1 more conflict. Requested by supervisor via rest."
	if got.Situation != wantSituation {
		t.Errorf("Situation = %q; want %q", got.Situation, wantSituation)
	}
	if want := "Whether the minor release approval mode changes from manual to auto."; got.Deciding != want {
		t.Errorf("Deciding = %q; want %q", got.Deciding, want)
	}
	wantOptions := []DecisionOption{{ID: "manual", Label: "manual"}, {ID: "auto", Label: "auto"}}
	if len(got.Options) != len(wantOptions) {
		t.Fatalf("Options has %d entries; want %d", len(got.Options), len(wantOptions))
	}
	for i, opt := range wantOptions {
		if got.Options[i] != opt {
			t.Errorf("Options[%d] = %+v; want %+v", i, got.Options[i], opt)
		}
	}
	if got.Default != "manual" {
		t.Errorf("Default = %q; want %q", got.Default, "manual")
	}
	if got.State != DecisionRecordOpen {
		t.Errorf("State = %q; want %q", got.State, DecisionRecordOpen)
	}
	if got.Source != "release_mode_change" {
		t.Errorf("Source = %q; want %q", got.Source, "release_mode_change")
	}
	if got.CreatedAt == nil || !got.CreatedAt.Equal(at) {
		t.Errorf("CreatedAt = %v; want %v", got.CreatedAt, at)
	}
}

func TestNewReleaseModeChangeDecision_ReadyWithNoMissing(t *testing.T) {
	evidence := ReleaseReadiness{
		Size:     "patch",
		Records:  10,
		Required: DefaultReleaseReadinessBar(),
		Ready:    true,
	}
	change := ReleaseModeChange{Size: "patch", OldMode: "auto", NewMode: "manual", Actor: "operator", Surface: "pwa"}

	got := NewReleaseModeChangeDecision(change, evidence, time.Now())

	if !strings.Contains(got.Situation, "Status: ready. Missing: none. Requested by operator via pwa.") {
		t.Errorf("Situation = %q; want ready status, no missing items, and the actor and surface", got.Situation)
	}
	if got.Title != "Change patch release approval mode from auto to manual" {
		t.Errorf("Title = %q", got.Title)
	}
}

func TestNewReleaseModeChangeDecision_IDsAreFresh(t *testing.T) {
	change := ReleaseModeChange{Size: "major", OldMode: "a", NewMode: "b", Actor: "daemon", Surface: "cli"}
	first := NewReleaseModeChangeDecision(change, ReleaseReadiness{}, time.Now())
	second := NewReleaseModeChangeDecision(change, ReleaseReadiness{}, time.Now())
	if first.ID == second.ID {
		t.Errorf("two records share ID %q", first.ID)
	}
}
