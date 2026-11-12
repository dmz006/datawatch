package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	rrBaseSHA   = "b45e0123456789abcdef0123456789abcdef0123"
	rrBaseText  = "BASE RULES: every change needs a test."
	rrWorkerTxt = "WORKER REWRITE: nothing is required."
)

// rrRecorder is a fake guardrail function that records every invocation.
type rrRecorder struct{ invs []GuardrailInvocation }

func (r *rrRecorder) fn(_ context.Context, req GuardrailInvocation) (GuardrailVerdict, error) {
	r.invs = append(r.invs, req)
	return GuardrailVerdict{Outcome: "pass", Summary: "ok"}, nil
}

// rrSetup builds a manager with a fake git runner whose base commit is
// rrBaseSHA, and a PRD whose working tree holds the given files (the
// "worker's" state). Base files are added to the fake with git.files.
func rrSetup(t *testing.T, cfg Config, workTree map[string]string) (*Manager, *fakeBaseGit, *PRD, *rrRecorder) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range workTree {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg.VerificationModel = "ollama/qwen3.8:27b"
	m := newRulesReviewManager(t, cfg)
	g := newFakeBaseGit()
	g.good(rrBaseSHA)
	m.driftGit = g.run
	rec := &rrRecorder{}
	m.SetGuardrail(rec.fn)
	prd := &PRD{
		ID: "rr1", Spec: "do the thing", ProjectDir: dir,
		PerPRDGuardrails: []string{"rules-review"},
		Story:            []Story{{ID: "s1", Tasks: []Task{{ID: "t1", BaseSHA: rrBaseSHA}}}},
	}
	return m, g, prd, rec
}

