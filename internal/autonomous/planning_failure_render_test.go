package autonomous

import (
	"strings"
	"testing"
)

const renderBody = `{"id":"p1","planning_failure":{"reason":"bad wording","category":"content","source":"release-version-fixed","phrases":["v9.1.0"],"retryable":true,"retry_counts":{"content":1,"infra":2},"retry_budget":3}}`

func TestPlanningFailureLinesCarryReasonCategoryRulePhrasesAndCounts(t *testing.T) {
	got := strings.Join(PlanningFailureLines([]byte(renderBody)), "\n")
	for _, want := range []string{"Planning failed", "reason:   bad wording", "category: content", "rule:     release-version-fixed", "phrases:  v9.1.0", "1 of 3 (content=1, infra=2), retryable"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPlanningFailureLinesNilWithoutFailure(t *testing.T) {
	if PlanningFailureLines([]byte(`{"id":"p1"}`)) != nil || PlanningFailureLines([]byte(`not json`)) != nil {
		t.Error("expected nil")
	}
	if PlanningFailureOneLine(nil) != "" {
		t.Error("nil failure must give an empty line")
	}
}

func TestPlanningFailureListLinesMarksOnlyFailedRows(t *testing.T) {
	body := `{"prds":[{"id":"a","title":"A","planning_failure":{"reason":"r","phrases":["x","y"]}},{"id":"b"}]}`
	got := PlanningFailureListLines([]byte(body))
	if len(got) != 1 || got[0] != "A (a): Planning failed: r (phrases: x; y)" {
		t.Errorf("got %q", got)
	}
	if PlanningFailureListLines([]byte(`nope`)) != nil {
		t.Error("bad json must give nil")
	}
}
