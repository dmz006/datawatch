package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// The held-question answer path (ProcessHeldAnswer) is the operator's door: the
// state is held, the daemon raised a question, and the operator picked an
// option. Each option routes by the held state through an EXISTING Manager
// path:
//
//	blocked            → resume / retry* / change-spec* / cancel* / hold
//	review-hold        → retry / retry-stronger / change-spec* / cancel / hold
//	revisions-requested→ resume (approve) / change-spec* / hold
//	awaiting-decision  → resume (release) / cancel / hold
//	pending-insert     → approve-insert / reject-insert / hold
//
// (* = the option only exists once the block is a review-hold).
//
// Applied options run the manager path and record "applied". Hold, free text,
// and reject-insert run nothing and record "recorded". Options with no daemon
// path record "refused" and name the state. And a routed answer whose held
// state has since moved is "stale": nothing runs, the question is withdrawn
// with a reason naming the old and the new state, and the caller gets
// ErrHeldStateChanged. Every outcome records a decision on the PRD and an
// audit event, and a repeated (question, answer) pair is idempotent.
//
// The tests keep the held state real in the store but stand in the
// review / control / resume paths behind Manager.heldAnswerPaths so the
// assertions are on which path ran, with what, and which fields landed on the
// recorded decision — not on an all-in-one lifecycle harness.

// ---- helpers ---------------------------------------------------------------

// newAnswerHarness is newHeldHarness with the manager's audit callback wired
// to the harness sink so a routed answer's recordHeldAnswer entry lands in
// h.audits / h.auditD. Without this, controlAudit (which prefers the
// coordinator, nil here, then m.auditFn, nil here) drops the record.
func newAnswerHarness(t *testing.T, nPRDs int) *heldHarness {
	t.Helper()
	h := newHeldHarness(t, nPRDs)
	h.m.SetAuditFn(func(action string, d map[string]any) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.audits = append(h.audits, action)
		h.auditD = append(h.auditD, d)
	})
	return h
}

// oneHeld raises the pass, asserts it produced exactly one opened held
// question, and that the question offers wantOpt. It returns the question.
func (h *heldHarness) oneHeld(t *testing.T, wantOpt string) Question {
	t.Helper()
	opens := h.openHeld()
	if len(opens) != 1 {
		t.Fatalf("expected exactly one opened held question, got %d", len(opens))
	}
	found := false
	for _, o := range opens[0].Options {
		if o.ID == wantOpt {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("held question offers no %q option; options=[%s]", wantOpt,
			strings.Join(openOptionIDs(opens[0]), ", "))
	}
	return opens[0]
}

// openOptionIDs lists the option ids a question offers, for diagnostics.
func openOptionIDs(q Question) []string {
	out := make([]string, 0, len(q.Options))
	for _, o := range q.Options {
		out = append(out, o.ID)
	}
	return out
}

// storeAnswer records the answer in the store (option and/or quote) and
// returns the question plus the stored record. It does NOT route the answer.
func (h *heldHarness) storeAnswer(t *testing.T, questionID, opt, quote string) (Question, Answer) {
	t.Helper()
	q, ok := h.qs.Get(questionID)
	if !ok {
		t.Fatalf("question %q missing", questionID)
	}
	a, err := h.qs.Answer(questionID, Answer{ChosenOptionID: opt, Quote: quote})
	if err != nil {
		t.Fatalf("answer %q: %v", questionID, err)
	}
	return q, a
}

// storeAnswerFull records an answer with an explicit actor and surface (as a
// real surface would) and returns the question plus stored record.
func (h *heldHarness) storeAnswerFull(t *testing.T, questionID string, a Answer) (Question, Answer) {
	t.Helper()
	q, ok := h.qs.Get(questionID)
	if !ok {
		t.Fatalf("question %q missing", questionID)
	}
	a.QuestionID = questionID
	stored, err := h.qs.Answer(questionID, a)
	if err != nil {
		t.Fatalf("answer %q: %v", questionID, err)
	}
	return q, stored
}

// processHeld routes an already-stored q+a through the manager, failing the
// test if routing returned an error.
func (h *heldHarness) processHeld(t *testing.T, q Question, a Answer) {
	t.Helper()
	if err := h.m.ProcessHeldAnswer(context.Background(), q, a); err != nil {
		t.Fatalf("process held answer %q: %v", q.ID, err)
	}
}

// processHeldErr routes an already-stored q+a and returns the (possibly
// non-nil) error, for assertions on the stale path.
func (h *heldHarness) processHeldErr(t *testing.T, q Question, a Answer) error {
	t.Helper()
	return h.m.ProcessHeldAnswer(context.Background(), q, a)
}

// answerHeld answers the opened question and routes it in one step.
func (h *heldHarness) answerHeld(t *testing.T, questionID, opt, quote string) (Question, Answer) {
	t.Helper()
	q, a := h.storeAnswer(t, questionID, opt, quote)
	h.processHeld(t, q, a)
	return q, a
}

// heldAnswerOutcome asserts the recorded result for this q+a pair. It prints
// the recorded detail on a mismatch so a refused or stale path is
// self-explaining.
func (h *heldHarness) heldAnswerOutcome(t *testing.T, q Question, a Answer, want string) {
	t.Helper()
	h.mu.Lock()
	var got, detail string
	found := false
	for i := len(h.auditD) - 1; i >= 0; i-- {
		d := h.auditD[i]
		if d["question_id"] == q.ID && d["answer_id"] == a.ID {
			got, _ = d["result"].(string)
			detail, _ = d["detail"].(string)
			found = true
			break
		}
	}
	h.mu.Unlock()
	if !found {
		t.Fatalf("no held-answer audit for question %q (answer %q)", q.ID, a.ID)
	}
	if got != want {
		t.Fatalf("result = %q (detail: %s), want %q", got, detail, want)
	}
}

// heldAnswerDetail returns the recorded detail for this q+a pair.
func (h *heldHarness) heldAnswerDetail(t *testing.T, q Question, a Answer) string {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.auditD) - 1; i >= 0; i-- {
		d := h.auditD[i]
		if d["question_id"] == q.ID && d["answer_id"] == a.ID {
			return d["detail"].(string)
		}
	}
	t.Fatalf("no held-answer audit for question %q (answer %q)", q.ID, a.ID)
	return ""
}

