package autonomous

import (
	"os"
	"path/filepath"
	"testing"
)

func newPreflightEditManager(t *testing.T) (*Manager, string, string) {
	t.Helper()
	mgr, id := newReadOnlyFlagManager(t, PRDNeedsReview)
	prd, _ := mgr.Store().GetPRD(id)
	return mgr, id, prd.ProjectDir
}

func writeEditScript(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func editLintFindings(t *testing.T, mgr *Manager, id string) []LintFinding {
	t.Helper()
	prd, _ := mgr.Store().GetPRD(id)
	if prd.DecomposeLint == nil {
		return nil
	}
	return prd.DecomposeLint.Findings
}

func TestVerifyScriptPreflightEdit(t *testing.T) {
	t.Run("syntax error records one finding", func(t *testing.T) {
		mgr, id, dir := newPreflightEditManager(t)
		writeEditScript(t, dir, "scripts/verify.sh", "#!/bin/bash\nif true; then\n  echo hi\n")
		if _, err := mgr.EditTask(id, "t1", "", nil, "", "scripts/verify.sh", "", "operator"); err != nil {
			t.Fatalf("EditTask: %v", err)
		}
		fs := editLintFindings(t, mgr, id)
		if len(fs) != 1 {
			t.Fatalf("findings = %+v, want exactly one", fs)
		}
		if fs[0].Script != "scripts/verify.sh" || fs[0].Task != "t1" {
			t.Fatalf("finding = %+v", fs[0])
		}
		prd, _ := mgr.Store().GetPRD(id)
		if prd.DecomposeLint.Mode != "off" {
			t.Fatalf("mode = %q, want off", prd.DecomposeLint.Mode)
		}
		// Re-editing the same script replaces, not duplicates.
		if _, err := mgr.EditTask(id, "t1", "", nil, "", "scripts/verify.sh", "", "operator"); err != nil {
			t.Fatalf("EditTask again: %v", err)
		}
		if n := len(editLintFindings(t, mgr, id)); n != 1 {
			t.Fatalf("after re-edit findings = %d, want 1", n)
		}
	})
	t.Run("clean script records none", func(t *testing.T) {
		mgr, id, dir := newPreflightEditManager(t)
		writeEditScript(t, dir, "scripts/verify.sh", "#!/bin/bash\nset -e\necho ok\n")
		if _, err := mgr.EditTask(id, "t1", "", nil, "", "scripts/verify.sh", "", "operator"); err != nil {
			t.Fatalf("EditTask: %v", err)
		}
		if fs := editLintFindings(t, mgr, id); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none", fs)
		}
	})
	t.Run("empty script argument records nothing new", func(t *testing.T) {
		mgr, id, dir := newPreflightEditManager(t)
		writeEditScript(t, dir, "scripts/verify.sh", "#!/bin/bash\nif true; then\n")
		if _, err := mgr.EditTask(id, "t1", "new spec", nil, "", "", "", "operator"); err != nil {
			t.Fatalf("EditTask: %v", err)
		}
		if fs := editLintFindings(t, mgr, id); len(fs) != 0 {
			t.Fatalf("findings = %+v, want none", fs)
		}
	})
}
