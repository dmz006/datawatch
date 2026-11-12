package autonomous

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestAttemptRecordsResumeOutcome drives executeOne through a first attempt
// that fails verification and a retry that resumes the session and fails
// again. The first record carries no retry decision; the resumed retry's
// record says it resumed and gives the decision's reason.
func TestAttemptRecordsResumeOutcome(t *testing.T) {
	run := runRetryResumeFlow(t, 150000, true, 5000, nil, []verifyStep{
		failedVerification("first failure"),
		failedVerification("second failure"),
	})
	if run.spawnCalls != 1 {
		t.Fatalf("spawn calls = %d, want 1 (the retry resumes sess-1)", run.spawnCalls)
	}
	if len(run.attempts) != 2 {
		t.Fatalf("attempt records = %d, want 2", len(run.attempts))
	}
	first := run.attempts[0]
	if first.Resumed || first.ResumeReason != "" {
		t.Errorf("first attempt Resumed=%v ResumeReason=%q, want false and empty (no retry decision)", first.Resumed, first.ResumeReason)
	}
	resumed := run.attempts[1]
	if !resumed.Resumed {
		t.Errorf("resumed attempt Resumed = false, want true")
	}
	if resumed.ResumeReason != resumeReasonResumed {
		t.Errorf("resumed attempt ResumeReason = %q, want %q", resumed.ResumeReason, resumeReasonResumed)
	}
}

// TestAttemptRecordsWhyTheRetryStartedFresh runs retries that do not resume
// for each reason the decision can give, and checks the failed retries'
// records say they started fresh and name that reason. Three scripted failures
// use the whole failure-escalation budget, so both retries are recorded.
func TestAttemptRecordsWhyTheRetryStartedFresh(t *testing.T) {
	tests := []struct {
		name       string
		maxTokens  int
		alive      bool
		ctxTokens  int
		sendErr    error
		wantReason string
		wantSends  int
	}{
		{
			name: "session ended", maxTokens: 150000, alive: false, ctxTokens: 5000,
			wantReason: sessionEndedResumeReason,
		},
		{
			name: "context above the limit", maxTokens: 150000, alive: true, ctxTokens: 150001,
			wantReason: "context above the limit; starting a fresh attempt",
		},
		{
			name: "resume disabled by setting", maxTokens: 0, alive: true, ctxTokens: 0,
			wantReason: "resume disabled by setting; starting a fresh attempt",
		},
		{
			name: "resume decided but send failed", maxTokens: 150000, alive: true, ctxTokens: 5000,
			// Both retries decide to resume and both sends fail.
			sendErr: errors.New("pane gone"), wantSends: 2,
			wantReason: resumeFallbackSendFailedReason,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			run := runRetryResumeFlow(t, tc.maxTokens, tc.alive, tc.ctxTokens, tc.sendErr, []verifyStep{
				failedVerification("first failure"),
				failedVerification("second failure"),
				failedVerification("third failure"),
			})
			if run.spawnCalls != 3 {
				t.Errorf("spawn calls = %d, want 3 (both retries start a fresh session)", run.spawnCalls)
			}
			if len(run.sentSIDs) != tc.wantSends {
				t.Errorf("sends = %d, want %d", len(run.sentSIDs), tc.wantSends)
			}
			if len(run.attempts) != 3 {
				t.Fatalf("attempt records = %d, want 3", len(run.attempts))
			}
			for i := 1; i <= 2; i++ {
				retry := run.attempts[i]
				if retry.Resumed {
					t.Errorf("retry record %d Resumed = true, want false", i+1)
				}
				if retry.ResumeReason != tc.wantReason {
					t.Errorf("retry record %d ResumeReason = %q, want %q", i+1, retry.ResumeReason, tc.wantReason)
				}
			}
		})
	}
}

