// An operator's answer must still reach its worker when the worker asks a
// second question first, and across a daemon restart: a task parked on a
// question keeps its live worker, and an adopted worker's new blocking
// question parks its task like a fresh worker's does.

package autonomous

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

// parkAgain raises another blocking question for the fixture's task and parks
// the task on it, as the worker wait loop does.
func (f *deliveryFixture) parkAgain(t *testing.T) string {
	t.Helper()
	q := sampleQuestion("A second and different decision?")
	q.Context, q.PRDID, q.TaskID, q.Blocking = QuestionContextTask, f.prd.ID, f.task(t).ID, true
	id := mustRaise(t, f.store, q)
	cur, _ := f.m.Store().GetPRD(f.prd.ID)
	if !f.m.ParkForDecision(cur, &cur.Story[0].Tasks[0], id) {
		t.Fatal("second park refused")
	}
	return id
}

func (f *deliveryFixture) lines() []string {
	f.smu.Lock()
	defer f.smu.Unlock()
	return append([]string(nil), f.calls...)
}

// reviewAlerts returns the "review needed" alerts that name the question.
func (f *deliveryFixture) reviewAlerts(questionID string) []string {
	var out []string
	for _, a := range f.raised() {
		if strings.HasPrefix(a, "error|") && strings.Contains(a, "review needed") && strings.Contains(a, "question="+questionID+" ") {
			out = append(out, a)
		}
	}
	return out
}

func TestSecondQuestionWhileTheFirstLineIsOpenIsStillDelivered(t *testing.T) {
	for _, withdrawn := range []bool{false, true} {
		name := "answered"
		if withdrawn {
			name = "withdrawn"
		}
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t, errRefusedBeforeTyping)
			f.decide(t)
			f.tick() // refused: the first line stays open
			if got := f.delivery(t); got != "not_typed" {
				t.Fatalf("delivery = %q, want not_typed", got)
			}
			q2 := f.parkAgain(t)
			f.tick()
			f.tick()
			if n := f.sends(); n != 1 {
				t.Fatalf("%d sends while the second question is open, want only the refused one", n)
			}
			if got := f.task(t); got.Status != TaskWaitingCapacity {
				t.Fatalf("task is %s with the second question open, want it parked", got.Status)
			}
			if withdrawn {
				if _, err := f.store.Withdraw(q2, "operator", "not needed"); err != nil {
					t.Fatal(err)
				}
			} else if _, err := f.store.Answer(q2, Answer{ChosenOptionID: "second", Actor: "operator"}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 4; i++ {
				f.tick()
			}
			got := f.task(t)
			if got.Status != TaskInProgress {
				t.Fatalf("task = %s %q, want it resumed for the second question (record %v)", got.Status, got.WaitReason, f.record(t))
			}
			lines := f.lines()
			if len(lines) != 2 || !strings.Contains(lines[1], "question="+q2) {
				t.Fatalf("typed %q, want the second question's line once after the refused first one", lines)
			}
			if r := f.record(t); r["question_id"] != q2 || r["delivery"] != "sent" {
				t.Fatalf("record = %v, want the second question's line sent", r)
			}
			if al := f.reviewAlerts(f.qid); len(al) != 1 {
				t.Fatalf("alerts = %q, want one 'review needed' for the line that will never be typed", f.raised())
			}
		})
	}
}

