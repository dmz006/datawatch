package autonomous

import (
	"reflect"
	"strings"
	"testing"
)

// Six end-to-end walks, one per held failure mode (plus the shadow default),
// against the same harness the pass and answer paths each use: real held
// state in the real store, mode "on", the pass raising the question, an
// operator answering the recommended option from a surface, and the store
// carrying the record. Fakes stand in only at Manager.heldAnswerPaths, so
// the assertions are on which path ran (with what), whether the open
// question was raised once, whether the pass re-asks instead of duplicating,
// and what the pass does when the held state is cleared through any other
// path.
//
// Every test walks: raise → re-run (one open question, no duplicates) →
// answer the recommended option (action ran where one exists; the decision
// record carries the quote, actor and surface) → re-process the same pair
// (idempotent) → re-run the pass (one fresh round at most) → clear the
// state another way → the open question is withdrawn, naming the cleared
// state, while the answered one keeps its record.

// wantOptionIDs asserts the held question offers exactly these option ids in
// this order and that every option carries a visible label.
func wantOptionIDs(t *testing.T, q Question, ids []string) {
	t.Helper()
	if got := openOptionIDs(q); !reflect.DeepEqual(got, ids) {
		t.Fatalf("options = %v, want %v", got, ids)
	}
	for _, o := range q.Options {
		if o.Label == "" {
			t.Errorf("option %s has no visible label", o.ID)
		}
	}
}

// nAlerts returns how many alerts the harness has collected.
func (h *heldHarness) nAlerts() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.alerts)
}

// wantWithdrawn asserts the store audit of a daemon withdraw names the
// daemon and the cleared state.
func wantWithdrawn(t *testing.T, h *heldHarness, questionID, state string) {
	t.Helper()
	actions, details := h.rec.snapshot()
	found := false
	for i, act := range actions {
		if act != "question_withdrawn" {
			continue
		}
		d := details[i]
		if d["question_id"] != questionID {
			continue
		}
		found = true
		if d["actor"] != heldQuestionActor {
			t.Errorf("withdraw audit actor = %v, want %q", d["actor"], heldQuestionActor)
		}
		reason, _ := d["reason"].(string)
		if !strings.Contains(reason, state) {
			t.Errorf("withdraw reason does not name the cleared state %q: %q", state, reason)
		}
	}
	if !found {
		t.Errorf("no question_withdrawn store audit for %s: %v", questionID, actions)
	}
}

// ---- 1. blocked -----------------------------------------------------

