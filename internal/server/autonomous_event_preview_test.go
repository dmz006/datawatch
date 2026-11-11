package server

import (
	"fmt"
	"testing"
	"time"
)

func previewEvents(now time.Time) []AutomataEvent {
	var evs []AutomataEvent
	for i := 0; i < 30; i++ {
		evs = append(evs, AutomataEvent{
			ID: fmt.Sprintf("w%d", i), Time: now.Add(-time.Duration(i) * time.Minute),
			Kind: AutomataEventWatch, Project: "alpha", Automation: "nightly",
			Summary: "watch", Severity: AutomataSeverityInfo,
		})
	}
	evs = append(evs,
		AutomataEvent{ID: "old", Time: now.Add(-25 * time.Hour), Kind: AutomataEventWatch, Project: "alpha", Summary: "old", Severity: AutomataSeverityInfo},
		AutomataEvent{ID: "r1", Time: now.Add(-40 * time.Minute), Kind: AutomataEventRetry, Project: "beta", Summary: "retry", Severity: AutomataSeverityWarn},
	)
	return MergeAutomataEvents(evs)
}

func TestEventPreviewSampleCap(t *testing.T) {
	now := time.Now().UTC()
	resp := eventPreviewBuild(previewEvents(now), automataEventLogQuery{}, "", now)
	if resp.Count != 31 {
		t.Fatalf("count = %d, want 31 (old event excluded)", resp.Count)
	}
	if len(resp.Sample) != eventPreviewSampleSize {
		t.Fatalf("sample = %d, want %d", len(resp.Sample), eventPreviewSampleSize)
	}
}

func TestEventPreviewFilters(t *testing.T) {
	now := time.Now().UTC()
	q, err := eventPreviewQuery(eventPreviewRequest{Kinds: []string{"retry"}})
	if err != nil {
		t.Fatal(err)
	}
	resp := eventPreviewBuild(previewEvents(now), q, "", now)
	if resp.Count != 1 || resp.Sample[0].ID != "r1" {
		t.Fatalf("kind filter: %+v", resp)
	}
	q, _ = eventPreviewQuery(eventPreviewRequest{Project: "beta"})
	if got := eventPreviewBuild(previewEvents(now), q, "", now).Count; got != 1 {
		t.Fatalf("project filter count = %d", got)
	}
	if got := eventPreviewBuild(previewEvents(now), automataEventLogQuery{}, "nightly", now).Count; got != 30 {
		t.Fatalf("owner filter count = %d", got)
	}
}

func TestEventPreviewUnknownKind(t *testing.T) {
	if _, err := eventPreviewQuery(eventPreviewRequest{Kinds: []string{"bogus"}}); err == nil {
		t.Fatal("want error for unknown kind")
	}
}

func TestEventPreviewEmptyIsNotNull(t *testing.T) {
	now := time.Now().UTC()
	resp := eventPreviewBuild(nil, automataEventLogQuery{}, "", now)
	if resp.Count != 0 || resp.Sample == nil {
		t.Fatalf("empty preview: %+v", resp)
	}
}
