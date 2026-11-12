package session

import (
	"path/filepath"
	"testing"
)

// TestStateStalled_KnownAndNonTerminal pins the state vocabulary contract:
// "stalled" is a known lifecycle state, it is NON-terminal (a stalled
// session resumes to running when activity returns; it is never counted as
// waiting-for-input by terminal/active gating), and its string form is the
// wire value "stalled" used by the REST filter and state-override endpoints.
func TestStateStalled_KnownAndNonTerminal(t *testing.T) {
	if string(StateStalled) != "stalled" {
		t.Fatalf("StateStalled = %q, want %q", StateStalled, "stalled")
	}
	found := false
	for _, st := range KnownStates() {
		if st == StateStalled {
			found = true
		}
	}
	if !found {
		t.Fatal("KnownStates() does not include StateStalled")
	}
	if StateStalled.IsTerminal() {
		t.Error("StateStalled.IsTerminal() = true, want false — stalled is resumable, not finished")
	}
	// waiting_input stays the only non-terminal state flagged as
	// waiting-for-input (used by DetectLongInputWait and server-side
	// waiting-input handling); stalled is distinct from it.
	if StateStalled == StateWaitingInput {
		t.Error("StateStalled must be distinct from StateWaitingInput")
	}
}

// TestStore_Save_Get_StalledRoundTrip: a session saved with the stalled
// state survives the JSON round-trip (Save → disk → fresh Load).
func TestStore_Save_Get_StalledRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	sess := makeTestSession("st11", "myhost", StateStalled)
	if err := s.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, ok := s.Get("myhost-st11")
	if !ok {
		t.Fatal("Get returned not found for saved stalled session")
	}
	if got.State != StateStalled {
		t.Errorf("in-memory State = %q, want %q", got.State, StateStalled)
	}

	// Reload from disk via a fresh Store to ensure the state value made it
	// through JSON serialisation, not just the in-memory map.
	s2, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore (reload): %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	got, ok = s2.Get("myhost-st11")
	if !ok {
		t.Fatal("Get returned not found for persisted stalled session")
	}
	if got.State != StateStalled {
		t.Errorf("reloaded State = %q, want %q", got.State, StateStalled)
	}
	if got.State.IsTerminal() {
		t.Errorf("reloaded State %q reports terminal, want non-terminal", got.State)
	}
}
