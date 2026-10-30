// The coordinator tick's question pass: an open question leaves the task
// parked, a decided one resumes it exactly once, the pass never reads session
// output, and the in-flight guard keeps the ticker from waiting or doubling.
package autonomous

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// tickFixture is a task parked on a blocking question, with the prompt lines
// sent to its session recorded.
type tickFixture struct {
	m     *Manager
	led   *capacity.Ledger
	store *QuestionStore
	prd   *PRD
	qid   string
	mu    sync.Mutex
	lines []string
}

func (f *tickFixture) sent() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

func newTickFixture(t *testing.T) *tickFixture {
	t.Helper()
	m, _, led := capFixture(t, 1)
	prd := newPRDWithTasks(t, m, 1)
	store := awaitDecisionStore(t, m)
	cur, tk := awaitDecisionTask(t, m, led, prd)
	f := &tickFixture{m: m, led: led, store: store, prd: prd}
	m.SetSessionContinueLineFn(func(sessionID, line string) error {
		if sessionID != "worker-sess" {
			t.Errorf("prompted session %q, want worker-sess", sessionID)
		}
		f.mu.Lock()
		f.lines = append(f.lines, line)
		f.mu.Unlock()
		return nil
	})
	f.qid = raiseTaskQuestion(t, store, prd.ID, tk.ID, true)
	if !m.ParkForDecision(cur, tk, f.qid) {
		t.Fatal("task did not park")
	}
	return f
}

// tick runs one pass and waits for the resumes it started.
func (f *tickFixture) tick() {
	f.m.QuestionTick(context.Background())
	f.m.waitQuestionResumes()
}

func (f *tickFixture) task(t *testing.T) Task {
	t.Helper()
	return testTaskAt(t, f.m, f.prd.ID, 0)
}

func (f *tickFixture) answer(t *testing.T, a Answer) Answer {
	t.Helper()
	got, err := f.store.Answer(f.qid, a)
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	return got
}

func TestQuestionTick_OpenQuestionLeavesTaskParked(t *testing.T) {
	f := newTickFixture(t)
	f.tick()
	f.tick()

	got := f.task(t)
	if got.Status != TaskWaitingCapacity || got.WaitReason != AwaitDecisionWaitReasonFor(f.qid) {
		t.Fatalf("task = %s %q, want it still parked on %s", got.Status, got.WaitReason, f.qid)
	}
	if got.DecisionResume != nil {
		t.Fatalf("DecisionResume = %+v, want nil for an open question", got.DecisionResume)
	}
	if n := len(f.sent()); n != 0 {
		t.Fatalf("%d prompts sent for an open question, want 0", n)
	}
	if f.led.LeaseOf(got.ID) != nil {
		t.Fatal("a parked task must not hold a lease")
	}
}

