// worker_question_branches_test.go — the refusal, fault and counter branches
// of the worker question raiser and of the coordinator tick's question pass:
// every way a marker line or a parked task is turned away, and the counters
// that make each outcome visible.
package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// wqDelta runs fn and returns how far each worker question counter moved.
func wqDelta(fn func()) WorkerQuestionCounts {
	before := WorkerQuestionCountsSnapshot()
	fn()
	after := WorkerQuestionCountsSnapshot()
	return WorkerQuestionCounts{
		Raised:    after.Raised - before.Raised,
		Duplicate: after.Duplicate - before.Duplicate,
		Refused:   after.Refused - before.Refused,
		Parked:    after.Parked - before.Parked,
		Resumed:   after.Resumed - before.Resumed,
	}
}

func TestWorkerQuestionCountersFollowEachOutcome(t *testing.T) {
	store, _ := wqStore(t)
	f := &wqFakes{}

	d := wqDelta(func() {
		RaiseWorkerQuestion(f.deps(store), wqEvent("DATAWATCH_QUESTION: counted"))
		RaiseWorkerQuestion(f.deps(store), wqEvent("DATAWATCH_QUESTION: counted")) // repeat while open
		RaiseWorkerQuestion(f.deps(store), session.WorkerQuestionEvent{SessionFullID: wqTestSession, RefusalReason: "bad_json"})
	})
	if d != (WorkerQuestionCounts{Raised: 1, Duplicate: 1, Refused: 1}) {
		t.Fatalf("counter movement = %+v, want one raised, one duplicate, one refused", d)
	}
}

func TestWorkerQuestionRecordWorkerQuestionIgnoresUnknownOutcome(t *testing.T) {
	d := wqDelta(func() { recordWorkerQuestion("not-an-outcome") })
	if d != (WorkerQuestionCounts{}) {
		t.Fatalf("an unknown outcome moved the counters: %+v", d)
	}
}

func TestWorkerQuestionEventWithoutSignalIsRefusedWithItsReason(t *testing.T) {
	store, _ := wqStore(t)
	f := &wqFakes{}

	for _, tc := range []struct{ reason, want string }{
		{"too_many_options", "too_many_options"},
		{"", workerQuestionReasonUnknown},
	} {
		f.audits, f.details, f.logs = nil, nil, nil
		ev := session.WorkerQuestionEvent{SessionFullID: wqTestSession, RefusalReason: tc.reason, LineSHA256: wqLineHash("x")}
		if _, raised := RaiseWorkerQuestion(f.deps(store), ev); raised {
			t.Fatalf("reason %q: an event with no signal was raised", tc.reason)
		}
		if len(f.audits) != 1 || f.audits[0] != audit.ActionQuestionMarkerRefused {
			t.Fatalf("reason %q: audits = %v, want one %s", tc.reason, f.audits, audit.ActionQuestionMarkerRefused)
		}
		if got := f.details[0]["reason"]; got != tc.want {
			t.Errorf("reason %q: audited reason = %v, want %q", tc.reason, got, tc.want)
		}
		if got := f.details[0]["line_sha256"]; got != wqLineHash("x") {
			t.Errorf("reason %q: audited hash = %v, want the event hash", tc.reason, got)
		}
	}
	if got := len(store.List(QuestionFilter{})); got != 0 {
		t.Fatalf("%d questions stored from refused events, want 0", got)
	}
}

func TestWorkerQuestionRefusalWithoutAuditSinkStillLogs(t *testing.T) {
	store, _ := wqStore(t)
	f := &wqFakes{}
	d := f.deps(store)
	d.Audit = nil
	RaiseWorkerQuestion(d, session.WorkerQuestionEvent{SessionFullID: wqTestSession, RefusalReason: "bad_json"})
	if len(f.logs) != 1 || !strings.Contains(f.logs[0], "refused") {
		t.Fatalf("logs = %q, want one refusal line", f.logs)
	}
}

func TestWorkerQuestionWithoutStoreIsNotRaised(t *testing.T) {
	f := &wqFakes{}
	d := f.deps(nil)
	if _, raised := RaiseWorkerQuestion(d, wqEvent("DATAWATCH_QUESTION: nostore")); raised {
		t.Fatal("a question was raised with no store")
	}
	if len(f.logs) != 1 || !strings.Contains(f.logs[0], "no question store") {
		t.Fatalf("logs = %q, want the no-store line", f.logs)
	}
	if len(f.audits) != 0 {
		t.Fatalf("audits = %v, want none: a missing store is a fault, not a refusal", f.audits)
	}
}