// countHeldAnswers counts the recordHeldAnswer audit entries for this q+a
// pair. It is the idempotence probe: a rerouted pair adds the action but no
// new entry.
func (h *heldHarness) countHeldAnswers(t *testing.T, q Question, a Answer) int {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, d := range h.auditD {
		if d["question_id"] == q.ID && d["answer_id"] == a.ID {
			n++
		}
	}
	return n
}

// heldDecisionRecord returns the Decision a routed answer recorded on the
// PRD, matching on question and answer id (and asserting it is the
// operator_answer kind). It is how the record content (quote, actor, surface,
// option, result, withdrawn state) is asserted.
func (h *heldHarness) heldDecisionRecord(t *testing.T, prdID, questionID, answerID string) Decision {
	t.Helper()
	p, ok := h.m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("prd %s missing", prdID)
	}
	for i := range p.Decisions {
		d := &p.Decisions[i]
		if d.Kind == heldAnswerDecisionKind && d.QuestionID == questionID && d.AnswerID == answerID {
			return *d
		}
	}
	t.Fatalf("no %s decision recorded for question %s answer %s on prd %s", heldAnswerDecisionKind, questionID, answerID, prdID)
	return Decision{}
}

func (h *heldHarness) prdStatus(t *testing.T, i int) PRDStatus {
	t.Helper()
	p, ok := h.m.store.GetPRD(h.prds[i].ID)
	if !ok {
		t.Fatalf("prd %d missing", i)
	}
	return p.Status
}

func (h *heldHarness) prdRevisions(t *testing.T, i int) int {
	t.Helper()
	p, ok := h.m.store.GetPRD(h.prds[i].ID)
	if !ok {
		t.Fatalf("prd %d missing", i)
	}
	return p.RevisionsRequested
}

func (h *heldHarness) taskOf(t *testing.T, prdID string) Task {
	t.Helper()
	p, ok := h.m.store.GetPRD(prdID)
	if !ok || len(p.Story) == 0 || len(p.Story[0].Tasks) == 0 {
		t.Fatalf("task not found under prd %s", prdID)
	}
	return p.Story[0].Tasks[0]
}

func (h *heldHarness) memberLanded(t *testing.T, automationID, prdID string) bool {
	t.Helper()
	a, ok := h.m.automations.Get(automationID)
	if !ok {
		t.Fatalf("automation %s missing", automationID)
	}
	for _, mem := range a.Members {
		if mem.PRDID == prdID {
			return true
		}
	}
	return false
}

// reviewHoldSetup puts PRD 0 into the review-hold held state: one task parked
// at TaskReviewNeeded under a running PRD.
func reviewHoldSetup(h *heldHarness) {
	h.addTask(0)
	h.setStatus(0, PRDRunning)
	h.saveTask(0, func(tk *Task) { tk.Status = TaskReviewNeeded; tk.Error = "build failed on the edge case" })
}

// blockForReview puts PRD 0 into the blocked held state BECAUSE of a
// review-hold task: the automaton is blocked and one of its tasks is at
// TaskReviewNeeded, so the blocked question's retry/change-spec/cancel route
// through the review path.
func (h *heldHarness) blockForReview(i int, reason string) {
	h.t.Helper()
	id := h.addTask(i)
	h.setStatus(i, PRDBlocked)
	h.setPRD(i, func(p *PRD) { p.BlockedReason = reason })
	h.saveTask(i, func(tk *Task) { tk.Status = TaskReviewNeeded; tk.Error = "build failed on the edge case" })
	_ = id
}

// awaitingSetup parks PRD 0's task waiting on its decision.
func awaitingSetup(h *heldHarness) {
	h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskWaitingDecision
		tk.WaitReason = AwaitDecisionWaitReason
	})
}

// bootPark puts PRD i into the blocked held state under a boot-park
// (a boot_resume_parked decision), so resume has a daemon path.
func (h *heldHarness) bootPark(i int) {
	h.t.Helper()
	h.setPRD(i, func(p *PRD) {
		p.Status = PRDBlocked
		p.BlockedReason = "waiting for the boot resume"
		p.Decisions = append(p.Decisions, Decision{Kind: bootResumeParkedKind, Note: "boot parked"})
	})
}

