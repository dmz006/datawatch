package autonomous

import (
	"strings"
	"testing"
	"time"
)

func TestReviewStateNotJudged(t *testing.T) {
	base := time.Now().Add(-3 * time.Hour)
	hold := reviewHold(base.Add(2 * time.Hour))
	prd := &PRD{
		ID:       "nj1",
		Verdicts: []GuardrailVerdict{*reviewBlock(base), *hold},
		RulesReviewOutcome: &RulesReviewOutcome{
			Action: RulesReviewActionRetriesSpent, JudgeOutcome: JudgeKindFoundProblem, Summary: "missing version bump",
		},
	}
	got := ReviewStateOf(prd)
	if got.State != ReviewStateTimedOut || got.Judged {
		t.Fatalf("state %s judged %v, want timed_out and not judged", got.State, got.Judged)
	}
	if got.JudgeOutcome != JudgeKindTimedOut {
		t.Errorf("judge outcome = %q, want %q", got.JudgeOutcome, JudgeKindTimedOut)
	}
	if strings.Contains(strings.ToLower(got.JudgeOutcomeText), "found a problem") || strings.Contains(got.Cause, "found a problem") {
		t.Errorf("found-problem text: %q / %q", got.JudgeOutcomeText, got.Cause)
	}
	if !strings.HasPrefix(got.Cause, "not judged: the judge timed out on 2 backend(s): claude-code, qwen") {
		t.Errorf("cause = %q", got.Cause)
	}
	if len(got.Backends) != 2 || got.Backends[0].Backend != "claude-code" || got.Backends[1].Outcome != JudgeOutcomeTimedOut {
		t.Errorf("backends = %+v", got.Backends)
	}
	if b := ReviewStateOf(&PRD{Verdicts: []GuardrailVerdict{*reviewBlock(base)}}); !b.Judged {
		t.Error("a real block must be judged")
	}
	if p := ReviewStateOf(&PRD{Verdicts: []GuardrailVerdict{*reviewPass(base)}}); !p.Judged {
		t.Error("a pass must be judged")
	}
}

func TestTimedOutJudgeRecordsAttemptSession(t *testing.T) {
	v := reviewHold(time.Now())
	v.JudgeSessionID = "sess-qwen"
	v.RawJudge = "tail of the output"
	v.JudgeAttempts[1].SessionID = "sess-qwen"
	v.JudgeAttempts[1].LastOutput = "tail of the output"
	prd := &PRD{Verdicts: []GuardrailVerdict{*v}}
	got := reviewVerdicts(prd)[0]
	if got.JudgeSessionID != "sess-qwen" || got.RawJudge != "tail of the output" {
		t.Fatalf("hold verdict session %q output %q", got.JudgeSessionID, got.RawJudge)
	}
	if r := got.JudgeAttempts[1]; r.SessionID != "sess-qwen" || r.LastOutput != "tail of the output" {
		t.Errorf("attempt record = %+v", r)
	}
	if got.JudgeAttempts[0].SessionID != "" {
		t.Error("an attempt without a session must stay empty")
	}
}
