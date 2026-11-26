// While a decision line is unfinished (waiting, sending or not typed) the task
// is not re-parked behind a question that was already open when the line was
// recorded. A question asked later may take over, and the old line is then
// listed at once. No line is left that nothing will type and nobody is told
// about, and no alert promises a retry that cannot happen.

package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// refusedBeforeAndAfterPrompt makes the next n sends refused, as while a
// prompt is on screen.
func refusals(n int) []error {
	out := make([]error, n)
	for i := range out {
		out[i] = errRefusedBeforeTyping
	}
	return out
}

func TestUnfinishedLineIsNotLeftForAQuestionOpenAtTheResume(t *testing.T) {
	for name, newLoop := range waitLoops {
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t, refusals(5)...)
			first := f.qid
			other := f.ask(t, "A second decision, open at the same time?")
			poll := newLoop(t, f)
			f.decide(t) // the prompt is on screen
			for i := 0; i < 4; i++ {
				f.tick()
				poll(f)
				if got := f.parkedOn(t); got != "" {
					t.Fatalf("parked on %q while the first line is unfinished (delivery %q)", got, f.delivery(t))
				}
			}
			for i := 0; i < 4; i++ { // the prompt is gone after the fifth refusal
				f.tick()
			}
			if got := f.delivery(t); got != "sent" || f.record(t)["question_id"] != first {
				t.Fatalf("first line: delivery=%q record=%v sends=%d alerts=%q, want it typed once the prompt clears", got, f.record(t), f.sends(), f.raised())
			}
			if al := f.reviewAlerts(first); len(al) != 0 {
				t.Fatalf("review alerts for a line that was typed: %q", al)
			}
			poll(f)
			f.answerID(t, other)
			for i := 0; i < 4; i++ {
				poll(f)
				f.tick()
			}
			lines := f.lines()
			if len(lines) != 7 || !strings.Contains(lines[5], "question="+first+" ") || !strings.Contains(lines[6], "question="+other+" ") {
				t.Fatalf("%d sends %q, want five refused, then each decision once", len(lines), f.typedFor(first, other))
			}
		})
	}
}

// resumeQueuedForCapacity answers the parked question with the only slot taken,
// so the real resume records its line and waits. release lets it in.
func resumeQueuedForCapacity(t *testing.T, f *deliveryFixture) (release func()) {
	t.Helper()
	if err := f.led.Acquire(context.Background(), capacity.Request{Holder: "other", PRDID: "other", Pools: []string{"node:n1"}, Node: "n1"}, time.Second, nil, nil); err != nil {
		t.Fatal(err)
	}
	f.decide(t)
	f.m.QuestionTick(context.Background())
	pollUntil(t, 5*time.Second, "the resume queued", func() bool {
		return len(f.task(t).WaitReason) > len(AwaitDecisionWaitReasonFor(f.qid))
	})
	return func() {
		f.led.Release("other")
		f.m.waitQuestionResumes()
	}
}

// The resume has been admitted and its sender has not claimed the line yet.
func TestUnfinishedLineSurvivesAParkBetweenAdmissionAndSend(t *testing.T) {
	for name, later := range map[string]bool{"a question open at the resume": false, "a question asked later": true} {
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t)
			first := f.qid
			var other string
			if !later {
				other = f.ask(t, "Other open question?")
			}
			release := resumeQueuedForCapacity(t, f)
			if later {
				other = f.ask(t, "Asked while the resume waited?")
			}
			f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.Status, c.WaitReason = TaskInProgress, "" })
			prd, task := f.live(t)
			if f.m.ParkForDecision(prd, task, other) {
				t.Errorf("parked on the other question while the sender is about to type")
			}
			release()
			if got := f.typedFor(first, other); !twoQOrder(got, []string{first}) {
				t.Fatalf("typed %v, delivery %q, alerts %q; want the first line once", got, f.delivery(t), f.raised())
			}
		})
	}
}

// A line left waiting with no sender, then taken over by a later question, is
// listed at once.
func TestUnfinishedWaitingLineTakenOverIsListedAtOnce(t *testing.T) {
	f := newDeliveryFixture(t)
	first := f.qid
	other := f.ask(t, "Other open question?")
	ans := f.decide(t)
	f.leave(t, ans, "waiting", TaskInProgress)
	prd, task := f.live(t)
	f.m.ParkForDecision(prd, task, other)
	for i := 0; i < 5; i++ {
		f.tick()
	}
	typed, listed := len(f.typedFor(first)), len(f.reviewAlerts(first))
	if typed+listed != 1 {
		t.Fatalf("typed=%d listed=%d parkedOn=%q delivery=%q alerts=%q, want it typed or listed, once", typed, listed, f.parkedOn(t), f.delivery(t), f.raised())
	}
}

