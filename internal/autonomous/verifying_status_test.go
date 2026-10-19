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