// dropPendingInsert removes the named pending insert from the automation, so
// an approve-insert can go stale.
func (h *heldHarness) dropPendingInsert(automationID, pendingID string) {
	h.t.Helper()
	if _, err := h.m.automations.Update(automationID, 0, func(a *Automation) error {
		var keep []PendingInsert
		for _, pi := range a.PendingInserts {
			if pi.ID != pendingID {
				keep = append(keep, pi)
			}
		}
		a.PendingInserts = keep
		return nil
	}); err != nil {
		h.t.Fatal(err)
	}
}

// pendingInsertStillThere reports whether the named pending insert is still in
// the automation's pending list.
func (h *heldHarness) pendingInsertStillThere(t *testing.T, automationID, pendingID string) bool {
	t.Helper()
	a, ok := h.m.automations.Get(automationID)
	if !ok {
		t.Fatalf("automation %s missing", automationID)
	}
	return findPending(a, pendingID) != nil
}

// ---- fakes -----------------------------------------------------------------

// heldAnswerFakes records every existing-path call a routed answer makes, so
// the tests can assert the option ran through the right path with the right
// arguments — without spinning up real review, control or resume machinery.
// It is installed behind Manager.heldAnswerPaths for the duration of a test.
type heldAnswerFakes struct {
	mu        sync.Mutex
	resolve   []ReviewResolution
	approves  []string // notes passed to approve
	revisions []string // notes/specs passed to request-revision
	resume    []string // notes passed to resume-parked
	cancels   []string // reasons passed to cancel-task
	inserts   []string // pending ids passed to approve-insert
	releases  int      // awaiting-decision releases
	withdraws []string // reasons passed to withdraw
}

func (f *heldAnswerFakes) install() *heldAnswerPaths {
	return &heldAnswerPaths{
		resolveReview: func(prdID, taskID string, res ReviewResolution, actor string) (*PRD, error) {
			f.mu.Lock()
			f.resolve = append(f.resolve, res)
			f.mu.Unlock()
			return nil, nil
		},
		approve: func(prdID, actor, note string) (*PRD, error) {
			f.mu.Lock()
			f.approves = append(f.approves, note)
			f.mu.Unlock()
			return nil, nil
		},
		requestRevision: func(prdID, actor, note string) (*PRD, error) {
			f.mu.Lock()
			f.revisions = append(f.revisions, note)
			f.mu.Unlock()
			return nil, nil
		},
		resumeParked: func(prdID, actor, note string) (*PRD, error) {
			f.mu.Lock()
			f.resume = append(f.resume, note)
			f.mu.Unlock()
			return nil, nil
		},
		cancelTask: func(prdID, taskID, actor, reason string) (*PRD, error) {
			f.mu.Lock()
			f.cancels = append(f.cancels, reason)
			f.mu.Unlock()
			return nil, nil
		},
		approveInsert: func(automationID, pendingID, actor string, expectedVersion int64) (*Automation, error) {
			f.mu.Lock()
			f.inserts = append(f.inserts, pendingID)
			f.mu.Unlock()
			return nil, nil
		},
		releaseAwaiting: func(ctx context.Context, p *PRD, t *Task, q Question, ans *Answer) error {
			f.mu.Lock()
			f.releases++
			f.mu.Unlock()
			return nil
		},
		withdraw: func(questionID, actor, reason string) error {
			f.mu.Lock()
			f.withdraws = append(f.withdraws, reason)
			f.mu.Unlock()
			return nil
		},
	}
}

// useFakes installs a fresh fake table on the manager and returns it, so a
// routed answer's dispatch calls land in the fakes instead of the real
// review / control / resume paths.
func (h *heldHarness) useFakes() *heldAnswerFakes {
	h.t.Helper()
	f := &heldAnswerFakes{}
	h.m.heldAnswerPaths = f.install()
	return f
}

func (f *heldAnswerFakes) resolveCalls() []ReviewResolution {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ReviewResolution(nil), f.resolve...)
}

func (f *heldAnswerFakes) approveCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.approves...)
}

func (f *heldAnswerFakes) revisionCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.revisions...)
}

func (f *heldAnswerFakes) resumeCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.resume...)
}

func (f *heldAnswerFakes) cancelCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancels...)
}

func (f *heldAnswerFakes) insertCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.inserts...)
}

func (f *heldAnswerFakes) releaseCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases
}

func (f *heldAnswerFakes) withdrawCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.withdraws...)
}

// anyAction is true once a dispatch (review, control, resume, insert, release)
// path has fired — used to assert that a refused / stale / recorded answer ran
// none of them. Withdraws do not count as an action; they close the question.
func (f *heldAnswerFakes) anyAction() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.resolve)+len(f.approves)+len(f.revisions)+len(f.resume)+len(f.cancels)+len(f.inserts)+f.releases > 0
}

// ---- tests -----------------------------------------------------------------

// ---- Answer: applied outcomes (each option runs its existing path) ---------

