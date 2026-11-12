package autonomous

import (
	"testing"
	"time"
)

func TestResolveVerifyingTask(t *testing.T) {
	verifiedAt := time.Now()

	okVerdict := &VerificationResult{OK: true, Summary: "all checks passed", VerifiedAt: verifiedAt}
	failedVerdict := &VerificationResult{OK: false, Summary: "build failed", VerifiedAt: verifiedAt}

	liveAlive := func(sessionID string) bool { return sessionID == "session-1" }
	deadAlive := func(sessionID string) bool { return false }

	tests := []struct {
		name  string
		alive func(sessionID string) bool
		task  *Task
		want  TaskStatus
	}{
		{
			name:  "live session stays verifying",
			alive: liveAlive,
			task:  &Task{SessionID: "session-1", Verification: okVerdict},
			want:  TaskVerifying,
		},
		{
			name:  "dead session with ok verdict completes",
			alive: deadAlive,
			task:  &Task{SessionID: "session-1", Verification: okVerdict},
			want:  TaskCompleted,
		},
		{
			name:  "dead session with failed verdict retries",
			alive: deadAlive,
			task:  &Task{SessionID: "session-1", Verification: failedVerdict},
			want:  TaskPending,
		},
		{
			name:  "dead session with no verdict retries",
			alive: deadAlive,
			task:  &Task{SessionID: "session-1"},
			want:  TaskPending,
		},
		{
			name:  "nil alive with ok verdict completes, never verifying",
			alive: nil,
			task:  &Task{SessionID: "session-1", Verification: okVerdict},
			want:  TaskCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveVerifyingTask(tt.alive, tt.task)
			if got != tt.want {
				t.Errorf("ResolveVerifyingTask() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestResolveVerifyingResumeStartFailed(t *testing.T) {
	okVerdict := &VerificationResult{OK: true, Summary: "all checks passed", VerifiedAt: time.Now()}

	live := func(string) bool { return true }
	dead := func(string) bool { return false }
	started := func(string) bool { return true }
	notStarted := func(string) bool { return false }

	tests := []struct {
		name         string
		alive        func(string) bool
		agentStarted func(string) bool
		task         *Task
		want         VerifyingResume
	}{
		{"live bare shell with no worker result is a start failure", live, notStarted, &Task{SessionID: "s"}, VerifyingResumeStartFailed},
		{"live session whose agent started is kept", live, started, &Task{SessionID: "s"}, VerifyingResumeKeepLive},
		{"nil agent hook keeps the live session", live, nil, &Task{SessionID: "s"}, VerifyingResumeKeepLive},
		{"live bare shell with a recorded worker result is kept", live, notStarted, &Task{SessionID: "s", AttemptCommitSHAs: []string{"abc123"}}, VerifyingResumeKeepLive},
		{"live bare shell with a recorded verdict is kept", live, notStarted, &Task{SessionID: "s", Verification: okVerdict}, VerifyingResumeKeepLive},
		{"dead session is not a start failure", dead, notStarted, &Task{SessionID: "s"}, VerifyingResumeReset},
		{"no session is not a start failure", live, notStarted, &Task{}, VerifyingResumeReset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVerifyingResume(tt.alive, tt.agentStarted, nil, nil, tt.task); got != tt.want {
				t.Errorf("ResolveVerifyingResume() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestStableAgentStarted(t *testing.T) {
	old := agentStartSampleGap
	agentStartSampleGap = 0
	defer func() { agentStartSampleGap = old }()

	// A held launch reads not-started on every sample: not started.
	calls := 0
	never := stableAgentStarted(func(string) bool { calls++; return false })
	if never("s") {
		t.Fatal("a session that never started on any sample reported started")
	}
	if calls != agentStartSamples {
		t.Fatalf("calls = %d, want %d samples", calls, agentStartSamples)
	}

	// A worker caught between its ticks on the first sample is started on a later one.
	seq := []bool{false, false, true}
	i := 0
	between := stableAgentStarted(func(string) bool { v := seq[i]; i++; return v })
	if !between("s") {
		t.Fatal("a worker started on a later sample reported not started")
	}

	if stableAgentStarted(nil) != nil {
		t.Fatal("a nil check must stay nil so the start gate is skipped")
	}
}

func TestResolveVerifyingResume(t *testing.T) {
	okVerdict := &VerificationResult{OK: true, Summary: "all checks passed", VerifiedAt: time.Now()}

	alive := func(id string) bool { return id == "live" }
	dead := func(string) bool { return false }
	finishedOut := func(string) (string, bool) { return "work done\nDATAWATCH_COMPLETE: did the thing\n", true }
	unfinishedOut := func(string) (string, bool) { return "still thinking", true }

	tests := []struct {
		name   string
		alive  func(string) bool
		output func(string) (string, bool)
		task   *Task
		want   VerifyingResume
	}{
		{"live verifier session kept", alive, unfinishedOut, &Task{SessionID: "live"}, VerifyingResumeKeepLive},
		{"finished worker with no verdict re-verifies only", dead, finishedOut, &Task{SessionID: "gone"}, VerifyingResumeReverify},
		{"recorded worker result with no verdict re-verifies only", dead, nil, &Task{SessionID: "gone", AttemptCommitSHAs: []string{"abc123"}}, VerifyingResumeReverify},
		{"recorded ok verdict completes", dead, unfinishedOut, &Task{SessionID: "gone", Verification: okVerdict}, VerifyingResumeComplete},
		{"worker unfinished resets", dead, unfinishedOut, &Task{SessionID: "gone"}, VerifyingResumeReset},
		{"empty session resets", dead, finishedOut, &Task{}, VerifyingResumeReset},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVerifyingResume(tt.alive, nil, tt.output, nil, tt.task); got != tt.want {
				t.Errorf("ResolveVerifyingResume() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestResolveVerifyingResumeVerifierResume pins the three boot-resume
// outcomes a task in the verifying state gets when the daemon restarted:
// a live verifier session is re-attached (waiter, no second verifier), a
// parseable persisted verdict file is consumed as the outcome, and with
// neither surviving the finished worker is kept for exactly one
// verification. The worker is never reset around any of them.
func TestResolveVerifyingResumeVerifierResume(t *testing.T) {
	okFile := func() (*VerificationResult, bool) { return &VerificationResult{OK: true, Summary: "pass"}, true }
	deadFile := func() (*VerificationResult, bool) { return &VerificationResult{OK: false, Summary: "fail"}, true }

	verifierAlive := func(id string) bool { return id == "ver-1" }
	dead := func(string) bool { return false }

	tests := []struct {
		name        string
		alive       func(string) bool
		verdictFile func() (*VerificationResult, bool)
		task        *Task
		want        VerifyingResume
	}{
		{
			name:        "live verifier session is re-attached",
			alive:       verifierAlive,
			verdictFile: nil,
			task:        &Task{SessionID: "worker-1", VerifierSessionID: "ver-1", AttemptCommitSHAs: []string{"abc123"}},
			want:        VerifyingResumeReattach,
		},
		{
			name:        "live verifier wins over a parseable verdict file",
			alive:       verifierAlive,
			verdictFile: okFile,
			task:        &Task{SessionID: "worker-1", VerifierSessionID: "ver-1", AttemptCommitSHAs: []string{"abc123"}},
			want:        VerifyingResumeReattach,
		},
		{
			name:        "verifier ended with a parseable verdict file is consumed",
			alive:       dead,
			verdictFile: deadFile,
			task:        &Task{SessionID: "worker-1", VerifierSessionID: "ver-1", AttemptCommitSHAs: []string{"abc123"}},
			want:        VerifyingResumeConsume,
		},
		{
			name:        "verifier never started with a parseable verdict file is consumed",
			alive:       dead,
			verdictFile: okFile,
			task:        &Task{SessionID: "worker-1", AttemptCommitSHAs: []string{"abc123"}},
			want:        VerifyingResumeConsume,
		},
		{
			name:  "neither survives: finished worker kept for one verification",
			alive: dead,
			task:  &Task{SessionID: "worker-1", AttemptCommitSHAs: []string{"abc123"}},
			want:  VerifyingResumeReverify,
		},
		{
			name:  "neither survives and no worker result: reset",
			alive: dead,
			task:  &Task{SessionID: "worker-1"},
			want:  VerifyingResumeReset,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveVerifyingResume(tt.alive, nil, nil, tt.verdictFile, tt.task); got != tt.want {
				t.Errorf("ResolveVerifyingResume() = %d, want %d", got, tt.want)
			}
		})
	}
}