func TestHeldResumeFlagsTheOpenLineItReplaces(t *testing.T) {
	f := newDeliveryFixture(t, errRefusedBeforeTyping)
	f.decide(t)
	f.tick()
	f.parkAgain(t)
	held := mustRaise(t, f.store, sampleQuestion("Release the parked task?"))
	ans, err := f.store.Answer(held, Answer{ChosenOptionID: "first", Actor: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	q, _ := f.store.Get(held)
	cur, _ := f.m.store.GetPRD(f.prd.ID)
	tk := f.task(t)
	if err := f.m.resumeHeldAwaitingTask(context.Background(), cur, &tk, q, &ans); err != nil {
		t.Fatalf("held resume: %v", err)
	}
	if r := f.record(t); r["question_id"] != held || r["delivery"] != "sent" {
		t.Fatalf("record = %v, want the held line sent", r)
	}
	if al := f.reviewAlerts(f.qid); len(al) != 1 {
		t.Fatalf("alerts = %q, want one 'review needed' for the replaced line", f.raised())
	}
}

// A line that was sent is not "replaced unsent": no alert for it.
func TestSecondQuestionAfterASentLineRaisesNoReview(t *testing.T) {
	f := newDeliveryFixture(t)
	f.decide(t)
	f.tick()
	q2 := f.parkAgain(t)
	if _, err := f.store.Answer(q2, Answer{ChosenOptionID: "second", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	f.tick()
	if n, al := f.sends(), f.raised(); n != 2 || len(al) != 0 {
		t.Fatalf("sends=%d alerts=%q, want two lines and no alert", n, al)
	}
}

// liveWorker wires the boot reset's session checks: the fixture's worker is
// alive (or not) and its agent started (or not). It returns the kill record.
func (f *deliveryFixture) liveWorker(alive, started bool) *fakeSessions {
	fs := newFakeSessions()
	if alive {
		fs.alive["worker-sess"] = true
	}
	fs.wire(f.m)
	f.m.SetSessionAgentStartedFn(func(string) bool { return started })
	return fs
}

// boot runs the real boot reset on the fixture's Automaton. The capacity
// ledger is in memory, so the new daemon starts with none of its leases.
func (f *deliveryFixture) boot(t *testing.T) {
	t.Helper()
	cur, ok := f.m.store.GetPRD(f.prd.ID)
	if !ok {
		t.Fatal("automaton missing")
	}
	f.m.resetInProgressTasksForResume(cur)
}

func TestBootResetKeepsAParkedTaskAndItsLiveWorker(t *testing.T) {
	f := newDeliveryFixture(t)
	fs := f.liveWorker(true, true)
	f.boot(t)
	got := f.task(t)
	if fs.killCount() != 0 {
		t.Fatalf("the live parked worker was ended: %v", fs.killed())
	}
	if got.Status != TaskWaitingCapacity || got.WaitReason != AwaitDecisionWaitReasonFor(f.qid) || got.SessionID != "worker-sess" {
		t.Fatalf("task = %s %q session %q, want it still parked with its worker", got.Status, got.WaitReason, got.SessionID)
	}
	if !got.AdoptedSession {
		t.Fatal("the kept task is not marked adopted, so the executor would replace its worker")
	}
	if f.led.LeaseOf(got.ID) != nil {
		t.Fatal("a parked task holds no lease after a restart either")
	}
	ans := f.decide(t)
	f.tick()
	f.tick()
	if lines := f.lines(); len(lines) != 1 || !strings.Contains(lines[0], "answer="+ans.ID) {
		t.Fatalf("typed %q, want the answer once into the kept worker", lines)
	}
}

func TestWaitingAnswerIsTypedAfterTheRealBootReset(t *testing.T) {
	f := newDeliveryFixture(t)
	fs := f.liveWorker(true, true)
	ans := f.decide(t)
	f.leave(t, ans, "waiting", TaskWaitingCapacity)
	f.boot(t)
	f.tick()
	f.tick()
	f.tick()
	if fs.killCount() != 0 {
		t.Fatalf("the live worker was ended: %v", fs.killed())
	}
	if n := f.sends(); n != 1 {
		t.Fatalf("%d sends after the restart, want the waiting answer once (record %v, task %s)", n, f.record(t), f.task(t).Status)
	}
	if got := f.delivery(t); got != "sent" {
		t.Fatalf("delivery = %q, want sent", got)
	}
}

// A parked task that no later answer could resume is requeued, as before.
func TestBootResetStillRequeuesAParkedTaskNothingCanResume(t *testing.T) {
	cases := map[string]func(t *testing.T, f *deliveryFixture){
		"the worker is gone": func(t *testing.T, f *deliveryFixture) { f.liveWorker(false, false) },
		"the session is a bare shell": func(t *testing.T, f *deliveryFixture) {
			f.liveWorker(true, false)
		},
		"the resume for this question is already recorded": func(t *testing.T, f *deliveryFixture) {
			f.liveWorker(true, true)
			ans := f.decide(t)
			f.leave(t, ans, "", TaskWaitingCapacity)
		},
		"the question expired": func(t *testing.T, f *deliveryFixture) {
			f.liveWorker(true, true)
			if _, err := f.store.Expire(f.qid, "too old"); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t)
			prepare(t, f)
			f.boot(t)
			if got := f.task(t); got.Status != TaskPending || got.SessionID != "" {
				t.Fatalf("task = %s session %q, want it requeued", got.Status, got.SessionID)
			}
		})
	}
}

// A held answer queued for capacity when the daemon stopped is claimed again.
func TestHeldAnswerStillWaitingIsResumedAfterARestart(t *testing.T) {
	f := newDeliveryFixture(t)
	f.liveWorker(true, true)
	held := mustRaise(t, f.store, sampleQuestion("Release the parked task?"))
	ans, err := f.store.Answer(held, Answer{ChosenOptionID: "first", Actor: "operator"})
	if err != nil {
		t.Fatal(err)
	}
	f.leave(t, ans, "waiting", TaskWaitingCapacity)
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) {
		rec := *c.DecisionResume
		rec.QuestionID = held
		c.DecisionResume = &rec
	})
	f.boot(t)
	q, _ := f.store.Get(held)
	cur, _ := f.m.store.GetPRD(f.prd.ID)
	tk := f.task(t)
	if err := f.m.resumeHeldAwaitingTask(context.Background(), cur, &tk, q, &ans); err != nil {
		t.Fatalf("held resume after the restart: %v", err)
	}
	if n, got := f.sends(), f.delivery(t); n != 1 || got != "sent" {
		t.Fatalf("sends=%d delivery=%q, want the held line once", n, got)
	}
}

