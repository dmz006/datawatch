package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pilog"
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

// writeFakeLog writes a pi-style JSONL log under dir whose newest event is
// age old relative to now, and returns its path.
func writeFakeLog(t *testing.T, dir string, now time.Time, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, "2026-10-27T14-00-00-000Z_fake.jsonl")
	ts := now.Add(-age).UTC().Format(time.RFC3339Nano)
	line := `{"type":"message","timestamp":"` + ts + `","message":{"role":"assistant","content":[{"type":"text","text":"working"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// logAge reads the fake log the way the daemon does and returns the age of
// its newest event.
func logAge(t *testing.T, path string, now time.Time) time.Duration {
	t.Helper()
	p, err := pilog.Read(path)
	if err != nil {
		t.Fatalf("pilog.Read: %v", err)
	}
	if p.LastEventAt.IsZero() {
		t.Fatal("fake log has no timestamped event")
	}
	return now.Sub(p.LastEventAt)
}

func idleObs() SessionIdleObservation {
	return SessionIdleObservation{
		SessionID: "idle-sess",
		Backend:   "pi-prompt",
		Model:     "qwen3.8",
		State:     "waiting_input",
		IdleFor:   40 * time.Minute,
		Limit:     15 * time.Minute,
	}
}

func TestSessionIdle_GrowingLogIsNotStalled(t *testing.T) {
	now := time.Now()
	path := writeFakeLog(t, t.TempDir(), now, 30*time.Second)
	o := idleObs()
	o.LogLastEventAge = logAge(t, path, now)
	o.OutputAge = 2 * time.Hour // silent pane must not matter
	if stalled, reason := o.Stalled(); stalled {
		t.Fatalf("log event %s old, limit %s: want not stalled, got %q", o.LogLastEventAge, o.Limit, reason)
	}
}

func TestSessionIdle_SilentLogPastLimitIsIdle(t *testing.T) {
	now := time.Now()
	path := writeFakeLog(t, t.TempDir(), now, 30*time.Minute)
	o := idleObs()
	o.LogLastEventAge = logAge(t, path, now)
	o.OutputAge = time.Second // a fresh pane signal must not rescue a dead log
	stalled, reason := o.Stalled()
	if !stalled {
		t.Fatalf("log event %s old, limit %s: want stalled", o.LogLastEventAge, o.Limit)
	}
	if !strings.HasPrefix(reason, SessionIdleFailureReason) {
		t.Errorf("reason prefix: got %q", reason)
	}
}

func TestSessionIdle_UnknownLogFallsBackToOutputAge(t *testing.T) {
	o := idleObs() // LogLastEventAge == 0: unknown
	o.OutputAge = time.Minute
	if stalled, reason := o.Stalled(); stalled {
		t.Fatalf("unknown log, recent OutputAge: want not stalled, got %q", reason)
	}
	o.OutputAge = 30 * time.Minute
	if stalled, _ := o.Stalled(); !stalled {
		t.Fatal("unknown log, OutputAge past limit: want stalled")
	}
	o.OutputAge = 0 // nothing known: the old IdleFor rule alone decides
	if stalled, _ := o.Stalled(); !stalled {
		t.Fatal("no age signal, IdleFor past limit: want stalled")
	}
}