func TestHeldQuestionActionBlocked(t *testing.T) {
	t.Run("boot_park_resume_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.bootPark(0)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionResume).ID, HeldOptionResume, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.resumeCalls()); n != 1 {
			t.Fatalf("boot-park resume ran %d time(s), want exactly 1 through the manager path", n)
		}
		if f.anyAction() && len(f.resumeCalls()) != 1 {
			t.Fatalf("unexpected path calls: resolve=%d approve=%d rev=%d resume=%d cancel=%d insert=%d release=%d",
				len(f.resolveCalls()), len(f.approveCalls()), len(f.revisionCalls()), len(f.resumeCalls()),
				len(f.cancelCalls()), len(f.insertCalls()), f.releaseCalls())
		}
	})

	t.Run("review_block_retry_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.blockForReview(0, "review hold blocked the run")
		h.setMode("on")
		h.setStates([]string{"blocked"})
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionRetry).ID, HeldOptionRetry, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.resolveCalls()); n != 1 {
			t.Fatalf("review-blocked retry ran %d resolve call(s), want exactly 1", n)
		}
		if got := f.resolveCalls()[0].Action; got != reviewActionRetry {
			t.Fatalf("retry resolution Action = %q, want %q", got, reviewActionRetry)
		}
	})

	t.Run("review_block_change_spec_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.blockForReview(0, "review hold blocked the run")
		h.setMode("on")
		h.setStates([]string{"blocked"})
		h.c.heldQuestionPass()
		spec := "spec v2: add the retry guard"
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, spec)
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.resolveCalls()); n != 1 {
			t.Fatalf("review-blocked change-spec ran %d resolve call(s), want exactly 1", n)
		}
		if got := f.resolveCalls()[0].Spec; got != spec {
			t.Fatalf("change-spec resolution Spec = %q, want %q", got, spec)
		}
	})

	t.Run("review_block_cancel_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.blockForReview(0, "review hold blocked the run")
		h.setMode("on")
		h.setStates([]string{"blocked"})
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionCancel).ID, HeldOptionCancel, "not worth another try")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.resolveCalls()); n != 1 {
			t.Fatalf("review-blocked cancel ran %d resolve call(s), want exactly 1", n)
		}
		if got := f.resolveCalls()[0].Action; got != reviewActionCancel {
			t.Fatalf("cancel resolution Action = %q, want %q", got, reviewActionCancel)
		}
	})
}

func TestHeldQuestionActionReviewHold(t *testing.T) {
	t.Run("retry_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionRetry).ID, HeldOptionRetry, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.resolveCalls()); n != 1 {
			t.Fatalf("retry ran %d resolve call(s), want exactly 1", n)
		}
		if got := f.resolveCalls()[0].Action; got != reviewActionRetry {
			t.Fatalf("retry resolution Action = %q, want %q", got, reviewActionRetry)
		}
	})

	t.Run("retry_stronger_applied_effort_raised", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionRetryStronger).ID, HeldOptionRetryStronger, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.resolveCalls()); n != 1 {
			t.Fatalf("retry-stronger ran %d resolve call(s), want exactly 1", n)
		}
		// From an unset task effort, one rung up is Quick.
		if got := f.resolveCalls()[0].Effort; got != EffortQuick {
			t.Fatalf("retry-stronger resolution Effort = %q, want %q (one rung up from the floor)", got, EffortQuick)
		}
	})

	t.Run("change_spec_applied_carries_spec", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		spec := "spec v2: add the retry guard"
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, spec)
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.resolveCalls()); n != 1 {
			t.Fatalf("change-spec ran %d resolve call(s), want exactly 1", n)
		}
		if got := f.resolveCalls()[0].Spec; got != spec {
			t.Fatalf("change-spec resolution Spec = %q, want %q", got, spec)
		}
	})

	t.Run("cancel_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionCancel).ID, HeldOptionCancel, "not worth another try")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.resolveCalls()); n != 1 {
			t.Fatalf("cancel ran %d resolve call(s), want exactly 1", n)
		}
		if got := f.resolveCalls()[0].Action; got != reviewActionCancel {
			t.Fatalf("cancel resolution Action = %q, want %q", got, reviewActionCancel)
		}
	})
}

func TestHeldQuestionActionRevision(t *testing.T) {
	t.Run("resume_applied_via_approve", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.revisionsAsked(0, "the draft is too thin")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionResume).ID, HeldOptionResume, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.approveCalls()); n != 1 {
			t.Fatalf("revision resume ran %d approve call(s), want exactly 1", n)
		}
		if len(f.revisionCalls()) != 0 {
			t.Fatal("revision resume must not request another revision")
		}
	})

	t.Run("change_spec_applied_carries_spec", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.revisionsAsked(0, "the draft is too thin")
		h.setMode("on")
		h.c.heldQuestionPass()
		spec := "drive the retry ladder from the policy"
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, spec)
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.revisionCalls()); n != 1 {
			t.Fatalf("revision change-spec ran %d request-revision call(s), want exactly 1", n)
		}
		if got := f.revisionCalls()[0]; got != spec {
			t.Fatalf("revision change-spec note = %q, want the operator's spec %q", got, spec)
		}
		if len(f.approveCalls()) != 0 {
			t.Fatal("revision change-spec must not approve the revision hold")
		}
	})
}

func TestHeldQuestionActionAwaiting(t *testing.T) {
	t.Run("resume_applied_via_release", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		awaitingSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionResume).ID, HeldOptionResume, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := f.releaseCalls(); n != 1 {
			t.Fatalf("awaiting-decision resume ran %d release call(s), want exactly 1", n)
		}
		if len(f.cancelCalls()) != 0 {
			t.Fatal("awaiting-decision resume must not cancel the task")
		}
	})

	t.Run("cancel_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		awaitingSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionCancel).ID, HeldOptionCancel, "no")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if n := len(f.cancelCalls()); n != 1 {
			t.Fatalf("awaiting-decision cancel ran %d cancel call(s), want exactly 1", n)
		}
		if f.releaseCalls() != 0 {
			t.Fatal("awaiting-decision cancel must not release the task")
		}
	})
}

