package router

import (
	"net/http"
	"strings"
	"testing"
)

func TestRulesReviewCommAttentionShowsActionAndFindings(t *testing.T) {
	body := `{"items":[{"id":"p1","title":"T","rules_review_outcome":{"action":"fix_task_created","findings":["missing tests","no docs"],"retry_budget":3,"retries_spent":1}}]}`
	_, port := laneFillCommDaemon(t, http.StatusOK, body)
	r := setupPeerRouter(t, port)
	got := strings.Join(r.HandleTestMessage("autonomous attention"), " ")
	for _, want := range []string{"Completion review: fix task created (retries 1 of 3)", "- missing tests", "- no docs"} {
		if !strings.Contains(got, want) {
			t.Errorf("reply lacks %q: %q", want, got)
		}
	}
}

func TestRulesReviewCommTruncatesAtFiveFindings(t *testing.T) {
	o := rulesReviewOutcomeView{Action: "retries_spent", RetryBudget: 2, RetriesSpent: 2,
		Findings: []string{"a", "b", "c", "d", "e", "f", "g"}}
	got := formatRulesReviewOutcome(o)
	if !strings.Contains(got, "- e") || strings.Contains(got, "- f") || !strings.Contains(got, "+2 more") {
		t.Errorf("want five findings then +2 more, got %q", got)
	}
	if !strings.Contains(formatRulesReviewOutcome(rulesReviewOutcomeView{Action: "auto_fix_disabled"}), "auto-fix off") {
		t.Error("auto_fix_disabled not rendered in words")
	}
}