func TestWorkerQuestionInvalidQuestionIsRefusedNotStored(t *testing.T) {
	store, _ := wqStore(t)
	f := &wqFakes{}
	ev := wqEvent("DATAWATCH_QUESTION: empty text")
	ev.Signal.Question = ""

	d := wqDelta(func() {
		if _, raised := RaiseWorkerQuestion(f.deps(store), ev); raised {
			t.Fatal("a question with no text was raised")
		}
	})
	if d.Refused != 1 || d.Raised != 0 {
		t.Fatalf("counter movement = %+v, want one refusal", d)
	}
	if len(f.details) != 1 || f.details[0]["reason"] != workerQuestionReasonInvalid {
		t.Fatalf("audit details = %v, want reason %q", f.details, workerQuestionReasonInvalid)
	}
}

func TestWorkerQuestionStoreFaultIsLoggedNotRefused(t *testing.T) {
	dir := t.TempDir()
	store, err := NewQuestionStore(filepath.Join(dir, "sub", "questions.jsonl"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	// The store's directory goes away under it: the append fails.
	if err := os.RemoveAll(filepath.Join(dir, "sub")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := &wqFakes{}

	d := wqDelta(func() {
		if _, raised := RaiseWorkerQuestion(f.deps(store), wqEvent("DATAWATCH_QUESTION: fault")); raised {
			t.Fatal("a question was raised though the store could not write")
		}
	})
	if d.Raised != 0 || d.Refused != 0 {
		t.Fatalf("counter movement = %+v, want none for a store fault", d)
	}
	if len(f.audits) != 0 {
		t.Fatalf("audits = %v, want none for a store fault", f.audits)
	}
	if len(f.logs) != 1 || !strings.Contains(f.logs[0], "not stored") {
		t.Fatalf("logs = %q, want one not-stored line", f.logs)
	}
	if len(f.notified) != 0 {
		t.Fatalf("%d notifications for a question that was not stored", len(f.notified))
	}
}

func TestWorkerQuestionRaiseWithoutNotifierOrLogger(t *testing.T) {
	store, _ := wqStore(t)
	d := WorkerQuestionDeps{Store: store}
	if _, raised := RaiseWorkerQuestion(d, wqEvent("DATAWATCH_QUESTION: quiet")); !raised {
		t.Fatal("a question with no notifier was not raised")
	}
	// A refusal with no sinks at all must not panic.
	RaiseWorkerQuestion(d, session.WorkerQuestionEvent{SessionFullID: wqTestSession, RefusalReason: "bad_json"})
}

func TestWorkerLineHashPrefersTheEventThenTheSignal(t *testing.T) {
	sig := &session.WorkerQuestionSignal{LineSHA256: "from-signal"}
	for _, tc := range []struct {
		name string
		ev   session.WorkerQuestionEvent
		want string
	}{
		{"event hash wins", session.WorkerQuestionEvent{LineSHA256: "from-event", Signal: sig}, "from-event"},
		{"signal hash when the event has none", session.WorkerQuestionEvent{Signal: sig}, "from-signal"},
		{"empty when neither names one", session.WorkerQuestionEvent{}, ""},
	} {
		if got := workerLineHash(tc.ev); got != tc.want {
			t.Errorf("%s: workerLineHash = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestWorkerQuestionCountsSnapshotMatchesTheCounters(t *testing.T) {
	d := wqDelta(func() {
		for _, o := range []string{
			WorkerQuestionOutcomeRaised, WorkerQuestionOutcomeDuplicate, WorkerQuestionOutcomeRefused,
			WorkerQuestionOutcomeParked, WorkerQuestionOutcomeResumed, WorkerQuestionOutcomeResumed,
		} {
			recordWorkerQuestion(o)
		}
	})
	if d != (WorkerQuestionCounts{Raised: 1, Duplicate: 1, Refused: 1, Parked: 1, Resumed: 2}) {
		t.Fatalf("snapshot movement = %+v", d)
	}
}

func TestParkForDecisionCountsTheParkOnce(t *testing.T) {
	f := newTickFixtureUnparked(t)
	d := wqDelta(func() {
		f.m.ParkForDecision(f.prdCur, f.task0, f.qid)
		f.m.ParkForDecision(f.prdCur, f.task0, f.qid) // idempotent: no second count
	})
	if d.Parked != 1 {
		t.Fatalf("parked counter moved by %d, want 1", d.Parked)
	}
}

// unparkedFixture is a running task with a blocking question, not yet parked.
type unparkedFixture struct {
	m      *Manager
	prdCur *PRD
	task0  *Task
	qid    string
}

func newTickFixtureUnparked(t *testing.T) *unparkedFixture {
	t.Helper()
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	store := awaitDecisionStore(t, m)
	cur, tk := awaitDecisionTask(t, m, led, prd)
	return &unparkedFixture{m: m, prdCur: cur, task0: tk, qid: raiseTaskQuestion(t, store, prd.ID, tk.ID, true)}
}

func TestQuestionPassIsNilSafeAndRunsTheTick(t *testing.T) {
	var nilCoord *Coordinator
	nilCoord.questionPass(context.Background())
	(&Coordinator{}).questionPass(context.Background())

	f := newTickFixture(t)
	f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})
	c := &Coordinator{m: f.m}
	d := wqDelta(func() {
		c.questionPass(context.Background())
		f.m.waitQuestionResumes()
	})
	if n := len(f.sent()); n != 1 {
		t.Fatalf("%d prompts after the coordinator's question pass, want 1", n)
	}
	if d.Resumed != 1 {
		t.Fatalf("resumed counter moved by %d, want 1", d.Resumed)
	}
}

func TestQuestionTickTaskSkipsWhatItMustNotResume(t *testing.T) {
	f := newTickFixture(t)
	ans := f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})
	q, _ := f.store.Get(f.qid)
	answered := map[string]Question{f.qid: q}
	parked := f.task(t)

	ctx := context.Background()
	for name, tk := range map[string]Task{
		"terminal task": func() Task { c := parked; c.Status = TaskCompleted; return c }(),
		"bare reason":   func() Task { c := parked; c.WaitReason = AwaitDecisionWaitReason; return c }(),
		"not waiting":   func() Task { c := parked; c.Status = TaskInProgress; return c }(),
		"already resumed for it": func() Task {
			c := parked
			c.DecisionResume = &DecisionResume{QuestionID: f.qid, AnswerID: ans.ID, CurrentAnswerID: ans.ID}
			return c
		}(),
	} {
		f.m.questionTickTask(ctx, f.store, tk, answered, nil)
		f.m.waitQuestionResumes()
		if n := len(f.sent()); n != 0 {
			t.Fatalf("%s: %d prompts sent, want 0", name, n)
		}
	}

	// An answered question the store holds no answer for resumes nothing.
	open := raiseTaskQuestion(t, f.store, f.prd.ID, parked.ID, true)
	oq, _ := f.store.Get(open)
	c := parked
	c.WaitReason = AwaitDecisionWaitReasonFor(open)
	f.m.questionTickTask(ctx, f.store, c, map[string]Question{open: oq}, nil)
	f.m.waitQuestionResumes()
	if n := len(f.sent()); n != 0 {
		t.Fatalf("%d prompts for a question with no answer, want 0", n)
	}
}

func TestQuestionTickRecordCurrentAnswerIgnoresAnotherQuestion(t *testing.T) {
	f := newTickFixture(t)
	f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})
	f.tick()
	before := f.task(t).DecisionResume
	if before == nil {
		t.Fatal("task was not resumed")
	}
	f.m.recordCurrentAnswer(f.task(t).ID, "another-question", "a-9")
	if after := f.task(t).DecisionResume; after.CurrentAnswerID != before.CurrentAnswerID {
		t.Fatalf("CurrentAnswerID = %q, want it unchanged at %q", after.CurrentAnswerID, before.CurrentAnswerID)
	}
	// A task with no resume record at all is left alone too.
	f.m.recordCurrentAnswer("no-such-task", f.qid, "a-9")
}

