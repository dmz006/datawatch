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
		"#!/usr/bin/env bash",
		"run with bash",
		"the task's own files plus the changelog, README, docs, locales and tests",
		"cumulative across stories",
		"CSS hex colours",
		"not to be preceded by `#`",
		"Never pipe into `grep -q`",
		"here-string",
		"only that story's own scripts",
		"single final_gate",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("helper text missing %q", want)
		}
	}
}

func TestVerifyStable_HelperTextBoundedAndGeneric(t *testing.T) {
	got := VerificationScriptHelperText()
	if verificationScriptHelperMaxBytes != 3200 {
		t.Errorf("helper size cap changed to %d; update this test with the reason", verificationScriptHelperMaxBytes)
	}
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
