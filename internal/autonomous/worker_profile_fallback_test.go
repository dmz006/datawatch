package autonomous

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The claude-code launch that applies a worker profile falls back to no
// profile when the profile cannot be applied; its command-line equality with a
// no-profile launch is tested in internal/llm/claudecode, which owns the
// command. The tests here cover what the autonomous side does with the
// reason: the attempt carries it, drops the profile name and starting context,
// and keeps the record across a save.

const profileFallbackText = `apply worker profile "full": write worker-mcp.json: permission denied`

func TestNoteProfileFallbackClearsProfileAndKeepsReason(t *testing.T) {
	run := attemptRun{WorkerProfile: "full", WorkerRole: "worker", LaunchContext: "starting context"}
	m := &Manager{}

	m.noteProfileFallback(&run, profileFallbackText)

	if run.ProfileFallbackReason != profileFallbackText {
		t.Errorf("ProfileFallbackReason = %q, want the apply error text", run.ProfileFallbackReason)
	}
	if run.WorkerProfile != "" {
		t.Errorf("WorkerProfile = %q, want cleared", run.WorkerProfile)
	}
	if run.LaunchContext != "" {
		t.Errorf("LaunchContext = %q, want cleared (a launch with no profile has no starting context)", run.LaunchContext)
	}
	if run.WorkerRole != "worker" {
		t.Errorf("WorkerRole = %q, want it kept", run.WorkerRole)
	}
}

// TestRecordFailedAttemptHoldsProfileFallbackReason runs the recorder on a
// real store: the stored attempt holds the reason and an empty worker profile.
func TestRecordFailedAttemptHoldsProfileFallbackReason(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	fail := attemptFailure{Outcome: attemptOutcomeFailed, Category: "verifier", Summary: "not done"}

	run := attemptRun{Backend: "claude-code", WorkerProfile: "full", LaunchContext: "ctx"}
	m.noteProfileFallback(&run, profileFallbackText)
	task := &prd.Story[0].Tasks[0]
	m.recordFailedAttempt(context.Background(), prd, task, run, fail)

	stored, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found after recording")
	}
	got := stored.Story[0].Tasks[0].Attempts
	if len(got) != 1 {
		t.Fatalf("Attempts = %d, want 1", len(got))
	}
	if got[0].ProfileFallbackReason != profileFallbackText {
		t.Errorf("ProfileFallbackReason = %q, want %q", got[0].ProfileFallbackReason, profileFallbackText)
	}
	if got[0].WorkerProfile != "" {
		t.Errorf("WorkerProfile = %q, want empty on a fallback attempt", got[0].WorkerProfile)
	}
	if got[0].StartupTokens != 0 {
		t.Errorf("StartupTokens = %d, want 0 (no starting context was used)", got[0].StartupTokens)
	}
}

// TestRecordFailedAttemptWithoutFallbackLeavesReasonEmpty: an attempt whose
// profile applied (or had none) carries no fallback reason.
func TestRecordFailedAttemptWithoutFallbackLeavesReasonEmpty(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	fail := attemptFailure{Outcome: attemptOutcomeFailed, Category: "verifier", Summary: "not done"}

	task := &prd.Story[0].Tasks[0]
	m.recordFailedAttempt(context.Background(), prd, task, attemptRun{Backend: "claude-code"}, fail)

	stored, _ := m.Store().GetPRD(prd.ID)
	got := stored.Story[0].Tasks[0].Attempts
	if len(got) != 1 {
		t.Fatalf("Attempts = %d, want 1", len(got))
	}
	if got[0].ProfileFallbackReason != "" {
		t.Errorf("ProfileFallbackReason = %q, want empty", got[0].ProfileFallbackReason)
	}
}

// TestTaskAttemptProfileFallbackJSON: the reason is written under its JSON
// name and omitted when empty.
func TestTaskAttemptProfileFallbackJSON(t *testing.T) {
	raw, err := json.Marshal(TaskAttempt{Number: 1, ProfileFallbackReason: profileFallbackText})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"profile_fallback_reason":"`) {
		t.Fatalf("profile_fallback_reason missing from JSON: %s", raw)
	}
	var back TaskAttempt
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.ProfileFallbackReason != profileFallbackText {
		t.Errorf("round trip lost the reason: %q", back.ProfileFallbackReason)
	}

	empty, err := json.Marshal(TaskAttempt{Number: 2})
	if err != nil {
		t.Fatalf("marshal empty: %v", err)
	}
	if strings.Contains(string(empty), "profile_fallback_reason") {
		t.Errorf("empty profile_fallback_reason must be omitted: %s", empty)
	}
}
