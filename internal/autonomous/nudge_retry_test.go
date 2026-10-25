// Nudge-to-retry coverage: a claude-code worker goes idle, receives its one
// nudge, stays silent past the stall window, and the task is retried in a
// fresh session. The worker is a stub in the style of launch_failure_test.go;
// the verify closure drives StepWorkerNudge on the injected clock the way the
// daemon's wait loop does, so the executor sees the same ErrWorkerStalled a
// live worker produces.
package autonomous

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// nudgeRetryLog collects the standard logger output. The logger serializes its
// writes, but background executors may still log while the test reads, so the
// buffer carries its own lock.
type nudgeRetryLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *nudgeRetryLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *nudgeRetryLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// nudgeRetryTask reads the task's current state from the store.
func nudgeRetryTask(m *Manager, prdID, taskID string) Task {
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		return Task{}
	}
	for _, s := range prd.Story {
		for _, tk := range s.Tasks {
			if tk.ID == taskID {
				return tk
			}
		}
	}
	return Task{}
}

// nudgeRetryHooks wires the stub worker to the real manager, so the nudge and
// its outcome land on the task exactly as the daemon writes them.
func nudgeRetryHooks(t *testing.T, m *Manager, w *fakeNudgeWorker, prdID, taskID, sessionID string) WorkerNudgeHooks {
	t.Helper()
	hooks := w.hooks()
	hooks.Record = func(at time.Time) (int, error) {
		if err := m.RecordTaskNudge(prdID, taskID, sessionID, at); err != nil {
			return 0, err
		}
		return m.TaskNudgeCount(prdID, taskID), nil
	}
	hooks.Outcome = func(outcome string) {
		if err := m.RecordTaskNudgeOutcome(prdID, taskID, sessionID, outcome); err != nil {
			t.Errorf("record nudge outcome %s for %s: %v", outcome, sessionID, err)
		}
	}
	return hooks
}

// nudgeRetryFirstSession runs the first worker: idle from the start, nudged at
// the idle window, silent until the stall window ends it. It returns the
// stall error the executor retries on.
func nudgeRetryFirstSession(t *testing.T, m *Manager, prdID string, tk *Task) error {
	t.Helper()
	const sessionID = "s-1"
	label := prdID + "/" + tk.ID
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true, task: tk.Spec}
	hooks := nudgeRetryHooks(t, m, w, prdID, tk.ID, sessionID)
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	step := func(at time.Duration) error {
		return StepWorkerNudge(tr, nudgeT0.Add(at), WorkerNudgeFamily, sessionID, label, hooks)
	}

	if err := step(0); err != nil {
		t.Errorf("idle start: %v", err)
	}
	if len(w.sent) != 0 {
		t.Errorf("nudged before the idle window: %q", w.sent)
	}
	if err := step(nudgeTestWindow); err != nil {
		t.Errorf("at the idle window: %v", err)
	}
	if len(w.sent) != 1 || w.sent[0] != WorkerNudgeText {
		t.Errorf("sent = %q, want the completion nudge once", w.sent)
	}
	events := nudgeRetryTask(m, prdID, tk.ID).NudgeEvents
	if len(events) != 1 || events[0].Outcome != NudgeOutcomeSent || events[0].SessionID != sessionID {
		t.Errorf("after the nudge, events = %+v, want one sent event for %s", events, sessionID)
	}

	if err := step(nudgeTestWindow + nudgeTestStall - time.Second); err != nil {
		t.Errorf("one second before the stall window: %v", err)
	}
	err := step(nudgeTestWindow + nudgeTestStall)
	if !errors.Is(err, ErrWorkerStalled) {
		t.Errorf("stall error = %v, want wrapping ErrWorkerStalled", err)
	}
	if len(w.ended) != 1 {
		t.Errorf("worker ended %d times, want 1", len(w.ended))
	}
	return err
}