// A blocked automaton holds waiting for an operator ruling. The question
// links the automaton, offers the blocked options with hold last,
// recommends hold with the cost of the hold in the rationale, and never
// re-raises a duplicate while the block stands. The operator's hold ruling
// is a record of decision — nothing runs, the block stays, and every field
// of the surface (quote, actor, surface) lands on the record. Clearing the
// block through any other path withdraws the open question and leaves the
// answered one — and its record — where it is.
func TestHeldQuestionFailureModeBlocked(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	prdID := h.prds[0].ID
	h.setMode("on")

	h.block(0, "waiting for an operator ruling on the retry budget")
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		t.Fatalf("open held questions = %d, want 1: %+v", len(opens), opens)
	}
	q := opens[0]
	wantQuestion(t, q, "held:"+prdID+":"+string(HeldBlocked))
	if q.AskerKind != QuestionAskerSystem || q.AskedBy != "autonomous" {
		t.Errorf("asker = %q/%q, want the autonomous daemon", q.AskedBy, q.AskerKind)
	}
	if q.Context != QuestionContextPrd || q.PRDID != prdID || q.TaskID != "" {
		t.Errorf("context link = %q prd %q task %q, want the automaton with no task", q.Context, q.PRDID, q.TaskID)
	}
	wantOptionIDs(t, q, []string{
		string(HeldOptionResume), string(HeldOptionRetry), string(HeldOptionChangeSpec),
		string(HeldOptionCancel), string(HeldOptionHold),
	})
	if got := q.Recommendation.OptionID; got != string(HeldOptionHold) {
		t.Errorf("recommendation = %q, want hold", got)
	}
	if !strings.Contains(q.Recommendation.Rationale, "block") {
		t.Errorf("rationale does not name the block it keeps: %q", q.Recommendation.Rationale)
	}
	if !strings.Contains(q.Text, prdID) {
		t.Errorf("question text does not name the automaton: %q", q.Text)
	}

	// Re-run the pass while the block stands: the state is unchanged, so
	// the same single question stays open — no second raise, no refusal.
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 1 || opens[0].ID != q.ID {
		t.Fatalf("open held questions after re-run = %+v, want the same single one", opens)
	}
	if n := h.countAudits("held_question_raised"); n != 1 {
		t.Errorf("held_question_raised = %d, want 1 (dedupe is silent)", n)
	}
	if n := h.countAudits("held_question_raise_refused"); n != 0 {
		t.Errorf("held_question_raise_refused = %d, want 0", n)
	}
	if n := h.nAlerts(); n != 0 {
		t.Errorf("alerts = %d, want 0 (held questions are questions, not alerts)", n)
	}

	// The operator answers the recommended option from their surface.
	// Hold runs nothing: the ruling is recorded, the block stays.
	quote := "ruling: keep it blocked until the budget call lands"
	q, a := h.storeAnswerFull(t, q.ID, Answer{
		Actor:          "ops-bob",
		Surface:        "telegram",
		ChosenOptionID: string(HeldOptionHold),
		Quote:          quote,
	})
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultRecorded)
	if f.anyAction() {
		t.Fatalf("hold ran a manager action: resolve=%v approve=%v revision=%v resume=%v cancel=%v insert=%v release=%d",
			f.resolveCalls(), f.approveCalls(), f.revisionCalls(), f.resumeCalls(),
			f.cancelCalls(), f.insertCalls(), f.releaseCalls())
	}
	if st := h.prdStatus(t, 0); st != PRDBlocked {
		t.Errorf("prd status = %q, want still blocked", st)
	}
	// Rerouting the same (question, answer) is idempotent: no second
	// record, no second action.
	h.processHeld(t, q, a)
	if got := h.countHeldAnswers(t, q, a); got != 1 {
		t.Fatalf("re-routed pair recorded %d times, want 1", got)
	}

	// The decision record carries the surface's quote, actor and surface.
	d := h.heldDecisionRecord(t, prdID, q.ID, a.ID)
	if d.Quote != quote {
		t.Errorf("record quote = %q, want %q", d.Quote, quote)
	}
	if d.Actor != "ops-bob" {
		t.Errorf("record actor = %q, want ops-bob", d.Actor)
	}
	if d.Surface != "telegram" {
		t.Errorf("record surface = %q, want telegram", d.Surface)
	}
	if d.Option != string(HeldOptionHold) {
		t.Errorf("record option = %q, want hold", d.Option)
	}
	if d.Result != heldResultRecorded {
		t.Errorf("record result = %q, want recorded", d.Result)
	}
	if d.DecisionState != string(QuestionAnswered) {
		t.Errorf("record decision state = %q, want answered", d.DecisionState)
	}
	if d.Note == "" {
		t.Error("record note is empty")
	}

	// The hold is still real (the ruling only recorded it), so the next
	// pass re-asks: one fresh round, never a second open question.
	h.c.heldQuestionPass()
	opens = h.openHeld()
	if len(opens) != 1 || opens[0].ID == q.ID {
		t.Fatalf("open held questions after the re-ask = %+v, want one fresh round", opens)
	}
	freshID := opens[0].ID

	// The state is cleared through any other path (the operator
	// unblocks): the pass withdraws the open question, naming the cleared
	// state, and leaves the answered one with its record.
	h.clearBlocked(0)
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 0 {
		t.Fatalf("open held questions after the block cleared = %+v, want none", opens)
	}
	fq, _ := h.qs.Get(freshID)
	if fq.State != QuestionWithdrawn {
		t.Errorf("fresh question state = %q, want withdrawn", fq.State)
	}
	if n := h.countAudits("held_question_withdraw"); n != 1 {
		t.Errorf("held_question_withdraw = %d, want 1", n)
	}
	wantWithdrawn(t, h, freshID, string(HeldBlocked))
	answered, _ := h.qs.Get(q.ID)
	if answered.State != QuestionAnswered {
		t.Errorf("answered question state = %q, want answered (a decision of record)", answered.State)
	}
	h.heldDecisionRecord(t, prdID, q.ID, a.ID) // the record survived the block clearing
}