func TestHeldQuestionActionPending(t *testing.T) {
	t.Run("approve_insert_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 2)
		f := h.useFakes()
		a := h.makeAuto(nil, 1)
		h.addPendingInsert(a, h.prds[1].ID, "ins1")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, r := h.answerHeld(t, h.oneHeld(t, HeldOptionApproveInsert).ID, HeldOptionApproveInsert, "")
		h.heldAnswerOutcome(t, q, r, heldResultApplied)
		if n := len(f.insertCalls()); n != 1 {
			t.Fatalf("pending approve ran %d approve-insert call(s), want exactly 1", n)
		}
		if got := f.insertCalls()[0]; got != "ins1" {
			t.Fatalf("approve-insert pending id = %q, want %q", got, "ins1")
		}
	})
}

// ---- Answer: recorded outcomes (hold, free text, reject-insert, idempotence)

func TestHeldQuestionAnswerHold(t *testing.T) {
	t.Run("blocked_hold_recorded_stays_blocked", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.block(0, "waiting for operator approval")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, a, heldResultRecorded)
		if s := h.prdStatus(t, 0); s != PRDBlocked {
			t.Fatalf("hold leaves the block in place; want blocked, got %q", s)
		}
		if f.anyAction() {
			t.Fatal("a hold must run no other path")
		}
	})

	t.Run("review_hold_recorded_stays_held", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, a, heldResultRecorded)
		if k := h.taskOf(t, h.prds[0].ID); k.Status != TaskReviewNeeded {
			t.Fatalf("hold leaves the review-hold in place; got task %q", k.Status)
		}
		if f.anyAction() {
			t.Fatal("a review-hold hold must run no other path")
		}
	})

	t.Run("revision_hold_recorded_stays_asked", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.revisionsAsked(0, "the draft is too thin")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, a, heldResultRecorded)
		before := h.prdRevisions(t, 0)
		if s := h.prdStatus(t, 0); s != PRDRevisionsAsked {
			t.Fatalf("hold leaves the revision hold in place; want revisions_asked, got %q", s)
		}
		if got := h.prdRevisions(t, 0); got != before {
			t.Fatalf("hold must not change the revision count; before=%d after=%d", before, got)
		}
		if f.anyAction() {
			t.Fatal("a revision hold must run no other path")
		}
	})

	t.Run("awaiting_hold_recorded_stays_parked", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		awaitingSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, a, heldResultRecorded)
		if k := h.taskOf(t, h.prds[0].ID); k.Status != TaskWaitingDecision {
			t.Fatalf("hold leaves the decision-park in place; got task %q", k.Status)
		}
		if f.anyAction() {
			t.Fatal("an awaiting-decision hold must run no other path")
		}
	})
}

func TestHeldQuestionAnswerPendingRecorded(t *testing.T) {
	t.Run("hold_recorded_insert_stays_pending", func(t *testing.T) {
		h := newAnswerHarness(t, 2)
		f := h.useFakes()
		a := h.makeAuto(nil, 1)
		h.addPendingInsert(a, h.prds[1].ID, "ins1")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, r := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, r, heldResultRecorded)
		if !h.pendingInsertStillThere(t, a.ID, "ins1") {
			t.Fatal("a hold leaves the pending insert in place")
		}
		if f.anyAction() {
			t.Fatal("a pending-insert hold must run no other path")
		}
	})

	t.Run("reject_recorded_insert_stays_pending", func(t *testing.T) {
		// Reject-insert is record-only: there is no daemon path to drop a
		// pending member, so the insert stays pending either way.
		h := newAnswerHarness(t, 2)
		f := h.useFakes()
		a := h.makeAuto(nil, 1)
		h.addPendingInsert(a, h.prds[1].ID, "ins1")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, r := h.answerHeld(t, h.oneHeld(t, HeldOptionRejectInsert).ID, HeldOptionRejectInsert, "remove it")
		h.heldAnswerOutcome(t, q, r, heldResultRecorded)
		if !h.pendingInsertStillThere(t, a.ID, "ins1") {
			t.Fatal("a reject-insert must not drop the pending insert")
		}
		if n := len(f.insertCalls()); n != 0 {
			t.Fatalf("a reject-insert must not run the approve-insert path; ran %d", n)
		}
	})
}

func TestHeldQuestionAnswerFreeText(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	h.block(0, "waiting for operator approval")
	h.setMode("on")
	h.c.heldQuestionPass()
	q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, "", "I'll look at it tomorrow")
	h.heldAnswerOutcome(t, q, a, heldResultRecorded)
	if s := h.prdStatus(t, 0); s != PRDBlocked {
		t.Fatalf("free text leaves the block in place; want blocked, got %q", s)
	}
	if f.anyAction() {
		t.Fatal("a free-text answer carries no option and must run no path")
	}
}

