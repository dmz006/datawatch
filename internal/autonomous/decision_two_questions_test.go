// With more than one blocking question on a task, every one that gets decided
// is typed into the worker once, or listed to the operator with an alert. The
// park never leaves a decision behind for another open question.
//
// The record of what is still owed is read as JSON, so these tests describe
// behaviour, not types.

package autonomous

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// waitLoopPoll is one poll of a worker wait loop's question block.
type waitLoopPoll func(f *deliveryFixture)

// waitLoops are the two wait loops that park a task on a question.
var waitLoops = map[string]func(t *testing.T, f *deliveryFixture) waitLoopPoll{
	// cmd/datawatch/main.go's block for a freshly spawned worker, as it is:
	// it remembers the question whether or not the park happened.
	"fresh worker loop (main.go as it is)": func(t *testing.T, f *deliveryFixture) waitLoopPoll {
		handled := f.qid
		return func(f *deliveryFixture) {
			prd, task := f.live(t)
			if q, ok := f.m.OpenBlockingQuestionForTask(task.ID); ok && q.ID != handled {
				f.m.ParkForDecision(prd, task, q.ID)
				handled = q.ID
			}
		}
	},
	// The change the report proposes for main.go.
	"fresh worker loop (proposed change)": func(t *testing.T, f *deliveryFixture) waitLoopPoll {
		handled := f.qid
		return func(f *deliveryFixture) {
			prd, task := f.live(t)
			if q, ok := f.m.OpenBlockingQuestionForTask(task.ID); ok && q.ID != handled && f.m.ParkForDecision(prd, task, q.ID) {
				handled = q.ID
			}
		}
	},
	"adopted worker wait": func(t *testing.T, f *deliveryFixture) waitLoopPoll {
		prd, task := f.live(t)
		d := &adoptedDecision{m: f.m, prd: prd, task: task, handled: f.qid}
		return func(*deliveryFixture) { d.hold() }
	},
}

// live is the stored Automaton and the fixture's task, as a wait loop holds
// them.
func (f *deliveryFixture) live(t *testing.T) (*PRD, *Task) {
	t.Helper()
	prd, ok := f.m.Store().GetPRD(f.prd.ID)
	if !ok {
		t.Fatal("automaton missing")
	}
	return prd, &prd.Story[0].Tasks[0]
}

// ask raises another blocking question for the fixture's task, as its worker
// does. Nothing has polled it yet.
func (f *deliveryFixture) ask(t *testing.T, text string) string {
	t.Helper()
	q := sampleQuestion(text)
	q.Context, q.PRDID, q.TaskID, q.Blocking = QuestionContextTask, f.prd.ID, f.task(t).ID, true
	q.AskerKind, q.AskedBy = QuestionAskerSession, "worker-sess"
	return mustRaise(t, f.store, q)
}

func (f *deliveryFixture) answerID(t *testing.T, questionID string) {
	t.Helper()
	if _, err := f.store.Answer(questionID, Answer{ChosenOptionID: "first", Quote: answerQuote, Actor: "operator"}); err != nil {
		t.Fatalf("answer %s: %v", questionID, err)
	}
}

// typedFor lists the questions whose lines were typed, in order.
func (f *deliveryFixture) typedFor(ids ...string) []string {
	var out []string
	for _, line := range f.lines() {
		for _, id := range ids {
			if strings.Contains(line, "question="+id+" ") {
				out = append(out, id)
			}
		}
	}
	return out
}

func (f *deliveryFixture) parkedOn(t *testing.T) string {
	t.Helper()
	got := f.task(t)
	if got.Status != TaskWaitingCapacity {
		return ""
	}
	return AwaitDecisionQuestionID(got.WaitReason)
}

