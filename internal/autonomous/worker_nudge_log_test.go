package autonomous

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"
)

// captureNudgeStepLog points the standard logger at a buffer for one test and
// restores the previous output when the test ends.
func captureNudgeStepLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestStepWorkerNudge_AnsweredLogNamesSessionAndTask(t *testing.T) {
	cases := []struct {
		name  string
		obs   WorkerNudgeObservation
		cause string
	}{
		{
			name:  "output after the nudge",
			obs:   WorkerNudgeObservation{State: "waiting_input", OutputAge: time.Second},
			cause: "new output",
		},
		{
			name:  "completion marker after the nudge",
			obs:   WorkerNudgeObservation{State: "waiting_input", CompletionSeen: true},
			cause: "completion marker",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logBuf := captureNudgeStepLog(t)
			w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true}
			tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
			for _, at := range []time.Duration{0, nudgeTestWindow} {
				if err := StepWorkerNudge(tr, nudgeT0.Add(at), WorkerNudgeFamily, "sess-7", "prd-7/task-7", w.hooks()); err != nil {
					t.Fatalf("step at %s: %v", at, err)
				}
			}
			w.obs = tc.obs
			if err := StepWorkerNudge(tr, nudgeT0.Add(nudgeTestWindow+3*time.Second), WorkerNudgeFamily, "sess-7", "prd-7/task-7", w.hooks()); err != nil {
				t.Fatalf("answer step: %v", err)
			}
			want := fmt.Sprintf("idle worker sess-7 task prd-7/task-7: nudge answered by %s", tc.cause)
			if !strings.Contains(logBuf.String(), want) {
				t.Fatalf("answered line missing %q; log:\n%s", want, logBuf.String())
			}
		})
	}
}

func TestCloseNudgeWait_AnsweredLogNamesSessionAndTask(t *testing.T) {
	logBuf := captureNudgeStepLog(t)
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true}
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	for _, at := range []time.Duration{0, nudgeTestWindow} {
		if err := StepWorkerNudge(tr, nudgeT0.Add(at), WorkerNudgeFamily, "sess-8", "prd-8/task-8", w.hooks()); err != nil {
			t.Fatalf("step at %s: %v", at, err)
		}
	}
	CloseNudgeWait(tr, w.hooks(), "sess-8", "prd-8/task-8")
	if want := "idle worker sess-8 task prd-8/task-8: nudge answered by session completion"; !strings.Contains(logBuf.String(), want) {
		t.Fatalf("close line missing %q; log:\n%s", want, logBuf.String())
	}
}

func TestStallRetryReason_NamesTheNudgeStall(t *testing.T) {
	nudgeErr := NudgeStallError{SessionID: "sess-9"}
	if !errors.Is(nudgeErr, ErrWorkerStalled) {
		t.Fatal("NudgeStallError does not wrap ErrWorkerStalled")
	}
	if want := "worker idle after completion nudge (session=sess-9): worker stalled"; nudgeErr.Error() != want {
		t.Fatalf("Error() = %q, want %q", nudgeErr.Error(), want)
	}
	if got := StallRetryReason(fmt.Errorf("verify: %w", nudgeErr)); got != "after completion nudge" {
		t.Fatalf("wrapped nudge stall reason = %q, want %q", got, "after completion nudge")
	}
	if got := StallRetryReason(ErrWorkerStalled); got != "worker stall" {
		t.Fatalf("plain stall reason = %q, want %q", got, "worker stall")
	}
}

func TestTaskNudgeEventForSession_LatestOfThatSession(t *testing.T) {
	first := time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)
	later := first.Add(time.Hour)
	task := Task{NudgeEvents: []NudgeEvent{
		{At: first, Count: 1, SessionID: "a", Outcome: NudgeOutcomeSent},
		{At: first.Add(time.Minute), Count: 2, SessionID: "b", Outcome: NudgeOutcomeStalled},
		{At: later, Count: 3, SessionID: "a", Outcome: NudgeOutcomeAnswered},
	}}

	ev, ok := task.NudgeEventForSession("a")
	if !ok || ev.Count != 3 || ev.Outcome != NudgeOutcomeAnswered {
		t.Fatalf("NudgeEventForSession(a) = %+v, %t; want the latest of session a", ev, ok)
	}
	if got := task.NudgeTimeForSession("a"); !got.Equal(later) {
		t.Errorf("NudgeTimeForSession(a) = %s, want %s", got, later)
	}
	if _, ok := task.NudgeEventForSession("c"); ok {
		t.Error("NudgeEventForSession(c) found a nudge for a session that has none")
	}
	if _, ok := task.NudgeEventForSession(""); ok {
		t.Error("an empty session id matched a nudge")
	}
}
