package claudecode

import (
	"strings"
	"testing"
)

func TestDeriveSessionUUID_Deterministic(t *testing.T) {
	// Same input must always produce the same UUID.
	name := "ralfthewise-f7ec"
	a := deriveSessionUUID(name)
	b := deriveSessionUUID(name)
	if a != b {
		t.Fatalf("deriveSessionUUID not deterministic: %q != %q", a, b)
	}
	if !isUUID(a) {
		t.Fatalf("deriveSessionUUID did not produce a valid UUID: %q", a)
	}
}

func TestDeriveSessionUUID_DifferentInputs(t *testing.T) {
	a := deriveSessionUUID("ralfthewise-f7ec")
	b := deriveSessionUUID("ralfthewise-a1b2")
	if a == b {
		t.Fatalf("different inputs produced the same UUID: %q", a)
	}
}

// TestLaunchAndResume_SameSessionID verifies that the --session-id set during
// Launch matches the --resume ID used during LaunchResume for the same session.
// This is the core invariant that makes resume work: the conversation ID Claude
// stores at launch time must equal the ID we ask it to resume by.
func TestLaunchAndResume_SameSessionID(t *testing.T) {
	// Simulate the datawatch identifiers for a session:
	//   fullID     = "ralfthewise-f7ec"
	//   tmuxSession = "cs-ralfthewise-f7ec"
	fullID := "ralfthewise-f7ec"
	tmuxSession := "cs-" + fullID

	// Launch derives UUID from tmuxSession minus "cs-" prefix.
	launchFullID := strings.TrimPrefix(tmuxSession, "cs-")
	launchUUID := deriveSessionUUID(launchFullID)

	// LaunchResume receives fullID as resumeID (from the UI dropdown).
	// Since fullID is not a UUID, it derives the same deterministic UUID.
	resumeID := fullID
	if isUUID(resumeID) {
		t.Fatalf("fullID %q should not be detected as UUID", resumeID)
	}
	resumeUUID := deriveSessionUUID(resumeID)

	if launchUUID != resumeUUID {
		t.Fatalf("session ID mismatch: Launch would set --session-id %q but Resume would use --resume %q",
			launchUUID, resumeUUID)
	}
	t.Logf("session ID match: %s", launchUUID)
}

func TestIsUUID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"a1085917-b6b3-5fa9-b8a9-513c7ed4c8c3", true},
		{"ralfthewise-f7ec", false},
		{"not-a-uuid", false},
		{"", false},
		{"00000000-0000-0000-0000-000000000000", true},
	}
	for _, tc := range tests {
		got := isUUID(tc.input)
		if got != tc.want {
			t.Errorf("isUUID(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