func TestQuestionResumeGuards(t *testing.T) {
	f := newTickFixture(t)
	ans := f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})
	q, _ := f.store.Get(f.qid)
	snap := f.task(t)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	noPRD := snap
	noPRD.PRDID = "no-such-prd"
	noTask := snap
	noTask.ID = "no-such-task"
	other := q
	other.ID = "some-other-question"

	f.m.resumeForQuestion(cancelled, snap, q, &ans)
	f.m.resumeForQuestion(context.Background(), noPRD, q, &ans)
	f.m.resumeForQuestion(context.Background(), noTask, q, &ans)
	f.m.resumeForQuestion(context.Background(), snap, other, &ans) // the task is parked on a different question

	got := f.task(t)
	if got.DecisionResume != nil || got.Status != TaskWaitingCapacity {
		t.Fatalf("task = %s resume=%+v after refused resumes, want it still parked and unrecorded", got.Status, got.DecisionResume)
	}
	if n := len(f.sent()); n != 0 {
		t.Fatalf("%d prompts from refused resumes, want 0", n)
	}

	// Once recorded for the question, a second resume for it does nothing.
	rec := &DecisionResume{QuestionID: f.qid, AnswerID: ans.ID, CurrentAnswerID: ans.ID, ResumedAt: time.Now().UTC()}
	f.m.store.MutateTask(snap.ID, func(c *Task) { c.DecisionResume = rec })
	f.m.resumeForQuestion(context.Background(), f.task(t), q, &ans)
	if got := f.task(t).DecisionResume; got == nil || !got.ResumedAt.Equal(rec.ResumedAt) || len(f.sent()) != 0 {
		t.Fatalf("a task already resumed for the question was resumed again: %+v, %d prompts", got, len(f.sent()))
	}
}

