package autonomous

import (
	"context"
	"strings"
	"testing"
)

// The held-question answer path (ProcessHeldAnswer) is the operator's door:
// the state is held, the daemon raised a question, and the operator picked an
// option. Each option routes by the held state:
//
//   blocked            → resume / retry / change-spec / cancel / hold
//   review-hold        → retry / change-spec / cancel / hold
//   revisions-requested→ resume (approve) / change-spec / hold
//   awaiting-decision  → resume / cancel / hold
//   pending-insert     → approve-insert / reject-insert / hold
//
// Every routed answer — applied, refused, hold, free text — records a
// decision on the PRD and an audit event linking question and answer by id.
// A held question answered while its state is already gone is refused (stale).

// ---- helpers ---------------------------------------------------------------

// newAnswerHarness is newHeldHarness with the manager's audit callback wired
// to the harness sink, so a routed answer's recordHeldAudit entry lands in
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
// returns the question plus the stored record. It does NOT route the answer
// — the caller decides when ProcessHeldAnswer runs. Each question is answered
// exactly once, so no Supersedes is needed.
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

// processHeld routes an already-stored q+a through the manager.
func (h *heldHarness) processHeld(t *testing.T, q Question, a Answer) {
	t.Helper()
	if err := h.m.ProcessHeldAnswer(context.Background(), q, a); err != nil {
		t.Fatalf("process held answer %q: %v", q.ID, err)
	}
}

// answerHeld answers the opened question and routes it in one step.
func (h *heldHarness) answerHeld(t *testing.T, questionID, opt, quote string) (Question, Answer) {
	t.Helper()
	q, a := h.storeAnswer(t, questionID, opt, quote)
	h.processHeld(t, q, a)
	return q, a
}

// heldAnswerOutcome asserts the recorded result for this q+a pair. It prints
// the recorded detail on a mismatch so a refused path is self-explaining.
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

// countHeldAnswers counts the recordHeldAnswer entries for this q+a pair.
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

func (h *heldHarness) decideHeld(t *testing.T, prdID, questionID, answerID string) bool {
	t.Helper()
	p, ok := h.m.store.GetPRD(prdID)
	if !ok {
		t.Fatalf("prd %s missing", prdID)
	}
	for _, d := range p.Decisions {
		if d.Kind == heldAnswerDecisionKind && d.QuestionID == questionID && d.AnswerID == answerID {
			return true
		}
	}
	return false
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
	h.saveTask(0, func(tk *Task) { tk.Status = TaskReviewNeeded })
}

// awaitingSetup parks PRD 0's task waiting on its decision.
func awaitingSetup(h *heldHarness) {
	h.addTask(0)
	h.saveTask(0, func(tk *Task) {
		tk.Status = TaskWaitingDecision
		tk.WaitReason = AwaitDecisionWaitReason
	})
}

// ---- tests -----------------------------------------------------------------

func TestHeldQuestion_AnswerBlocked(t *testing.T) {
	t.Run("hold", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		h.block(0, "waiting for operator approval")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, a, heldResultRecorded)
		if s := h.prdStatus(t, 0); s != PRDBlocked {
			t.Fatalf("want still blocked, got %q", s)
		}
	})

	t.Run("resume_refused_no_park", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		h.block(0, "waiting for operator approval")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionResume).ID, HeldOptionResume, "")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if s := h.prdStatus(t, 0); s != PRDBlocked {
			t.Fatalf("want still blocked, got %q", s)
		}
	})
}

func TestHeldQuestion_AnswerBlockedStale(t *testing.T) {
	h := newAnswerHarness(t, 1)
	h.block(0, "waiting for operator approval")
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionResume)
	// The block is cleared before the operator's answer arrives; the fresh
	// answer now routes to a refused, not an apply.
	h.clearBlocked(0)
	_, a := h.storeAnswer(t, q.ID, HeldOptionResume, "")
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultRefused)
	if s := h.prdStatus(t, 0); s == PRDBlocked {
		t.Fatal("state was cleared; the answer must not re-apply a blocked hold")
	}
}

