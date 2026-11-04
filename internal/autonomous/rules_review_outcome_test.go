package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRulesReviewOutcomeCopiesIssues(t *testing.T) {
	at := time.Date(2026, 11, 4, 1, 2, 3, 0, time.UTC)
	v := GuardrailVerdict{Summary: "blocked", Issues: []string{"a", "b"}}
	o := newRulesReviewOutcome(v, RulesReviewActionFixTaskCreated, "why", 2, 3, 1, at)
	if len(o.Findings) != 2 || o.Findings[0] != "a" || o.Findings[1] != "b" {
		t.Fatalf("findings = %v", o.Findings)
	}
	if o.Action != "fix_task_created" || o.Attempt != 2 || o.RetryBudget != 3 || o.RetriesSpent != 1 || o.Reason != "why" || !o.At.Equal(at) {
		t.Fatalf("fields wrong: %+v", o)
	}
	v.Issues[0] = "changed"
	if o.Findings[0] != "a" {
		t.Fatal("findings alias the verdict slice")
	}
}

func TestRulesReviewOutcomeFallsBackToSummary(t *testing.T) {
	o := newRulesReviewOutcome(GuardrailVerdict{Summary: "only summary"}, RulesReviewActionRetriesSpent, "", 1, 1, 1, time.Time{})
	if len(o.Findings) != 1 || o.Findings[0] != "only summary" {
		t.Fatalf("findings = %v", o.Findings)
	}
}

func TestRulesReviewOutcomeFindingsNeverNil(t *testing.T) {
	o := newRulesReviewOutcome(GuardrailVerdict{}, RulesReviewActionAutoFixDisabled, "", 0, 0, 0, time.Time{})
	if o.Findings == nil {
		t.Fatal("Findings is nil")
	}
	b, _ := json.Marshal(o)
	if !strings.Contains(string(b), `"findings":[]`) {
		t.Fatalf("json = %s", b)
	}
}

func TestRulesReviewOutcomeJSONRoundTrip(t *testing.T) {
	var plain PRD
	b, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "rules_review_outcome") {
		t.Fatal("unset field must be omitted")
	}
	var back PRD
	if err := json.Unmarshal(b, &back); err != nil || back.RulesReviewOutcome != nil {
		t.Fatalf("unset round trip: %v %+v", err, back.RulesReviewOutcome)
	}
	p := PRD{RulesReviewOutcome: newRulesReviewOutcome(GuardrailVerdict{Issues: []string{"x"}, Summary: "s"},
		RulesReviewActionFixTaskFailed, "r", 1, 2, 1, time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC))}
	b, err = json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var got PRD
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.RulesReviewOutcome == nil || got.RulesReviewOutcome.Action != "fix_task_failed" || got.RulesReviewOutcome.Findings[0] != "x" {
		t.Fatalf("set round trip: %+v", got.RulesReviewOutcome)
	}
}

func TestRulesReviewOutcomeReviewNowHonoursClock(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m := &Manager{attemptClock: func() time.Time { return fixed }}
	if !m.reviewNow().Equal(fixed) {
		t.Fatal("reviewNow ignored the fixed clock")
	}
	if (&Manager{}).reviewNow().IsZero() {
		t.Fatal("reviewNow with no clock must use wall time")
	}
}
