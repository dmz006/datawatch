// BL441 — diagnosis of the 2026-10-14 BL440 incident (PRD 5d599cef): a real
// PRD-completion rules-review content block left the PRD silently "running"
// and synthesizeCorrectiveWork never appeared to fire.
//
// FINDING (run on unmodified code): this test PASSES. The in-process
// rollup path is NOT broken, so hypotheses (a) and (b) are refuted:
//   - runPRDCompletionRulesReview returns (false, &verdict, nil) for an
//     empty-Category block while the default content budget remains, and
//     the guardrail-registry dispatch ("rules-review" via PerPRDGuardrails)
//     reaches it;
//   - executor.go's contentFixVerdict branch passes stillRunning(), calls
//     synthesizeCorrectiveWork(prd, v, ""), appends a standalone story+task
//     to the SAME PRD, and the recursive Run() executes it and re-runs the
//     review (PRD ends completed, 2 stories, RetryCountsByCategory[content]=1).
//
// The same result held with PerStoryApproval=true, MaxConcurrentTasks=4,
// tasks pre-completed (resume-style) and tasks executed inside Run, and with
// a verdict carrying Severity+Issues. The daemon's real guardrail
// (cmd/datawatch judgedGuardrailCategory) also yields Category "" for a
// parsed non-policy judgment, so the category mapping is not the cause.
//
// Also ruled out by code reading (not by test): a guardrail error does
// not leave the PRD running — API.Run reverts it to PRDApproved; and
// boot-resume (api.go resumeRunningPRDs) calls Run() for every stored
// PRDRunning PRD, so a daemon restart re-fires the rollup even when all
// tasks are already terminal.
//
// DETERMINATION: (a) and (b) are false on current code; a reproducer
// that FAILS on unmodified code does not exist, so this file is a
// regression guard, not the requested failing repro. Of the three
// hypotheses only (c)-style or live-only causes remain, and the repo
// alone cannot choose between them: the incident PRD (5d599cef, 2026-10-14)
// ran before BL451 (v9.1.1, 2026-10-15), whose commit-time stale-rollup
// check changed this path, and possibly on a binary older than the
// PRD-level branch's ci-watch rework (e736a623, 2026-10-13). Settling it
// needs the daemon log for that run's guardrail session timestamps
// (restart mid-guardrail vs. a block that took the Category!="" path).
package autonomous

import (
	"context"
	"testing"
)

// BL441 — PRD-completion rules-review content block must synthesize
// corrective work on the same PRD instead of leaving it silently running.
func TestBL441_PRDCompletionContentBlockSynthesizesCorrectiveWork(t *testing.T) {
	m, _ := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	prdLevelCalls := 0
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Level != "prd" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		prdLevelCalls++
		if prdLevelCalls == 1 {
			// Empty Category == content.
			return GuardrailVerdict{Outcome: "block", Summary: "docs missing", Issues: []string{"update docs/howto/x.md"}}, nil
		}
		return GuardrailVerdict{Outcome: "pass"}, nil
	})
	prd, _ := bl451RunningPRD(t, m, []string{"rules-review"}, TaskCompleted)

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	t.Logf("status=%s stories=%d prdLevelCalls=%d retry=%v verdicts=%d", p.Status, len(p.Story), prdLevelCalls, p.RetryCountsByCategory, len(p.Verdicts))
	if len(p.Story) != 2 {
		t.Fatalf("want corrective story appended to same PRD (2 stories), got %d", len(p.Story))
	}
	if len(p.Story[1].Tasks) != 1 {
		t.Fatalf("want 1 corrective task, got %d", len(p.Story[1].Tasks))
	}
	if p.Status == PRDRunning {
		t.Fatalf("PRD left silently running")
	}
}