func twoQOrder(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

func TestTwoQuestionsAnsweredNewerIsNotLeftForTheOlderOne(t *testing.T) {
	for loopName, newLoop := range waitLoops {
		for _, withdrawn := range []bool{false, true} {
			name := loopName + ", answered"
			if withdrawn {
				name = loopName + ", withdrawn"
			}
			t.Run(name, func(t *testing.T) {
				f := newDeliveryFixture(t)
				q1 := f.qid
				poll := newLoop(t, f)
				q2 := f.ask(t, "A second decision?")
				poll(f)
				if got := f.parkedOn(t); got != q2 {
					t.Fatalf("parked on %q, want the newer question", got)
				}
				if withdrawn {
					if _, err := f.store.Withdraw(q2, "operator", "not needed"); err != nil {
						t.Fatal(err)
					}
				} else {
					f.answerID(t, q2)
				}
				poll(f) // the older question is now the newest open one
				if got := f.parkedOn(t); got != q2 {
					t.Fatalf("parked on %q before the decision was typed, want still %s", got, q2)
				}
				f.tick()
				if got := f.typedFor(q1, q2); !twoQOrder(got, []string{q2}) {
					t.Fatalf("typed %v, want the decided question %s", got, q2)
				}
				for i := 0; i < 3; i++ {
					poll(f)
					f.tick()
				}
				f.answerID(t, q1)
				for i := 0; i < 4; i++ {
					poll(f)
					f.tick()
				}
				if got := f.typedFor(q1, q2); !twoQOrder(got, []string{q2, q1}) {
					t.Fatalf("typed %v, want %s then %s, once each (alerts %q)", got, q2, q1, f.raised())
				}
				if got := f.task(t); got.Status != TaskInProgress {
					t.Fatalf("task ended %s %q, want it running", got.Status, got.WaitReason)
				}
				if al := f.reviewAlerts(q1); len(al) != 0 {
					t.Fatalf("review alerts = %q for a line that was typed", al)
				}
			})
		}
	}
}

// With the proposed or the adopted loop the task parks again on the older
// question once the newer line is typed, so its slot is free while it waits.
func TestTwoQuestionsTaskParksOnTheOlderAfterTheNewerLine(t *testing.T) {
	for _, loopName := range []string{"fresh worker loop (proposed change)", "adopted worker wait"} {
		t.Run(loopName, func(t *testing.T) {
			f := newDeliveryFixture(t)
			q1 := f.qid
			poll := waitLoops[loopName](t, f)
			q2 := f.ask(t, "A second decision?")
			poll(f)
			f.answerID(t, q2)
			poll(f)
			f.tick()
			poll(f)
			if got := f.parkedOn(t); got != q1 {
				t.Fatalf("parked on %q, want the older open question", got)
			}
		})
	}
}

func TestTwoQuestionsOlderAnswerWaitsForTheNewerAndSaysSo(t *testing.T) {
	for loopName, newLoop := range waitLoops {
		t.Run(loopName, func(t *testing.T) {
			f := newDeliveryFixture(t)
			q1 := f.qid
			poll := newLoop(t, f)
			q2 := f.ask(t, "A second decision?")
			poll(f)
			f.answerID(t, q1) // the older one first
			for i := 0; i < 4; i++ {
				poll(f)
				f.tick()
			}
			if got := f.parkedOn(t); got != q2 || f.sends() != 0 {
				t.Fatalf("parked on %q with %d sends, want it waiting on %s with nothing typed", got, f.sends(), q2)
			}
			al := f.raised()
			if len(al) != 1 || !strings.HasPrefix(al[0], "warn|") || !strings.Contains(al[0], "question="+q1+" ") || !strings.Contains(al[0], q2) {
				t.Fatalf("alerts = %q, want one saying %s waits for %s", al, q1, q2)
			}
			if strings.Contains(al[0], answerQuote) {
				t.Fatalf("the alert carries the answer text: %q", al[0])
			}
			f.answerID(t, q2)
			for i := 0; i < 5; i++ {
				poll(f)
				f.tick()
			}
			if got := f.typedFor(q1, q2); !twoQOrder(got, []string{q2, q1}) {
				t.Fatalf("typed %v, want both once", got)
			}
		})
	}
}

// Both raised and decided between two polls, or before any poll at all.
func TestTwoQuestionsOneNoPollSawIsStillTyped(t *testing.T) {
	t.Run("only the newer one was ever parked on", func(t *testing.T) {
		f := newDeliveryFixture(t)
		f.decide(t)
		f.tick() // the task runs again
		q2, q3 := f.ask(t, "Second?"), f.ask(t, "Third?")
		prd, task := f.live(t)
		f.m.ParkForDecision(prd, task, q3)
		f.answerID(t, q2)
		f.answerID(t, q3)
		for i := 0; i < 5; i++ {
			f.tick()
		}
		if got := f.typedFor(q2, q3); !twoQOrder(got, []string{q3, q2}) {
			t.Fatalf("typed %v, want both once", got)
		}
	})
	for _, status := range []TaskStatus{TaskInProgress, TaskVerifying} {
		t.Run("decided before any poll or tick, task "+string(status), func(t *testing.T) {
			f := newDeliveryFixture(t)
			f.decide(t)
			f.tick()
			// A freshly spawned worker works while its task reads verifying.
			f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.Status = status })
			q2 := f.ask(t, "Second?")
			f.answerID(t, q2)
			for i := 0; i < 4; i++ {
				f.tick()
			}
			if got := f.typedFor(q2); !twoQOrder(got, []string{q2}) {
				t.Fatalf("typed %v, want the decision once (task %s, alerts %q)", got, f.task(t).Status, f.raised())
			}
		})
	}
}