func TestUnfinishedRefusedLineIsListedWhenALaterQuestionTakesOver(t *testing.T) {
	f := newDeliveryFixture(t, refusals(1)...)
	first := f.qid
	f.decide(t)
	f.tick()
	later := f.ask(t, "The worker moved on and asks this")
	prd, task := f.live(t)
	if !f.m.ParkForDecision(prd, task, later) {
		t.Fatal("a later question must be able to take over a refused line")
	}
	// Listed now, not only once the later question is decided.
	if al := f.reviewAlerts(first); len(al) != 1 || f.delivery(t) != "review_needed" {
		t.Fatalf("at the takeover: delivery=%q alerts=%q, want the old line listed", f.delivery(t), f.raised())
	}
	for i := 0; i < 3; i++ {
		f.tick()
	}
	if n, al := f.sends(), f.reviewAlerts(first); n != 1 || len(al) != 1 {
		t.Fatalf("sends=%d review alerts=%d, want the old line never typed and listed once", n, len(al))
	}
}

// The other open question expires: the refused line is still typed, and the
// task is not left parked.
func TestUnfinishedLineSurvivesTheOtherQuestionExpiring(t *testing.T) {
	f := newDeliveryFixture(t, refusals(1)...)
	first := f.qid
	other := f.ask(t, "Other open question?")
	f.decide(t)
	f.tick()
	prd, task := f.live(t)
	f.m.ParkForDecision(prd, task, other) // a wait loop's poll
	for i := 0; i < 3; i++ {
		f.tick()
	}
	if _, err := f.store.Expire(other, "too old"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		f.tick()
	}
	got := f.task(t)
	if typed := f.typedFor(first); len(typed) != 2 || got.Status != TaskInProgress || f.delivery(t) != "sent" {
		t.Fatalf("task=%s %q sends=%d delivery=%q alerts=%q, want the refused line typed once and the task running", got.Status, got.WaitReason, f.sends(), f.delivery(t), f.raised())
	}
}

func TestUnfinishedExpiredQuestionReleasesItsPark(t *testing.T) {
	for name, errs := range map[string][]error{"typed at once": nil, "refused first": refusals(2)} {
		t.Run(name, func(t *testing.T) {
			f := newDeliveryFixture(t, errs...)
			if _, err := f.store.Expire(f.qid, "too old"); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 6; i++ {
				f.tick()
			}
			got := f.task(t)
			if got.Status != TaskInProgress || f.delivery(t) != "sent" {
				t.Fatalf("task=%s %q delivery=%q, want it released from the expired question", got.Status, got.WaitReason, f.delivery(t))
			}
			lines := f.lines()
			if len(lines) != len(errs)+1 {
				t.Fatalf("%d sends, want %d", len(lines), len(errs)+1)
			}
			last := lines[len(lines)-1]
			if !strings.Contains(last, "question="+f.qid+" ") || !strings.Contains(last, "expired") || strings.Contains(last, "withdrawn") {
				t.Fatalf("line = %q, want it to say the question expired", last)
			}
		})
	}
}

// A task an older build left parked on another question, with the first line
// still open: the first line is listed.
func TestUnfinishedLineStrandedBehindAnotherParkIsListed(t *testing.T) {
	f := newDeliveryFixture(t)
	first := f.qid
	other := f.ask(t, "Other open question?")
	ans := f.decide(t)
	f.leave(t, ans, "not_typed", TaskWaitingCapacity)
	f.m.store.MutateTask(f.task(t).ID, func(c *Task) { c.WaitReason = AwaitDecisionWaitReasonFor(other) })
	for i := 0; i < 3; i++ {
		f.tick()
	}
	if al := f.reviewAlerts(first); len(al) != 1 || f.sends() != 0 {
		t.Fatalf("sends=%d alerts=%q, want the stranded line listed once", f.sends(), f.raised())
	}
}

// The retry must not depend on the question still being among the newest
// decided ones.
func TestUnfinishedLineIsRetriedHoweverManyLaterQuestionsWereDecided(t *testing.T) {
	f := newDeliveryFixture(t, refusals(1)...)
	f.decide(t)
	f.tick()
	for i := 0; i < maxQuestionListLimit; i++ {
		id := mustRaise(t, f.store, sampleQuestion("Unrelated question"))
		if _, err := f.store.Answer(id, Answer{ChosenOptionID: "first", Actor: "operator"}); err != nil {
			t.Fatal(err)
		}
	}
	f.tick()
	if n, got := f.sends(), f.delivery(t); n != 2 || got != "sent" {
		t.Fatalf("sends=%d delivery=%q, want the refused line typed", n, got)
	}
}

// The first warning says when datawatch stops trying, and that it then says so.
func TestUnfinishedWarningDoesNotPromiseARetryThatCanStop(t *testing.T) {
	f := newDeliveryFixture(t, refusals(1)...)
	f.decide(t)
	f.tick()
	al := f.raised()
	if len(al) != 1 {
		t.Fatalf("alerts = %q", al)
	}
	if strings.Contains(al[0], "until it can type") || !strings.Contains(al[0], "later question") || !strings.Contains(al[0], "review needed") {
		t.Fatalf("warning = %q, want the conditions of the retry and what happens when it stops", al[0])
	}
}

