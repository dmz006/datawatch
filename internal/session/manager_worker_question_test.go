// Worker question routing tests: a DATAWATCH_QUESTION line reaches the hook
// only behind the gate and only outside the restart-grace and task-echo
// windows, and it never moves the session out of its current state.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/session -run TestWorkerQuestion

package session

import (
	"context"
	"sync"
	"testing"
	"time"
)

const (
	wqValidLine   = `DATAWATCH_QUESTION: {"question":"Which store?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}`
	wqRefusedLine = `DATAWATCH_QUESTION: {"question":"Which store?","options":[{"id":"a","label":"A"}]}`
)

// wqRecorder collects the events a Manager's worker question hook receives.
type wqRecorder struct {
	mu     sync.Mutex
	events []WorkerQuestionEvent
}

func (r *wqRecorder) hook(ev WorkerQuestionEvent) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *wqRecorder) got() []WorkerQuestionEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]WorkerQuestionEvent(nil), r.events...)
}

// wqSetup builds a Manager with a running PRD-linked session, a fixed clock,
// the gate set to gateOn and a recording hook.
func wqSetup(t *testing.T, gateOn bool) (*Manager, *Session, *wqRecorder, time.Time) {
	t.Helper()
	mgr, _ := newTestManagerWithFake(t)
	now := time.Now()
	mgr.clock = func() time.Time { return now }
	sess := &Session{
		ID: "wq01", FullID: "testhost-wq01", TmuxSession: "cs-wq01",
		State: StateRunning, UpdatedAt: now, PRDID: "prd-1", TaskID: "task-1",
	}
	if err := mgr.SaveSession(sess); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	rec := &wqRecorder{}
	mgr.SetWorkerQuestionGate(func() bool { return gateOn })
	mgr.SetWorkerQuestionHook(rec.hook)
	return mgr, sess, rec, now
}

// wqFeed pushes line through processOutputLine like a real log tail would and
// returns the session as the store now holds it.
func wqFeed(t *testing.T, mgr *Manager, sess *Session, line string) *Session {
	t.Helper()
	var lastOutputTime time.Time
	var pendingLines []string
	var lastPromptMatch time.Time
	mgr.processOutputLine(context.Background(), sess, NewProjectGit(sess.ProjectDir), line, &lastOutputTime, &pendingLines, &lastPromptMatch, func() *Tracker { return nil })
	cur, ok := mgr.store.Get(sess.FullID)
	if !ok {
		t.Fatalf("session %s not in store", sess.FullID)
	}
	return cur
}

func TestWorkerQuestion_GateOffNoHookCall(t *testing.T) {
	mgr, sess, rec, _ := wqSetup(t, false)
	wqFeed(t, mgr, sess, wqValidLine)
	wqFeed(t, mgr, sess, wqRefusedLine)
	if n := len(rec.got()); n != 0 {
		t.Errorf("gate off: hook called %d times, want 0", n)
	}
}

func TestWorkerQuestion_NilHookAndNilGateAreNoOps(t *testing.T) {
	mgr, sess, rec, _ := wqSetup(t, true)
	mgr.SetWorkerQuestionHook(nil)
	wqFeed(t, mgr, sess, wqValidLine)
	mgr.SetWorkerQuestionHook(rec.hook)
	mgr.SetWorkerQuestionGate(nil)
	wqFeed(t, mgr, sess, wqValidLine)
	if n := len(rec.got()); n != 0 {
		t.Errorf("nil hook / nil gate: hook called %d times, want 0", n)
	}
}

func TestWorkerQuestion_GateOnTrustedCallsHookOnce(t *testing.T) {
	mgr, sess, rec, now := wqSetup(t, true)
	cur := wqFeed(t, mgr, sess, "● "+wqValidLine)
	evs := rec.got()
	if len(evs) != 1 {
		t.Fatalf("hook called %d times, want 1", len(evs))
	}
	ev := evs[0]
	if ev.SessionFullID != sess.FullID || ev.PRDID != "prd-1" || ev.TaskID != "task-1" {
		t.Errorf("event ids = (%q, %q, %q), want (%q, prd-1, task-1)", ev.SessionFullID, ev.PRDID, ev.TaskID, sess.FullID)
	}
	if ev.Signal == nil || ev.RefusalReason != "" {
		t.Fatalf("event = %+v, want a signal and no refusal", ev)
	}
	if ev.Signal.Question != "Which store?" || len(ev.Signal.Options) != 2 || ev.Signal.LineSHA256 == "" {
		t.Errorf("signal = %+v", ev.Signal)
	}
	if !ev.At.Equal(now) {
		t.Errorf("event time = %v, want %v", ev.At, now)
	}
	if cur.State != StateRunning {
		t.Errorf("worker question changed session state to %q, want %q", cur.State, StateRunning)
	}
}