// ---- 2. review-hold after repeated failure --------------------------

// A task that failed its review attempts stays held for review. The
// question links the task (inside the automaton), offers the review-hold
// options, and — with a content failure on the table — recommends changing
// the spec, saying why. The operator's spec is the answer: it runs the
// existing resolve path with the operator's words as the spec, once, and
// the record carries it. Clearing the hold through any other path
// withdraws the open question.
func TestHeldQuestionFailureModeReviewHeldAfterRepeatedFailure(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	prdID := h.prds[0].ID
	h.setMode("on")

	h.setStatus(0, PRDRunning)
	tid := h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskReviewNeeded
		tk.Error = "build failed on the edge case"
		tk.FailureReason = &FailureReason{Category: FailureCategoryContent}
		tk.EscalationFailures = 3
		tk.ReviewSummary = &ReviewSummary{
			FailureCount: 3,
			TotalSeconds: 420,
			Text:         "three failed attempts, all on content",
		}
	})

	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		t.Fatalf("open held questions = %d, want 1: %+v", len(opens), opens)
	}
	q := opens[0]
	wantQuestion(t, q, "held:"+tid+":"+string(HeldReviewHeld))
	if q.Context != QuestionContextTask || q.PRDID != prdID || q.TaskID != tid {
		t.Errorf("context link = %q prd %q task %q, want the task inside the automaton", q.Context, q.PRDID, q.TaskID)
	}
	wantOptionIDs(t, q, []string{
		string(HeldOptionRetry), string(HeldOptionRetryStronger), string(HeldOptionChangeSpec),
		string(HeldOptionCancel), string(HeldOptionHold),
	})
	if got := q.Recommendation.OptionID; got != string(HeldOptionChangeSpec) {
		t.Errorf("recommendation = %q, want change-spec (a content failure)", got)
	}
	if !strings.Contains(q.Recommendation.Rationale, "spec") {
		t.Errorf("rationale does not point at the spec for a content failure: %q", q.Recommendation.Rationale)
	}
	if !strings.Contains(q.Text, tid) {
		t.Errorf("question text does not name the held task: %q", q.Text)
	}

	// Re-run while the hold stands: still one open question, no duplicate.
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 1 || opens[0].ID != q.ID {
		t.Fatalf("open held questions after re-run = %+v, want the same single one", opens)
	}
	if n := h.countAudits("held_question_raised"); n != 1 {
		t.Errorf("held_question_raised = %d, want 1 (dedupe is silent)", n)
	}
	if n := h.nAlerts(); n != 0 {
		t.Errorf("alerts = %d, want 0", n)
	}

	// The operator answers the recommended option. The operator's spec
	// is the spec: it runs the resolve path once.
	spec := "spec v3: pin the edge-case fixture before the build"
	q, a := h.storeAnswerFull(t, q.ID, Answer{
		Actor:          "ops-bob",
		Surface:        "telegram",
		ChosenOptionID: string(HeldOptionChangeSpec),
		Quote:          spec,
	})
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultApplied)
	rels := f.resolveCalls()
	if len(rels) != 1 {
		t.Fatalf("change-spec ran %d resolve call(s), want exactly 1", len(rels))
	}
	if rels[0].Action != reviewActionRetry {
		t.Errorf("resolve action = %q, want %q (re-run with the revised spec)", rels[0].Action, reviewActionRetry)
	}
	if rels[0].Spec != spec {
		t.Errorf("resolve spec = %q, want the operator's spec %q", rels[0].Spec, spec)
	}
	if len(f.approveCalls())+len(f.revisionCalls())+f.releaseCalls()+
		len(f.cancelCalls())+len(f.insertCalls()) != 0 {
		t.Errorf("change-spec ran a foreign path: approve=%v revision=%v release=%v cancel=%v insert=%v",
			f.approveCalls(), f.revisionCalls(), f.releaseCalls(), f.cancelCalls(), f.insertCalls())
	}
	// The idempotence probe: the same (question, answer) re-routed runs
	// nothing new.
	h.processHeld(t, q, a)
	if got := h.countHeldAnswers(t, q, a); got != 1 {
		t.Fatalf("re-routed pair recorded %d times, want 1", got)
	}
	if len(f.resolveCalls()) != 1 {
		t.Fatalf("re-routed pair ran %d resolve call(s), want still 1", len(f.resolveCalls()))
	}

	// The record carries the operator's words as the quote.
	d := h.heldDecisionRecord(t, prdID, q.ID, a.ID)
	if d.Quote != spec {
		t.Errorf("record quote = %q, want the spec %q", d.Quote, spec)
	}
	if d.Actor != "ops-bob" || d.Surface != "telegram" {
		t.Errorf("record actor/surface = %q/%q, want ops-bob/telegram", d.Actor, d.Surface)
	}
	if d.Option != string(HeldOptionChangeSpec) {
		t.Errorf("record option = %q, want change-spec", d.Option)
	}
	if d.Result != heldResultApplied {
		t.Errorf("record result = %q, want applied", d.Result)
	}

	// While the hold is still real (the release ran through the fake),
	// the pass keeps re-asking: one fresh round at a time.
	h.c.heldQuestionPass()
	opens = h.openHeld()
	if len(opens) != 1 || opens[0].ID == q.ID {
		t.Fatalf("open held questions after the re-ask = %+v, want one fresh round", opens)
	}

	// The review hold clears through any other path (the task was
	// released out of review): the pass withdraws the open question.
	freshID := opens[0].ID
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskPending
		tk.Error = ""
		tk.FailureReason = nil
		tk.ReviewSummary = nil
	})
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 0 {
		t.Fatalf("open held questions after the review hold cleared = %+v, want none", opens)
	}
	if n := h.countAudits("held_question_withdraw"); n != 1 {
		t.Errorf("held_question_withdraw = %d, want 1", n)
	}
	fq, _ := h.qs.Get(freshID)
	if fq.State != QuestionWithdrawn {
		t.Errorf("fresh question state = %q, want withdrawn", fq.State)
	}
	wantWithdrawn(t, h, freshID, string(HeldReviewHeld))
	answered, _ := h.qs.Get(q.ID)
	if answered.State != QuestionAnswered {
		t.Errorf("answered question state = %q, want answered (a decision of record)", answered.State)
	}
	h.heldDecisionRecord(t, prdID, q.ID, a.ID) // the record survived the hold clearing
}

