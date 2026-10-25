package autonomous

import (
	"errors"
	"testing"
	"time"
)

// fakeNudgeWorker is the daemon side of one idle worker, as the nudge hooks
// see it: a mutable observation, a delivery flag, and records of every input
// sent, nudge recorded and kill made. No tmux, no clock.
type fakeNudgeWorker struct {
	obs       WorkerNudgeObservation
	delivered bool
	task      string
	onSend    func()

	sent     []string
	sources  []string
	recorded []time.Time
	ended    []WorkerNudgeObservation
}

func (w *fakeNudgeWorker) hooks() WorkerNudgeHooks {
	return WorkerNudgeHooks{
		Observe:  func() WorkerNudgeObservation { return w.obs },
		Delivery: func() (bool, string) { return w.delivered, w.task },
		SendInput: func(text, source string) error {
			w.sent = append(w.sent, text)
			w.sources = append(w.sources, source)
			if w.onSend != nil {
				w.onSend()
			}
			return nil
		},
		Record: func(at time.Time) (int, error) {
			w.recorded = append(w.recorded, at)
			return len(w.recorded), nil
		},
		End: func(obs WorkerNudgeObservation) {
			w.ended = append(w.ended, obs)
		},
	}
}

func TestStepWorkerNudge_UndeliveredTaskIsResent(t *testing.T) {
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: false, task: "build the thing"}
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)

	if err := StepWorkerNudge(tr, nudgeT0, WorkerNudgeFamily, "sess", "prd/task", w.hooks()); err != nil {
		t.Fatalf("first idle observation: %v", err)
	}
	if len(w.sent) != 0 {
		t.Fatalf("sent before the nudge window: %v", w.sent)
	}
	at := nudgeT0.Add(nudgeTestWindow)
	if err := StepWorkerNudge(tr, at, WorkerNudgeFamily, "sess", "prd/task", w.hooks()); err != nil {
		t.Fatalf("at the nudge window: %v", err)
	}
	if len(w.sent) != 1 || w.sent[0] != "build the thing" {
		t.Fatalf("sent = %q, want the task text once", w.sent)
	}
	if w.sources[0] != "task-redelivery" {
		t.Fatalf("source = %q, want task-redelivery", w.sources[0])
	}
	if len(w.recorded) != 1 || !w.recorded[0].Equal(at) {
		t.Fatalf("recorded = %v, want one nudge at %s", w.recorded, at)
	}
}

func TestStepWorkerNudge_DeliveredGetsCompletionNudge(t *testing.T) {
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true, task: "build the thing"}
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)

	for _, at := range []time.Duration{0, nudgeTestWindow} {
		if err := StepWorkerNudge(tr, nudgeT0.Add(at), WorkerNudgeFamily, "sess", "prd/task", w.hooks()); err != nil {
			t.Fatalf("step at %s: %v", at, err)
		}
	}
	if len(w.sent) != 1 || w.sent[0] != WorkerNudgeText {
		t.Fatalf("sent = %q, want the completion nudge once", w.sent)
	}
	if w.sources[0] != "idle-nudge" {
		t.Fatalf("source = %q, want idle-nudge", w.sources[0])
	}
}

func TestStepWorkerNudge_OtherFamilyIsNeverObservedOrNudged(t *testing.T) {
	observed := 0
	hooks := WorkerNudgeHooks{
		Observe:   func() WorkerNudgeObservation { observed++; return nudgeObs(time.Hour) },
		SendInput: func(string, string) error { t.Fatal("non-claude worker was nudged"); return nil },
	}
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	for at := time.Duration(0); at <= nudgeTestStall+nudgeTestWindow; at += time.Minute {
		if err := StepWorkerNudge(tr, nudgeT0.Add(at), "pi-prompt", "sess", "prd/task", hooks); err != nil {
			t.Fatalf("pi-prompt step at %s: %v", at, err)
		}
	}
	if observed != 0 {
		t.Fatalf("observed %d times for a non-claude family, want 0", observed)
	}
}

func TestStepWorkerNudge_StallEndsWorkerAndWrapsErrWorkerStalled(t *testing.T) {
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true}
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	if err := StepWorkerNudge(tr, nudgeT0, WorkerNudgeFamily, "sess", "prd/task", w.hooks()); err != nil {
		t.Fatalf("idle start: %v", err)
	}
	if err := StepWorkerNudge(tr, nudgeT0.Add(nudgeTestWindow), WorkerNudgeFamily, "sess", "prd/task", w.hooks()); err != nil {
		t.Fatalf("nudge: %v", err)
	}

	stallAt := nudgeT0.Add(nudgeTestWindow + nudgeTestStall)
	if err := StepWorkerNudge(tr, stallAt.Add(-time.Second), WorkerNudgeFamily, "sess", "prd/task", w.hooks()); err != nil {
		t.Fatalf("one second before the stall window: %v", err)
	}
	err := StepWorkerNudge(tr, stallAt, WorkerNudgeFamily, "sess", "prd/task", w.hooks())
	if !errors.Is(err, ErrWorkerStalled) {
		t.Fatalf("stall error = %v, want wrapping ErrWorkerStalled", err)
	}
	if len(w.ended) != 1 {
		t.Fatalf("worker ended %d times, want 1", len(w.ended))
	}
	if len(w.sent) != 1 {
		t.Fatalf("sent %d inputs, want only the one nudge", len(w.sent))
	}
}

func TestStepWorkerNudge_RecordFailureDoesNotFailTheWait(t *testing.T) {
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true}
	hooks := w.hooks()
	hooks.Record = func(time.Time) (int, error) { return 0, errors.New("store unavailable") }
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	for _, at := range []time.Duration{0, nudgeTestWindow} {
		if err := StepWorkerNudge(tr, nudgeT0.Add(at), WorkerNudgeFamily, "sess", "prd/task", hooks); err != nil {
			t.Fatalf("step at %s with a failing record: %v", at, err)
		}
	}
	if len(w.sent) != 1 {
		t.Fatalf("sent %d nudges, want 1 despite the record failure", len(w.sent))
	}
}