func TestHeldQuestionAnswerIdempotent(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	h.bootPark(0)
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionResume)
	// First answer: the action runs once and is recorded.
	_, a := h.storeAnswer(t, q.ID, HeldOptionResume, "")
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultApplied)
	firstResume := len(f.resumeCalls())
	first := h.countHeldAnswers(t, q, a)
	if first != 1 {
		t.Fatalf("first routing recorded %d held answer(s), want exactly 1", first)
	}
	if firstResume != 1 {
		t.Fatalf("first routing ran resume %d time(s), want exactly 1", firstResume)
	}
	// Same (question, answer) again: nothing runs and nothing is re-recorded.
	h.processHeld(t, q, a)
	if got := len(f.resumeCalls()); got != firstResume {
		t.Fatalf("rerouting re-ran resume: first=%d after=%d", firstResume, got)
	}
	if got := h.countHeldAnswers(t, q, a); got != first {
		t.Fatalf("rerouting re-recorded the held answer: first=%d after=%d", first, got)
	}
}

func TestHeldQuestionAnswerNotHeld(t *testing.T) {
	h := newAnswerHarness(t, 1)
	h.setMode("on")
	open := h.openHeld()
	if len(open) != 0 {
		t.Fatalf("expected no open held questions, got %d", len(open))
	}
	q, _ := h.qs.Get("no-such-held-question")
	if err := h.m.ProcessHeldAnswer(context.Background(), q, Answer{}); err == nil {
		t.Fatal("expected refusing a held-answer route for a non-held question")
	} else if !strings.Contains(err.Error(), "held question") {
		t.Fatalf("error should name the held question; got %q", err.Error())
	}
}

func TestHeldQuestionAnswerContent(t *testing.T) {
	t.Run("applied_record_carries_quote_actor_surface", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		h.useFakes()
		h.revisionsAsked(0, "the draft is too thin")
		h.setMode("on")
		h.c.heldQuestionPass()
		q := h.oneHeld(t, HeldOptionChangeSpec)
		spec := "drive the retry ladder from the policy"
		_, a := h.storeAnswerFull(t, q.ID, Answer{
			ChosenOptionID: HeldOptionChangeSpec,
			Quote:          spec,
			Actor:          "ops-bob",
			Surface:        "telegram",
		})
		h.processHeld(t, q, a)
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		rec := h.heldDecisionRecord(t, h.prds[0].ID, q.ID, a.ID)
		if rec.Quote != spec {
			t.Fatalf("record Quote = %q, want the operator's spec %q", rec.Quote, spec)
		}
		if rec.Actor != "ops-bob" {
			t.Fatalf("record Actor = %q, want the answer's actor %q", rec.Actor, "ops-bob")
		}
		if rec.Surface != "telegram" {
			t.Fatalf("record Surface = %q, want the answer's surface %q", rec.Surface, "telegram")
		}
		if rec.Option != HeldOptionChangeSpec {
			t.Fatalf("record Option = %q, want %q", rec.Option, HeldOptionChangeSpec)
		}
		if rec.Result != heldResultApplied {
			t.Fatalf("record Result = %q, want %q", rec.Result, heldResultApplied)
		}
	})

	t.Run("stale_record_carries_quote_actor_surface_state", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		h.useFakes()
		h.block(0, "waiting for operator approval")
		h.setMode("on")
		h.c.heldQuestionPass()
		q := h.oneHeld(t, HeldOptionResume)
		h.clearBlocked(0)
		_, a := h.storeAnswerFull(t, q.ID, Answer{
			ChosenOptionID: HeldOptionResume,
			Actor:          "ops-bob",
			Surface:        "console",
		})
		if err := h.processHeldErr(t, q, a); !errors.Is(err, ErrHeldStateChanged) {
			t.Fatalf("stale answer error = %v, want ErrHeldStateChanged", err)
		}
		rec := h.heldDecisionRecord(t, h.prds[0].ID, q.ID, a.ID)
		if rec.Actor != "ops-bob" || rec.Surface != "console" {
			t.Fatalf("stale record Actor/Surface = %q/%q, want the answer's %q/%q", rec.Actor, rec.Surface, "ops-bob", "console")
		}
		if rec.Result != heldResultStale {
			t.Fatalf("stale record Result = %q, want %q", rec.Result, heldResultStale)
		}
		if rec.DecisionState != "withdrawn" {
			t.Fatalf("stale record DecisionState = %q, want %q", rec.DecisionState, "withdrawn")
		}
	})
}

// ---- Answer: refusal reasons (refused outcomes name the state) -------------

func TestHeldQuestionRefusedBlocked(t *testing.T) {
	t.Run("resume_refused_no_park", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.block(0, "waiting for operator approval")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionResume).ID, HeldOptionResume, "")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if s := h.prdStatus(t, 0); s != PRDBlocked {
			t.Fatalf("refused resume leaves the block in place; want blocked, got %q", s)
		}
		if f.anyAction() {
			t.Fatal("a refused resume must run no path")
		}
	})

	t.Run("retry_refused_no_review_task", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.block(0, "waiting for operator approval")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionRetry).ID, HeldOptionRetry, "")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if f.anyAction() {
			t.Fatal("a refused retry with no review task must run no path")
		}
	})

	t.Run("change_spec_refused_no_review_task", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.block(0, "waiting for operator approval")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, "some revised spec")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if f.anyAction() {
			t.Fatal("a refused change-spec with no review task must run no path")
		}
	})

	t.Run("cancel_refused_no_review_task", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.block(0, "waiting for operator approval")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionCancel).ID, HeldOptionCancel, "drop it")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if f.anyAction() {
			t.Fatal("a refused cancel with no review task must run no path")
		}
	})
}