// ---- 3. the draft's revision was requested --------------------------

// The automaton is held on its own draft: the review asked for a revision
// and the automation is stopped in the draft state. The question links the
// automaton, offers revision options, and — without a failure category to
// point at a cause — recommends changing the spec, naming the repetition
// it would otherwise trigger. The operator's spec runs the existing
// request-revision path once, and the record carries it.
func TestHeldQuestionFailureModeRevisionsAsked(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	prdID := h.prds[0].ID
	h.setMode("on")

	h.revisionsAsked(0, "the spec misses the auth requirement")
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		t.Fatalf("open held questions = %d, want 1: %+v", len(opens), opens)
	}
	q := opens[0]
	wantQuestion(t, q, "held:"+prdID+":"+string(HeldRevisionRequested))
	if q.Context != QuestionContextPrd || q.PRDID != prdID || q.TaskID != "" {
		t.Errorf("context link = %q prd %q task %q, want the automaton with no task", q.Context, q.PRDID, q.TaskID)
	}
	wantOptionIDs(t, q, []string{
		string(HeldOptionChangeSpec), string(HeldOptionResume), string(HeldOptionHold),
	})
	if got := q.Recommendation.OptionID; got != string(HeldOptionChangeSpec) {
		t.Errorf("recommendation = %q, want change-spec (an unclassified draft repetition)", got)
	}
	if !strings.Contains(q.Recommendation.Rationale, "spec") {
		t.Errorf("rationale does not name the spec it would repeat: %q", q.Recommendation.Rationale)
	}

	// Re-run while the draft hold stands: same single question, no
	// duplicate.
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 1 || opens[0].ID != q.ID {
		t.Fatalf("open held questions after re-run = %+v, want the same single one", opens)
	}
	if n := h.countAudits("held_question_raised"); n != 1 {
		t.Errorf("held_question_raised = %d, want 1 (dedupe is silent)", n)
	}
	if n := h.nAlerts(); n != 0 {
		t.Errorf("alerts = %d, want 0", n)
	}

	// The operator answers the recommended option with the corrected
	// spec. It runs the request-revision path once — not the approve
	// path.
	spec := "spec v2: add the auth requirement before the retry ladder"
	q, a := h.storeAnswerFull(t, q.ID, Answer{
		Actor:          "ops-bob",
		Surface:        "web",
		ChosenOptionID: string(HeldOptionChangeSpec),
		Quote:          spec,
	})
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultApplied)
	if got := f.revisionCalls(); len(got) != 1 {
		t.Fatalf("change-spec ran %d request-revision call(s), want exactly 1: %v", len(got), got)
	}
	if got := f.revisionCalls()[0]; got != spec {
		t.Errorf("request-revision note = %q, want the operator's spec %q", got, spec)
	}
	if len(f.approveCalls()) != 0 {
		t.Errorf("change-spec approved the revision hold: %v", f.approveCalls())
	}
	// Rerouting the same (question, answer) changes nothing else.
	h.processHeld(t, q, a)
	if got := h.countHeldAnswers(t, q, a); got != 1 {
		t.Fatalf("re-routed pair recorded %d times, want 1", got)
	}
	if len(f.revisionCalls()) != 1 {
		t.Fatalf("re-routed pair ran %d request-revision call(s), want still 1", len(f.revisionCalls()))
	}

	// The record carries the operator's words as the spec.
	d := h.heldDecisionRecord(t, prdID, q.ID, a.ID)
	if d.Quote != spec {
		t.Errorf("record quote = %q, want the spec %q", d.Quote, spec)
	}
	if d.Actor != "ops-bob" || d.Surface != "web" {
		t.Errorf("record actor/surface = %q/%q, want ops-bob/web", d.Actor, d.Surface)
	}
	if d.Result != heldResultApplied {
		t.Errorf("record result = %q, want applied", d.Result)
	}

	// The draft hold is still real (the revision ran through the fake):
	// the pass re-asks, one fresh round.
	h.c.heldQuestionPass()
	opens = h.openHeld()
	if len(opens) != 1 || opens[0].ID == q.ID {
		t.Fatalf("open held questions after the re-ask = %+v, want one fresh round", opens)
	}
	freshID := opens[0].ID

	// The draft hold clears through any other path (the review approved
	// the draft): the pass withdraws the open question.
	h.setPRD(0, func(p *PRD) {
		p.Status = PRDApproved
		p.RevisionsRequested = 0
		p.RejectionReason = ""
	})
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 0 {
		t.Fatalf("open held questions after the draft cleared = %+v, want none", opens)
	}
	fq, _ := h.qs.Get(freshID)
	if fq.State != QuestionWithdrawn {
		t.Errorf("fresh question state = %q, want withdrawn", fq.State)
	}
	wantWithdrawn(t, h, freshID, string(HeldRevisionRequested))
	answered, _ := h.qs.Get(q.ID)
	if answered.State != QuestionAnswered {
		t.Errorf("answered question state = %q, want answered (a decision of record)", answered.State)
	}
	h.heldDecisionRecord(t, prdID, q.ID, a.ID) // the record survived the draft clearing
}

