package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const realFinding = "update docs/howto/x.md for the new flag"

func realJudgeBlock() GuardrailVerdict {
	return GuardrailVerdict{Outcome: "block", Severity: "high", Summary: "docs missing", Issues: []string{realFinding}}
}

// A judge that returns a real verdict of a problem (not a delivery failure) is
// recorded as a block, holds the Automaton with the finding kept, and is never
// retried as infrastructure.
func TestRulesReviewRealJudgeBlockStillBlocks(t *testing.T) {
	noInfraSleep(t)
	cfg := DefaultConfig()
	cfg.AutoFixRetries = 0
	cfg.RetryPolicy = nil
	m, run, reviews := rulesReviewAutoFixFixture(t, cfg, -1, realJudgeBlock())
	var key, prdID string
	inner := m.guardrail
	m.SetGuardrail(func(ctx context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Level == "prd" {
			prdID, key = req.PRDID, judgeCouldNotStartTryKey(req)
		}
		return inner(ctx, req)
	})

	prd := run()

	if *reviews != 1 {
		t.Fatalf("judge calls = %d, want 1 (a real verdict is not retried as infrastructure)", *reviews)
	}
	if tries := m.JudgeTries(prdID, key); len(tries) != 0 {
		t.Fatalf("infrastructure tries recorded = %v, want none", tries)
	}
	var blocks []GuardrailVerdict
	for _, v := range prd.Verdicts {
		if v.Guardrail == "rules-review" && v.Outcome == "block" {
			blocks = append(blocks, v)
		}
	}
	if len(blocks) != 1 {
		t.Fatalf("recorded rules-review block verdicts = %+v, want exactly one", prd.Verdicts)
	}
	if len(blocks[0].Issues) != 1 || blocks[0].Issues[0] != realFinding {
		t.Fatalf("block verdict issues = %v, want the finding kept", blocks[0].Issues)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDBlocked {
		t.Fatalf("status = %s, want the Automaton held on the block", prd.Status)
	}
	o := prd.RulesReviewOutcome
	if o == nil {
		t.Fatal("rules review outcome missing, want the finding recorded")
	}
	found := false
	for _, f := range o.Findings {
		if strings.Contains(f, realFinding) {
			found = true
		}
	}
	if !found {
		t.Fatalf("outcome findings = %v, want %q kept", o.Findings, realFinding)
	}
}

// Called directly, a real block verdict reports blocked with no error, and the
// judge runs once.
func TestRunPRDCompletionRulesReviewRealBlockIsBlockedNotRetried(t *testing.T) {
	noInfraSleep(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newRulesReviewManager(t, Config{VerificationModel: "ollama/qwen3.8:27b"})
	calls := 0
	m.SetGuardrail(func(context.Context, GuardrailInvocation) (GuardrailVerdict, error) {
		calls++
		return realJudgeBlock(), nil
	})
	prd := &PRD{ID: "p1", ProjectDir: dir, PerPRDGuardrails: []string{"rules-review"}}
	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !blocked {
		t.Fatal("a real block verdict must block")
	}
	if calls != 1 {
		t.Fatalf("judge calls = %d, want 1", calls)
	}
	if len(prd.Verdicts) != 1 || prd.Verdicts[0].Outcome != "block" || len(prd.Verdicts[0].Issues) != 1 || prd.Verdicts[0].Issues[0] != realFinding {
		t.Fatalf("verdicts = %+v, want the block with its finding", prd.Verdicts)
	}
}
