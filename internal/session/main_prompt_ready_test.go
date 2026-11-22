package session

import (
	"os"
	"strings"
	"testing"
)

func TestMainPromptVisible_Claude2_1_330Idle(t *testing.T) {
	b, err := os.ReadFile("testdata/startup/claude-2-1-330-idle-prompt.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !mainPromptVisible(string(b)) {
		t.Fatal("claude 2.1.330 idle screen not recognised as main prompt")
	}
	if !mainPromptVisible("  ──────\n  ❯ \n  ──────\n") {
		t.Fatal("bare prompt line not recognised")
	}
}

func TestMainPromptVisible_ChoiceDialogNotReady(t *testing.T) {
	b, err := os.ReadFile("testdata/startup/trust-folder.txt")
	if err != nil {
		t.Fatal(err)
	}
	if mainPromptVisible(string(b)) {
		t.Fatal("trust-folder dialog read as ready")
	}
	dlg := "│ Do you trust this folder?\n│ ❯ 1. Yes, I trust this folder\n│   2. No, exit\n"
	if mainPromptVisible(dlg) {
		t.Fatal("framed choice dialog read as ready")
	}
	if got := summarizePane(dlg); !strings.Contains(got, "choice dialog") {
		t.Fatalf("summary = %q", got)
	}
	if got := summarizePane("\n a  b\n\n c\n"); !strings.Contains(got, "a b | c") {
		t.Fatalf("summary = %q", got)
	}
}
