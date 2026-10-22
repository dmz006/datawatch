package autonomous

import (
	"testing"
	"time"
)

func TestWorkerNudgeTracker(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	idle := func(age time.Duration) WorkerNudgeObservation {
		return WorkerNudgeObservation{State: "waiting_input", OutputAge: age}
	}
	tr := NewWorkerNudgeTracker(180*time.Second, 600*time.Second)
	if a := tr.Observe(t0, "pi-prompt", true, idle(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("wrong family: %s", a)
	}
	if a := tr.Observe(t0, "claude-code", false, idle(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("not prd-owned: %s", a)
	}
	if a := tr.Observe(t0, "claude-code", true, idle(0)); a != WorkerNudgeNone {
		t.Fatalf("start: %s", a)
	}
	// live child resets the idle clock
	live := idle(0)
	live.HasLiveChildProcess = true
	tr.Observe(t0.Add(100*time.Second), "claude-code", true, live)
	if a := tr.Observe(t0.Add(200*time.Second), "claude-code", true, idle(200*time.Second)); a != WorkerNudgeNone {
		t.Fatalf("clock should have reset: %s", a)
	}
	if a := tr.Observe(t0.Add(400*time.Second), "claude-code", true, idle(400*time.Second)); a != WorkerNudgeSend {
		t.Fatalf("want nudge: %s", a)
	}
	// exactly one nudge
	if a := tr.Observe(t0.Add(500*time.Second), "claude-code", true, idle(500*time.Second)); a != WorkerNudgeNone {
		t.Fatalf("second nudge: %s", a)
	}
	if a := tr.Observe(t0.Add(999*time.Second), "claude-code", true, idle(999*time.Second)); a != WorkerNudgeNone {
		t.Fatalf("stall too early: %s", a)
	}
	if a := tr.Observe(t0.Add(1001*time.Second), "claude-code", true, idle(1001*time.Second)); a != WorkerNudgeStall {
		t.Fatalf("want stall: %s", a)
	}
}

func TestWorkerNudgeTrackerOutputAndCompletion(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tr := NewWorkerNudgeTracker(10*time.Second, 20*time.Second)
	tr.Observe(t0, "claude-code", true, WorkerNudgeObservation{State: "idle", OutputAge: 60 * time.Second})
	if a := tr.Observe(t0.Add(10*time.Second), "claude-code", true, WorkerNudgeObservation{State: "idle", OutputAge: 70 * time.Second}); a != WorkerNudgeSend {
		t.Fatalf("want nudge: %s", a)
	}
	// fresh output after the nudge ends tracking
	if a := tr.Observe(t0.Add(40*time.Second), "claude-code", true, WorkerNudgeObservation{State: "idle", OutputAge: 5 * time.Second}); a != WorkerNudgeNone {
		t.Fatalf("output should end tracking: %s", a)
	}
	if a := tr.Observe(t0.Add(500*time.Second), "claude-code", true, WorkerNudgeObservation{State: "idle", OutputAge: time.Hour}); a != WorkerNudgeNone {
		t.Fatalf("tracking should be over: %s", a)
	}
	tr2 := NewWorkerNudgeTracker(10*time.Second, 20*time.Second)
	if a := tr2.Observe(t0, "claude-code", true, WorkerNudgeObservation{State: "idle", OutputAge: time.Hour, CompletionSeen: true}); a != WorkerNudgeNone {
		t.Fatalf("completion: %s", a)
	}
	if a := tr2.Observe(t0.Add(time.Hour), "claude-code", true, WorkerNudgeObservation{State: "idle", OutputAge: time.Hour}); a != WorkerNudgeNone {
		t.Fatalf("completion should end tracking: %s", a)
	}
}
