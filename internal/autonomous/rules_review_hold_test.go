// Review-blocked Automaton hold: lane fill must not approve and run an
// Automaton parked in needs_review by a rules review block that has no
// corrective task coming, while the operator path (add a task, approve, run)
// still works and the review record survives until a review passes.
package autonomous

import (
	"context"
	"strings"
	"testing"
)

// classifyStoredForLaneFill runs the stored Automaton through the same hold
// check and classification lane fill uses.
func classifyStoredForLaneFill(prd *PRD) (LaneFillSource, string, string) {
	code, text := PRDHoldCode(&PRD{ID: prd.ID, Title: prd.Title, Spec: prd.Spec, Status: prd.Status, RulesReviewOutcome: prd.RulesReviewOutcome}, nil)
	src, excluded := ClassifyLaneFillCandidate(LaneFillCandidate{
		ID: prd.ID, Title: prd.Title, Status: NormalizePRDStatus(prd.Status),
		HoldReason: text, HoldCode: code,
	})
	return src, excluded, text
}

func TestRulesReviewHoldLaneFillRefusesHeldAndAcceptsFixCreated(t *testing.T) {
	for _, action := range []string{RulesReviewActionFixTaskFailed, RulesReviewActionRetriesSpent, RulesReviewActionAutoFixDisabled} {
		prd := &PRD{ID: "aa000001", Title: "T", Status: PRDNeedsReview,
			RulesReviewOutcome: &RulesReviewOutcome{Action: action, Findings: []string{"a", "b"}}}
		src, excluded, text := classifyStoredForLaneFill(prd)
		if src != LaneFillSourceNone || !strings.Contains(excluded, "held") {
			t.Fatalf("%s: source=%d excluded=%q, want refused as held", action, src, excluded)
		}
		if !strings.Contains(text, "2 findings") || strings.Contains(strings.ToLower(text), "prd") {
			t.Fatalf("%s: hold text = %q", action, text)
		}
	}
	prd := &PRD{ID: "aa000002", Title: "T", Status: PRDNeedsReview,
		RulesReviewOutcome: &RulesReviewOutcome{Action: RulesReviewActionFixTaskCreated, Findings: []string{"a"}}}
	if src, excluded, _ := classifyStoredForLaneFill(prd); src != LaneFillSourceReady || excluded != "" {
		t.Fatalf("fix_task_created: source=%d excluded=%q, want ready", src, excluded)
	}
	// Once approved (status no longer needs_review) the record does not hold.
	prd.Status = PRDApproved
	prd.RulesReviewOutcome.Action = RulesReviewActionRetriesSpent
	if src, _, _ := classifyStoredForLaneFill(prd); src != LaneFillSourceReady {
		t.Fatalf("approved with a record: source=%d, want ready", src)
	}
}

// heldAutomaton runs the fixture until the review blocks with the retry
// already spent and returns the Automaton parked in needs_review.
func heldAutomaton(t *testing.T, blockN int) (*Manager, *PRD, *int) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 1
	m, run, reviews := rulesReviewAutoFixFixture(t, cfg, blockN, contentBlock())
	// The first run spends the one retry on a fix task; its re-review also
	// blocks, so the Automaton is held with the retry spent.
	prd := run()
	if prd.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review", prd.Status)
	}
	o := prd.RulesReviewOutcome
	if o == nil || o.Action != RulesReviewActionRetriesSpent {
		t.Fatalf("outcome = %+v, want retries_spent", o)
	}
	return m, prd, reviews
}

func TestRulesReviewHoldOperatorAddTaskApproveRunPasses(t *testing.T) {
	// Reviews 1 and 2 block (the second after the one fix task); the third passes.
	m, prd, reviews := heldAutomaton(t, 2)
	if src, _, _ := classifyStoredForLaneFill(prd); src != LaneFillSourceNone {
		t.Fatalf("held Automaton classified %d, want refused", src)
	}
	spentBefore := prd.RetryCountsByCategory["content"]
	storyID := prd.Story[len(prd.Story)-1].ID
	prd, err := m.AddTask(prd.ID, storyID, "Update docs", "update docs/howto/x.md", "operator", nil)
	if err != nil {
		t.Fatalf("AddTask on a review-held Automaton: %v", err)
	}
	if prd.RulesReviewOutcome == nil {
		t.Fatal("AddTask wiped the review record")
	}
	prd, err = m.Approve(prd.ID, "operator", "added corrective task")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if prd.Status != PRDApproved || prd.RulesReviewOutcome == nil {
		t.Fatalf("after approve status=%s outcome=%+v, want approved with the record kept", prd.Status, prd.RulesReviewOutcome)
	}
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if *reviews != 3 {
		t.Fatalf("reviews = %d, want 3", *reviews)
	}
	if prd.Status != PRDCompleted || prd.RulesReviewOutcome != nil {
		t.Fatalf("status=%s outcome=%+v, want completed with the record cleared", prd.Status, prd.RulesReviewOutcome)
	}
	for si := range prd.Story {
		for _, tk := range prd.Story[si].Tasks {
			if tk.Title == "Update docs" && tk.Status != TaskCompleted {
				t.Fatalf("added task status = %s, want completed", tk.Status)
			}
		}
	}
	if prd.RetryCountsByCategory["content"] != spentBefore {
		t.Fatalf("content retries spent changed %d -> %d", spentBefore, prd.RetryCountsByCategory["content"])
	}
}

func TestRulesReviewHoldSecondReviewBlocksHoldsAgainWithCountersUnchanged(t *testing.T) {
	m, prd, reviews := heldAutomaton(t, -1)
	before := *prd.RulesReviewOutcome
	spentBefore := prd.RetryCountsByCategory["content"]
	storyID := prd.Story[len(prd.Story)-1].ID
	if _, err := m.AddTask(prd.ID, storyID, "Update docs", "update docs/howto/x.md", "operator", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if _, err := m.Approve(prd.ID, "operator", "retry"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	prd, _ = m.Store().GetPRD(prd.ID)
	if prd.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want needs_review again", prd.Status)
	}
	o := prd.RulesReviewOutcome
	if o == nil || o.Action != RulesReviewActionRetriesSpent || o.RetriesSpent != before.RetriesSpent || o.RetryBudget != before.RetryBudget {
		t.Fatalf("outcome = %+v, want retries_spent with counters %d of %d", o, before.RetriesSpent, before.RetryBudget)
	}
	if got := prd.RetryCountsByCategory["content"]; got != spentBefore {
		t.Fatalf("content retries spent %d -> %d, want unchanged", spentBefore, got)
	}
	if src, _, _ := classifyStoredForLaneFill(prd); src != LaneFillSourceNone {
		t.Fatalf("re-held Automaton classified %d, want refused", src)
	}
	if *reviews < 3 {
		t.Fatalf("reviews = %d, want a new review after the run", *reviews)
	}
}
