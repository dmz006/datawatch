package autonomous

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func heldReviewPRD(id string, action string, at time.Time) *PRD {
	return &PRD{
		ID:        id,
		Status:    PRDNeedsReview,
		UpdatedAt: at,
		RulesReviewOutcome: &RulesReviewOutcome{
			Action:       action,
			Findings:     []string{"missing tests", "stale docs"},
			Summary:      "two problems",
			Attempt:      2,
			RetryBudget:  2,
			RetriesSpent: 2,
			At:           at,
		},
	}
}

func TestRulesReviewSummaryCarriesOutcome(t *testing.T) {
	p := heldReviewPRD("p1", RulesReviewActionRetriesSpent, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	v := BuildSummaryView(p)
	if v.RulesReviewOutcome == nil || v.RulesReviewOutcome.Action != RulesReviewActionRetriesSpent {
		t.Fatalf("summary outcome = %+v", v.RulesReviewOutcome)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"rules_review_outcome"`) || !strings.Contains(string(raw), "missing tests") {
		t.Errorf("summary JSON lacks the outcome: %s", raw)
	}
}

func TestRulesReviewSummaryOmitsOutcomeWhenAbsent(t *testing.T) {
	raw, err := json.Marshal(BuildSummaryView(&PRD{ID: "p1", Status: PRDRunning}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "rules_review_outcome") {
		t.Errorf("summary JSON carries an outcome it should omit: %s", raw)
	}
}

func TestRulesReviewAttentionListsHeldWithoutNeedsReviewStatus(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	base := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, p := range []*PRD{
		heldReviewPRD("p-failed", RulesReviewActionFixTaskFailed, base),
		heldReviewPRD("p-spent", RulesReviewActionRetriesSpent, base.Add(time.Hour)),
		heldReviewPRD("p-off", RulesReviewActionAutoFixDisabled, base.Add(2*time.Hour)),
		heldReviewPRD("p-created", RulesReviewActionFixTaskCreated, base.Add(3*time.Hour)),
		{ID: "p-plain", Status: PRDNeedsReview, UpdatedAt: base},
	} {
		if err := st.SavePRD(p); err != nil {
			t.Fatal(err)
		}
	}
	items := NewAPI(m).AttentionAutomata([]string{"running"})
	var got []string
	for _, x := range items {
		got = append(got, x.(SummaryView).ID)
	}
	want := []string{"p-off", "p-spent", "p-failed"} // newest first
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attention = %v, want %v", got, want)
	}
}

func TestRulesReviewAttentionDropsOnceCleared(t *testing.T) {
	m, st, _ := newTagTestManager(t)
	p := heldReviewPRD("p1", RulesReviewActionAutoFixDisabled, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err := st.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	api := NewAPI(m)
	if items := api.AttentionAutomata([]string{"running"}); len(items) != 1 {
		t.Fatalf("held attention = %d items, want 1", len(items))
	}
	p.RulesReviewOutcome = nil
	if err := st.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	items := api.AttentionAutomata([]string{"running"})
	if items == nil || len(items) != 0 {
		t.Errorf("cleared attention = %#v, want empty non-nil", items)
	}
}
