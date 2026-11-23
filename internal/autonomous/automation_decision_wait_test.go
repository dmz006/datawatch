package autonomous

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// dwHarness is a coordinator over a question store: the fakes are the starter
// (slotStarter moves a started PRD to running like the real API.Run, and records
// every start), the alert and audit sinks of coordHarness, and a real question
// store in a temp dir.
type dwHarness struct {
	*coordHarness
	store *QuestionStore
	st    *slotStarter
}

func newDWHarness(t *testing.T, nPRDs int, hook bool) *dwHarness {
	t.Helper()
	h := &dwHarness{coordHarness: newCoordHarness(t, nPRDs)}
	h.c.markReady()
	h.st = &slotStarter{m: h.m}
	h.c.starter = h.st
	h.m.coordinator = h.c
	h.wireStore(hook)
	return h
}

// wireStore wires a fresh question store into the Manager; with hook set the
// store's change hook is routed to the decision wait handler, as main.go routes
// it through API.OnQuestionChange.
func (h *dwHarness) wireStore(hook bool) {
	h.t.Helper()
	store, err := NewQuestionStore(filepath.Join(h.t.TempDir(), "questions.jsonl"))
	if err != nil {
		h.t.Fatal(err)
	}
	h.store = store
	h.m.SetDecisionGate(store, func() config.AutonomousQuestionsConfig {
		return config.AutonomousQuestionsConfig{Enabled: true}
	})
	if hook {
		store.SetChangeHook(func(c QuestionChange) { h.m.onDecisionWaitChange(c) })
	}
}

// chainGraph builds e0 -> fork f1 over two branches -> join j1 -> p1, and fires
// the fork. Branch one is the chain d1 (decision_wait) -> c1 (PRD 2); branch two
// is b1 (PRD 1). The join's rule is the one given.
func (h *dwHarness) chainGraph(rule string) *Automation {
	h.t.Helper()
	members := []AutomationMember{
		{ID: "e0", PRDID: h.prdID(0), OnComplete: "f1"},
		{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"d1", "b1"}},
		{ID: "d1", Kind: MemberKindDecisionWait, Question: "Ship the migration?", OnComplete: "c1"},
		{ID: "c1", PRDID: h.prdID(2)},
		{ID: "b1", PRDID: h.prdID(1)},
		{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"c1", "b1"}, OnBranchFailure: rule, OnComplete: "p1"},
		{ID: "p1", PRDID: h.prdID(3)},
	}
	a, err := h.m.CreateAutomation(AutomationInput{Name: "dw", EntryMemberID: "e0", Members: members}, "operator")
	if err != nil {
		h.t.Fatalf("CreateAutomation: %v", err)
	}
	a = h.activateEntry(a)
	h.setStatus(0, PRDCompleted)
	h.event(0, PRDCompleted)
	return a
}

// openQuestions lists the open decision wait questions.
func (h *dwHarness) openQuestions() []Question {
	return h.store.List(QuestionFilter{State: QuestionOpen, Asker: DecisionWaitQuestionAsker})
}

// allQuestions lists every decision wait question whatever its state.
func (h *dwHarness) allQuestions() []Question {
	return h.store.List(QuestionFilter{Asker: DecisionWaitQuestionAsker})
}

func (h *dwHarness) answer(qid string) {
	h.t.Helper()
	if _, err := h.store.Answer(qid, Answer{Actor: "operator", ChosenOptionID: DecisionWaitOptionContinue}); err != nil {
		h.t.Fatalf("answer %s: %v", qid, err)
	}
}

// completePRD ends the member PRD in slot i as completed.
func (h *dwHarness) completePRD(i int) {
	h.setStatus(i, PRDCompleted)
	h.event(i, PRDCompleted)
}

func (h *dwHarness) startedPRDs() map[string]bool {
	out := map[string]bool{}
	for _, id := range h.st.started() {
		out[id] = true
	}
	return out
}

// neverBlocked fails the test when the Automation is blocked or a blocked alert
// or audit was raised.
func (h *dwHarness) neverBlocked(a *Automation) {
	h.t.Helper()
	if got := h.get(a); got.Status == AutomationBlocked {
		h.t.Fatalf("Automation is blocked: %+v", got.Decisions)
	}
	if h.alertCount("warn|Automation blocked") != 0 || h.hasAudit("automation_blocked") {
		h.t.Fatalf("a blocked alert or audit was raised: alerts=%v audits=%v", h.alerts, h.audits)
	}
}