// runningWithOwedQuestion returns a fixture whose task runs on its worker with
// one more blocking question that nothing has parked it on.
func runningWithOwedQuestion(t *testing.T) (*deliveryFixture, string) {
	t.Helper()
	f := newDeliveryFixture(t)
	f.decide(t)
	f.tick()
	return f, f.ask(t, "One more?")
}

func TestTwoQuestionsDecisionThatCannotBeTypedIsListed(t *testing.T) {
	set := func(change func(*Task)) func(*testing.T, *deliveryFixture) {
		return func(t *testing.T, f *deliveryFixture) { f.m.store.MutateTask(f.task(t).ID, change) }
	}
	cases := map[string]func(t *testing.T, f *deliveryFixture){
		"task cancelled": set(func(c *Task) { c.Status = TaskCancelled }),
		"task completed": set(func(c *Task) { c.Status = TaskCompleted }),
		"task requeued":  set(func(c *Task) { c.Status, c.SessionID = TaskPending, "" }),
		"another worker": set(func(c *Task) { c.SessionID = "replacement-sess" }),
		"worker session ended": func(t *testing.T, f *deliveryFixture) {
			f.m.SetSessionAliveFn(func(string) bool { return false })
		},
		"worker finished, verifier running": set(func(c *Task) { c.Status, c.VerifierSessionID = TaskVerifying, "judge-sess" }),
		"automaton completed": func(t *testing.T, f *deliveryFixture) {
			set(func(c *Task) { c.Status = TaskCompleted })(t, f)
			prd, _ := f.m.Store().GetPRD(f.prd.ID)
			f.m.store.WithLock(func() { prd.Status = PRDCompleted })
		},
	}
	for name, move := range cases {
		t.Run(name, func(t *testing.T) {
			f, q2 := runningWithOwedQuestion(t)
			move(t, f)
			f.answerID(t, q2)
			for i := 0; i < 4; i++ {
				f.tick()
			}
			if got := f.typedFor(q2); len(got) != 0 {
				t.Fatalf("typed %v into a task that moved on", got)
			}
			if al := f.reviewAlerts(q2); len(al) != 1 {
				t.Fatalf("review alerts = %q (all %q), want exactly one naming the question", al, f.raised())
			}
		})
	}
}

func TestTwoQuestionsParkedAnswerOnACancelledTaskIsListed(t *testing.T) {
	f := newDeliveryFixture(t)
	q2 := f.ask(t, "A second decision?")
	prd, task := f.live(t)
	f.m.ParkForDecision(prd, task, q2)
	f.answerID(t, q2)
	f.m.store.MutateTask(task.ID, func(c *Task) { c.Status = TaskCancelled })
	f.tick()
	f.tick()
	if al := f.reviewAlerts(q2); len(al) != 1 || f.sends() != 0 {
		t.Fatalf("sends=%d review alerts=%q, want the answered question listed once", f.sends(), f.raised())
	}
}