func TestHeldQuestionRefusedReviewHold(t *testing.T) {
	t.Run("change_spec_no_text_refused", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, "   ")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if k := h.taskOf(t, h.prds[0].ID); k.Status != TaskReviewNeeded {
			t.Fatalf("a change-spec with no text must not move the task; got %q", k.Status)
		}
		if n := len(f.resolveCalls()); n != 0 {
			t.Fatalf("a refused change-spec must not run the review path; ran %d", n)
		}
	})
}

func TestHeldQuestionRefusedRevision(t *testing.T) {
	t.Run("change_spec_no_text_refused", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		f := h.useFakes()
		h.revisionsAsked(0, "the draft is too thin")
		h.setMode("on")
		h.c.heldQuestionPass()
		before := h.prdRevisions(t, 0)
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, "  ")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if got := h.prdRevisions(t, 0); got != before {
			t.Fatalf("a change-spec with no text must not request a revision; before=%d after=%d", before, got)
		}
		if n := len(f.revisionCalls()); n != 0 {
			t.Fatalf("a refused change-spec must not run the revision path; ran %d", n)
		}
		if len(f.approveCalls()) != 0 {
			t.Fatal("a refused change-spec must not approve the revision hold")
		}
	})
}

func TestHeldQuestionAnswerRoutedDespiteSubmitHook(t *testing.T) {
	// Regression for the wired-daemon bug: the daemon answers the question
	// through the store, whose change hook (main.go: SetChangeHook →
	// RecordQuestionChange) writes an operator_answer entry for the question
	// and answer at SUBMIT time. If the idempotence probe matches that
	// entry, routeHeldAnswer never runs: the action never happens and the
	// question is re-raised every pass. The probe must match only the
	// ROUTED record (the one with a Result), so routing runs even though
	// the hook's raw submission entry is already on the PRD's decisions.
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	h.qs.SetChangeHook(h.m.RecordQuestionChange) // the daemon's wiring
	h.bootPark(0)
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionResume)
	_, a := h.storeAnswer(t, q.ID, HeldOptionResume, "")
	if err := h.m.ProcessHeldAnswer(context.Background(), q, a); err != nil {
		t.Fatalf("process held answer %q with the submit hook wired: %v", q.ID, err)
	}
	h.heldAnswerOutcome(t, q, a, heldResultApplied)
	if n := len(f.resumeCalls()); n != 1 {
		t.Fatalf("submit-time hook entry blocked routing: resume ran %d time(s), want exactly 1", n)
	}
	// Exactly two operator_answer entries now: the hook's raw submission
	// (no Result) and the routed record (Result set).
	p, ok := h.m.store.GetPRD(h.prds[0].ID)
	if !ok {
		t.Fatalf("prd missing")
	}
	hookEntries, routedEntries := 0, 0
	for i := range p.Decisions {
		d := &p.Decisions[i]
		if d.Kind != heldAnswerDecisionKind || d.QuestionID != q.ID || d.AnswerID != a.ID {
			continue
		}
		if d.Result == "" {
			hookEntries++
		} else {
			routedEntries++
		}
	}
	if hookEntries != 1 {
		t.Fatalf("want exactly 1 submit-time operator_answer entry, got %d", hookEntries)
	}
	if routedEntries != 1 {
		t.Fatalf("want exactly 1 routed operator_answer entry, got %d", routedEntries)
	}
	// Idempotence still holds: the routed record (with Result) is what the
	// probe matches, so a second routing adds nothing and runs nothing.
	h.processHeld(t, q, a)
	if got := len(f.resumeCalls()); got != 1 {
		t.Fatalf("rerouting re-ran resume: after=%d, want 1", got)
	}
}

// ---- Answer: stale outcomes (nothing runs; ErrHeldStateChanged; withdrawn) --

// processStale routes a q+a whose held state has moved, asserting the typed
// error and the recorded detail naming the raised state and the current one.
// It returns the recorded detail so the caller can re-use it (it is also the
// reason the question was withdrawn with).
func (h *heldHarness) processStale(t *testing.T, q Question, a Answer, wantFragments ...string) string {
	t.Helper()
	err := h.m.ProcessHeldAnswer(context.Background(), q, a)
	if !errors.Is(err, ErrHeldStateChanged) {
		t.Fatalf("ProcessHeldAnswer error = %v, want ErrHeldStateChanged", err)
	}
	detail := h.heldAnswerDetail(t, q, a)
	if !strings.Contains(detail, "changed") {
		t.Fatalf("stale detail %q should say the held state changed", detail)
	}
	for _, frag := range wantFragments {
		if !strings.Contains(detail, frag) {
			t.Fatalf("stale detail %q must name %q (old and current state)", detail, frag)
		}
	}
	return detail
}

func TestHeldQuestionStaleBlocked(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	h.block(0, "waiting for operator approval")
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionResume)
	// The block clears before the operator's answer arrives.
	h.clearBlocked(0)
	_, a := h.storeAnswer(t, q.ID, HeldOptionResume, "")
	detail := h.processStale(t, q, a, "blocked", "approved")
	if w := f.withdrawCalls(); len(w) != 1 || w[0] != detail {
		t.Fatalf("stale blocked resume must withdraw once with the reason; withdraws=%q want %q", w, detail)
	}
	if len(f.resumeCalls()) != 0 {
		t.Fatal("a stale answer must not run the resume path")
	}
	rec := h.heldDecisionRecord(t, h.prds[0].ID, q.ID, a.ID)
	if rec.DecisionState != "withdrawn" {
		t.Fatalf("stale record DecisionState = %q, want %q", rec.DecisionState, "withdrawn")
	}
	if s := h.prdStatus(t, 0); s == PRDBlocked {
		t.Fatal("state was cleared; the answer must not re-apply a blocked hold")
	}
}