func TestUnfinishedWakeWarningSaysWaitingWakesCanBeDropped(t *testing.T) {
	r := newWakeDeliveryRig("sup", errRefusedBeforeTyping)
	set := r.register(t, nil)
	r.failed(set, "t1")
	_, _, al, _ := r.state()
	if len(al) != 1 || !strings.Contains(al[0], "restart") || !strings.Contains(al[0], "turned off") {
		t.Fatalf("warning = %q, want it to say when the retry stops", al)
	}
}

// The review's live case through the real boot reset and executor: two open
// questions, a restart, the parked one answered while a prompt is on screen.
func TestUnfinishedLineAfterARestartWithTwoOpenQuestions(t *testing.T) {
	r := newRestartRig(t)
	cur, _ := r.m.Store().GetPRD(r.f.prd.ID)
	taskID := cur.Story[0].Tasks[0].ID
	other := raiseTaskQuestion(t, r.f.store, cur.ID, taskID, true)
	r.f.qid = raiseTaskQuestion(t, r.f.store, cur.ID, taskID, true)
	parkedQ := r.f.qid
	if !r.m.ParkForDecision(cur, &cur.Story[0].Tasks[0], parkedQ) {
		t.Fatal("task did not park")
	}
	prompt := true
	r.m.SetSessionContinueLineFn(func(sessionID, line string) error {
		r.f.smu.Lock()
		defer r.f.smu.Unlock()
		r.f.calls = append(r.f.calls, line)
		if prompt {
			return session.ErrPromptOnScreen
		}
		return nil
	})
	w := &fakeNudgeWorker{obs: nudgeObs(time.Hour), delivered: true}
	var recorded []time.Time
	var parkedWhileRefused []string
	polls, completed := 0, false
	var waitErr error
	r.m.SetSessionWaitFn(func(ctx context.Context, aw AdoptedWait) error {
		clock := scenarioWait
		waitErr = WaitAdopted(ctx, aw, AdoptedWaitPorts{
			NudgeAfter: nudgeTestWindow, StallAfter: nudgeTestStall,
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
				case polls == 3:
					r.f.answerID(t, parkedQ)
				case polls > 3 && polls < 12:
					r.f.tick() // refused while the prompt is up
					if p := r.f.parkedOn(t); p != "" {
						parkedWhileRefused = append(parkedWhileRefused, p)
					}
				case polls == 12:
					prompt = false
				case polls > 12 && polls < 20:
					r.f.tick()
				case polls == 20:
					r.f.answerID(t, other)
				case polls > 20 && polls < 30:
					r.f.tick()
				case polls == 30:
					completed = true
				case polls > 40:
					return errors.New("test guard: the wait did not end")
				}
				return ctx.Err()
			},
		})
		return waitErr
	})
	got := r.run(t)
	if len(parkedWhileRefused) != 0 {
		t.Errorf("re-parked while the first line was refused: %v", parkedWhileRefused)
	}
	var typed []string
	for _, line := range r.f.lines() {
		for _, id := range []string{parkedQ, other} {
			if strings.Contains(line, "question="+id+" ") {
				typed = append(typed, id)
			}
		}
	}
	// Eight refused attempts, then the first line, then the other.
	if len(typed) < 2 || typed[len(typed)-1] != other || typed[len(typed)-2] != parkedQ || strings.Count(strings.Join(typed, ","), other) != 1 {
		t.Fatalf("sends by question = %v (alerts %q, wait ended %v), want the parked decision once the prompt clears, then the other once", typed, r.f.raised(), waitErr)
	}
	if al := r.f.reviewAlerts(parkedQ); len(al) != 0 {
		t.Fatalf("review alerts for a line that was typed: %q", al)
	}
	if got.Status != PRDCompleted || r.spawns != 0 {
		t.Fatalf("automaton=%s spawns=%d", got.Status, r.spawns)
	}
}

// A second decision given while the first line cannot be typed is not silent:
// the operator is told once that it waits.
func TestUnfinishedLineSaysThatAnotherDecisionWaitsBehindIt(t *testing.T) {
	f := newDeliveryFixture(t, refusals(5)...)
	first := f.qid
	other := f.ask(t, "A second decision?")
	f.decide(t)
	f.tick() // refused
	f.answerID(t, other)
	for i := 0; i < 3; i++ {
		f.tick()
	}
	var about []string
	for _, a := range f.raised() {
		if strings.Contains(a, "question="+other+" ") {
			about = append(about, a)
		}
	}
	if len(about) != 1 || !strings.HasPrefix(about[0], "warn|") || !strings.Contains(about[0], first) || strings.Contains(about[0], answerQuote) {
		t.Fatalf("alerts about the second decision = %q (all %q), want one warning that it waits for %s", about, f.raised(), first)
	}
	for i := 0; i < 8; i++ {
		f.tick()
	}
	if got := f.typedFor(first, other); len(got) != 7 || got[5] != first || got[6] != other {
		t.Fatalf("sends by question = %v, want five refused, the first line, then the second", got)
	}
}
