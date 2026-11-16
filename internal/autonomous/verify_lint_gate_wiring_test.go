package autonomous

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// lintWiringSetup returns a manager + PRD whose project dir is a throwaway
// git repo on a feature branch, with a passing verification script.
func lintWiringSetup(t *testing.T) (*Manager, *PRD, string, string) {
	t.Helper()
	m, prd, _ := newScriptGuardrailPRD(t, []string{"rules"}, []string{"rules"})
	m.SetGuardrail(fatalGuardrail(t))
	dir := lintGateRepo(t)
	cmd := exec.Command("git", "checkout", "-q", "-b", "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v %s", err, out)
	}
	prd.ProjectDir = dir
	script := writeVerifyScript(t, dir, "t1.sh", "exit 0\n")
	return m, prd, dir, script
}

func withFakeLint(t *testing.T, fake LintRunner) {
	t.Helper()
	orig := VerifyLintRunner
	VerifyLintRunner = fake
	t.Cleanup(func() { VerifyLintRunner = orig })
}

const unusedFinding = "pkg/a/a.go:3:6: func helper is unused (unused)"

func TestLintGateTaskVerificationFailsOnFindings(t *testing.T) {
	m, prd, dir, script := lintWiringSetup(t)
	writeLintFile(t, dir, "pkg/a/a.go", "package a\n\nfunc helper() {}\n")
	exitErr := exitError(t)
	withFakeLint(t, func(context.Context, string, []string) (string, error) { return unusedFinding, exitErr })

	tk := &Task{ID: "t1", Title: "T", Spec: "do", VerificationScript: script}
	blocked, _, err := m.runPerTaskGuardrails(context.Background(), prd, tk)
	if err != nil || !blocked {
		t.Fatalf("blocked=%v err=%v, want blocked", blocked, err)
	}
	if tk.FailureReason == nil || !strings.HasPrefix(tk.FailureReason.Detail, "lint gate failed") || !strings.Contains(tk.FailureReason.Detail, unusedFinding) {
		t.Fatalf("failure reason = %+v", tk.FailureReason)
	}
}

func TestLintGateTaskVerificationStoryFailsOnFindings(t *testing.T) {
	m, prd, dir, script := lintWiringSetup(t)
	writeLintFile(t, dir, "pkg/a/a.go", "package a\n\nfunc helper() {}\n")
	exitErr := exitError(t)
	withFakeLint(t, func(context.Context, string, []string) (string, error) { return unusedFinding, exitErr })

	st := &Story{ID: "s1", Title: "S", Description: "story", VerificationScript: script}
	blocked, _, err := m.runPerStoryGuardrails(context.Background(), prd, st)
	if err != nil || !blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
	if st.FailureReason == nil || !strings.Contains(st.FailureReason.Detail, unusedFinding) {
		t.Fatalf("failure reason = %+v", st.FailureReason)
	}
}

func TestLintGateTaskVerificationPassesWithoutFindings(t *testing.T) {
	m, prd, dir, script := lintWiringSetup(t)
	writeLintFile(t, dir, "pkg/a/a.go", "package a\n\nfunc helper() {}\n")
	called := false
	withFakeLint(t, func(context.Context, string, []string) (string, error) { called = true; return "", nil })

	tk := &Task{ID: "t1", Title: "T", Spec: "do", VerificationScript: script}
	blocked, _, err := m.runPerTaskGuardrails(context.Background(), prd, tk)
	if err != nil || blocked || !called || tk.FailureReason != nil {
		t.Fatalf("blocked=%v err=%v called=%v reason=%+v", blocked, err, called, tk.FailureReason)
	}
}

func TestLintGateTaskVerificationMarkdownOnlySkipsLint(t *testing.T) {
	m, prd, dir, script := lintWiringSetup(t)
	writeLintFile(t, dir, "docs/notes.md", "# notes\n")
	withFakeLint(t, func(context.Context, string, []string) (string, error) {
		t.Fatal("lint runner called for markdown-only change")
		return "", nil
	})

	tk := &Task{ID: "t1", Title: "T", Spec: "do", VerificationScript: script}
	blocked, _, err := m.runPerTaskGuardrails(context.Background(), prd, tk)
	if err != nil || blocked {
		t.Fatalf("blocked=%v err=%v", blocked, err)
	}
}

func TestLintGateTaskVerificationScriptFailSkipsLint(t *testing.T) {
	m, prd, dir, _ := lintWiringSetup(t)
	m.SetGuardrail(func(_ context.Context, inv GuardrailInvocation) (GuardrailVerdict, error) {
		return GuardrailVerdict{Outcome: "pass", Summary: "ok"}, nil
	})
	writeLintFile(t, dir, "pkg/a/a.go", "package a\n\nfunc helper() {}\n")
	script := writeVerifyScript(t, dir, "bad.sh", "exit 1\n")
	withFakeLint(t, func(context.Context, string, []string) (string, error) {
		t.Fatal("lint runner called after a failing script")
		return "", nil
	})
	tk := &Task{ID: "t1", Title: "T", Spec: "do", VerificationScript: script}
	if _, _, err := m.runPerTaskGuardrails(context.Background(), prd, tk); err != nil {
		t.Fatal(err)
	}
}