func TestDecisionWaitValidation(t *testing.T) {
	valid := func() AutomationMember {
		return AutomationMember{ID: "d1", Kind: MemberKindDecisionWait, Question: "ok?"}
	}
	cases := []struct {
		name string
		edit func(m *AutomationMember)
		want string // "" = valid
	}{
		{"valid", func(m *AutomationMember) {}, ""},
		{"valid with on_complete", func(m *AutomationMember) { m.OnComplete = "z" }, ""},
		{"blank question", func(m *AutomationMember) { m.Question = "  " }, "needs a question"},
		{"question too long", func(m *AutomationMember) { m.Question = strings.Repeat("q", decisionWaitQuestionMaxBytes+1) }, "the cap is"},
		{"prd id", func(m *AutomationMember) { m.PRDID = "prd1" }, "must not carry prd_id"},
		{"fork members", func(m *AutomationMember) { m.ForkMembers = []string{"a", "b"} }, "fork_members is only valid on kind fork"},
		{"join members", func(m *AutomationMember) { m.JoinMembers = []string{"a", "b"} }, "only valid on kind join"},
		{"branch failure", func(m *AutomationMember) { m.OnBranchFailure = BranchAllMustSucceed }, "only valid on kind join"},
		{"max parallel", func(m *AutomationMember) { m.MaxParallel = 2 }, "max_parallel is only valid on kind fork"},
		{"automation id", func(m *AutomationMember) { m.AutomationID = "abc" }, "automation_id is only valid"},
		{"on_blocked", func(m *AutomationMember) { m.OnBlocked = "z" }, "on_complete only"},
		{"on_failed", func(m *AutomationMember) { m.OnFailed = "z" }, "on_complete only"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := valid()
			c.edit(&m)
			err := validateMemberKind(nil, m)
			if err == nil {
				err = validateEdgesOf(m)
			}
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("want valid, got %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}

	// The question fields are valid on no other kind.
	for name, m := range map[string]AutomationMember{
		"prd question":    {ID: "p", PRDID: "x", Question: "?"},
		"prd question id": {ID: "p", PRDID: "x", QuestionID: "q-1"},
		"prd waiting":     {ID: "p", PRDID: "x", WaitingForOperator: true},
		"fork question":   {ID: "f", Kind: MemberKindFork, ForkMembers: []string{"a", "b"}, Question: "?"},
	} {
		if err := validateMemberKind(nil, m); err == nil || !strings.Contains(err.Error(), "only valid on kind decision_wait") {
			t.Errorf("%s: want only-valid-on-decision_wait error, got %v", name, err)
		}
	}
}

// validateEdgesOf runs the one edge rule validateMemberKind leaves to
// validateAutomation: a decision_wait member carries on_complete only.
func validateEdgesOf(m AutomationMember) error {
	return validateDecisionWaitMember(m, m.kind())
}

func TestDecisionWaitIsAValidBranchChainMember(t *testing.T) {
	h := newDWHarness(t, 4, true)
	// chainGraph creates the Automation through the store's validation: a
	// decision_wait chain head, a decision_wait tail and a chain behind one.
	a := h.chainGraph(BranchAllMustSucceed)
	if got := h.get(a); got.Status != AutomationActive {
		t.Fatalf("status = %s", got.Status)
	}
	// As a branch tail, with its own chain head.
	tail := []AutomationMember{
		{ID: "e0", PRDID: h.prdID(0), OnComplete: "f1"},
		{ID: "f1", Kind: MemberKindFork, ForkMembers: []string{"c1", "b1"}},
		{ID: "c1", PRDID: h.prdID(2), OnComplete: "d1"},
		{ID: "d1", Kind: MemberKindDecisionWait, Question: "tail?"},
		{ID: "b1", PRDID: h.prdID(1)},
		{ID: "j1", Kind: MemberKindJoin, JoinMembers: []string{"d1", "b1"}},
	}
	h.prds = h.prds[:3]
	if _, err := h.m.CreateAutomation(AutomationInput{Name: "tail", EntryMemberID: "e0", Members: tail}, "operator"); err == nil || !strings.Contains(err.Error(), "already") {
		// PRDs 0..2 belong to the first Automation: the ownership rule fires, not a kind rule.
		t.Fatalf("want an ownership error (kind accepted), got %v", err)
	}
	// A decision_wait on a branch that carries an outcome edge is refused by
	// the member rule before any graph rule.
	bad := []AutomationMember{
		{ID: "d1", Kind: MemberKindDecisionWait, Question: "x", OnFailed: "d2"},
		{ID: "d2", Kind: MemberKindDecisionWait, Question: "y"},
	}
	if _, err := h.m.CreateAutomation(AutomationInput{Name: "bad", EntryMemberID: "d1", Members: bad}, "operator"); err == nil || !strings.Contains(err.Error(), "on_complete only") {
		t.Fatalf("want on_complete-only error, got %v", err)
	}
}

func TestDecisionWaitQuestionRaisedOnce(t *testing.T) {
	h := newDWHarness(t, 4, true)
	a := h.chainGraph(BranchAllMustSucceed)

	open := h.openQuestions()
	if len(open) != 1 {
		t.Fatalf("open decision wait questions = %d, want 1", len(open))
	}
	q := open[0]
	if !strings.Contains(q.Text, "Ship the migration?") || q.AskerKind != QuestionAskerSystem || !q.Blocking || q.Default != DecisionWaitOptionContinue {
		t.Fatalf("question not built as expected: %+v", q)
	}
	d1 := h.member(a, "d1")
	if d1.Status != MemberRunning || !d1.WaitingForOperator || d1.QuestionID != q.ID || d1.StartedAt == nil {
		t.Fatalf("member not waiting on its question: %+v", d1)
	}
	if !isLiveCursor(h.get(a), "d1") {
		t.Fatalf("a waiting member must hold its cursor: %v", h.get(a).ActiveCursors)
	}

	// Everything that can observe or re-enter the wait leaves one question.
	for i := 0; i < 3; i++ {
		h.c.ensureDecisionWaitQuestion(a.ID, "d1")
		h.c.reconcileDecisionWait(a.ID, "d1")
		h.c.decisionWaitPass()
		h.c.reconcile()
	}
	if n := len(h.allQuestions()); n != 1 {
		t.Fatalf("questions after repeated observation = %d, want 1", n)
	}
	if got := h.member(a, "d1").QuestionID; got != q.ID {
		t.Fatalf("question link changed to %s", got)
	}

	// One alert, and it is the operator-question alert, never the blocked one.
	if n := h.alertCount("warn|Automation waiting for you|"); n != 1 {
		t.Fatalf("waiting-for-you alerts = %d, want 1: %v", n, h.alerts)
	}
	if !strings.Contains(h.alerts[len(h.alerts)-1], q.ID) {
		t.Fatalf("alert does not name the question: %v", h.alerts)
	}
	h.neverBlocked(a)
}

func TestDecisionWaitOtherBranchesAdvanceWhileItWaits(t *testing.T) {
	h := newDWHarness(t, 4, true)
	a := h.chainGraph(BranchAllMustSucceed)
	started := h.startedPRDs()
	if !started[h.prdID(1)] {
		t.Fatalf("the sibling branch b1 did not start: %v", h.st.started())
	}
	if started[h.prdID(2)] {
		t.Fatalf("c1 sits behind the waiting d1 and must not start: %v", h.st.started())
	}

	// The sibling branch finishes while d1 waits: its tail ended, the join waits.
	h.completePRD(1)
	got := h.get(a)
	if b1 := automationMember(got, "b1"); b1.Status != MemberCompleted {
		t.Fatalf("b1 = %s", b1.Status)
	}
	if d1 := automationMember(got, "d1"); !memberWaitingForOperator(d1) {
		t.Fatalf("d1 should still wait: %+v", d1)
	}
	if j1 := automationMember(got, "j1"); j1.Status != MemberRunning {
		t.Fatalf("join must stay pending, got %s", j1.Status)
	}
	if got.Status != AutomationActive {
		t.Fatalf("status = %s", got.Status)
	}
	if h.startedPRDs()[h.prdID(3)] {
		t.Fatal("the member behind the join started before the join fired")
	}
	h.neverBlocked(a)
}

func TestDecisionWaitChainContinuesAfterAnswer(t *testing.T) {
	for _, mode := range []string{"hook", "tick"} {
		t.Run(mode, func(t *testing.T) {
			h := newDWHarness(t, 4, mode == "hook")
			a := h.chainGraph(BranchAllMustSucceed)
			q := h.openQuestions()[0]
			h.completePRD(1) // the sibling branch is done; only d1's chain is left

			h.answer(q.ID)
			if mode == "tick" {
				// No change hook: the coordinator tick reads the answer from the store.
				if d1 := h.member(a, "d1"); !memberWaitingForOperator(&d1) {
					t.Fatalf("without the hook the member waits until the tick: %+v", d1)
				}
				h.c.decisionWaitPass()
			}

			got := h.get(a)
			d1 := automationMember(got, "d1")
			if d1.Status != MemberCompleted || d1.EdgeTaken != edgeOnComplete || d1.Outcome != decisionWaitOutcomeAnswered || d1.WaitingForOperator || d1.FinishedAt == nil {
				t.Fatalf("d1 not completed by the answer: %+v", d1)
			}
			if !h.startedPRDs()[h.prdID(2)] {
				t.Fatalf("the chain did not continue to c1: %v", h.st.started())
			}
			if c1 := automationMember(got, "c1"); c1.Status != MemberRunning {
				t.Fatalf("c1 = %s", c1.Status)
			}
			ans, _ := h.store.CurrentAnswer(q.ID)
			var logged bool
			for _, d := range got.Decisions {
				if d.Kind == decisionWaitDecisionKind && d.QuestionID == q.ID && d.AnswerID == ans.ID && d.DecisionState == string(QuestionAnswered) {
					logged = true
				}
			}
			if !logged {
				t.Fatalf("no decision_wait Decision links the question and answer: %+v", got.Decisions)
			}

			// The rest of the branch ends: the join fires and the Automation goes on.
			h.completePRD(2)
			if !h.startedPRDs()[h.prdID(3)] {
				t.Fatalf("the join did not fire after the chain ended: %v", h.st.started())
			}
			h.completePRD(3)
			if final := h.get(a); final.Status != AutomationCompleted {
				t.Fatalf("status = %s, want completed", final.Status)
			}
			h.neverBlocked(a)
		})
	}
}

func TestDecisionWaitNeverEntersBlockedAndReportsWaitingForYou(t *testing.T) {
	h := newDWHarness(t, 4, true)
	a := h.chainGraph(BranchAllMustSucceed)

	// The sibling branch fails while d1 waits: under all_must_succeed the join
	// stays pending (it fails only once every branch is terminal).
	h.setStatus(1, PRDFailed)
	h.event(1, PRDFailed)
	got := h.get(a)
	if got.Status != AutomationActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
	if j1 := automationMember(got, "j1"); j1.Status != MemberRunning {
		t.Fatalf("join = %s, want still waiting", j1.Status)
	}
	h.neverBlocked(a)

	// The status data says waiting for you, never blocked.
	var sum *AutomationStatusSummary
	for _, s := range h.m.automationStatusSummaries(time.Now()) {
		if s.ID == a.ID {
			s := s
			sum = &s
		}
	}
	if sum == nil {
		t.Fatal("no status summary")
	}
	if sum.Status != string(AutomationActive) || sum.BlockedReason != "" {
		t.Fatalf("summary status=%s blocked_reason=%q", sum.Status, sum.BlockedReason)
	}
	if len(sum.WaitingForOperator) != 1 || sum.WaitingForOperator[0] != "d1" {
		t.Fatalf("waiting_for_operator = %v", sum.WaitingForOperator)
	}
	var cur *ActiveCursorSummary
	for i := range sum.ActiveCursors {
		if sum.ActiveCursors[i].MemberID == "d1" {
			cur = &sum.ActiveCursors[i]
		}
	}
	q := h.openQuestions()[0]
	if cur == nil || cur.Status != CursorStatusWaitingForOperator || cur.WaitReason != "waiting for you" ||
		cur.Kind != MemberKindDecisionWait || cur.QuestionID != q.ID {
		t.Fatalf("cursor summary = %+v", cur)
	}
	if sum.JoinWait == nil || len(sum.JoinWait.PendingBranches) != 1 || sum.JoinWait.PendingBranches[0] != "c1" {
		t.Fatalf("join wait = %+v", sum.JoinWait)
	}
	if got.IdleSince != nil {
		t.Fatalf("a waiting Automation is not idle: %v", got.IdleSince)
	}
	// The idle check never alerts on it.
	h.c.checkIdle()
	if h.alertCount("warn|Automation dw idle") != 0 {
		t.Fatalf("idle alert raised for a waiting member: %v", h.alerts)
	}
}

func TestDecisionWaitWithdrawFailsOnlyThatBranch(t *testing.T) {
	h := newDWHarness(t, 4, true)
	a := h.chainGraph(BranchContinueAnyway)
	q := h.openQuestions()[0]

	if _, err := h.store.Withdraw(q.ID, "operator", "no longer needed"); err != nil {
		t.Fatal(err)
	}
	got := h.get(a)
	d1 := automationMember(got, "d1")
	if d1.Status != MemberFailed || d1.Outcome != decisionWaitOutcomeWithdrawn || d1.WaitingForOperator || d1.EdgeTaken != "" {
		t.Fatalf("d1 not failed by the withdrawal: %+v", d1)
	}
	if c1 := automationMember(got, "c1"); c1.Status != MemberSkipped {
		t.Fatalf("the chain behind the failed member must be skipped, got %s", c1.Status)
	}
	if b1 := automationMember(got, "b1"); b1.Status != MemberRunning {
		t.Fatalf("the other branch must keep running, got %s", b1.Status)
	}
	if got.Status != AutomationActive {
		t.Fatalf("status = %s: a withdrawn question fails the branch, not the Automation", got.Status)
	}
	if h.startedPRDs()[h.prdID(2)] {
		t.Fatal("c1 started behind a failed branch member")
	}
	var reason string
	for _, d := range got.Decisions {
		if d.Kind == decisionWaitDecisionKind && d.DecisionState == string(QuestionWithdrawn) {
			reason = d.Note
		}
	}
	if !strings.Contains(reason, q.ID) || !strings.Contains(reason, "withdrawn") || !strings.Contains(reason, "no longer needed") || !strings.Contains(reason, "branch fails") {
		t.Fatalf("withdraw reason is not clear: %q", reason)
	}
	h.neverBlocked(a)

	// The sibling branch finishes: under continue_anyway the join fires.
	h.completePRD(1)
	if !h.startedPRDs()[h.prdID(3)] {
		t.Fatalf("join did not fire once the surviving branch ended: %v", h.st.started())
	}
}

func TestDecisionWaitWithdrawUnderAllMustSucceedBlocksOnlyAtTheJoin(t *testing.T) {
	h := newDWHarness(t, 4, true)
	a := h.chainGraph(BranchAllMustSucceed)
	q := h.openQuestions()[0]
	if _, err := h.store.Withdraw(q.ID, "operator", "stale"); err != nil {
		t.Fatal(err)
	}
	// The branch failed, but the join still waits for the sibling branch.
	got := h.get(a)
	if got.Status != AutomationActive || automationMember(got, "j1").Status != MemberRunning {
		t.Fatalf("status=%s join=%s: the join must stay pending while a branch runs", got.Status, automationMember(got, "j1").Status)
	}
	h.completePRD(1)
	// Only now, with every branch terminal, does all_must_succeed give up.
	if got := h.get(a); got.Status != AutomationBlocked || !strings.Contains(latestBlockedReason(got.Decisions), "not all branches succeeded") {
		t.Fatalf("status=%s reason=%q", got.Status, latestBlockedReason(got.Decisions))
	}
}

func TestDecisionWaitWithdrawOutsideAForkBlocksWithTheReason(t *testing.T) {
	h := newDWHarness(t, 2, true)
	members := []AutomationMember{
		{ID: "d0", Kind: MemberKindDecisionWait, Question: "Go?", OnComplete: "p0"},
		{ID: "p0", PRDID: h.prdID(0)},
	}
	a, err := h.m.CreateAutomation(AutomationInput{Name: "lin", EntryMemberID: "d0", Members: members}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.ActivateAutomation(a.ID, "operator", 0); err != nil {
		t.Fatalf("activate: %v", err)
	}
	got := h.get(a)
	if got.Status != AutomationActive || !memberWaitingForOperator(automationMember(got, "d0")) {
		t.Fatalf("activation should leave the entry waiting: status=%s %+v", got.Status, automationMember(got, "d0"))
	}
	if len(h.st.started()) != 0 {
		t.Fatalf("nothing may start behind a waiting entry: %v", h.st.started())
	}
	q := h.openQuestions()[0]
	if _, err := h.store.Withdraw(q.ID, "operator", "oops"); err != nil {
		t.Fatal(err)
	}
	got = h.get(a)
	if got.Status != AutomationBlocked || !strings.Contains(latestBlockedReason(got.Decisions), "withdrawn (oops) without an answer; the Automation blocks") {
		t.Fatalf("a withdrawn question with no branch to fail blocks with its reason: status=%s reason=%q", got.Status, latestBlockedReason(got.Decisions))
	}
}

func TestDecisionWaitLinearEntryAnswerRunsTheNextMember(t *testing.T) {
	h := newDWHarness(t, 2, true)
	members := []AutomationMember{
		{ID: "d0", Kind: MemberKindDecisionWait, Question: "Go?", OnComplete: "p0"},
		{ID: "p0", PRDID: h.prdID(0)},
	}
	a, err := h.m.CreateAutomation(AutomationInput{Name: "lin", EntryMemberID: "d0", Members: members}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.ActivateAutomation(a.ID, "operator", 0); err != nil {
		t.Fatal(err)
	}
	h.answer(h.openQuestions()[0].ID)
	got := h.get(a)
	if got.CurrentMemberID != "p0" || got.Status != AutomationActive {
		t.Fatalf("cursor=%s status=%s", got.CurrentMemberID, got.Status)
	}
	if runs := h.st.started(); len(runs) != 1 || runs[0] != h.prdID(0) {
		t.Fatalf("starter runs = %v", runs)
	}
	h.completePRD(0)
	if got := h.get(a); got.Status != AutomationCompleted {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestDecisionWaitPausedAutomationEntersOnResume(t *testing.T) {
	h := newDWHarness(t, 2, true)
	members := []AutomationMember{
		{ID: "p0", PRDID: h.prdID(0), OnComplete: "d1"},
		{ID: "d1", Kind: MemberKindDecisionWait, Question: "Go?"},
	}
	a, err := h.m.CreateAutomation(AutomationInput{Name: "pz", EntryMemberID: "p0", Members: members}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.ActivateAutomation(a.ID, "operator", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.PauseAutomation(a.ID, "operator", h.get(a).Version); err != nil {
		t.Fatal(err)
	}
	h.completePRD(0)
	if got := h.get(a); got.Status != AutomationPaused || len(h.allQuestions()) != 0 {
		t.Fatalf("a paused Automation starts nothing: status=%s questions=%d", got.Status, len(h.allQuestions()))
	}
	if _, err := h.m.ResumeAutomation(a.ID, "operator", h.get(a).Version); err != nil {
		t.Fatal(err)
	}
	got := h.get(a)
	if !memberWaitingForOperator(automationMember(got, "d1")) || len(h.openQuestions()) != 1 {
		t.Fatalf("resume must enter the waiting member and raise its one question: %+v questions=%d", automationMember(got, "d1"), len(h.openQuestions()))
	}
	h.neverBlocked(a)
}

func TestDecisionWaitReconcileAfterRestartKeepsOneOpenQuestion(t *testing.T) {
	restart := func(h *dwHarness) *Coordinator {
		c := NewCoordinator(h.m, h.st)
		c.alertFn = h.c.alertFn
		c.auditFn = h.c.auditFn
		return c
	}

	t.Run("linked question stays linked", func(t *testing.T) {
		h := newDWHarness(t, 4, true)
		a := h.chainGraph(BranchAllMustSucceed)
		q := h.openQuestions()[0]
		restart(h).reconcile()
		if open := h.openQuestions(); len(open) != 1 || open[0].ID != q.ID {
			t.Fatalf("open questions after restart = %+v, want only %s", open, q.ID)
		}
		if got := h.member(a, "d1"); !memberWaitingForOperator(&got) || got.QuestionID != q.ID {
			t.Fatalf("member lost its wait: %+v", got)
		}
		h.neverBlocked(a)
	})

	t.Run("crash between raise and link re-links the same question", func(t *testing.T) {
		h := newDWHarness(t, 4, true)
		a := h.chainGraph(BranchAllMustSucceed)
		q := h.openQuestions()[0]
		if _, err := h.m.automations.Update(a.ID, 0, func(cur *Automation) error {
			automationMember(cur, "d1").QuestionID = ""
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		restart(h).reconcile()
		if n := len(h.allQuestions()); n != 1 {
			t.Fatalf("questions after reconcile = %d, want 1 (no second question)", n)
		}
		if got := h.member(a, "d1").QuestionID; got != q.ID {
			t.Fatalf("re-linked to %q, want %s", got, q.ID)
		}
	})

	t.Run("crash before the raise raises exactly one", func(t *testing.T) {
		h := newDWHarness(t, 4, true)
		a := h.chainGraph(BranchAllMustSucceed)
		h.wireStore(true) // the question never reached the (new, empty) store
		if len(h.allQuestions()) != 0 {
			t.Fatal("store should be empty")
		}
		c := restart(h)
		c.reconcile()
		c.reconcile()
		open := h.openQuestions()
		if len(open) != 1 {
			t.Fatalf("open questions = %d, want 1", len(open))
		}
		if got := h.member(a, "d1").QuestionID; got != open[0].ID {
			t.Fatalf("member links %q, store holds %s", got, open[0].ID)
		}
	})

	t.Run("answered while the daemon was down", func(t *testing.T) {
		h := newDWHarness(t, 4, false) // no change hook: nothing saw the answer
		a := h.chainGraph(BranchAllMustSucceed)
		h.completePRD(1)
		q := h.openQuestions()[0]
		h.answer(q.ID)
		if got := h.member(a, "d1"); !memberWaitingForOperator(&got) {
			t.Fatalf("nothing observed the answer yet: %+v", got)
		}
		restart(h).reconcile()
		got := h.get(a)
		if d1 := automationMember(got, "d1"); d1.Status != MemberCompleted {
			t.Fatalf("reconcile did not complete the answered member: %+v", d1)
		}
		if !h.startedPRDs()[h.prdID(2)] {
			t.Fatalf("the chain did not continue after reconcile: %v", h.st.started())
		}
	})

	t.Run("withdrawn while the daemon was down", func(t *testing.T) {
		h := newDWHarness(t, 4, false)
		a := h.chainGraph(BranchContinueAnyway)
		if _, err := h.store.Withdraw(h.openQuestions()[0].ID, "operator", "gone"); err != nil {
			t.Fatal(err)
		}
		restart(h).reconcile()
		got := h.get(a)
		if d1 := automationMember(got, "d1"); d1.Status != MemberFailed {
			t.Fatalf("reconcile did not fail the withdrawn member: %+v", d1)
		}
		if got.Status != AutomationActive || automationMember(got, "b1").Status != MemberRunning {
			t.Fatalf("the other branch must be untouched: status=%s", got.Status)
		}
	})
}

func TestDecisionWaitChannelUnavailableFailsTheBranch(t *testing.T) {
	h := newDWHarness(t, 4, true)
	// The operator question channel is off: the question could never be seen.
	h.m.SetDecisionGate(h.store, func() config.AutonomousQuestionsConfig { return config.AutonomousQuestionsConfig{Enabled: false} })
	a := h.chainGraph(BranchContinueAnyway)

	got := h.get(a)
	d1 := automationMember(got, "d1")
	if d1.Status != MemberFailed || d1.WaitingForOperator {
		t.Fatalf("a member that cannot ask must fail, not wait unseen: %+v", d1)
	}
	var reason string
	for _, d := range got.Decisions {
		if d.Kind == decisionWaitDecisionKind {
			reason = d.Note
		}
	}
	if !strings.Contains(reason, "operator question channel is not available") {
		t.Fatalf("reason = %q", reason)
	}
	if len(h.allQuestions()) != 0 {
		t.Fatal("no question may be raised on a disabled channel")
	}
	if got.Status != AutomationActive || automationMember(got, "b1").Status != MemberRunning {
		t.Fatalf("the failure ends only its branch: status=%s", got.Status)
	}
}

func TestDecisionWaitSkipAndCancelWithdrawTheQuestion(t *testing.T) {
	t.Run("skip", func(t *testing.T) {
		h := newDWHarness(t, 4, true)
		a := h.chainGraph(BranchAllMustSucceed)
		q := h.openQuestions()[0]
		if _, err := h.m.SkipMember(a.ID, "d1", "", "operator", h.get(a).Version); err != nil {
			t.Fatal(err)
		}
		if got, _ := h.store.Get(q.ID); got.State != QuestionWithdrawn {
			t.Fatalf("question state = %s, want withdrawn after the member was skipped", got.State)
		}
		got := h.get(a)
		if d1 := automationMember(got, "d1"); d1.Status != MemberSkipped || d1.WaitingForOperator {
			t.Fatalf("d1 = %+v", d1)
		}
		if !h.startedPRDs()[h.prdID(2)] {
			t.Fatalf("a skip moves the chain on: %v", h.st.started())
		}
	})
	t.Run("cancel", func(t *testing.T) {
		h := newDWHarness(t, 4, true)
		a := h.chainGraph(BranchAllMustSucceed)
		q := h.openQuestions()[0]
		if _, err := h.m.CancelAutomation(a.ID, "operator", false, true, h.get(a).Version); err != nil {
			t.Fatal(err)
		}
		if got, _ := h.store.Get(q.ID); got.State != QuestionWithdrawn {
			t.Fatalf("question state = %s, want withdrawn after the Automation was cancelled", got.State)
		}
		if d1 := h.member(a, "d1"); d1.Status != MemberCancelled || d1.WaitingForOperator {
			t.Fatalf("d1 = %+v", d1)
		}
	})
}

func TestDecisionWaitJoinThatFiresWithoutItWithdrawsTheQuestion(t *testing.T) {
	h := newDWHarness(t, 4, true)
	a := h.chainGraph(BranchAnyOneIsEnough)
	q := h.openQuestions()[0]
	h.completePRD(1) // the sibling branch wins: the join fires and cancels the waiting one
	if got, _ := h.store.Get(q.ID); got.State != QuestionWithdrawn {
		t.Fatalf("question state = %s, want withdrawn once the join no longer awaits the branch", got.State)
	}
	got := h.get(a)
	if d1 := automationMember(got, "d1"); d1.Status != MemberCancelled || d1.WaitingForOperator {
		t.Fatalf("d1 = %+v", d1)
	}
	if !h.startedPRDs()[h.prdID(3)] {
		t.Fatalf("the join fired and p1 should have started: %v", h.st.started())
	}
	h.neverBlocked(a)
}

func TestDecisionWaitSweepWithdrawsOnlyOrphanedOldQuestions(t *testing.T) {
	h := newDWHarness(t, 4, true)
	a := h.chainGraph(BranchAllMustSucceed)
	linked := h.openQuestions()[0]

	raise := func(text string, age time.Duration) Question {
		now := time.Now().Add(-age)
		h.store.now = func() time.Time { return now }
		defer func() { h.store.now = time.Now }()
		q, err := h.store.Raise(Question{
			Label: DecisionWaitQuestionLabel, AskedBy: DecisionWaitQuestionAsker, AskerKind: QuestionAskerSystem,
			Context: QuestionContextNone, Text: text, Options: []QuestionOption{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}}, Default: "a",
		})
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	old := raise("orphan", time.Hour)
	young := raise("fresh orphan", 10*time.Second)

	h.c.decisionWaitPass()
	state := func(id string) QuestionState { q, _ := h.store.Get(id); return q.State }
	if state(old.ID) != QuestionWithdrawn {
		t.Fatalf("old orphan state = %s, want withdrawn", state(old.ID))
	}
	if state(young.ID) != QuestionOpen {
		t.Fatalf("a question inside the grace period must be left alone, got %s", state(young.ID))
	}
	if state(linked.ID) != QuestionOpen || !memberWaitingForOperator(automationMember(h.get(a), "d1")) {
		t.Fatalf("the linked question must be untouched: %s", state(linked.ID))
	}
}

func TestDecisionWaitChangeHookRouting(t *testing.T) {
	h := newDWHarness(t, 4, false)
	foreign := QuestionChange{Kind: QuestionChangeAnswered, Question: Question{ID: "q-1", AskedBy: "someone-else", AskerKind: QuestionAskerSystem}}
	if h.m.onDecisionWaitChange(foreign) {
		t.Fatal("a question that is not a decision wait question must fall through to the other handlers")
	}
	ours := Question{ID: "q-2", AskedBy: DecisionWaitQuestionAsker, AskerKind: QuestionAskerSystem}
	for _, kind := range []string{QuestionChangeRaised, QuestionChangeUpdated, QuestionChangeAnswered, QuestionChangeWithdrawn, QuestionChangeExpired} {
		if !h.m.onDecisionWaitChange(QuestionChange{Kind: kind, Question: ours}) {
			t.Fatalf("a decision wait question change (%s) must be consumed", kind)
		}
	}
	// With no coordinator running the change is consumed and does nothing.
	h.m.coordinator = nil
	if !h.m.onDecisionWaitChange(QuestionChange{Kind: QuestionChangeAnswered, Question: ours}) {
		t.Fatal("consumed without a coordinator")
	}
}

func TestDecisionWaitStatusOfEndedMemberIsNotWaiting(t *testing.T) {
	m := &AutomationMember{ID: "d", Kind: MemberKindDecisionWait, Question: "?", WaitingForOperator: true, Status: MemberCompleted}
	if memberWaitingForOperator(m) {
		t.Fatal("an ended member is never waiting, whatever its stored flag says")
	}
	if memberWaitingForOperator(nil) || memberWaitingForOperator(&AutomationMember{ID: "p", PRDID: "x", Status: MemberRunning}) {
		t.Fatal("only a running decision_wait member waits for the operator")
	}
}

func TestDecisionWaitRuntimeFieldsCannotBeSuppliedOnCreateOrUpdate(t *testing.T) {
	h := newDWHarness(t, 2, true)
	members := []AutomationMember{
		{ID: "d0", Kind: MemberKindDecisionWait, Question: "Go?", QuestionID: "q-injected", WaitingForOperator: true, OnComplete: "p0"},
		{ID: "p0", PRDID: h.prdID(0)},
	}
	a, err := h.m.CreateAutomation(AutomationInput{Name: "inj", EntryMemberID: "d0", Members: members}, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if d0 := automationMember(h.get(a), "d0"); d0.QuestionID != "" || d0.WaitingForOperator {
		t.Fatalf("create kept caller-supplied runtime state: %+v", d0)
	}
	// An update of the draft is sanitised the same way, and keeps the question text.
	members[0].Question = "Go now?"
	updated, err := h.m.UpdateAutomation(a.ID, h.get(a).Version, AutomationInput{Name: "inj", EntryMemberID: "d0", Members: members}, "operator")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if d0 := automationMember(updated, "d0"); d0.QuestionID != "" || d0.WaitingForOperator || d0.Question != "Go now?" {
		t.Fatalf("update kept caller-supplied runtime state or lost the question: %+v", d0)
	}
}