// ---- 4. awaiting an unanswered decision -----------------------------

// The automaton holds on an unanswered decision: its task is parked and
// nothing can move it. The question links the task, offers release or
// cancel with hold last, and recommends hold — because the decision being
// waited on is an operator call, not an automation failure. Answering
// hold records the ruling and runs nothing; clearing the hold through any
// other path withdraws the open question.
func TestHeldQuestionFailureModeAwaitingUnansweredDecision(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	prdID := h.prds[0].ID
	h.setMode("on")

	tid := h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskWaitingDecision
		tk.WaitReason = AwaitDecisionWaitReason
	})
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		t.Fatalf("open held questions = %d, want 1: %+v", len(opens), opens)
	}
	q := opens[0]
	wantQuestion(t, q, "held:"+tid+":"+string(HeldAwaitingDecision))
	if q.Context != QuestionContextTask || q.PRDID != prdID || q.TaskID != tid {
		t.Errorf("context link = %q prd %q task %q, want the task inside the automaton", q.Context, q.PRDID, q.TaskID)
	}
	wantOptionIDs(t, q, []string{
		string(HeldOptionResume), string(HeldOptionCancel), string(HeldOptionHold),
	})
	if got := q.Recommendation.OptionID; got != string(HeldOptionHold) {
		t.Errorf("recommendation = %q, want hold (an operator decision)", got)
	}

	// Re-run while the wait stands: same single question, no duplicate.
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 1 || opens[0].ID != q.ID {
		t.Fatalf("open held questions after re-run = %+v, want the same single one", opens)
	}
	if n := h.countAudits("held_question_raised"); n != 1 {
		t.Errorf("held_question_raised = %d, want 1 (dedupe is silent)", n)
	}
	if n := h.nAlerts(); n != 0 {
		t.Errorf("alerts = %d, want 0", n)
	}

	// The operator answers hold: the ruling is recorded and the task
	// keeps waiting — the release path never runs.
	quote := "the decision is still open upstream; wait"
	q, a := h.storeAnswerFull(t, q.ID, Answer{
		Actor:          "ops-bob",
		Surface:        "telegram",
		ChosenOptionID: string(HeldOptionHold),
		Quote:          quote,
	})
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultRecorded)
	if f.anyAction() {
		t.Fatalf("hold ran a manager action: resolve=%v approve=%v revision=%v resume=%v cancel=%v insert=%v release=%d",
			f.resolveCalls(), f.approveCalls(), f.revisionCalls(), f.resumeCalls(),
			f.cancelCalls(), f.insertCalls(), f.releaseCalls())
	}
	if f.releaseCalls() != 0 {
		t.Errorf("hold released the await: %d", f.releaseCalls())
	}
	if tk, ok := h.m.store.GetTask(tid); !ok || tk.Status != TaskWaitingDecision {
		t.Errorf("task moved off the decision wait: %+v", tk)
	}
	// Idempotence: the re-routed pair records nothing new.
	h.processHeld(t, q, a)
	if got := h.countHeldAnswers(t, q, a); got != 1 {
		t.Fatalf("re-routed pair recorded %d times, want 1", got)
	}

	d := h.heldDecisionRecord(t, prdID, q.ID, a.ID)
	if d.Quote != quote {
		t.Errorf("record quote = %q, want %q", d.Quote, quote)
	}
	if d.Actor != "ops-bob" || d.Surface != "telegram" {
		t.Errorf("record actor/surface = %q/%q, want ops-bob/telegram", d.Actor, d.Surface)
	}
	if d.Result != heldResultRecorded {
		t.Errorf("record result = %q, want recorded", d.Result)
	}

	// The wait is still real: the pass re-asks, one fresh round.
	h.c.heldQuestionPass()
	opens = h.openHeld()
	if len(opens) != 1 || opens[0].ID == q.ID {
		t.Fatalf("open held questions after the re-ask = %+v, want one fresh round", opens)
	}
	freshID := opens[0].ID

	// The wait clears through any other path (the decision was made and
	// the task resumed): the pass withdraws the open question.
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskPending
		tk.WaitReason = ""
	})
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 0 {
		t.Fatalf("open held questions after the wait cleared = %+v, want none", opens)
	}
	fq, _ := h.qs.Get(freshID)
	if fq.State != QuestionWithdrawn {
		t.Errorf("fresh question state = %q, want withdrawn", fq.State)
	}
	wantWithdrawn(t, h, freshID, string(HeldAwaitingDecision))
	answered, _ := h.qs.Get(q.ID)
	if answered.State != QuestionAnswered {
		t.Errorf("answered question state = %q, want answered (a decision of record)", answered.State)
	}
	h.heldDecisionRecord(t, prdID, q.ID, a.ID) // the record survived the wait clearing
}

