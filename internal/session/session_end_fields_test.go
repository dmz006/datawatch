package session

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSessionEndFieldsRoundTripJSON(t *testing.T) {
	ended := time.Date(2026, 11, 11, 22, 30, 0, 0, time.UTC)
	in := Session{
		ID:      "ab12",
		FullID:  "host-ab12",
		State:   StateFailed,
		EndedAt: &ended,
		EndNote: "process gone; record finalised",
	}
	data, err := json.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"ended_at"`, `"end_note"`} {
		if !strings.Contains(string(data), key) {
			t.Errorf("JSON lacks %s: %s", key, data)
		}
	}
	var out Session
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.EndedAt == nil || !out.EndedAt.Equal(ended) {
		t.Errorf("EndedAt = %v, want %v", out.EndedAt, ended)
	}
	if out.EndNote != in.EndNote {
		t.Errorf("EndNote = %q, want %q", out.EndNote, in.EndNote)
	}
}

func TestSessionEndFieldsOmittedWhenEmpty(t *testing.T) {
	in := Session{ID: "ab12", FullID: "host-ab12", State: StateRunning}
	data, err := json.Marshal(&in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"ended_at", "end_note"} {
		if strings.Contains(string(data), key) {
			t.Errorf("JSON contains %s for a record with no end fields: %s", key, data)
		}
	}
	var out Session
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("a record without the end fields must still load: %v", err)
	}
	if out.EndedAt != nil || out.EndNote != "" {
		t.Errorf("loaded record has end fields: %v %q", out.EndedAt, out.EndNote)
	}
}
