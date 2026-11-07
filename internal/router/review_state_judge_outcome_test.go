package router

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func reviewStateBody(t *testing.T, kind string) string {
	t.Helper()
	prd := &autonomous.PRD{ID: "p1", RulesReviewOutcome: &autonomous.RulesReviewOutcome{JudgeOutcome: kind}}
	b, err := json.Marshal(map[string]any{"review_state": autonomous.ReviewStateOf(prd)})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestReviewStateJudgeOutcomeCouldNotStartShowsWording(t *testing.T) {
	body := reviewStateBody(t, autonomous.JudgeKindCouldNotStart)
	if !strings.Contains(body, `"judge_outcome":"judge_could_not_start"`) || !strings.Contains(body, `"judge_outcome_text":"judge could not start"`) {
		t.Fatalf("body missing judge outcome fields: %s", body)
	}
	if line := reviewStateStatusLine(body); !strings.Contains(line, "judge could not start") {
		t.Fatalf("status line = %q, want the wording", line)
	}
}

func TestReviewStateJudgeOutcomeEmptyShowsNoField(t *testing.T) {
	body := reviewStateBody(t, "")
	if strings.Contains(body, "judge_outcome") {
		t.Fatalf("empty outcome must omit both fields: %s", body)
	}
	if line := reviewStateStatusLine(body); line != "" {
		t.Fatalf("status line = %q, want empty", line)
	}
}
