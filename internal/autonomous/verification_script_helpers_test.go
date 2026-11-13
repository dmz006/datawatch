package autonomous

import (
	"regexp"
	"strings"
	"testing"
)

func TestVerifyStable_HelperTextContent(t *testing.T) {
	got := VerificationScriptHelperText()
	for _, want := range []string{
		"make sync-docs",
		"internal/server/web/docs",
		"git merge-base",
		"cumulative",
		"every file any story of the Automaton may touch",
		"changelog draft",
		"parity issue",
		"registry plan files",
		"gofmt",
		"whole package or directory",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("helper text missing %q", want)
		}
	}
}

func TestVerifyStable_HelperTextBoundedAndGeneric(t *testing.T) {
	got := VerificationScriptHelperText()
	if len(got) > verificationScriptHelperMaxBytes {
		t.Errorf("helper text %d bytes exceeds bound %d", len(got), verificationScriptHelperMaxBytes)
	}
	if regexp.MustCompile(`\bv?\d+\.\d+\.\d+\b`).MatchString(got) {
		t.Errorf("helper text must not contain a fixed release number: %q", got)
	}
	if regexp.MustCompile(`\b(BL|GH|SEC|HLLM)-?\d+\b|\bB\d{2,}\b`).MatchString(got) {
		t.Errorf("helper text must not contain a tracker id: %q", got)
	}
	for _, bad := range []string{"datawatch", "/home/", "dmz"} {
		if strings.Contains(strings.ToLower(got), bad) {
			t.Errorf("helper text must not name %q", bad)
		}
	}
}
