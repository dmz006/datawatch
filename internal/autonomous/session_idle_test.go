package autonomous

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSessionIdle_StallsWhenWaitingInputNoChild(t *testing.T) {
	o := SessionIdleObservation{
		SessionID:           "idle-sess",
		Backend:             "pi-prompt",
		Model:               "qwen3.8",
		State:               "waiting_input",
		IdleFor:             10 * time.Minute,
		HasLiveChildProcess: false,
		Limit:               15 * time.Minute,
	}
	stalled, reason := o.Stalled()
	// The observation is idle for 10m against a 15m limit, so it must not stall.
	if stalled {
		t.Fatalf("idle 10m < limit 15m: want not stalled, got reason %q", reason)
	}
	o.IdleFor = 20 * time.Minute
	stalled, reason = o.Stalled()
	if !stalled {
		t.Fatal("idle 20m > limit 15m with no child: want stalled")
	}
	if !strings.HasPrefix(reason, "session idle without completion") {
		t.Errorf("reason prefix: got %q", reason)
	}
	for _, want := range []string{"idle-sess", "backend=pi-prompt", "model=qwen3.8"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason %q missing %q", reason, want)
		}
	}
}

func TestSessionIdle_ActiveChildNeverStalls(t *testing.T) {
	o := SessionIdleObservation{
		SessionID:           "idle-sess",
		Backend:             "pi-prompt",
		Model:               "qwen3.8",
		State:               "waiting_input",
		IdleFor:             2 * time.Hour,
		HasLiveChildProcess: true,
		Limit:               15 * time.Minute,
	}
	if stalled, reason := o.Stalled(); stalled {
		t.Fatalf("live child process: want never stalled, got %q", reason)
	}
}

func TestSessionIdle_RunningStateNeverStalls(t *testing.T) {
	o := SessionIdleObservation{
		SessionID: "idle-sess",
		Backend:   "pi-prompt",
		Model:     "qwen3.8",
		State:     "running",
		IdleFor:   2 * time.Hour,
		Limit:     15 * time.Minute,
	}
	if stalled, reason := o.Stalled(); stalled {
		t.Fatalf("state running: want never stalled, got %q", reason)
	}
}

func TestSessionIdle_DisabledWhenLimitZero(t *testing.T) {
	o := SessionIdleObservation{
		SessionID: "idle-sess",
		Backend:   "pi-prompt",
		State:     "waiting_input",
		IdleFor:   2 * time.Hour,
		Limit:     0,
	}
	if stalled, reason := o.Stalled(); stalled {
		t.Fatalf("limit 0: want detector disabled, got %q", reason)
	}
}

func TestSessionIdle_IdleStateStalls(t *testing.T) {
	o := SessionIdleObservation{
		SessionID: "idle-sess",
		Backend:   "claude",
		State:     "idle",
		IdleFor:   time.Hour,
		Limit:     15 * time.Minute,
	}
	if stalled, _ := o.Stalled(); !stalled {
		t.Fatal("state idle past limit with no child: want stalled")
	}
}

func TestSessionIdle_ErrorIsWorkerStalled(t *testing.T) {
	o := SessionIdleObservation{
		SessionID: "idle-sess",
		Backend:   "pi-prompt",
		Model:     "qwen3.8",
		State:     "waiting_input",
		IdleFor:   time.Hour,
		Limit:     15 * time.Minute,
	}
	err := SessionIdleError(o)
	if !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("errors.Is(SessionIdleError, ErrWorkerStalled) = false; err = %v", err)
	}
	if !strings.HasPrefix(err.Error(), SessionIdleFailureReason) {
		t.Errorf("error text %q does not start with failure reason", err.Error())
	}
}
