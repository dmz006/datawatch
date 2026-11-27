package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/mergeshadow"
)

// The hook hands the plan the Automaton's release size, draft path, review
// commit and root-file check.
func TestMergeShadowHookFillsThePlanInputs(t *testing.T) {
	repo := shadowRepo(t)
	m := shadowManager(t)
	prd := finishedShadowPRD(repo)
	prd.Kind = "bug"
	prd.Verdicts = []GuardrailVerdict{{Guardrail: "rules-review", Outcome: "pass", JudgedCommitSHA: "abc123", VerdictAt: time.Now()}}
	in, ok := m.mergeShadowInput(prd, m.mergeShadowSettings())
	if !ok {
		t.Fatal("no input")
	}
	if in.ReleaseSize != mergeshadow.ReleaseSizePatch {
		t.Errorf("release size %q, want patch for a bug", in.ReleaseSize)
	}
	if in.Review == nil || in.Review.Commit != "abc123" {
		t.Errorf("review %+v", in.Review)
	}
	if in.ChangelogDraftPath != "docs/plans/drafts/p-shadow-changelog.md" || !in.CheckRootFiles {
		t.Errorf("draft %q root check %v", in.ChangelogDraftPath, in.CheckRootFiles)
	}
	prd.ReleaseSize = "major"
	if in, _ := m.mergeShadowInput(prd, m.mergeShadowSettings()); in.ReleaseSize != mergeshadow.ReleaseSizeMajor {
		t.Errorf("own release size: %q", in.ReleaseSize)
	}
}

// A plan that cannot be made is stored with the reason.
func TestMergeShadowHookRecordsWhyNoPlanWasMade(t *testing.T) {
	repo := shadowRepo(t)
	prev := mergeShadowRun
	mergeShadowRun = func(context.Context, mergeshadow.Input) (*mergeshadow.MergePlan, error) {
		return nil, errors.New("git broke")
	}
	t.Cleanup(func() { mergeShadowRun = prev })
	check := func(m *Manager, id, why string) {
		t.Helper()
		p, err := mergeshadow.Load(m.dataDir, id)
		if err != nil {
			t.Fatalf("no plan stored: %v", err)
		}
		if len(p.Findings) != 1 || p.Findings[0].Code != mergeshadow.PlanNotMadeCode || !p.Findings[0].Blocking ||
			!strings.Contains(p.Findings[0].Message, why) {
			t.Errorf("findings %+v, want %s saying %q", p.Findings, mergeshadow.PlanNotMadeCode, why)
		}
	}
	m := shadowManager(t)
	done := m.startMergeShadow(finishedShadowPRD(repo))
	select {
	case <-done:
	case <-time.After(time.Minute):
		t.Fatal("run did not finish")
	}
	check(m, "p-shadow", "git broke")

	m = shadowManager(t)
	prd := finishedShadowPRD(repo)
	prd.Git.Branch = ""
	if done := m.startMergeShadow(prd); done != nil {
		t.Fatal("a run was started with no branch")
	}
	check(m, "p-shadow", "no work branch")
}