func rrDecisions(prd *PRD, kind string) []Decision {
	var out []Decision
	for _, d := range prd.Decisions {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

func TestRulesReviewBaseJudgeUsesBaseCommitRules(t *testing.T) {
	m, g, prd, rec := rrSetup(t, Config{}, map[string]string{"AGENT.md": rrWorkerTxt})
	g.files[rrBaseSHA+":AGENT.md"] = rrBaseText

	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v, want a clean pass", blocked, err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(rec.invs))
	}
	spec := rec.invs[0].UnitSpec
	if !strings.Contains(spec, rrBaseText) {
		t.Errorf("UnitSpec lacks the base commit's rules text:\n%s", spec)
	}
	if strings.Contains(spec, rrWorkerTxt) {
		t.Errorf("UnitSpec contains the worker's rewritten rules:\n%s", spec)
	}
	head := "Rules file AGENT.md as of commit " + shortSHA(rrBaseSHA) + " (the state before this Automaton's changes)"
	if !strings.Contains(spec, head) {
		t.Errorf("UnitSpec lacks the delimited heading %q:\n%s", head, spec)
	}
	for _, want := range []string{"do NOT read the rules from your working directory", "report the difference as a finding", "judgment review"} {
		if !strings.Contains(strings.ToLower(spec), strings.ToLower(want)) {
			t.Errorf("UnitSpec lacks %q", want)
		}
	}
	if strings.Contains(spec, "at the root of your working directory yourself") {
		t.Error("the base-commit text must not carry the working-directory pointer")
	}
	if got := rrDecisions(prd, "rules_review_source_fallback"); len(got) != 0 {
		t.Errorf("unexpected fallback Decision: %+v", got)
	}
}

func TestRulesReviewBaseDeletedRulesFileStillReviewed(t *testing.T) {
	// The worker deleted AGENT.md: the working tree has no rules file at all.
	m, g, prd, rec := rrSetup(t, Config{}, nil)
	g.files[rrBaseSHA+":AGENT.md"] = rrBaseText

	if _, _, err := m.runPRDCompletionRulesReview(context.Background(), prd); err != nil {
		t.Fatalf("review: %v", err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("judge calls = %d, want 1 (a deleted rules file must not skip the review)", len(rec.invs))
	}
	if !strings.Contains(rec.invs[0].UnitSpec, rrBaseText) {
		t.Errorf("UnitSpec lacks the base text:\n%s", rec.invs[0].UnitSpec)
	}
}

func TestRulesReviewBaseFileAbsentAtBaseIsLeftOut(t *testing.T) {
	// CONTEXT.md exists only in the worker's tree; AGENT.md only at the base.
	m, g, prd, rec := rrSetup(t, Config{}, map[string]string{"CONTEXT.md": "worker added context"})
	g.files[rrBaseSHA+":AGENT.md"] = rrBaseText

	if _, _, err := m.runPRDCompletionRulesReview(context.Background(), prd); err != nil {
		t.Fatalf("review: %v", err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(rec.invs))
	}
	spec := rec.invs[0].UnitSpec
	if !strings.Contains(spec, "Rules file AGENT.md as of commit") {
		t.Errorf("AGENT.md block missing:\n%s", spec)
	}
	if strings.Contains(spec, "CONTEXT.md") || strings.Contains(spec, "worker added context") {
		t.Errorf("a file absent at the base must be left out:\n%s", spec)
	}
}

func TestRulesReviewBaseNoFileAtBaseSkipsReview(t *testing.T) {
	// The worker created both files, neither exists at the base.
	m, _, prd, rec := rrSetup(t, Config{}, map[string]string{"AGENT.md": rrWorkerTxt, "CONTEXT.md": "ctx"})

	if got := m.rulesReviewSuffix(context.Background(), prd); got != "" {
		t.Errorf("suffix = %q, want empty when no file exists at the base", got)
	}
	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v, want skipped", blocked, err)
	}
	if len(rec.invs) != 0 {
		t.Errorf("judge was called %d times, want 0", len(rec.invs))
	}
	if got := rrDecisions(prd, "rules_review_source_fallback"); len(got) != 0 {
		t.Errorf("a missing file is not a fallback: %+v", got)
	}
}

func TestRulesReviewBaseWorktreeSourceKeepsOldPointer(t *testing.T) {
	m, g, prd, rec := rrSetup(t, Config{Automation: AutomationConfig{RulesReviewSource: config.RulesReviewSourceWorktree}},
		map[string]string{"AGENT.md": rrWorkerTxt})
	g.files[rrBaseSHA+":AGENT.md"] = rrBaseText

	if _, _, err := m.runPRDCompletionRulesReview(context.Background(), prd); err != nil {
		t.Fatalf("review: %v", err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(rec.invs))
	}
	spec := rec.invs[0].UnitSpec
	if !strings.Contains(spec, "Read AGENT.md at the root of your working directory yourself") {
		t.Errorf("worktree source lost the pointer text:\n%s", spec)
	}
	if strings.Contains(spec, rrBaseText) || strings.Contains(spec, rrWorkerTxt) {
		t.Errorf("worktree source must not embed any rules text:\n%s", spec)
	}
	// The only git call allowed is the head read that anchors the verdict;
	// the rules text itself is never read through git.
	for _, call := range g.calls {
		if strings.Join(call, " ") != "rev-parse HEAD" {
			t.Errorf("worktree source must not read rules through git, saw %v", g.calls)
			break
		}
	}
	if got := rrDecisions(prd, "rules_review_source_fallback"); len(got) != 0 {
		t.Errorf("unexpected fallback Decision: %+v", got)
	}
}

func TestRulesReviewBaseUnresolvedBaseFallsBack(t *testing.T) {
	m, _, prd, rec := rrSetup(t, Config{}, map[string]string{"AGENT.md": rrWorkerTxt})
	prd.Story[0].Tasks[0].BaseSHA = "" // no base commit to resolve

	blocked, _, err := m.runPRDCompletionRulesReview(context.Background(), prd)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v, want the review to proceed", blocked, err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(rec.invs))
	}
	if !strings.Contains(rec.invs[0].UnitSpec, "Read AGENT.md at the root of your working directory yourself") {
		t.Errorf("fallback should use the working-tree pointer:\n%s", rec.invs[0].UnitSpec)
	}
	got := rrDecisions(prd, "rules_review_source_fallback")
	if len(got) != 1 {
		t.Fatalf("fallback Decisions = %d, want exactly 1", len(got))
	}
	if got[0].Actor != "autonomous" || !strings.Contains(got[0].Note, "base commit") {
		t.Errorf("fallback Decision = %+v", got[0])
	}
}

