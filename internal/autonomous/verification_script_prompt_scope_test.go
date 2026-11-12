// Scope-rule tests for verificationScriptSuffix: a verification script
// must check only the files its task changes and must never format a
// whole package or directory.
package autonomous

import (
	"strings"
	"testing"
)

func TestVerificationScriptSuffix_ChangedFilesOnly(t *testing.T) {
	fn := func(req DecomposeRequest) (string, error) {
		return `{}`, nil
	}
	m, err := NewManager(t.TempDir(), DefaultConfig(), fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	prd, err := m.CreatePRD("add a feature", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if prd.ProjectDir == "" {
		t.Fatal("PRD.ProjectDir is empty, test precondition failed")
	}

	got := m.verificationScriptSuffix(prd)
	for _, want := range []string{"only the files", "gofmt"} {
		if !strings.Contains(got, want) {
			t.Errorf("verificationScriptSuffix does not contain %q", want)
		}
	}
}
