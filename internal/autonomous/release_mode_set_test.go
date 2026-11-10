package autonomous

import (
	"errors"
	"testing"

	"github.com/dmz006/datawatch/internal/audit"
)

// SetReleaseApprovalMode emits one release_mode_changed event per successful
// change and none for a no-op, a refused change or a failed apply.

type releaseModeEvent struct {
	action  string
	details map[string]any
}

func releaseModeTestManager(t *testing.T) (*Manager, *[]releaseModeEvent) {
	t.Helper()
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	var events []releaseModeEvent
	m.SetAuditFn(func(action string, details map[string]any) {
		events = append(events, releaseModeEvent{action: action, details: details})
	})
	return m, &events
}

func releaseModeTestChange(oldMode, newMode string) ReleaseModeChange {
	return ReleaseModeChange{Size: ReleaseSizeMinor, OldMode: oldMode, NewMode: newMode, Actor: "operator", Surface: "pwa"}
}

func TestSetReleaseApprovalMode_OneEventPerChange(t *testing.T) {
	m, events := releaseModeTestManager(t)
	var applied []string
	apply := func(mode string) error { applied = append(applied, mode); return nil }
	var recorded []DecisionRecord
	record := func(rec DecisionRecord) (string, error) {
		recorded = append(recorded, rec)
		return "dec-" + rec.Title, nil
	}

	id1, err := m.SetReleaseApprovalMode(releaseModeTestChange("manual", "auto"), ReleaseReadiness{Size: ReleaseSizeMinor}, apply, record)
	if err != nil {
		t.Fatalf("first change: %v", err)
	}
	id2, err := m.SetReleaseApprovalMode(releaseModeTestChange("auto", "ask"), ReleaseReadiness{Size: ReleaseSizeMinor}, apply, record)
	if err != nil {
		t.Fatalf("second change: %v", err)
	}

	if len(*events) != 2 {
		t.Fatalf("events = %d, want 2 (one per change)", len(*events))
	}
	if len(applied) != 2 || applied[0] != "auto" || applied[1] != "ask" {
		t.Errorf("applied = %v, want [auto ask]", applied)
	}
	if len(recorded) != 2 || recorded[0].Source != ReleaseModeChangeSource {
		t.Errorf("recorded = %+v, want two release mode decision records", recorded)
	}

	first := (*events)[0]
	if first.action != audit.ActionReleaseModeChanged {
		t.Errorf("action = %q, want %q", first.action, audit.ActionReleaseModeChanged)
	}
	if audit.ActionReleaseModeChanged != "release_mode_changed" {
		t.Errorf("ActionReleaseModeChanged = %q, want release_mode_changed", audit.ActionReleaseModeChanged)
	}
	want := map[string]any{
		"size":               ReleaseSizeMinor,
		"old_mode":           "manual",
		"new_mode":           "auto",
		"actor":              "operator",
		"surface":            "pwa",
		"decision_record_id": id1,
		"headline":           "minor release approval mode changed from manual to auto",
	}
	for k, v := range want {
		if first.details[k] != v {
			t.Errorf("details[%q] = %v, want %v", k, first.details[k], v)
		}
	}
	if id1 == "" || id2 == "" || id1 == id2 {
		t.Errorf("record IDs = %q, %q; want two distinct non-empty IDs", id1, id2)
	}
	if got := (*events)[1].details["decision_record_id"]; got != id2 {
		t.Errorf("second event decision_record_id = %v, want %q", got, id2)
	}
}

func TestSetReleaseApprovalMode_NoOpEmitsNothing(t *testing.T) {
	m, events := releaseModeTestManager(t)
	applyCalls, recordCalls := 0, 0
	apply := func(string) error { applyCalls++; return nil }
	record := func(DecisionRecord) (string, error) { recordCalls++; return "dec", nil }

	id, err := m.SetReleaseApprovalMode(releaseModeTestChange("auto", "auto"), ReleaseReadiness{}, apply, record)
	if err != nil || id != "" {
		t.Fatalf("no-op = (%q, %v), want (\"\", nil)", id, err)
	}
	if len(*events) != 0 {
		t.Errorf("no-op emitted %d events, want 0", len(*events))
	}
	if applyCalls != 0 || recordCalls != 0 {
		t.Errorf("no-op called apply %d times and record %d times, want 0", applyCalls, recordCalls)
	}
}

func TestSetReleaseApprovalMode_RefusedOrFailedEmitsNothing(t *testing.T) {
	m, events := releaseModeTestManager(t)
	noRecord := func(DecisionRecord) (string, error) { t.Fatal("record must not run"); return "", nil }

	if _, err := m.SetReleaseApprovalMode(ReleaseModeChange{Size: "huge", OldMode: "manual", NewMode: "auto"}, ReleaseReadiness{}, func(string) error { return nil }, noRecord); err == nil {
		t.Error("an unknown size must be refused")
	}
	if _, err := m.SetReleaseApprovalMode(releaseModeTestChange("manual", "bogus"), ReleaseReadiness{}, func(string) error { return nil }, noRecord); err == nil {
		t.Error("an unknown mode must be refused")
	}
	failed := errors.New("disk full")
	if _, err := m.SetReleaseApprovalMode(releaseModeTestChange("manual", "auto"), ReleaseReadiness{}, func(string) error { return failed }, noRecord); !errors.Is(err, failed) {
		t.Errorf("failed apply error = %v, want %v", err, failed)
	}
	if len(*events) != 0 {
		t.Errorf("refused or failed changes emitted %d events, want 0", len(*events))
	}
}

func TestSetReleaseApprovalMode_RecordFailureStillEmitsOneEvent(t *testing.T) {
	m, events := releaseModeTestManager(t)
	recErr := errors.New("store closed")
	id, err := m.SetReleaseApprovalMode(releaseModeTestChange("manual", "auto"), ReleaseReadiness{},
		func(string) error { return nil },
		func(DecisionRecord) (string, error) { return "", recErr })
	if !errors.Is(err, recErr) {
		t.Errorf("error = %v, want the record error", err)
	}
	if id != "" {
		t.Errorf("id = %q, want empty when the record is not stored", id)
	}
	if len(*events) != 1 || (*events)[0].details["decision_record_id"] != "" {
		t.Errorf("events = %+v, want one event with an empty decision_record_id", *events)
	}
}