func TestQuestionTick_AnsweredResumesExactlyOnceAcrossTwoTicks(t *testing.T) {
	f := newTickFixture(t)
	ans := f.answer(t, Answer{ChosenOptionID: "second", Quote: "take the second,\nthen \"ship\" it", Actor: "operator", Surface: "rest"})

	f.tick()
	f.tick()

	lines := f.sent()
	if len(lines) != 1 {
		t.Fatalf("%d prompts after two ticks, want exactly 1: %q", len(lines), lines)
	}
	line := lines[0]
	if strings.ContainsAny(line, "\r\n") {
		t.Fatalf("prompt is not one line: %q", line)
	}
	for _, want := range []string{
		"question=" + f.qid,
		`option="second"`,
		`label="Do the second option"`,
		`quote="take the second,\nthen \"ship\" it"`,
		"answer=" + ans.ID,
		`actor="operator"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("prompt %q lacks %q", line, want)
		}
	}

	got := f.task(t)
	if got.Status != TaskInProgress || got.WaitReason != "" {
		t.Fatalf("task = %s %q, want in_progress with no wait reason", got.Status, got.WaitReason)
	}
	if got.RetryCount != 2 {
		t.Fatalf("RetryCount = %d, want 2 (a resume counts no retry)", got.RetryCount)
	}
	if dr := got.DecisionResume; dr == nil || dr.QuestionID != f.qid || dr.AnswerID != ans.ID || dr.Withdrawn {
		t.Fatalf("DecisionResume = %+v, want it recorded for answer %s", dr, ans.ID)
	}
	if f.led.LeaseOf(got.ID) == nil {
		t.Fatal("the resumed task must hold a lease again")
	}
}

func TestQuestionTick_WithdrawnResumesWithTheWithdrawnNote(t *testing.T) {
	f := newTickFixture(t)
	if _, err := f.store.Withdraw(f.qid, "operator", "no longer needed"); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	f.tick()
	f.tick()

	lines := f.sent()
	if len(lines) != 1 {
		t.Fatalf("%d prompts, want 1: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "question="+f.qid+" was withdrawn") || !strings.Contains(lines[0], "no default was applied") {
		t.Fatalf("prompt %q does not say the question was withdrawn with no default applied", lines[0])
	}
	if strings.ContainsAny(lines[0], "\r\n") {
		t.Fatalf("prompt is not one line: %q", lines[0])
	}
	got := f.task(t)
	if got.Status != TaskInProgress {
		t.Fatalf("status = %s, want in_progress", got.Status)
	}
	if dr := got.DecisionResume; dr == nil || !dr.Withdrawn || dr.AnswerID != "" {
		t.Fatalf("DecisionResume = %+v, want a withdrawn record", dr)
	}
}

func TestQuestionTick_NeverReadsSessionOutput(t *testing.T) {
	f := newTickFixture(t)
	f.m.SetSessionOutputFn(func(sessionID string) (string, bool) {
		t.Errorf("the question pass read the output of session %s", sessionID)
		return "", false
	})
	f.tick()
	f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})
	f.tick()
	f.tick()
	if n := len(f.sent()); n != 1 {
		t.Fatalf("%d prompts, want 1", n)
	}
}

func TestQuestionTick_SupersedingAnswerDoesNotResumeTwice(t *testing.T) {
	f := newTickFixture(t)
	first := f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})
	f.tick()

	second := f.answer(t, Answer{ChosenOptionID: "second", Actor: "operator", Supersedes: first.ID})
	f.tick()
	f.tick()

	if n := len(f.sent()); n != 1 {
		t.Fatalf("%d prompts after a superseding answer, want 1", n)
	}
	dr := f.task(t).DecisionResume
	if dr == nil || dr.AnswerID != first.ID || dr.CurrentAnswerID != second.ID {
		t.Fatalf("DecisionResume = %+v, want resumed for %s and showing %s as the current answer", dr, first.ID, second.ID)
	}
}

func TestQuestionTick_TwoAnswersBeforeATickResumeOnceWithTheNewest(t *testing.T) {
	f := newTickFixture(t)
	first := f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})
	second := f.answer(t, Answer{ChosenOptionID: "second", Actor: "operator", Supersedes: first.ID})

	f.tick()
	f.tick()

	lines := f.sent()
	if len(lines) != 1 || !strings.Contains(lines[0], "answer="+second.ID) {
		t.Fatalf("prompts = %q, want one carrying answer %s", lines, second.ID)
	}
}

func TestQuestionTick_InFlightGuardHoldsAndTickerDoesNotWait(t *testing.T) {
	f := newTickFixture(t)
	// Another holder fills the one slot, so the resume cannot be re-admitted.
	if err := f.led.Acquire(context.Background(), capacity.Request{Holder: "other", PRDID: "other", Pools: []string{"node:n1"}, Node: "n1"}, time.Second, nil, nil); err != nil {
		t.Fatalf("other holder acquire: %v", err)
	}
	f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})

	done := make(chan struct{})
	go func() {
		f.m.QuestionTick(context.Background())
		f.m.QuestionTick(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the tick waited on capacity")
	}
	if n := len(f.sent()); n != 0 {
		t.Fatalf("%d prompts while the task is still queued, want 0", n)
	}
	if got := f.task(t); got.Status != TaskWaitingCapacity || AwaitDecisionQuestionID(got.WaitReason) != f.qid {
		t.Fatalf("queued task = %s %q, want it still await_decision for %s", got.Status, got.WaitReason, f.qid)
	}

	f.led.Release("other")
	f.m.waitQuestionResumes()
	f.tick()

	if n := len(f.sent()); n != 1 {
		t.Fatalf("%d prompts after capacity freed, want exactly 1 (the guard held)", n)
	}
	if got := f.task(t); got.Status != TaskInProgress {
		t.Fatalf("status = %s, want in_progress", got.Status)
	}
}

func TestQuestionTick_FailedReAdmissionIsRetriedByALaterTick(t *testing.T) {
	f := newTickFixture(t)
	if err := f.led.Acquire(context.Background(), capacity.Request{Holder: "other", PRDID: "other", Pools: []string{"node:n1"}, Node: "n1"}, time.Second, nil, nil); err != nil {
		t.Fatalf("other holder acquire: %v", err)
	}
	f.answer(t, Answer{ChosenOptionID: "first", Actor: "operator"})

	// A cancelled context ends the bounded re-admission.
	ctx, cancel := context.WithCancel(context.Background())
	f.m.QuestionTick(ctx)
	cancel()
	f.m.waitQuestionResumes()
	if dr := f.task(t).DecisionResume; dr != nil {
		t.Fatalf("DecisionResume = %+v after a failed re-admission, want it undone", dr)
	}

	f.led.Release("other")
	f.tick()
	if n := len(f.sent()); n != 1 {
		t.Fatalf("%d prompts after the retry, want 1", n)
	}
}

func TestQuestionTick_NoStoreIsANoOp(t *testing.T) {
	m, _, _ := capFixture(t, 1)
	m.QuestionTick(context.Background())
	m.waitQuestionResumes()
}

func TestQuestionTick_AwaitDecisionQuestionID(t *testing.T) {
	for reason, want := range map[string]string{
		"await_decision: q-1":                           "q-1",
		"await_decision: q-1 — pool node:n1 full (1/1)": "q-1",
		"await_decision":                                "",
		"await_external: q-1":                           "",
		"":                                              "",
	} {
		if got := AwaitDecisionQuestionID(reason); got != want {
			t.Errorf("AwaitDecisionQuestionID(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestQuestionTick_ResumeLineStaysOneLine(t *testing.T) {
	q := Question{ID: "q-1", Options: []QuestionOption{{ID: "a", Label: "Line\nbreak"}}}
	a := &Answer{ID: "a-1", ChosenOptionID: "a", Actor: "op\r\nerator", Quote: "x\ny z\u0085w\rv"}
	line := BuildDecisionResumeLine(q, a)
	for _, r := range "\n\r  \u0085" {
		if strings.ContainsRune(line, r) {
			t.Fatalf("line holds a line break %U: %q", r, line)
		}
	}
	// The quote is a JSON string value that decodes back to the original.
	i := strings.Index(line, "quote=") + len("quote=")
	dec := json.NewDecoder(strings.NewReader(line[i:]))
	var got string
	if err := dec.Decode(&got); err != nil || got != a.Quote {
		t.Fatalf("quote decoded to %q (%v), want %q", got, err, a.Quote)
	}
}