// TestAttemptResumeRecordJSONRoundTrip: a resumed record writes resumed and
// resume_reason and reads them back unchanged; a fresh record omits both.
func TestAttemptResumeRecordJSONRoundTrip(t *testing.T) {
	start := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	in := TaskAttempt{
		Number: 2, Backend: "claude-code", Model: "claude-opus-5-5",
		StartedAt: start, Outcome: attemptOutcomeFailed,
		Resumed: true, ResumeReason: resumeReasonResumed,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{`"resumed":true`, `"resume_reason":"resumed previous session"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("JSON lacks %s: %s", want, raw)
		}
	}
	var out TaskAttempt
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.Resumed != in.Resumed || out.ResumeReason != in.ResumeReason {
		t.Errorf("round trip = (%v, %q), want (%v, %q)", out.Resumed, out.ResumeReason, in.Resumed, in.ResumeReason)
	}

	fresh, _ := json.Marshal(TaskAttempt{Number: 1, Outcome: attemptOutcomeFailed})
	if strings.Contains(string(fresh), "resumed") || strings.Contains(string(fresh), "resume_reason") {
		t.Errorf("empty resume fields should be omitted: %s", fresh)
	}
}

// TestOldAttemptRecordLoadsWithoutResumeFields: a record stored before the
// resume fields existed loads with them false and empty, every other field
// intact.
func TestOldAttemptRecordLoadsWithoutResumeFields(t *testing.T) {
	old := []byte(`{"number":2,"backend":"claude-code","model":"claude-opus-5-5","outcome":"failed",` +
		`"failure_category":"verifier","failure_summary":"tests failed","escalated":true,` +
		`"backend_reason":"escalated after 1 failure","end_reason":"verifier_failed"}`)
	var a TaskAttempt
	if err := json.Unmarshal(old, &a); err != nil {
		t.Fatalf("Unmarshal old record: %v", err)
	}
	if a.Resumed || a.ResumeReason != "" {
		t.Errorf("Resumed=%v ResumeReason=%q, want false and empty for an old record", a.Resumed, a.ResumeReason)
	}
	if a.Backend != "claude-code" || a.FailureSummary != "tests failed" || a.BackendReason != "escalated after 1 failure" || a.EndReason != "verifier_failed" {
		t.Errorf("old record fields lost: %+v", a)
	}
}

// TestHeldEntryCarriesLastResume: the Automaton status row for a held task
// shows whether its last attempt resumed and why, and an old attempt shows
// neither.
func TestHeldEntryCarriesLastResume(t *testing.T) {
	now := time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC)
	prd := &PRD{
		ID: "p-resume", Title: "resume view", Status: PRDBlocked,
		Story: []Story{{ID: "s1", Tasks: []Task{
			{
				ID: "t-resumed", Title: "resumed record", Status: TaskReviewNeeded,
				ReviewSummary: &ReviewSummary{HeldAt: now, FailureCount: 2},
				Attempts: []TaskAttempt{
					{Number: 1, Backend: "claude-code", Outcome: "failed"},
					{Number: 2, Backend: "claude-code", Outcome: "failed", Resumed: true, ResumeReason: resumeReasonResumed},
				},
			},
			{
				ID: "t-old", Title: "old record", Status: TaskReviewNeeded,
				ReviewSummary: &ReviewSummary{HeldAt: now, FailureCount: 1},
				Attempts:      []TaskAttempt{{Number: 1, Backend: "pi-prompt", Model: "m", Outcome: "failed"}},
			},
		}}},
	}
	view := BuildSummaryView(prd)
	if len(view.ReviewNeeded) != 2 {
		t.Fatalf("ReviewNeeded = %d entries, want 2", len(view.ReviewNeeded))
	}
	if got := view.ReviewNeeded[0]; !got.LastResumed || got.LastResumeReason != resumeReasonResumed {
		t.Errorf("resumed row = (%v, %q), want (true, %q)", got.LastResumed, got.LastResumeReason, resumeReasonResumed)
	}
	if got := view.ReviewNeeded[1]; got.LastResumed || got.LastResumeReason != "" {
		t.Errorf("old row = (%v, %q), want (false, empty)", got.LastResumed, got.LastResumeReason)
	}
	raw, err := json.Marshal(view.ReviewNeeded[0])
	if err != nil {
		t.Fatalf("Marshal entry: %v", err)
	}
	for _, want := range []string{`"last_resumed":true`, `"last_resume_reason":"resumed previous session"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("entry JSON lacks %s: %s", want, raw)
		}
	}
}
