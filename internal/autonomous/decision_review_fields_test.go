package autonomous

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDecisionReviewFieldsRoundTrip(t *testing.T) {
	in := Decision{
		At:              time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Kind:            "guardrail_rejudge",
		JudgedCommitSHA: "0123456789abcdef0123456789abcdef01234567",
		VerdictReused:   true,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Decision
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.JudgedCommitSHA != in.JudgedCommitSHA || !out.VerdictReused {
		t.Fatalf("round trip lost fields: %+v", out)
	}
}

func TestDecisionReviewFieldsOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(Decision{Kind: "run"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"judged_commit_sha", "verdict_reused"} {
		if strings.Contains(string(b), key) {
			t.Fatalf("key %q must be omitted when empty, got %s", key, b)
		}
	}
}

func TestDecisionReviewFieldsOldStoredDecisionLoadsZero(t *testing.T) {
	old := `{"at":"2026-01-02T03:04:05Z","kind":"verify","verdict_outcome":"pass"}`
	var d Decision
	if err := json.Unmarshal([]byte(old), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.JudgedCommitSHA != "" || d.VerdictReused {
		t.Fatalf("old decision must load with zero values, got %+v", d)
	}
	if d.Kind != "verify" || d.VerdictOutcome != "pass" {
		t.Fatalf("existing fields changed: %+v", d)
	}
}
