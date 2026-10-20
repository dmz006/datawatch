package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous/scan"
)

// rulesReviewBlockManager builds a Manager whose PRD-level judge blocks
// with a non-retryable "policy" category, so the content auto-fix never
// fires and the rules-review block reaches PRDBlocked. The judge is a
// recorded fake: task-level and story-level units pass, and the PRD-level
// review returns the block verdict with a judge session id set.
func rulesReviewBlockManager(t *testing.T) (*Manager, *PRD) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fakeDecompose)
	if err != nil {
		t.Fatal(err)
	}
	m.SetGuardrail(func(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
		if req.Level != "prd" {
			return GuardrailVerdict{Outcome: "pass"}, nil
		}
		return GuardrailVerdict{
			Outcome:        "block",
			Category:       "policy",
			Summary:        "AGENT.md requires a changelog entry",
			JudgeSessionID: "judge-7f3a",
		}, nil
	})

	prd, err := m.CreatePRD("spec", dir, "claude", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.PerPRDGuardrails = []string{"rules-review"}
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	return m, prd
}

func TestBlockedReason_RulesReviewBlockNamesGuardrailAndJudge(t *testing.T) {
	m, prd := rulesReviewBlockManager(t)

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked by rules-review", got.Status)
	}
	for _, want := range []string{"rules-review", "judge-7f3a"} {
		if !strings.Contains(got.BlockedReason, want) {
			t.Errorf("BlockedReason %q missing %q", got.BlockedReason, want)
		}
	}
	if want := blockedReasonFor(GuardrailVerdict{
		Guardrail:      "rules-review",
		Outcome:        "block",
		Summary:        "AGENT.md requires a changelog entry",
		JudgeSessionID: "judge-7f3a",
	}); got.BlockedReason != want {
		t.Errorf("BlockedReason = %q, want %q", got.BlockedReason, want)
	}
}

func TestBlockedReason_SavePRDNonBlockedStatusClearsReason(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "prds.json"))
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "p1", Status: PRDBlocked, BlockedReason: "guardrail rules-review returned block (judge-7f3a) - x"}
	if err := st.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	if prd.BlockedReason == "" {
		t.Fatal("blocked PRD lost its BlockedReason on save")
	}

	prd.Status = PRDApproved
	if err := st.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetPRD("p1")
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q after save with status %s, want empty", got.BlockedReason, got.Status)
	}
}

func TestBlockedReason_CommitPRDIfRevNonBlockedStatusClearsReason(t *testing.T) {
	st, err := NewStore(filepath.Join(t.TempDir(), "prds.json"))
	if err != nil {
		t.Fatal(err)
	}
	prd := &PRD{ID: "p1", Status: PRDBlocked, BlockedReason: "stale"}
	if err := st.SavePRD(prd); err != nil {
		t.Fatal(err)
	}
	rev, _ := st.PRDRev("p1")
	err = st.CommitPRDIfRev("p1", rev, func(p *PRD) bool {
		p.Status = PRDCompleted
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetPRD("p1")
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q after commit to %s, want empty", got.BlockedReason, got.Status)
	}
}

func TestBlockedReason_ProjectRulesBlockLeavesReasonEmpty(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Scan.ProjectRules = []scan.ProjectRule{
		{ID: "prd-rule", Name: "prd-level check", Type: scan.RuleTypePresence,
			Granularity: scan.GranularityPRD, Severity: scan.SeverityError, Pattern: "nonexistent-prd.md"},
	}
	m, err := NewManager(t.TempDir(), cfg, fakeDecompose)
	if err != nil {
		t.Fatal(err)
	}
	prd, err := m.CreatePRD("spec", t.TempDir(), "claude", "", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "do"}}}})
	prd, _ = m.Store().GetPRD(prd.ID)
	prd.Status = PRDApproved
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatal(err)
	}

	if err := m.Run(context.Background(), prd.ID, fakeSpawn, fakeVerify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDBlocked {
		t.Fatalf("status = %s, want blocked by project-rules", got.Status)
	}
	if got.BlockedReason != "" {
		t.Errorf("BlockedReason = %q for a project-rules block, want empty", got.BlockedReason)
	}
}