// restartRig drives the real executor over an Automaton whose one task was
// left with a live worker by the previous daemon.
type restartRig struct {
	m      *Manager
	api    *API
	f      *deliveryFixture
	fs     *fakeSessions
	spawns int
}

func newRestartRig(t *testing.T) *restartRig {
	t.Helper()
	m, api, led := capFixture(t, 1)
	fs := newFakeSessions("worker-sess")
	fs.wire(m)
	m.SetSessionAgentStartedFn(func(string) bool { return true })
	prd := setupBootedAdoptState(t, m, "worker-sess")
	store := awaitDecisionStore(t, m)
	f := &deliveryFixture{tickFixture: &tickFixture{m: m, led: led, store: store, prd: prd}}
	m.SetSessionContinueLineFn(func(sessionID, line string) error {
		f.smu.Lock()
		defer f.smu.Unlock()
		f.calls = append(f.calls, line)
		f.to = append(f.to, sessionID)
		return nil
	})
	m.SetAlertSink(func(level, title, body string) string {
		f.smu.Lock()
		defer f.smu.Unlock()
		f.alerts = append(f.alerts, level+"|"+title+"|"+body)
		return "a"
	})
	return &restartRig{m: m, api: api, f: f, fs: fs}
}

// run boots and runs the Automaton to its end.
func (r *restartRig) run(t *testing.T) *PRD {
	t.Helper()
	r.f.boot(t)
	spawn := func(context.Context, SpawnRequest) (SpawnResult, error) {
		r.spawns++
		return SpawnResult{SessionID: fmt.Sprintf("replacement-%d", r.spawns)}, nil
	}
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "good"}, nil
	}
	return runToTerminal(t, r.m, r.api, r.f.prd.ID, spawn, verify)
}

func TestRestartedExecutorWaitsOnAParkedWorkerAndTheAnswerReachesIt(t *testing.T) {
	r := newRestartRig(t)
	cur, _ := r.m.Store().GetPRD(r.f.prd.ID)
	r.f.qid = raiseTaskQuestion(t, r.f.store, cur.ID, cur.Story[0].Tasks[0].ID, true)
	if !r.m.ParkForDecision(cur, &cur.Story[0].Tasks[0], r.f.qid) {
		t.Fatal("task did not park")
	}
	waited := false
	r.m.SetSessionWaitFn(func(ctx context.Context, w AdoptedWait) error {
		waited = true
		got := r.f.task(t)
		if got.Status != TaskWaitingCapacity || w.SessionID != "worker-sess" {
			t.Errorf("while waiting: task %s on session %s, want it parked on the kept worker", got.Status, w.SessionID)
		}
		if r.f.led.LeaseOf(got.ID) != nil {
			t.Error("the parked task took a capacity slot while waiting for its answer")
		}
		if _, err := r.f.store.Answer(r.f.qid, Answer{ChosenOptionID: "first", Actor: "operator"}); err != nil {
			t.Error(err)
		}
		r.f.tick()
		return ctx.Err()
	})
	got := r.run(t)
	if r.spawns != 0 || r.fs.killCount() != 0 {
		t.Fatalf("spawns=%d kills=%v, want the parked worker kept", r.spawns, r.fs.killed())
	}
	if !waited {
		t.Fatal("the executor never waited on the parked worker")
	}
	if lines := r.f.lines(); len(lines) != 1 || !strings.Contains(lines[0], "question="+r.f.qid) {
		t.Fatalf("typed %q, want the decision line once", lines)
	}
	if got.Status != PRDCompleted {
		t.Fatalf("automaton ended %s, want completed", got.Status)
	}
}