func TestHeldQuestionStaleReviewHold(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	reviewHoldSetup(h)
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionRetry)
	// The task leaves the review-hold before the operator's answer arrives.
	h.saveTask(0, func(tk *Task) { tk.Status = TaskCompleted })
	_, a := h.storeAnswer(t, q.ID, HeldOptionRetry, "")
	detail := h.processStale(t, q, a, "review_held", "completed")
	if w := f.withdrawCalls(); len(w) != 1 || w[0] != detail {
		t.Fatalf("stale review retry must withdraw once with the reason; withdraws=%q want %q", w, detail)
	}
	if len(f.resolveCalls()) != 0 {
		t.Fatal("a stale answer must not run the review path")
	}
	rec := h.heldDecisionRecord(t, h.prds[0].ID, q.ID, a.ID)
	if rec.DecisionState != "withdrawn" {
		t.Fatalf("stale record DecisionState = %q, want %q", rec.DecisionState, "withdrawn")
	}
	if k := h.taskOf(t, h.prds[0].ID); k.Status == TaskReviewNeeded {
		t.Fatal("state was cleared; the answer must not re-apply the review-hold")
	}
}

func TestHeldQuestionStaleRevision(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	h.revisionsAsked(0, "the draft is too thin")
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionChangeSpec)
	// The PRD leaves the revision hold before the operator's answer arrives.
	h.setPRD(0, func(p *PRD) {
		p.Status = PRDApproved
		p.RevisionsRequested = 0
		p.RejectionReason = ""
	})
	_, a := h.storeAnswer(t, q.ID, HeldOptionChangeSpec, "spec: tighten the guardrails")
	detail := h.processStale(t, q, a, "revision_requested", "approved")
	if w := f.withdrawCalls(); len(w) != 1 || w[0] != detail {
		t.Fatalf("stale revision answer must withdraw once with the reason; withdraws=%q want %q", w, detail)
	}
	if len(f.revisionCalls()) != 0 || len(f.approveCalls()) != 0 {
		t.Fatal("a stale answer must not run the revision path")
	}
	rec := h.heldDecisionRecord(t, h.prds[0].ID, q.ID, a.ID)
	if rec.DecisionState != "withdrawn" {
		t.Fatalf("stale record DecisionState = %q, want %q", rec.DecisionState, "withdrawn")
	}
	if s := h.prdStatus(t, 0); s == PRDRevisionsAsked {
		t.Fatal("state was cleared; the answer must not re-apply the revision hold")
	}
}

func TestHeldQuestionStaleAwaiting(t *testing.T) {
	h := newAnswerHarness(t, 1)
	f := h.useFakes()
	awaitingSetup(h)
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionResume)
	// The task leaves the decision-park before the operator's answer arrives.
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskCompleted
		tk.WaitReason = ""
	})
	_, a := h.storeAnswer(t, q.ID, HeldOptionResume, "")
	detail := h.processStale(t, q, a, "awaiting_decision", "completed")
	if w := f.withdrawCalls(); len(w) != 1 || w[0] != detail {
		t.Fatalf("stale awaiting resume must withdraw once with the reason; withdraws=%q want %q", w, detail)
	}
	if f.releaseCalls() != 0 {
		t.Fatal("a stale answer must not run the release path")
	}
	rec := h.heldDecisionRecord(t, h.prds[0].ID, q.ID, a.ID)
	if rec.DecisionState != "withdrawn" {
		t.Fatalf("stale record DecisionState = %q, want %q", rec.DecisionState, "withdrawn")
	}
	if k := h.taskOf(t, h.prds[0].ID); k.Status == TaskWaitingDecision {
		t.Fatal("state was cleared; the answer must not re-apply the decision-park")
	}
}

func TestHeldQuestionStalePending(t *testing.T) {
	h := newAnswerHarness(t, 2)
	f := h.useFakes()
	a := h.makeAuto(nil, 1)
	h.addPendingInsert(a, h.prds[1].ID, "ins1")
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionApproveInsert)
	// The pending insert is no longer pending before the operator answers.
	h.dropPendingInsert(a.ID, "ins1")
	_, r := h.storeAnswer(t, q.ID, HeldOptionApproveInsert, "")
	detail := h.processStale(t, q, r, "pending_insert", "no longer pending")
	if w := f.withdrawCalls(); len(w) != 1 || w[0] != detail {
		t.Fatalf("stale pending approve must withdraw once with the reason; withdraws=%q want %q", w, detail)
	}
	if len(f.insertCalls()) != 0 {
		t.Fatal("a stale answer must not run the approve-insert path")
	}
	// For a pending insert the record lands on the member PRD the question
	// names.
	rec := h.heldDecisionRecord(t, h.prds[1].ID, q.ID, r.ID)
	if rec.DecisionState != "withdrawn" {
		t.Fatalf("stale record DecisionState = %q, want %q", rec.DecisionState, "withdrawn")
	}
	if h.memberLanded(t, a.ID, h.prds[1].ID) {
		t.Fatal("a stale pending approve must not merge the member in")
	}
}
