package autonomous

import (
	"testing"
	"time"
)

// Injected-clock tests for WorkerNudgeTracker. No tmux, no sleeps: the
// tracker is pure and the wait loop only feeds it observations.

const (
	nudgeTestWindow = 180 * time.Second
	nudgeTestStall  = 600 * time.Second
)

var nudgeT0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func nudgeObs(age time.Duration) WorkerNudgeObservation {
	return WorkerNudgeObservation{State: "waiting_input", OutputAge: age}
}

func nudgeObserve(tr *WorkerNudgeTracker, at time.Duration, o WorkerNudgeObservation) WorkerNudgeAction {
	return tr.Observe(nudgeT0.Add(at), WorkerNudgeFamily, true, o)
}

func TestWorkerNudge_SentOnce(t *testing.T) {
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	sent := 0
	// Idle the whole time; poll every 3s like the wait loop for 9 minutes.
	for at := time.Duration(0); at <= 9*time.Minute; at += 3 * time.Second {
		if nudgeObserve(tr, at, nudgeObs(at+time.Minute)) == WorkerNudgeSend {
			sent++
		}
	}
	if sent != 1 {
		t.Fatalf("nudge sent %d times, want exactly 1", sent)
	}
	if !tr.Nudged() {
		t.Fatal("Nudged() = false after send")
	}
}

func TestWorkerNudge_NoNudgeWhileChildProcess(t *testing.T) {
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	busy := nudgeObs(time.Hour)
	busy.HasLiveChildProcess = true
	// A live child suppresses the nudge and any stall, however long it runs.
	for at := time.Duration(0); at <= 2*time.Hour; at += 30 * time.Second {
		if a := nudgeObserve(tr, at, busy); a != WorkerNudgeNone {
			t.Fatalf("at %s with live child: action %s, want none", at, a)
		}
	}
	if tr.Nudged() {
		t.Fatal("nudged while a child process was running")
	}
	// Once the child exits the idle clock starts fresh: no instant nudge.
	if a := nudgeObserve(tr, 2*time.Hour+time.Second, nudgeObs(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("immediately after child exit: %s, want none", a)
	}
	// A child appearing after the nudge also suppresses the stall.
	tr2 := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	nudgeObserve(tr2, 0, nudgeObs(time.Hour))
	if a := nudgeObserve(tr2, nudgeTestWindow, nudgeObs(time.Hour)); a != WorkerNudgeSend {
		t.Fatalf("want nudge, got %s", a)
	}
	if a := nudgeObserve(tr2, nudgeTestWindow+nudgeTestStall+time.Minute, busy); a != WorkerNudgeNone {
		t.Fatalf("stall with live child: %s, want none", a)
	}
}

func TestWorkerNudge_CompletionAfterNudge(t *testing.T) {
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	nudgeObserve(tr, 0, nudgeObs(time.Hour))
	if a := nudgeObserve(tr, nudgeTestWindow, nudgeObs(time.Hour)); a != WorkerNudgeSend {
		t.Fatalf("want nudge, got %s", a)
	}
	done := nudgeObs(time.Hour)
	done.CompletionSeen = true
	if a := nudgeObserve(tr, nudgeTestWindow+10*time.Second, done); a != WorkerNudgeNone {
		t.Fatalf("completion marker: %s, want none", a)
	}
	// Tracking is over: no stall even long past the second window.
	if a := nudgeObserve(tr, nudgeTestWindow+nudgeTestStall*2, nudgeObs(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("stall after completion: %s, want none", a)
	}
}

func TestWorkerNudge_StallAfterSecondWindow(t *testing.T) {
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	nudgeObserve(tr, 0, nudgeObs(time.Hour))
	if a := nudgeObserve(tr, nudgeTestWindow, nudgeObs(time.Hour)); a != WorkerNudgeSend {
		t.Fatalf("want nudge, got %s", a)
	}
	at := nudgeTestWindow + nudgeTestStall
	if a := nudgeObserve(tr, at-time.Second, nudgeObs(time.Hour)); a != WorkerNudgeNone {
		t.Fatalf("stall before second window elapsed: %s", a)
	}
	// The wait loop maps WorkerNudgeStall to a kill plus an
	// ErrWorkerStalled-wrapped error; the tracker's contract is the decision.
	if a := nudgeObserve(tr, at, nudgeObs(time.Hour)); a != WorkerNudgeStall {
		t.Fatalf("want stall at second window, got %s", a)
	}
}

func TestWorkerNudge_RecordTaskNudgeCounters(t *testing.T) {
	cfg := DefaultConfig()
	m, err := NewManager(t.TempDir(), cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("spec", "/projnudgecounters", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "s", Tasks: []Task{{Title: "a", Status: TaskInProgress}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	id := prd.Story[0].Tasks[0].ID
	t1, t2 := nudgeT0, nudgeT0.Add(time.Minute)
	for _, at := range []time.Time{t1, t2} {
		if err := m.RecordTaskNudge(prd.ID, id, "sess-counters", at); err != nil {
			t.Fatalf("RecordTaskNudge: %v", err)
		}
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	task := prd.Story[0].Tasks[0]
	if task.NudgeCount != 2 || len(task.NudgeEvents) != 2 {
		t.Fatalf("NudgeCount=%d events=%d, want 2/2", task.NudgeCount, len(task.NudgeEvents))
	}
	if ev := task.NudgeEvents[1]; ev.Count != 2 || !ev.At.Equal(t2) || ev.Outcome != "sent" {
		t.Fatalf("second event = %+v", ev)
	}
	if err := m.RecordTaskNudge(prd.ID, "missing", "sess-counters", t1); err == nil {
		t.Fatal("expected error for unknown task")
	}
}