// ---- 5. an automation member insert is pending ----------------------

// The automaton holds on a membership change: a member insert waits for
// operator approval, recorded against the member's automation. The
// question names the pending insert, offers approve, reject and hold, and
// recommends hold. Approving is an applied decision on the record;
// approving twice changes nothing. Withdrawing the pending insert through
// any other path withdraws the open question.
func TestHeldQuestionFailureModePendingAutomationInsert(t *testing.T) {
	h := newAnswerHarness(t, 2)
	f := h.useFakes()
	memberPRD := h.prds[1].ID
	h.setMode("on")

	auto := h.makeAuto(nil, 2)
	h.addPendingInsert(auto, memberPRD, "ins1")
	h.c.heldQuestionPass()

	opens := h.openHeld()
	if len(opens) != 1 {
		t.Fatalf("open held questions = %d, want 1: %+v", len(opens), opens)
	}
	q := opens[0]
	wantQuestion(t, q, "held:ins1:"+string(HeldPendingInsert))
	// The insert belongs to the member's automaton: the context links
	// that automaton and names nothing else held.
	if q.Context != QuestionContextPrd || q.PRDID != memberPRD || q.TaskID != "" {
		t.Errorf("context link = %q prd %q task %q, want the member's automaton with no task", q.Context, q.PRDID, q.TaskID)
	}
	wantOptionIDs(t, q, []string{
		string(HeldOptionApproveInsert), string(HeldOptionRejectInsert), string(HeldOptionHold),
	})
	if got := q.Recommendation.OptionID; got != string(HeldOptionHold) {
		t.Errorf("recommendation = %q, want hold (a membership change)", got)
	}
	if !strings.Contains(q.Text, "ins1") {
		t.Errorf("question text does not name the pending insert: %q", q.Text)
	}

	// Re-run while the insert is pending: same single question, no
	// duplicate.
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 1 || opens[0].ID != q.ID {
		t.Fatalf("open held questions after re-run = %+v, want the same single one", opens)
	}
	if n := h.countAudits("held_question_raised"); n != 1 {
		t.Errorf("held_question_raised = %d, want 1 (dedupe is silent)", n)
	}
	if n := h.nAlerts(); n != 0 {
		t.Errorf("alerts = %d, want 0", n)
	}

	// The operator approves the change from their surface. It runs the
	// insert-approval path once, and the insert lands on the record as
	// an applied decision.
	quote := "membership change approved: add the review member"
	q, a := h.storeAnswerFull(t, q.ID, Answer{
		Actor:          "ops-bob",
		Surface:        "telegram",
		ChosenOptionID: string(HeldOptionApproveInsert),
		Quote:          quote,
	})
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultApplied)
	if got := f.insertCalls(); len(got) != 1 || got[0] != "ins1" {
		t.Fatalf("approve-insert calls = %v, want exactly [ins1]", got)
	}
	if len(f.resolveCalls())+len(f.approveCalls())+len(f.revisionCalls())+
		f.releaseCalls()+len(f.cancelCalls()) != 0 {
		t.Errorf("approve-insert ran a foreign path: resolve=%v approve=%v revision=%v release=%v cancel=%v",
			f.resolveCalls(), f.approveCalls(), f.revisionCalls(), f.releaseCalls(), f.cancelCalls())
	}
	// Approving the same (question, answer) a second time changes
	// nothing: the decision is already recorded on the member's
	// automaton.
	h.processHeld(t, q, a)
	if got := h.countHeldAnswers(t, q, a); got != 1 {
		t.Fatalf("re-routed pair recorded %d times, want 1", got)
	}
	if got := f.insertCalls(); len(got) != 1 {
		t.Fatalf("re-routed pair ran %d approve-insert call(s), want still 1: %v", len(got), got)
	}

	d := h.heldDecisionRecord(t, memberPRD, q.ID, a.ID)
	if d.Quote != quote {
		t.Errorf("record quote = %q, want %q", d.Quote, quote)
	}
	if d.Actor != "ops-bob" || d.Surface != "telegram" {
		t.Errorf("record actor/surface = %q/%q, want ops-bob/telegram", d.Actor, d.Surface)
	}
	if d.Option != string(HeldOptionApproveInsert) {
		t.Errorf("record option = %q, want approve-insert", d.Option)
	}
	if d.Result != heldResultApplied {
		t.Errorf("record result = %q, want applied", d.Result)
	}

	// The insert still sits in the store (approval ran through the
	// fake), so the pass re-asks: one fresh round.
	au, ok := h.m.automations.Get(auto.ID)
	if !ok || len(au.PendingInserts) != 1 {
		t.Fatalf("pending inserts = %d, want 1 while the approval is faked", len(au.PendingInserts))
	}
	h.c.heldQuestionPass()
	opens = h.openHeld()
	if len(opens) != 1 || opens[0].ID == q.ID {
		t.Fatalf("open held questions after the re-ask = %+v, want one fresh round", opens)
	}
	freshID := opens[0].ID

	// The pending insert is withdrawn through any other path (the
	// operator removed it): the pass withdraws the open question.
	h.dropPendingInsert(auto.ID, "ins1")
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 0 {
		t.Fatalf("open held questions after the insert cleared = %+v, want none", opens)
	}
	fq, _ := h.qs.Get(freshID)
	if fq.State != QuestionWithdrawn {
		t.Errorf("fresh question state = %q, want withdrawn", fq.State)
	}
	wantWithdrawn(t, h, freshID, string(HeldPendingInsert))
	answered, _ := h.qs.Get(q.ID)
	if answered.State != QuestionAnswered {
		t.Errorf("answered question state = %q, want answered (a decision of record)", answered.State)
	}
	h.heldDecisionRecord(t, memberPRD, q.ID, a.ID) // the record survived the insert clearing
}

