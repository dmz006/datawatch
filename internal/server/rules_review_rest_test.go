package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

func saveHeldReview(t *testing.T, mgr *autonomous.Manager, id string) {
	t.Helper()
	err := mgr.Store().SavePRD(&autonomous.PRD{
		ID:     id,
		Status: autonomous.PRDNeedsReview,
		RulesReviewOutcome: &autonomous.RulesReviewOutcome{
			Action:       autonomous.RulesReviewActionRetriesSpent,
			Findings:     []string{"missing tests", "stale docs"},
			Summary:      "two problems",
			Attempt:      2,
			RetryBudget:  2,
			RetriesSpent: 2,
			At:           time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}

type reviewOutcomeJSON struct {
	Action   string   `json:"action"`
	Findings []string `json:"findings"`
}

func TestRulesReviewRESTAttentionShowsOutcome(t *testing.T) {
	s, mgr := newTagTestServer(t)
	saveHeldReview(t, mgr, "p-held")
	// needs_review is not an attention status here; the hold still lists.
	s.autonomousSettings = func(key string) ([]string, bool) {
		if key == autonomous.AttentionStatusesKey {
			return []string{"running"}, true
		}
		return nil, false
	}
	rec := httptest.NewRecorder()
	s.handleAutonomousAttention(rec, httptest.NewRequest(http.MethodGet, "/api/autonomous/attention", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("attention: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Items []struct {
			ID      string             `json:"id"`
			Outcome *reviewOutcomeJSON `json:"rules_review_outcome"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "p-held" || out.Items[0].Outcome == nil {
		t.Fatalf("items = %s", rec.Body.String())
	}
	o := out.Items[0].Outcome
	if o.Action != "retries_spent" || len(o.Findings) != 2 || o.Findings[0] != "missing tests" {
		t.Errorf("outcome = %+v", o)
	}
}

func TestRulesReviewRESTGetAutomatonShowsOutcome(t *testing.T) {
	s, mgr := newTagTestServer(t)
	saveHeldReview(t, mgr, "p-held")
	rec := tagCall(t, s, http.MethodGet, "/api/autonomous/prds/p-held", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Outcome *reviewOutcomeJSON `json:"rules_review_outcome"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Outcome == nil || out.Outcome.Action != "retries_spent" || len(out.Outcome.Findings) != 2 {
		t.Errorf("outcome = %+v in %s", out.Outcome, rec.Body.String())
	}
}