func TestHeldQuestion_AnswerReviewHold(t *testing.T) {
	t.Run("retry_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionRetry).ID, HeldOptionRetry, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		k := h.taskOf(t, h.prds[0].ID)
		if k.Status == TaskReviewNeeded {
			t.Fatal("retry should take the task out of review-hold")
		}
	})

	t.Run("retry_stronger_applies_and_raises_effort", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionRetryStronger).ID, HeldOptionRetryStronger, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		k := h.taskOf(t, h.prds[0].ID)
		if k.Status == TaskReviewNeeded {
			t.Fatal("retry_stronger should take the task out of review-hold")
		}
		if k.Effort != EffortQuick {
			t.Fatalf("task Effort = %q, want %q (one rung up from the floor)", k.Effort, EffortQuick)
		}
	})

	t.Run("change_spec_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, "spec v2: add the retry guard")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
	})

	t.Run("change_spec_no_text_refused", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, "   ")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if k := h.taskOf(t, h.prds[0].ID); k.Status != TaskReviewNeeded {
			t.Fatalf("a change-spec with no text must not move the task; got %q", k.Status)
		}
	})

	t.Run("hold", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, a, heldResultRecorded)
		if k := h.taskOf(t, h.prds[0].ID); k.Status != TaskReviewNeeded {
			t.Fatalf("hold leaves the review-hold in place; got %q", k.Status)
		}
	})

	t.Run("cancel_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		reviewHoldSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionCancel).ID, HeldOptionCancel, "not worth another try")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if k := h.taskOf(t, h.prds[0].ID); k.Status != TaskCancelled {
			t.Fatalf("cancel should cancel the task; got %q", k.Status)
		}
	})
}

func TestHeldQuestion_AnswerReviewHoldStale(t *testing.T) {
	h := newAnswerHarness(t, 1)
	reviewHoldSetup(h)
	h.setMode("on")
	h.c.heldQuestionPass()
	q := h.oneHeld(t, HeldOptionRetry)
	// The task leaves the review-hold before the operator's answer arrives.
	h.saveTask(0, func(tk *Task) { tk.Status = TaskCompleted })
	_, a := h.storeAnswer(t, q.ID, HeldOptionRetry, "")
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultRefused)
	if k := h.taskOf(t, h.prds[0].ID); k.Status == TaskReviewNeeded {
		t.Fatal("state was cleared; the answer must not re-apply the review-hold")
	}
}

func TestHeldQuestion_AnswerRevisions(t *testing.T) {
	t.Run("change_spec_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		h.revisionsAsked(0, "the draft is too thin")
		h.setMode("on")
		h.c.heldQuestionPass()
		before := h.prdRevisions(t, 0)
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, "spec: drive the retry ladder from the policy")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if got := h.prdRevisions(t, 0); got <= before {
			t.Fatalf("RevisionsRequested = %d, want > %d after an applied revision", got, before)
		}
	})

	t.Run("change_spec_no_text_refused", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		h.revisionsAsked(0, "the draft is too thin")
		h.setMode("on")
		h.c.heldQuestionPass()
		before := h.prdRevisions(t, 0)
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionChangeSpec).ID, HeldOptionChangeSpec, "  ")
		h.heldAnswerOutcome(t, q, a, heldResultRefused)
		if got := h.prdRevisions(t, 0); got != before {
			t.Fatalf("a change-spec with no text must not request a revision; RevisionsRequested = %d", got)
		}
	})

	t.Run("hold", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		h.revisionsAsked(0, "the draft is too thin")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, a, heldResultRecorded)
		if s := h.prdStatus(t, 0); s != PRDRevisionsAsked {
			t.Fatalf("hold leaves the revision hold in place; got %q", s)
		}
	})
}

func TestHeldQuestion_AnswerRevisionsStale(t *testing.T) {
	h := newAnswerHarness(t, 1)
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
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultRefused)
	if s := h.prdStatus(t, 0); s == PRDRevisionsAsked {
		t.Fatal("state was cleared; the answer must not re-apply the revision hold")
	}
}

