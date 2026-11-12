package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRulesReviewFormatsEachAction(t *testing.T) {
	cases := map[string]string{
		"fix_task_created":  "fix task created",
		"fix_task_failed":   "fix task failed",
		"retries_spent":     "retries spent",
		"auto_fix_disabled": "auto-fix off",
	}
	for action, words := range cases {
		got := formatRulesReviewOutcome(rulesReviewOutcomeView{Action: action, Findings: []string{"one", "two"}, RetryBudget: 3, RetriesSpent: 2})
		for _, want := range []string{"Completion review: " + words, "Retries spent: 2 of 3", "  - one", "  - two"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: output lacks %q: %q", action, want, got)
			}
		}
	}
}

func TestRulesReviewBlocksFromAttentionBody(t *testing.T) {
	body := `{"items":[{"id":"p1","title":"T","rules_review_outcome":{"action":"retries_spent","findings":["x"],"retry_budget":2,"retries_spent":2}},{"id":"p2"}]}`
	blocks := rulesReviewOutcomeBlocks([]byte(body))
	if len(blocks) != 1 || !strings.Contains(blocks[0], "p1 T") || !strings.Contains(blocks[0], "  - x") {
		t.Errorf("blocks = %q", blocks)
	}
	if len(rulesReviewOutcomeBlocks([]byte(`{"items":[]}`))) != 0 {
		t.Error("empty list must yield no block")
	}
}

func TestRulesReviewCLIAttentionPrintsBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/autonomous/attention" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"id":"p1","title":"T","rules_review_outcome":{"action":"fix_task_failed","findings":["bad"],"retry_budget":1,"retries_spent":1}}]}`))
	}))
	defer srv.Close()
	old := serverURL
	serverURL = srv.URL
	defer func() { serverURL = old }()
	rd, wr, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = wr
	err := newAutonomousAttentionCmd().RunE(nil, nil)
	os.Stdout = stdout
	_ = wr.Close()
	out, _ := io.ReadAll(rd)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "Completion review: fix task failed") || !strings.Contains(got, "  - bad") {
		t.Errorf("output = %q", got)
	}
}
