// Where decision delivery meets capacity slots: a line that waits to be typed,
// a parked task and a task taken over after a restart each take, keep and give
// back one slot, and nothing is typed outside those rules. Fake sessions, temp
// dirs, no daemon.
package autonomous

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// slotsOf counts the leases the task holds, as worker or as verifier.
func slotsOf(led *capacity.Ledger, taskID string) int {
	n := 0
	for _, ls := range led.Leases() {
		if ls.Holder == taskID || ls.Holder == VerifyHolder(taskID) {
			n++
		}
	}
	return n
}

// queuedAnswer is a parked task whose answer waits for the only slot, which
// another holder has.
func queuedAnswer(t *testing.T) (*deliveryFixture, *gapSessions, string) {
	t.Helper()
	f := newDeliveryFixture(t)
	s := newGapSessions(f.m)
	gapTake(t, f.led, "other-hold")
	f.decide(t)
	f.m.QuestionTick(context.Background())
	id := f.task(t).ID
	pollUntil(t, 3*time.Second, "resume queued", func() bool { return gapQueued(f.led, id) })
	return f, s, id
}

// wantListedNotTyped checks the end of an answer whose worker can no longer
// take it: nothing typed, listed for review once, no slot, no later try.
func wantListedNotTyped(t *testing.T, f *deliveryFixture, id string) {
	t.Helper()
	for i := 0; i < 4; i++ {
		f.tick()
	}
	if n := f.sends(); n != 0 {
		t.Errorf("typed %d line(s) into a worker that can no longer take it: %q", n, f.lines())
	}
	if got := f.delivery(t); got != "review_needed" {
		t.Errorf("delivery = %q, want review_needed", got)
	}
	if al := f.reviewAlerts(f.qid); len(al) != 1 {
		t.Errorf("review alerts = %q, want one", f.raised())
	}
	if n := slotsOf(f.led, id); n != 0 {
		t.Errorf("the task holds %d slot(s), want none", n)
	}
	if gapQueued(f.led, id) {
		t.Error("the resume is queued again")
	}
}

func TestCapacityCross_AnswerWaitingForASlotWhenItsSessionEndsIsListedNotTyped(t *testing.T) {
	f, s, id := queuedAnswer(t)
	s.end("worker-sess")
	pollUntil(t, 3*time.Second, "the resume leaves the queue", func() bool { return !gapQueued(f.led, id) })
	gapResumesDone(t, f.m, "session ended")
	f.led.Release("other-hold")
	wantListedNotTyped(t, f, id)
}

// The held-question path resumes through its own function.
func TestCapacityCross_HeldAnswerWhoseSessionEndsWhileQueuedIsListedNotTyped(t *testing.T) {
	f := newDeliveryFixture(t)
	s := newGapSessions(f.m)
	gapTake(t, f.led, "other-hold")
	ans := f.decide(t)
	q, ok := f.store.Get(f.qid)
	if !ok {
		t.Fatal("question missing")
	}
	prd, task := f.live(t)
	done := make(chan error, 1)
	go func() { done <- f.m.resumeHeldAwaitingTask(context.Background(), prd, task, q, &ans) }()
	pollUntil(t, 3*time.Second, "resume queued", func() bool { return gapQueued(f.led, task.ID) })
	s.end("worker-sess")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("resumeHeldAwaitingTask: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the held resume is still waiting")
	}
	f.led.Release("other-hold")
	wantListedNotTyped(t, f, task.ID)
}