func TestAdoptedWorkersNewBlockingQuestionParksTheTask(t *testing.T) {
	r := newRestartRig(t)
	taskID := r.f.task(t).ID
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true}
	var recorded []time.Time
	var parked, answered bool
	var waitErr error
	clock, polls, completed := scenarioWait, 0, false
	// Long past the nudge window and the stall window after it.
	answerAt := int((nudgeTestWindow+nudgeTestStall)/scenarioTick) + 20
	r.m.SetSessionWaitFn(func(ctx context.Context, aw AdoptedWait) error {
		waitErr = WaitAdopted(ctx, aw, AdoptedWaitPorts{
			NudgeAfter: nudgeTestWindow,
			StallAfter: nudgeTestStall,
			Status: func(string) (session.State, string, bool) {
				return session.StateWaitingInput, WorkerNudgeFamily, true
			},
			Completed: func(string) bool { return completed },
			Nudge: func(sessionID string) WorkerNudgeHooks {
				return scenarioHooks(t, r.m, w, aw.PRDID, aw.TaskID, sessionID, &recorded)
			},
			Now: func() time.Time { return clock },
			Pause: func(ctx context.Context) error {
				polls++
				clock = clock.Add(scenarioTick)
				switch {
				case polls == 1:
					// The worker raises a blocking question after the restart.
					r.f.qid = raiseTaskQuestion(t, r.f.store, aw.PRDID, taskID, true)
				case polls == answerAt:
					got := r.f.task(t)
					parked = got.Status == TaskWaitingCapacity &&
						got.WaitReason == AwaitDecisionWaitReasonFor(r.f.qid) && r.f.led.LeaseOf(taskID) == nil
					if len(w.sent) != 0 {
						t.Errorf("a worker waiting for the operator was nudged: %q", w.sent)
					}
					if _, err := r.f.store.Answer(r.f.qid, Answer{ChosenOptionID: "first", Actor: "operator"}); err != nil {
						return err
					}
					r.f.tick()
					answered = true
				case polls == answerAt+2:
					completed = true
				case polls > answerAt+10:
					return errors.New("test guard: the wait did not end")
				}
				return ctx.Err()
			},
		})
		return waitErr
	})
	got := r.run(t)
	if !parked {
		t.Errorf("the task was not parked on the adopted worker's blocking question (wait ended: %v)", waitErr)
	}
	if lines := r.f.lines(); !answered || len(lines) != 1 || !strings.Contains(lines[0], "question="+r.f.qid) {
		t.Fatalf("answered=%v typed %q, want the decision line once into the adopted worker (wait ended: %v)", answered, lines, waitErr)
	}
	if r.spawns != 0 || got.Status != PRDCompleted {
		t.Fatalf("spawns=%d automaton=%s, want the adopted worker to finish", r.spawns, got.Status)
	}
}

// A worker that asked a question has started, whatever its pane shows now: the
// bound for a worker that never started must not end it.
func TestAdoptedStartBoundDoesNotEndAWorkerThatAskedAQuestion(t *testing.T) {
	f := newDeliveryFixture(t) // parked on an open question
	cur, _ := f.m.store.GetPRD(f.prd.ID)
	clock := &adoptStepClock{at: time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC), step: adoptedTestStep}
	var killed []string
	sc := &adoptedStartWatch{
		paneFn:   func(string) (string, error) { return adoptedTestPaneSilent, nil },
		stateFn:  func(string) string { return "waiting_input" },
		killFn:   func(id, _, _ string) error { killed = append(killed, id); return nil },
		limit:    adoptedTestLimit,
		now:      clock.now,
		interval: time.Millisecond,
	}
	w := AdoptedWait{SessionID: "worker-sess", decision: &adoptedDecision{m: f.m, prd: cur, task: &cur.Story[0].Tasks[0]}}
	// The session runs long past the bound's probes, then completes.
	wait := func(ctx context.Context, _ AdoptedWait) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
			return nil
		}
	}
	if err := waitForAdoptedSession(context.Background(), wait, nil, w, sc); err != nil || len(killed) != 0 {
		t.Fatalf("err=%v killed=%v, want the waiting worker left alone", err, killed)
	}
}

func TestAdoptedQuestionDoesNotReopenAFinishedTask(t *testing.T) {
	f := newDeliveryFixture(t)
	taskID := f.task(t).ID
	f.m.store.MutateTask(taskID, func(c *Task) { c.Status, c.WaitReason = TaskCancelled, "" })
	cur, _ := f.m.store.GetPRD(f.prd.ID)
	d := &adoptedDecision{m: f.m, prd: cur, task: &cur.Story[0].Tasks[0]}
	d.hold()
	if got := f.task(t).Status; got != TaskCancelled {
		t.Fatalf("a cancelled task became %s on its worker's open question", got)
	}
}