// ---- 6. shadow by default: the unset config raises nothing ----------

// The default configuration is unconfigured, and unconfigured reads as
// shadow. Shadow mode neither raises a held question, records an answered
// decision, nor alerts: the operator sees a shadow note naming the state
// on the automaton's record, the block stays exactly as it was, and the
// note appears once per state.
func TestHeldQuestionFailureModeShadowDefault(t *testing.T) {
	h := newAnswerHarness(t, 1)
	prdID := h.prds[0].ID
	marker := "held:" + prdID + ":" + string(HeldBlocked)

	// No mode was set: the effective mode must already be shadow.
	h.wantMode(t, "shadow")

	h.block(0, "waiting for an operator ruling")
	h.c.heldQuestionPass()

	if opens := h.openHeld(); len(opens) != 0 {
		t.Fatalf("shadow mode raised questions: %+v", opens)
	}
	if got := h.prdStatus(t, 0); got != PRDBlocked {
		t.Fatalf("prd status = %q, want the block untouched", got)
	}
	// The shadow note appears on the automaton's record, once, naming
	// the state and the recommended option — but it is a note, not a
	// question: no id the operator has to answer.
	notes := h.heldNotes(0)
	if len(notes) != 1 {
		t.Fatalf("shadow notes = %d, want 1: %+v", len(notes), notes)
	}
	n := notes[0]
	if n.Source != "held" {
		t.Errorf("note source = %q, want held", n.Source)
	}
	if !strings.HasPrefix(n.Text, marker) {
		t.Errorf("note text does not carry the dedupe marker %q:\n%s", marker, n.Text)
	}
	for _, want := range []string{"blocked", "would raise a held question", string(HeldOptionHold)} {
		if !strings.Contains(n.Text, want) {
			t.Errorf("note text missing %q:\n%s", want, n.Text)
		}
	}
	// Nothing is recorded as a decision of record, and nothing alerts.
	p, ok := h.m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("prd missing")
	}
	for _, d := range p.Decisions {
		if d.Kind == heldAnswerDecisionKind {
			t.Fatalf("shadow mode recorded a decision: %+v", d)
		}
	}
	if n := h.countAudits("held_question_raised"); n != 0 {
		t.Errorf("held_question_raised = %d, want 0 in shadow", n)
	}
	if n := h.countAudits("held_question_shadow"); n != 1 {
		t.Errorf("held_question_shadow = %d, want 1", n)
	}
	if n := h.nAlerts(); n != 0 {
		t.Errorf("alerts = %d, want 0 (shadow does not alert)", n)
	}

	// Re-running (even across a daemon restart) never raises and never
	// re-notes: the note dedupes on the marker.
	h.c.heldQuestionPass()
	h.restart()
	h.c.heldQuestionPass()
	if opens := h.openHeld(); len(opens) != 0 {
		t.Fatalf("shadow mode raised questions on re-runs: %+v", opens)
	}
	if notes := h.heldNotes(0); len(notes) != 1 {
		t.Errorf("shadow notes after re-runs and a restart = %d, want still 1: %+v", len(notes), notes)
	}
	if n := h.countAudits("held_question_raised"); n != 0 {
		t.Errorf("held_question_raised after re-runs = %d, want 0", n)
	}
	if got := h.prdStatus(t, 0); got != PRDBlocked {
		t.Errorf("prd status = %q, want the block untouched", got)
	}
}