func TestWorkerQuestion_RestartGraceDropsIt(t *testing.T) {
	mgr, sess, rec, now := wqSetup(t, true)
	mgr.mu.Lock()
	mgr.restartedAt[sess.FullID] = now.Add(-time.Second)
	mgr.mu.Unlock()
	wqFeed(t, mgr, sess, wqValidLine)
	if n := len(rec.got()); n != 0 {
		t.Fatalf("inside restart grace: hook called %d times, want 0", n)
	}

	// Once the grace has passed the same line is believed.
	mgr.mu.Lock()
	mgr.restartedAt[sess.FullID] = now.Add(-mgr.restartGraceWindow() - time.Second)
	mgr.mu.Unlock()
	wqFeed(t, mgr, sess, wqValidLine)
	if n := len(rec.got()); n != 1 {
		t.Errorf("after restart grace: hook called %d times, want 1", n)
	}
}

func TestWorkerQuestion_TaskEchoWindowDropsIt(t *testing.T) {
	mgr, sess, rec, now := wqSetup(t, true)
	mgr.mu.Lock()
	mgr.taskDeliveredAt[sess.FullID] = now.Add(-time.Second)
	mgr.mu.Unlock()
	wqFeed(t, mgr, sess, wqValidLine)
	if n := len(rec.got()); n != 0 {
		t.Fatalf("inside task-echo window: hook called %d times, want 0", n)
	}

	mgr.mu.Lock()
	mgr.taskDeliveredAt[sess.FullID] = now.Add(-mgr.echoWindow() - time.Second)
	mgr.mu.Unlock()
	wqFeed(t, mgr, sess, wqValidLine)
	if n := len(rec.got()); n != 1 {
		t.Errorf("after task-echo window: hook called %d times, want 1", n)
	}
}

func TestWorkerQuestion_RefusedLineCallsHookWithReasonOnly(t *testing.T) {
	mgr, sess, rec, _ := wqSetup(t, true)
	cur := wqFeed(t, mgr, sess, wqRefusedLine)
	evs := rec.got()
	if len(evs) != 1 {
		t.Fatalf("hook called %d times, want 1", len(evs))
	}
	if evs[0].Signal != nil {
		t.Errorf("refused line carried a signal: %+v", evs[0].Signal)
	}
	if evs[0].RefusalReason != workerQuestionReasonTooFewOptions {
		t.Errorf("refusal reason = %q, want %q", evs[0].RefusalReason, workerQuestionReasonTooFewOptions)
	}
	if cur.State != StateRunning {
		t.Errorf("refused worker question changed session state to %q", cur.State)
	}
}

func TestWorkerQuestion_InstructionAndPlainLinesAreIgnored(t *testing.T) {
	mgr, sess, rec, _ := wqSetup(t, true)
	for _, l := range []string{
		"just output",
		"When blocked, print DATAWATCH_QUESTION: {json}",
		"DATAWATCH_QUESTION: <json>",
	} {
		wqFeed(t, mgr, sess, l)
	}
	if n := len(rec.got()); n != 0 {
		t.Errorf("non-question lines called the hook %d times, want 0", n)
	}
}

// Regression: the worker question route must not disturb the needs-input
// marker, which still moves the session to waiting_input with the gate on.
func TestWorkerQuestion_NeedsInputStillWaitsWithGateOn(t *testing.T) {
	mgr, sess, rec, _ := wqSetup(t, true)
	cur := wqFeed(t, mgr, sess, "DATAWATCH_NEEDS_INPUT: which branch should I use?")
	if cur.State != StateWaitingInput {
		t.Errorf("needs-input marker with gate on: state=%q, want %q", cur.State, StateWaitingInput)
	}
	if n := len(rec.got()); n != 0 {
		t.Errorf("needs-input line called the worker question hook %d times, want 0", n)
	}
}
