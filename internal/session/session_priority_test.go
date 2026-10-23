package session

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSessionPriorityDefaultsToZero(t *testing.T) {
	var s Session
	if err := json.Unmarshal([]byte(`{"id":"ab12"}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.Priority != 0 {
		t.Fatalf("default Priority = %d, want 0", s.Priority)
	}
	b, _ := json.Marshal(&Session{ID: "ab12"})
	if string(b) != "" && json.Valid(b) {
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		if _, ok := m["priority"]; ok {
			t.Fatalf("zero Priority should be omitted, got %s", b)
		}
	}
}

func TestSessionPriorityPersistsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/sessions.json"
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := store.Save(&Session{ID: "p001", FullID: "h-p001", State: StateRunning, Priority: 7, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get("h-p001")
	if !ok {
		t.Fatal("session not reloaded")
	}
	if got.Priority != 7 {
		t.Fatalf("reloaded Priority = %d, want 7", got.Priority)
	}
}