func TestQuestionResumeAuditsAndSurvivesAFailedPrompt(t *testing.T) {
	f := newTickFixture(t)
	a := &testAudit{}
	f.m.SetAuditFn(a.fn)
	prompts := 0
	f.m.SetSessionContinueLineFn(func(string, string) error {
		prompts++
		return os.ErrClosed
	})
	ans := f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})

	d := wqDelta(f.tick)
	if prompts != 1 {
		t.Fatalf("%d prompts, want 1", prompts)
	}
	if d.Resumed != 1 {
		t.Fatalf("resumed counter moved by %d, want 1: a failed prompt is logged, the task still resumed", d.Resumed)
	}
	if got := f.task(t); got.Status != TaskInProgress {
		t.Fatalf("status = %s, want in_progress", got.Status)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.events) != 1 || a.events[0].action != "autonomous_decision_resume" {
		t.Fatalf("audit events = %+v, want one autonomous_decision_resume", a.events)
	}
	det := a.events[0].details
	if det["question_id"] != f.qid || det["answer_id"] != ans.ID || det["withdrawn"] != false {
		t.Fatalf("audit details = %v", det)
	}
}

func TestQuestionResumeCancelledWhileQueuedKeepsItsCancellation(t *testing.T) {
	f := newTickFixture(t)
	// Another holder fills the one slot, so the resume waits for capacity.
	fillSlot(t, f)
	f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})

	ctx, cancel := context.WithCancel(context.Background())
	f.m.QuestionTick(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for f.task(t).DecisionResume == nil {
		if time.Now().After(deadline) {
			t.Fatal("the resume never recorded itself")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.Status = TaskCancelled })
	cancel()
	f.m.waitQuestionResumes()

	if got := f.task(t); got.Status != TaskCancelled {
		t.Fatalf("status = %s, want cancelled to stand", got.Status)
	}
	if n := len(f.sent()); n != 0 {
		t.Fatalf("%d prompts to a cancelled task, want 0", n)
	}
}

func TestQuestionResumePanicIsContainedAndFreesTheSlot(t *testing.T) {
	f := newTickFixture(t)
	f.m.SetSessionContinueLineFn(func(string, string) error { panic("boom") })
	f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})

	f.tick() // must not crash the test binary
	if !f.m.questionTick.begin("probe") {
		t.Fatal("the in-flight guard was not released after a panic")
	}
	f.m.questionTick.end("probe")
}

// fillSlot has another holder take the fixture's one capacity slot.
func fillSlot(t *testing.T, f *tickFixture) {
	t.Helper()
	if err := f.led.Acquire(context.Background(), capacity.Request{Holder: "other", PRDID: "other", Pools: []string{"node:n1"}, Node: "n1"}, time.Second, nil, nil); err != nil {
		t.Fatalf("other holder acquire: %v", err)
	}
}
