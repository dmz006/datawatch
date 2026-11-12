package autonomous

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"", 0},                    // the empty context: zero tokens
		{"a", 1},                   // rounded up to one token
		{"abc", 1},                 // three bytes, one token
		{"abcd", 1},                // exactly four bytes, one token
		{"abcde", 2},               // five bytes, two tokens
		{strings.Repeat("x", 8), 2},
		{strings.Repeat("x", 9), 3},
		{strings.Repeat("x", 32), 8},
		// Four bytes per token regardless of the bytes' content: an é is two
		// bytes, so four of them are eight bytes — two tokens.
		{strings.Repeat("é", 4), 2},
		{strings.Repeat("é", 5), 3},
	}
	for _, c := range cases {
		if got := EstimateTokens(c.text); got != c.want {
			t.Errorf("EstimateTokens(%q) = %d, want %d", c.text, got, c.want)
		}
	}
	if got := EstimateTokens(BuildWorkerStartingContext(StartingContextInput{
		Role:     "worker",
		TaskSpec: "do the task",
	})); got <= 0 {
		t.Errorf("EstimateTokens of a generated starting context = %d, want > 0", got)
	}
}

func TestTaskAttemptStartupFieldsJSONRoundTrip(t *testing.T) {
	in := TaskAttempt{
		Number:             3,
		Outcome:            "failed",
		StartupTokens:      41,
		WorkerProfile:      "digest",
		WorkerRole:         "worker",
		StartupTokenSource: StartupTokenSourceEstimated,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"startup_tokens":41`,
		`"worker_profile":"digest"`,
		`"worker_role":"worker"`,
		`"startup_token_source":"estimated"`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("marshalled TaskAttempt missing %s; got %s", want, raw)
		}
	}
	var out TaskAttempt
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.StartupTokens != 41 || out.WorkerProfile != "digest" ||
		out.WorkerRole != "worker" || out.StartupTokenSource != "estimated" {
		t.Errorf("round-trip = %+v, want startup_tokens=41 worker_profile=digest worker_role=worker startup_token_source=estimated", out)
	}
}

func TestTaskAttemptNoProfileRecordsEmptyProfileAndZeroTokens(t *testing.T) {
	a := TaskAttempt{Number: 1, Outcome: "failed"}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"startup_tokens":0`,
		`"worker_profile":""`,
		`"worker_role":""`,
		`"startup_token_source":""`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("marshalled no-profile attempt missing %s; got %s", want, raw)
		}
	}
	var out TaskAttempt
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.WorkerProfile != "" || out.WorkerRole != "" {
		t.Errorf("no-profile attempt profile/role = %q / %q, want empty", out.WorkerProfile, out.WorkerRole)
	}
	if out.StartupTokens != 0 || out.StartupTokenSource != "" {
		t.Errorf("no-profile attempt tokens = %d (%s), want 0 (no source)", out.StartupTokens, out.StartupTokenSource)
	}
}

func TestRecordFailedAttemptRecordsStartupTokensFromLaunchContext(t *testing.T) {
	m, _ := newFailureEscalationFixture(t, t.TempDir())
	prd := newFailureEscalationPRD(t, m, t.TempDir(), nil)
	taskID := prd.Story[0].Tasks[0].ID
	task := lookupTask(prd, taskID)

	// A launch with a profile names it, its role, and the starting context
	// it generated; the record estimates the token size and marks it as
	// estimated.
	ctxText := strings.Repeat("x", 100) // 100 bytes -> 25 tokens
	m.recordFailedAttempt(context.Background(), prd, task,
		attemptRun{WorkerProfile: "digest", WorkerRole: "worker", LaunchContext: ctxText},
		attemptFailure{Outcome: attemptOutcomeFailed, Summary: "failed"})
	got, _ := m.Store().GetPRD(prd.ID)
	a := lookupTask(got, taskID).Attempts[0]
	if a.WorkerProfile != "digest" || a.WorkerRole != "worker" {
		t.Fatalf("attempt profile/role = %q / %q, want digest / worker", a.WorkerProfile, a.WorkerRole)
	}
	if a.StartupTokens != 25 {
		t.Errorf("startup tokens = %d, want 25 (EstimateTokens of the 100-byte context)", a.StartupTokens)
	}
	if a.StartupTokenSource != StartupTokenSourceEstimated {
		t.Errorf("startup token source = %q, want %q", a.StartupTokenSource, StartupTokenSourceEstimated)
	}

	// A launch without a starting context records zero tokens and no
	// source, with an empty profile name.
	m.recordFailedAttempt(context.Background(), got, lookupTask(got, taskID),
		attemptRun{},
		attemptFailure{Outcome: attemptOutcomeFailed, Summary: "again"})
	got, _ = m.Store().GetPRD(prd.ID)
	b := lookupTask(got, taskID).Attempts[1]
	if b.WorkerProfile != "" || b.WorkerRole != "" {
		t.Errorf("no-context attempt profile/role = %q / %q, want empty", b.WorkerProfile, b.WorkerRole)
	}
	if b.StartupTokens != 0 || b.StartupTokenSource != "" {
		t.Errorf("no-context attempt tokens = %d (source %q), want 0 (no source)", b.StartupTokens, b.StartupTokenSource)
	}
}
