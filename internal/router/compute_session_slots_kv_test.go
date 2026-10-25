package router

import "testing"

// TestComputeSessionSlotsKV checks the comm channel reads session_slots as a
// whole number, so `compute node update <name> session_slots=3` saves 3, and
// that an absent key leaves the override out of the record.
func TestComputeSessionSlotsKV(t *testing.T) {
	out := parseKVPairs("kind=ollama session_slots=3")
	got, ok := out["session_slots"].(int)
	if !ok {
		t.Fatalf("session_slots = %#v, want an int", out["session_slots"])
	}
	if got != 3 {
		t.Errorf("session_slots = %d, want 3", got)
	}

	absent := parseKVPairs("kind=ollama")
	if _, ok := absent["session_slots"]; ok {
		t.Errorf("session_slots present without the key: %#v", absent["session_slots"])
	}
}
