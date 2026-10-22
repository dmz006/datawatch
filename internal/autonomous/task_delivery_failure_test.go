// RecordTaskDeliveryFailure: persisting one delivery failure per call on the
// task's DeliveryFailureEvents.

package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestRecordTaskDeliveryFailure(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/projdeliveryfail", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{
		Title: "story",
		Tasks: []Task{{Title: "a", Status: TaskInProgress}},
	}})
	prd, _ = m.Store().GetPRD(prd.ID)
	taskID := prd.Story[0].Tasks[0].ID

	const reason = "task delivery failed: main prompt never appeared after 3 attempts"
	if err := m.RecordTaskDeliveryFailure(prd.ID, taskID, reason, time.Now()); err != nil {
		t.Fatalf("RecordTaskDeliveryFailure: %v", err)
	}

	prd, _ = m.Store().GetPRD(prd.ID)
	events := prd.Story[0].Tasks[0].DeliveryFailureEvents
	if len(events) != 1 {
		t.Fatalf("DeliveryFailureEvents = %d, want exactly 1: %+v", len(events), events)
	}
	if events[0].Reason != reason {
		t.Errorf("Reason = %q, want %q", events[0].Reason, reason)
	}
	if events[0].At.IsZero() {
		t.Error("event At is zero, want the recorded time")
	}

	// A second failure appends; the first event is kept.
	if err := m.RecordTaskDeliveryFailure(prd.ID, taskID, "second failure", time.Now()); err != nil {
		t.Fatalf("second RecordTaskDeliveryFailure: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	events = prd.Story[0].Tasks[0].DeliveryFailureEvents
	if len(events) != 2 {
		t.Fatalf("DeliveryFailureEvents after second call = %d, want 2: %+v", len(events), events)
	}
	if events[0].Reason != reason || events[1].Reason != "second failure" {
		t.Errorf("events = %+v, want first %q then %q", events, reason, "second failure")
	}

	// An unknown task ID is an error and changes nothing.
	if err := m.RecordTaskDeliveryFailure(prd.ID, "no-such-task", reason, time.Now()); err == nil {
		t.Error("RecordTaskDeliveryFailure with unknown task ID returned nil, want error")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown task error = %v, want a not-found error", err)
	}

	// Re-reading the PRD from the store shows the events persisted.
	reloaded, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing from store after recording")
	}
	persisted := reloaded.Story[0].Tasks[0].DeliveryFailureEvents
	if len(persisted) != 2 || persisted[0].Reason != reason || persisted[1].Reason != "second failure" {
		t.Errorf("persisted DeliveryFailureEvents = %+v, want 2 events in order", persisted)
	}
}