func TestRulesReviewBaseGitReadFailureFallsBack(t *testing.T) {
	m, g, prd, rec := rrSetup(t, Config{}, map[string]string{"AGENT.md": rrWorkerTxt})
	g.showErr = os.ErrPermission // a read failure that is not "file missing"

	if _, _, err := m.runPRDCompletionRulesReview(context.Background(), prd); err != nil {
		t.Fatalf("review must not fail on a fallback: %v", err)
	}
	if len(rec.invs) != 1 || !strings.Contains(rec.invs[0].UnitSpec, "at the root of your working directory") {
		t.Fatalf("want one working-tree review, got %+v", rec.invs)
	}
	got := rrDecisions(prd, "rules_review_source_fallback")
	if len(got) != 1 || !strings.Contains(got[0].Note, "permission") {
		t.Fatalf("fallback Decisions = %+v, want one naming the git failure", got)
	}
}

func TestRulesReviewBaseCustomRulesFileNameReadFromBase(t *testing.T) {
	m, g, prd, rec := rrSetup(t, Config{RulesFile: "RULES.md"}, map[string]string{"RULES.md": rrWorkerTxt})
	g.files[rrBaseSHA+":RULES.md"] = "BASE RULES.md TEXT"
	g.files[rrBaseSHA+":AGENT.md"] = "must not be read"

	if _, _, err := m.runPRDCompletionRulesReview(context.Background(), prd); err != nil {
		t.Fatalf("review: %v", err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(rec.invs))
	}
	spec := rec.invs[0].UnitSpec
	if !strings.Contains(spec, "Rules file RULES.md as of commit") || !strings.Contains(spec, "BASE RULES.md TEXT") {
		t.Errorf("RULES.md not read from the base:\n%s", spec)
	}
	if strings.Contains(spec, "must not be read") || strings.Contains(spec, rrWorkerTxt) {
		t.Errorf("wrong file or working-tree text embedded:\n%s", spec)
	}
	if !g.sawCall("show", rrBaseSHA+":RULES.md") {
		t.Errorf("RULES.md was never read with git show; calls %v", g.calls)
	}
}

func TestRulesReviewBaseTruncationNote(t *testing.T) {
	m, g, prd, _ := rrSetup(t, Config{}, nil)
	g.files[rrBaseSHA+":AGENT.md"] = strings.Repeat("x", baseCommitFileCap+100)

	spec := m.rulesReviewSuffix(context.Background(), prd)
	if !strings.Contains(spec, "[truncated: AGENT.md is longer than") {
		t.Errorf("truncated file carries no note:\n%.400s", spec)
	}

	g.files[rrBaseSHA+":AGENT.md"] = rrBaseText
	if spec := m.rulesReviewSuffix(context.Background(), prd); strings.Contains(spec, "truncated") {
		t.Errorf("a complete file must carry no truncation note:\n%s", spec)
	}
}

func TestRulesReviewBaseRejudgeUsesBaseCommit(t *testing.T) {
	m, prd, _ := newRejudgeBlockedPRD(t)
	if err := os.WriteFile(filepath.Join(prd.ProjectDir, "AGENT.md"), []byte(rrWorkerTxt), 0o644); err != nil {
		t.Fatal(err)
	}
	g := newFakeBaseGit()
	g.good(rrBaseSHA)
	g.files[rrBaseSHA+":AGENT.md"] = rrBaseText
	m.driftGit = g.run
	prd.Story[0].Tasks[0].BaseSHA = rrBaseSHA
	if err := m.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}
	rec := &rrRecorder{}
	m.SetGuardrail(rec.fn)

	if _, err := m.RejudgeCompletionGuardrail(context.Background(), prd.ID, "operator", ""); err != nil {
		t.Fatalf("rejudge: %v", err)
	}
	if len(rec.invs) != 1 {
		t.Fatalf("judge calls = %d, want 1", len(rec.invs))
	}
	spec := rec.invs[0].UnitSpec
	if !strings.Contains(spec, rrBaseText) || strings.Contains(spec, rrWorkerTxt) {
		t.Errorf("rejudge did not build its invocation from the base commit:\n%s", spec)
	}
}