// nudgeRetrySecondSession runs the retry's worker up to just before its own
// idle window. It must receive no nudge: the stall belonged to the first
// session, and a fresh session starts its own idle clock.
func nudgeRetrySecondSession(t *testing.T, m *Manager, prdID string, tk *Task) {
	t.Helper()
	label := prdID + "/" + tk.ID
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true, task: tk.Spec}
	hooks := nudgeRetryHooks(t, m, w, prdID, tk.ID, tk.SessionID)
	tr := NewWorkerNudgeTracker(nudgeTestWindow, nudgeTestStall)
	for _, at := range []time.Duration{0, nudgeTestWindow - time.Second} {
		if err := StepWorkerNudge(tr, nudgeT0.Add(at), WorkerNudgeFamily, tk.SessionID, label, hooks); err != nil {
			t.Errorf("retry session step at %s: %v", at, err)
		}
	}
	if len(w.sent) != 0 {
		t.Errorf("retry session %s nudged before its own idle window: %q", tk.SessionID, w.sent)
	}
}

func TestNudgeRetry_StallEndsSessionAndRetriesInFreshSession(t *testing.T) {
	m, api, _, _ := apiFixture(t)
	cfg := m.Config()
	cfg.AutoFixRetries = 2
	m.SetConfig(cfg)
	prd, _ := m.CreatePRD("s", t.TempDir(), "claude-code", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "write docs/x.md"}}}})
	// Legacy budget: one attempt plus auto_fix_retries, so the retry count is exact.
	fp, _ := m.Store().GetPRD(prd.ID)
	fp.FailureEscalation = &FailureEscalationPolicy{Enabled: escalationOn(false)}
	_ = m.Store().SavePRD(fp)

	logs := &nudgeRetryLog{}
	prevOut := log.Writer()
	log.SetOutput(logs)
	defer log.SetOutput(prevOut)

	spawns := 0
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		spawns++
		return SpawnResult{SessionID: fmt.Sprintf("s-%d", spawns)}, nil
	}
	verifyCalls := 0
	var errAfterStall, retrySession string
	verify := func(_ context.Context, p *PRD, tk *Task) (VerificationResult, error) {
		verifyCalls++
		if verifyCalls == 1 {
			return VerificationResult{}, nudgeRetryFirstSession(t, m, p.ID, tk)
		}
		// The retry: the executor's hint for the stalled attempt, then the new session.
		errAfterStall = tk.Error
		retrySession = tk.SessionID
		nudgeRetrySecondSession(t, m, p.ID, tk)
		return VerificationResult{OK: true, Summary: "good"}, nil
	}
	got := runToTerminal(t, m, api, prd.ID, spawn, verify)
	if got.Status != PRDCompleted {
		t.Fatalf("a stalled nudge that recovers on retry must complete, got %s", got.Status)
	}
	taskID := got.Story[0].Tasks[0].ID
	label := prd.ID + "/" + taskID

	if verifyCalls != 2 || spawns != 2 {
		t.Fatalf("want 2 verify calls and 2 spawns, got %d/%d", verifyCalls, spawns)
	}
	if retrySession != "s-2" {
		t.Fatalf("retry ran in session %q, want a fresh s-2", retrySession)
	}
	if !strings.Contains(errAfterStall, "session=s-1") || !strings.Contains(errAfterStall, ErrWorkerStalled.Error()) {
		t.Fatalf("the stalled attempt should record the stall of s-1, got %q", errAfterStall)
	}

	// The one nudge stays on the first session and ends as stalled.
	tk := nudgeRetryTask(m, prd.ID, taskID)
	if tk.NudgeCount != 1 {
		t.Fatalf("task NudgeCount = %d, want 1", tk.NudgeCount)
	}
	if len(tk.NudgeEvents) != 1 {
		t.Fatalf("nudge events = %+v, want exactly the first session's nudge", tk.NudgeEvents)
	}
	if ev := tk.NudgeEvents[0]; ev.SessionID != "s-1" || ev.Outcome != NudgeOutcomeStalled {
		t.Fatalf("nudge event = %+v, want session s-1 with outcome stalled", ev)
	}

	// Each log line names the session and the task. The stall retry reports
	// the attempt number and the retries left: budget 3 (1 + 2 retries) drops
	// to 2 remaining after the first stalled attempt.
	out := logs.String()
	for _, want := range []string{
		fmt.Sprintf("[autonomous] idle worker s-1 task %s: nudge 1 sent after ", label),
		fmt.Sprintf("[autonomous] idle worker s-1 task %s: still silent for %s after nudge; ending session for retry", label, nudgeTestStall),
		fmt.Sprintf("[autonomous] task %s: stall retry 1 of 2 after session s-1 (after completion nudge)", label),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("log is missing %q\nlog:\n%s", want, out)
		}
	}
}