func TestHeldQuestion_AnswerAwaitingDecision(t *testing.T) {
	t.Run("resume_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		awaitingSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionResume).ID, HeldOptionResume, "")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if k := h.taskOf(t, h.prds[0].ID); k.Status == TaskWaitingDecision {
			t.Fatal("resume should release the decision-hold")
		}
	})

	t.Run("cancel_applied", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		awaitingSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionCancel).ID, HeldOptionCancel, "no")
		h.heldAnswerOutcome(t, q, a, heldResultApplied)
		if k := h.taskOf(t, h.prds[0].ID); k.Status != TaskCancelled {
			t.Fatalf("cancel should cancel the task; got %q", k.Status)
		}
	})

	t.Run("hold", func(t *testing.T) {
		h := newAnswerHarness(t, 1)
		awaitingSetup(h)
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
		h.heldAnswerOutcome(t, q, a, heldResultRecorded)
		if k := h.taskOf(t, h.prds[0].ID); k.Status != TaskWaitingDecision {
			t.Fatalf("hold leaves the decision-park in place; got %q", k.Status)
		}
	})
}

func TestHeldQuestion_AnswerAwaitingDecisionStale(t *testing.T) {
	h := newAnswerHarness(t, 1)
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
	h.processHeld(t, q, a)
	h.heldAnswerOutcome(t, q, a, heldResultRefused)
	if k := h.taskOf(t, h.prds[0].ID); k.Status == TaskWaitingDecision {
		t.Fatal("state was cleared; the answer must not re-apply the decision-park")
	}
}

func TestHeldQuestion_AnswerPendingInsert(t *testing.T) {
	t.Run("approve_applied_member_lands", func(t *testing.T) {
		h := newAnswerHarness(t, 2)
		a := h.makeAuto(nil, 1)
		h.addPendingInsert(a, h.prds[1].ID, "ins1")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a2 := h.answerHeld(t, h.oneHeld(t, HeldOptionApproveInsert).ID, HeldOptionApproveInsert, "")
		h.heldAnswerOutcome(t, q, a2, heldResultApplied)
		if !h.memberLanded(t, a.ID, h.prds[1].ID) {
			t.Fatal("approve should merge the pending PRD in as a member")
		}
	})

	t.Run("reject_recorded_insert_stays", func(t *testing.T) {
		h := newAnswerHarness(t, 2)
		a := h.makeAuto(nil, 1)
		h.addPendingInsert(a, h.prds[1].ID, "ins1")
		h.setMode("on")
		h.c.heldQuestionPass()
		q, a2 := h.answerHeld(t, h.oneHeld(t, HeldOptionRejectInsert).ID, HeldOptionRejectInsert, "")
		// Reject is record-only: a pending member cannot be dropped by the
		// daemon, so it stays pending.
		h.heldAnswerOutcome(t, q, a2, heldResultRecorded)
	})
}

func TestHeldQuestion_AnswerFreeText(t *testing.T) {
	h := newAnswerHarness(t, 1)
	h.block(0, "waiting for operator approval")
	h.setMode("on")
	h.c.heldQuestionPass()
	q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, "", "I'll look at it tomorrow")
	h.heldAnswerOutcome(t, q, a, heldResultRecorded)
}

func TestHeldQuestion_AnswerNotHeld(t *testing.T) {
	h := newAnswerHarness(t, 1)
	h.setMode("on")
	h.c.heldQuestionPass() // no held state: no held question raised
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

func TestHeldQuestion_AnswerIdempotent(t *testing.T) {
	h := newAnswerHarness(t, 1)
	h.block(0, "waiting for operator approval")
	h.setMode("on")
	h.c.heldQuestionPass()
	q, a := h.answerHeld(t, h.oneHeld(t, HeldOptionHold).ID, HeldOptionHold, "")
	first := h.countHeldAnswers(t, q, a)
	if first != 1 {
		t.Fatalf("expected one recorded held answer, got %d", first)
	}
	// Re-process the same answer; the same decision must not repeat.
	if err := h.m.ProcessHeldAnswer(context.Background(), q, a); err != nil {
		t.Fatalf("re-process held answer: %v", err)
	}
	if got := h.countHeldAnswers(t, q, a); got != first {
		t.Fatalf("idempotency broken: %d recorded held answers, want %d", got, first)
	}
	if !h.decideHeld(t, h.prds[0].ID, q.ID, a.ID) {
		t.Fatal("the held-answer decision should be present on the PRD")
	}
}
