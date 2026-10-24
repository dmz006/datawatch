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
			if got := ResolveVerifyingResume(tt.alive, tt.output, tt.task); got != tt.want {
				t.Errorf("ResolveVerifyingResume() = %d, want %d", got, tt.want)
			}
		})
	}
}
