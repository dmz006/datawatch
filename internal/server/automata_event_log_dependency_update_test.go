package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func TestAutomataDependencyUpdateEvents(t *testing.T) {
	at := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	runs := []automataDependencyUpdateRun{
		{Title: "Weekly one", At: at, Report: autonomous.DependencyUpdateReport{Outcome: autonomous.DependencyUpdateUpdated, Summary: "2 updated, 0 dropped, 0 skipped"}},
		{Title: "Weekly two", At: at.Add(time.Hour), Report: autonomous.DependencyUpdateReport{Outcome: autonomous.DependencyUpdateNothing, Summary: "0 updated, 0 dropped, 0 skipped"}},
		{Title: "Weekly three", At: at.Add(2 * time.Hour), Report: autonomous.DependencyUpdateReport{Outcome: autonomous.DependencyUpdateNeedsAttention, Summary: "0 updated, 1 dropped, 0 skipped"}},
	}
	got, err := AutomataEventsFromDependencyUpdate(func() []automataDependencyUpdateRun { return runs })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows (no row for nothing to update), got %d", len(got))
	}
	for _, e := range got {
		if err := e.Valid(); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(e.Summary, "Weekly two") {
			t.Fatal("nothing-to-update run wrote a row")
		}
	}
	if got[0].Severity != AutomataSeverityInfo || !strings.Contains(got[0].Summary, "Weekly one: 2 updated") {
		t.Fatalf("updated row: %+v", got[0])
	}
	if got[1].Severity != AutomataSeverityWarn || got[1].NextAction == "" {
		t.Fatalf("attention row: %+v", got[1])
	}
	merged := MergeAutomataEvents(got)
	if merged[0].Time.Before(merged[1].Time) {
		t.Fatal("merge order is not newest first")
	}
}

func TestAutomataDependencyUpdateNilSource(t *testing.T) {
	if _, err := AutomataEventsFromDependencyUpdate(nil); err == nil {
		t.Fatal("nil source must be an error")
	}
	got, _ := AutomataEventsFromDependencyUpdate(func() []automataDependencyUpdateRun { return nil })
	if len(got) != 0 {
		t.Fatal("empty source must give no rows")
	}
}

// depUpdateFake serves weekly run reports through the manager seam.
type depUpdateFake struct {
	eventLogFake
	runs []autonomous.DependencyUpdateRunReport
}

func (f *depUpdateFake) DependencyUpdateReports() []autonomous.DependencyUpdateRunReport {
	return f.runs
}

// The merged Events stream carries weekly run rows, and skips a run whose
// outcome is nothing to update.
func TestCollectAutomataEventsIncludesDependencyUpdate(t *testing.T) {
	at := time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)
	fake := &depUpdateFake{runs: []autonomous.DependencyUpdateRunReport{
		{Title: "Weekly one", At: at, Report: autonomous.DependencyUpdateReport{Outcome: autonomous.DependencyUpdateUpdated, Summary: "2 updated, 0 dropped, 0 skipped"}},
		{Title: "Weekly two", At: at.Add(time.Hour), Report: autonomous.DependencyUpdateReport{Outcome: autonomous.DependencyUpdateNothing, Summary: "0 updated, 0 dropped, 0 skipped"}},
		{Title: "Weekly three", At: at.Add(2 * time.Hour), Report: autonomous.DependencyUpdateReport{Outcome: autonomous.DependencyUpdateNeedsAttention, Summary: "1 updated, 1 dropped, 0 skipped"}},
	}}
	evs, err := eventLogServer(fake).collectAutomataEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, e := range evs {
		if strings.HasPrefix(e.ID, "dependency_update:") {
			titles = append(titles, e.Summary)
		}
	}
	if len(titles) != 2 {
		t.Fatalf("want 2 dependency rows, got %v", titles)
	}
}