// The worker's turn can end while its answer still waits: its verification
// takes over. The task then holds one slot, the verifier's, never a second one
// for a worker line.
func TestCapacityCross_QueuedAnswerGivesWayToTheTasksVerification(t *testing.T) {
	t.Run("the verifier asks for its slot", func(t *testing.T) {
		f, _, id := queuedAnswer(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		got := make(chan error, 1)
		go func() {
			got <- f.led.Acquire(ctx, capacity.Request{Holder: VerifyHolder(id), PRDID: f.prd.ID, Pools: []string{"node:n1"}, Node: "n1"}, 3*time.Second, nil, nil)
		}()
		pollUntil(t, 3*time.Second, "verifier queued", func() bool { return gapQueued(f.led, VerifyHolder(id)) })
		f.led.Release("other-hold")
		select {
		case err := <-got:
			if err != nil {
				t.Fatalf("the verifier waits behind its own task's answer: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the verifier got no slot")
		}
		gapResumesDone(t, f.m, "verification took over")
		if n := slotsOf(f.led, id); n != 1 || f.led.Held(id) {
			t.Errorf("the task holds %d slot(s), worker slot held: %v; want the verifier's alone", n, f.led.Held(id))
		}
		f.led.Release(VerifyHolder(id))
		wantListedNotTyped(t, f, id)
	})
	t.Run("the task goes to verification", func(t *testing.T) {
		f, _, id := queuedAnswer(t)
		f.m.store.MutateTask(id, func(c *Task) { c.Status, c.WaitReason = TaskVerifying, "" })
		pollUntil(t, 3*time.Second, "the resume leaves the queue", func() bool { return !gapQueued(f.led, id) })
		gapResumesDone(t, f.m, "verification started")
		f.led.Release("other-hold")
		if got := f.task(t).Status; got != TaskVerifying {
			t.Errorf("status = %s, want verifying left as it is", got)
		}
		wantListedNotTyped(t, f, id)
	})
}

// A line refused before typing is tried again. All that time the running worker
// holds one slot, bound to its session.
func TestCapacityCross_LineWaitingToBeTypedKeepsOneBoundSlot(t *testing.T) {
	f := newDeliveryFixture(t, errRefusedBeforeTyping, errRefusedBeforeTyping)
	newGapSessions(f.m)
	id := f.task(t).ID
	if n := slotsOf(f.led, id); n != 0 {
		t.Fatalf("a parked task holds %d slot(s)", n)
	}
	f.decide(t)
	for i := 0; i < 3; i++ {
		f.tick()
		ls := f.led.LeaseOf(id)
		if n := slotsOf(f.led, id); n != 1 || ls == nil || ls.SessionID != "worker-sess" {
			t.Fatalf("tick %d: %d slot(s), lease %+v; want one bound to worker-sess", i+1, n, ls)
		}
	}
	if got := f.delivery(t); got != "sent" || f.sends() != 3 {
		t.Fatalf("delivery=%q sends=%d, want sent on the third try", got, f.sends())
	}
}

// A worker that is running is not parked on anything. Typing a decision it is
// still owed must not hand its slot to someone else first.
func TestCapacityCross_OwedDecisionOfARunningWorkerKeepsItsSlot(t *testing.T) {
	f, q2 := runningWithOwedQuestion(t)
	newGapSessions(f.m)
	id := f.task(t).ID
	if !f.led.Held(id) {
		t.Fatal("the resumed worker holds no slot")
	}
	f.answerID(t, q2) // decided before any wait loop parked the task on it
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var otherGot int32
	otherDone := make(chan struct{})
	go func() {
		defer close(otherDone)
		if f.led.Acquire(ctx, capacity.Request{Holder: "other", PRDID: "other", Pools: []string{"node:n1"}, Node: "n1"}, 0, nil, nil) == nil {
			atomic.StoreInt32(&otherGot, 1)
		}
	}()
	pollUntil(t, 3*time.Second, "other work queued", func() bool { return gapQueued(f.led, "other") })
	f.m.QuestionTick(context.Background())
	pollUntil(t, 3*time.Second, "second line typed or resume queued", func() bool {
		return len(f.typedFor(q2)) == 1 || gapQueued(f.led, id)
	})
	if atomic.LoadInt32(&otherGot) != 0 || !f.led.Held(id) {
		t.Fatalf("the running worker's slot went to other work (lines for the owed question: %d)", len(f.typedFor(q2)))
	}
	gapResumesDone(t, f.m, "owed decision")
	if n := len(f.typedFor(q2)); n != 1 || slotsOf(f.led, id) != 1 {
		t.Fatalf("owed lines=%d slots=%d, want one of each", n, slotsOf(f.led, id))
	}
	cancel()
	<-otherDone
}

// A task parked on a question is taken over after a restart: no slot while it
// waits, nothing typed while the restart still counts running workers, then one
// slot and one line.
func TestCapacityCross_ParkedTaskTakenOverAfterRestartTakesOneSlotThenTypes(t *testing.T) {
	f := newDeliveryFixture(t)
	newGapSessions(f.m)
	id := f.task(t).ID
	ans := f.decide(t)
	f.leave(t, ans, "waiting", TaskWaitingCapacity) // the daemon stopped while it was queued
	f.led.BeginAdoption()
	prd, _ := f.live(t)
	f.m.resetInProgressTasksForResume(prd)
	if n := slotsOf(f.led, id); n != 0 {
		t.Fatalf("a task kept parked holds %d slot(s)", n)
	}
	f.m.QuestionTick(context.Background())
	pollUntil(t, 3*time.Second, "resume queued", func() bool { return gapQueued(f.led, id) })
	if !f.m.DecisionResumeWaiting(id) {
		t.Error("a decided task whose line waits for a slot must still count as waiting")
	}
	if n := f.sends(); n != 0 {
		t.Fatalf("typed %d line(s) before the slot", n)
	}
	f.led.MarkAdoptionComplete()
	gapResumesDone(t, f.m, "restart over")
	f.tick()
	ls := f.led.LeaseOf(id)
	if f.sends() != 1 || slotsOf(f.led, id) != 1 || ls == nil || ls.SessionID != "worker-sess" {
		t.Fatalf("sends=%d lease=%+v, want one line and one bound slot", f.sends(), ls)
	}
	if f.m.DecisionResumeWaiting(id) {
		t.Error("a delivered decision must not count as waiting")
	}
}

// The daemon stopped after the resume got its slot and before the line went
// out. After the restart the worker is counted first; only then is it typed.
func TestCapacityCross_NothingIsTypedWhileRestartAdoptionRuns(t *testing.T) {
	f := newDeliveryFixture(t)
	newGapSessions(f.m)
	id := f.task(t).ID
	ans := f.decide(t)
	f.leave(t, ans, "waiting", TaskInProgress)
	f.led.BeginAdoption()
	f.tick()
	if n := f.sends(); n != 0 || slotsOf(f.led, id) != 0 {
		t.Fatalf("typed %d line(s) with %d slot(s) before the restart counted the worker", n, slotsOf(f.led, id))
	}
	prd, _ := f.live(t)
	f.m.resetInProgressTasksForResume(prd)
	f.led.MarkAdoptionComplete()
	f.tick()
	f.tick()
	if f.sends() != 1 || slotsOf(f.led, id) != 1 {
		t.Fatalf("sends=%d slots=%d, want one line and one slot", f.sends(), slotsOf(f.led, id))
	}
}

// While a resume waits for its slot, or for the tree, the worker is still
// parked: the wait loops must neither nudge nor stall it.
func TestCapacityCross_AdoptedWaitHoldsWhileTheAnswerWaitsForASlot(t *testing.T) {
	f, _, id := queuedAnswer(t)
	prd, task := f.live(t)
	d := &adoptedDecision{m: f.m, prd: prd, task: task, handled: f.qid}
	if !f.m.DecisionResumeWaiting(id) || !d.hold() {
		t.Fatal("the queued answer is not held")
	}
	// The wait reason changes while the resume waits for the tree.
	f.m.store.MutateTask(id, func(c *Task) { c.WaitReason = "tree lock: held by another writer" })
	if !f.m.DecisionResumeWaiting(id) || !d.hold() {
		t.Fatal("the answer waiting for the tree is not held")
	}
	f.led.Release("other-hold")
	gapResumesDone(t, f.m, "slot free")
	if f.m.DecisionResumeWaiting(id) || d.hold() {
		t.Fatal("a delivered answer is still held")
	}
}

// A new attempt is admitted while the ended session's resume is still queued.
// The old resume must be gone first, or it can give back the new attempt's slot.
func TestCapacityCross_NewAttemptKeepsItsSlotWhenAnOldResumeWasQueued(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: time.Hour})
	led.SetLimit("node:n1", 1)
	m.SetCapacity(led, func(string, string) ([]string, string) { return []string{"node:n1"}, "n1" })
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	tk := &cur.Story[0].Tasks[0]
	tk.Status, tk.SessionID = TaskInProgress, "worker-sess"
	_ = m.Store().SaveTask(tk)
	s := newGapSessions(m)
	gapTake(t, led, "other-hold")
	resumed := make(chan error, 1)
	go func() { resumed <- m.AwaitExternal(context.Background(), cur, tk, "CI run #1") }()
	pollUntil(t, 3*time.Second, "old resume queued", func() bool { return gapQueued(led, tk.ID) })
	s.end("worker-sess")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admitted := make(chan error, 1)
	go func() { admitted <- m.admit(ctx, cur, tk, "opencode", "", capacity.RoleWorker) }()
	// Wait until the new attempt is in the queue and the old resume has either
	// left it or still waits beside the new attempt.
	queuedForTask := func() (n int) {
		for _, w := range led.Snapshot().Waiting {
			if w.Holder == tk.ID {
				n++
			}
		}
		return n
	}
	var oldGone bool
	pollUntil(t, 3*time.Second, "new attempt queued", func() bool {
		select {
		case <-resumed:
			oldGone = true
		default:
		}
		return (oldGone && queuedForTask() == 1) || queuedForTask() == 2
	})
	led.SetLimit("node:n1", 3) // room for every waiter in one pass
	select {
	case err := <-admitted:
		if err != nil {
			t.Fatalf("admit: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the new attempt was not admitted")
	}
	if !oldGone {
		select {
		case <-resumed:
		case <-time.After(3 * time.Second):
			t.Fatal("the old resume is still waiting")
		}
	}
	if !led.Held(tk.ID) {
		t.Fatal("the new attempt runs with no slot")
	}
}

// One lane, one slot: a task parked on a question across the restart had a
// lane, so it gets one back before a task that never started. Otherwise its
// answered worker takes the slot with no lane to see it finish.
func TestCapacityCross_TaskParkedOnAQuestionKeepsItsLaneAfterRestart(t *testing.T) {
	m, api, led := capFixture(t, 1)
	m.mu.Lock()
	m.cfg.CapacityWaitTimeoutSeconds = 3
	m.mu.Unlock()
	old := capRetryPause
	capRetryPause = 10 * time.Millisecond
	t.Cleanup(func() { capRetryPause = old })
	prd := newPRDWithTasks(t, m, 2)
	store := awaitDecisionStore(t, m)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status, cur.ProjectDir, cur.MaxConcurrentTasks = PRDRunning, t.TempDir(), 1
	_ = m.Store().SavePRD(cur)
	ids, err := topoSort(flattenTasks(cur))
	if err != nil {
		t.Fatal(err)
	}
	parked := lookupTask(cur, ids[1]) // behind the new task in the run order
	qid := raiseTaskQuestion(t, store, prd.ID, parked.ID, true)
	parked.Status, parked.SessionID = TaskWaitingCapacity, "parked-sess"
	parked.WaitReason = AwaitDecisionWaitReasonFor(qid)
	_ = m.Store().SaveTask(parked)
	newGapSessions(m)
	typed := make(chan struct{})
	var once sync.Once
	var early int32
	m.SetSessionContinueLineFn(func(string, string) error {
		once.Do(func() { close(typed) })
		return nil
	})
	m.SetSessionWaitFn(func(ctx context.Context, w AdoptedWait) error {
		select {
		case <-typed:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if _, err := store.Answer(qid, Answer{ChosenOptionID: "first", Actor: "operator"}); err != nil {
		t.Fatal(err)
	}
	led.BeginAdoption()
	api.SetExecutors(func(context.Context, SpawnRequest) (SpawnResult, error) {
		select {
		case <-typed:
		default:
			atomic.AddInt32(&early, 1)
		}
		return SpawnResult{SessionID: "new-1"}, nil
	}, func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	ticking := make(chan struct{})
	go func() { // the coordinator tick, starting a little after the run
		defer close(ticking)
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(100 * time.Millisecond):
				m.QuestionTick(ctx)
			}
		}
	}()
	pollPRDTerminal(t, m, cur.ID, 15*time.Second)
	cancel()
	<-ticking
	m.waitQuestionResumes()
	if err := api.WaitIdle(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	if n := atomic.LoadInt32(&early); n != 0 {
		t.Errorf("%d new session(s) started ahead of the task parked on its question", n)
	}
	snap, _ := m.Store().SnapshotPRD(cur.ID)
	for _, tk := range snap.Story[0].Tasks {
		if tk.Status != TaskCompleted {
			t.Errorf("task %s = %s (%s), want completed", tk.ID, tk.Status, tk.Error)
		}
	}
	if n := len(led.Leases()); n != 0 {
		t.Errorf("%d lease(s) left", n)
	}
}