func TestTwoQuestionsOpenDeliveryOnAFinishedTaskIsListed(t *testing.T) {
	f := newDeliveryFixture(t, errRefusedBeforeTyping)
	f.decide(t)
	f.tick() // refused
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.Status = TaskCompleted })
	f.tick()
	f.tick()
	if got := f.delivery(t); got != "review_needed" || len(f.reviewAlerts(f.qid)) != 1 {
		t.Fatalf("delivery=%q alerts=%q, want review_needed with one alert", got, f.raised())
	}
}

func TestTwoQuestionsRefusedSecondLineIsRetriedAndTypedOnce(t *testing.T) {
	f := newDeliveryFixture(t, nil, errRefusedBeforeTyping)
	f.decide(t)
	f.tick()
	q2 := f.ask(t, "One more?")
	f.answerID(t, q2)
	for i := 0; i < 5; i++ {
		f.tick()
	}
	if got := f.typedFor(q2); len(got) != 2 {
		t.Fatalf("%d attempts for the second line, want the refused one and one more", len(got))
	}
	if got := f.delivery(t); got != "sent" {
		t.Fatalf("delivery = %q, want sent", got)
	}
}

// What is still owed is part of the stored task, so a restart keeps it.
func TestTwoQuestionsOwedQuestionIsStored(t *testing.T) {
	f, q2 := runningWithOwedQuestion(t)
	reloaded, err := NewStore(filepath.Dir(f.m.store.dir)) // the data directory
	if err != nil {
		t.Fatal(err)
	}
	tk, ok := reloaded.GetTask(f.task(t).ID)
	if !ok {
		t.Fatal("task missing after the reload")
	}
	b, err := json.Marshal(tk)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Owed []map[string]any `json:"owed_decisions"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Owed) != 1 || out.Owed[0]["question_id"] != q2 {
		t.Fatalf("stored owed_decisions = %v, want the open question %s", out.Owed, q2)
	}
}

// The daemon's own questions about a task have their own handlers: they are
// never typed into the worker or listed by this path.
func TestTwoQuestionsSystemQuestionsAreLeftAlone(t *testing.T) {
	f := newDeliveryFixture(t)
	f.decide(t)
	f.tick()
	q := sampleQuestion("A daemon hold question about the task")
	q.Context, q.PRDID, q.TaskID, q.Blocking = QuestionContextTask, f.prd.ID, f.task(t).ID, true
	q.AskerKind, q.AskedBy = QuestionAskerSystem, "attempt-cap"
	id := mustRaise(t, f.store, q)
	f.answerID(t, id)
	f.tick()
	f.tick()
	if n, al := f.sends(), f.raised(); n != 1 || len(al) != 0 {
		t.Fatalf("sends=%d alerts=%q, want only the worker's own line", n, al)
	}
}

// An open question never parks a task from the tick: a release by the
// operator stays released.
func TestTwoQuestionsTickDoesNotParkOnAnOpenQuestion(t *testing.T) {
	f, _ := runningWithOwedQuestion(t)
	for i := 0; i < 3; i++ {
		f.tick()
	}
	if got := f.task(t); got.Status != TaskInProgress || f.sends() != 1 {
		t.Fatalf("task = %s with %d sends, want it left running", got.Status, f.sends())
	}
}

// A verifier session id stays on a task after its verification ended; it must
// not make the next attempt's working worker look finished.
func TestTwoQuestionsEndedVerifierDoesNotStopTheDelivery(t *testing.T) {
	f, q2 := runningWithOwedQuestion(t)
	f.m.SetSessionAliveFn(func(id string) bool { return id == "worker-sess" })
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.Status, c.VerifierSessionID = TaskVerifying, "old-judge-sess" })
	f.answerID(t, q2)
	for i := 0; i < 3; i++ {
		f.tick()
	}
	if got := f.typedFor(q2); !twoQOrder(got, []string{q2}) {
		t.Fatalf("typed %v, want the decision once (alerts %q)", got, f.raised())
	}
}

// A requeued task that is waiting for capacity has no worker: its decision is
// listed then, not when it gets one.
func TestTwoQuestionsRequeuedAndWaitingForCapacityIsListed(t *testing.T) {
	f, q2 := runningWithOwedQuestion(t)
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) {
		c.Status, c.SessionID, c.WaitReason = TaskWaitingCapacity, "", "pool node:n1 full"
	})
	f.answerID(t, q2)
	f.tick()
	f.tick()
	if al := f.reviewAlerts(q2); len(al) != 1 || f.sends() != 1 {
		t.Fatalf("sends=%d alerts=%q, want it listed once and not typed", f.sends(), f.raised())
	}
}

// An expired question has no decision: a task parked on it does not hold
// another decision back.
func TestTwoQuestionsParkOnAnExpiredQuestionDoesNotHoldADecision(t *testing.T) {
	f := newDeliveryFixture(t)
	q1 := f.qid
	q2 := f.ask(t, "A second decision?")
	prd, task := f.live(t)
	f.m.ParkForDecision(prd, task, q2)
	if _, err := f.store.Expire(q2, "too old"); err != nil {
		t.Fatal(err)
	}
	f.answerID(t, q1)
	for i := 0; i < 4; i++ {
		f.tick()
	}
	if got := f.typedFor(q1, q2); !twoQOrder(got, []string{q1}) {
		t.Fatalf("typed %v, want the decided question once (task %s %q)", got, f.task(t).Status, f.task(t).WaitReason)
	}
}

// A task that runs again can still carry the park text of an earlier attempt.
// That does not hand the earlier worker's question to the new one.
func TestTwoQuestionsStaleParkTextDoesNotMoveAQuestionToANewWorker(t *testing.T) {
	f := newDeliveryFixture(t)
	f.decide(t)
	f.tick()
	q := sampleQuestion("Asked by the first worker")
	q.Context, q.PRDID, q.TaskID, q.Blocking = QuestionContextTask, f.prd.ID, f.task(t).ID, true
	q.AskerKind, q.AskedBy, q.SessionID = QuestionAskerSession, "worker-sess", "worker-sess"
	q2 := mustRaise(t, f.store, q)
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) {
		c.SessionID, c.WaitReason = "replacement-9f31", AwaitDecisionWaitReasonFor(q2)
	})
	f.tick() // the question is still open
	f.answerID(t, q2)
	f.tick()
	f.tick()
	if got := f.typedFor(q2); len(got) != 0 || len(f.reviewAlerts(q2)) != 1 {
		t.Fatalf("typed %v alerts %q, want it listed and not typed into the new worker", got, f.raised())
	}
}

// A withdrawal is not an answer: when no worker can take it, there is nothing
// for the operator to review.
func TestTwoQuestionsWithdrawnOnAFinishedTaskIsNotListed(t *testing.T) {
	f, q2 := runningWithOwedQuestion(t)
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.Status = TaskCancelled })
	if _, err := f.store.Withdraw(q2, "operator", "task is gone"); err != nil {
		t.Fatal(err)
	}
	f.tick()
	f.tick()
	if al := f.raised(); len(al) != 0 || f.sends() != 1 || len(f.task(t).OwedDecisions) != 0 {
		t.Fatalf("sends=%d alerts=%q owed=%v, want it dropped without an alert", f.sends(), al, f.task(t).OwedDecisions)
	}
}

// A blocking question another session raised about the task is not typed into
// the worker, and its answer is still listed.
func TestTwoQuestionsAnotherSessionsAnswerIsListedNotTyped(t *testing.T) {
	f := newDeliveryFixture(t)
	f.decide(t)
	f.tick()
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.Status, c.VerifierSessionID = TaskVerifying, "host-7c1d" })
	q := sampleQuestion("Asked by the verifier")
	q.Context, q.PRDID, q.TaskID, q.Blocking = QuestionContextTask, f.prd.ID, f.task(t).ID, true
	q.AskerKind, q.AskedBy, q.SessionID = QuestionAskerSession, "worker-7c1d", "host-7c1d"
	id := mustRaise(t, f.store, q)
	f.tick()
	f.answerID(t, id)
	f.tick()
	f.tick()
	if got := f.typedFor(id); len(got) != 0 || len(f.reviewAlerts(id)) != 1 {
		t.Fatalf("typed %v alerts %q, want it listed once and not typed", got, f.raised())
	}
}
