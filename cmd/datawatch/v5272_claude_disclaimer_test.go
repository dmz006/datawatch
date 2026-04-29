// v5.27.2 — tests for claudeDisclaimerResponse.
//
// The pure-function pattern classifier extracted from the
// DetectPrompt callback. Covers every seed-filter pattern from
// the bottom of main.go's filter list.

package main

import "testing"

func TestClaudeDisclaimerResponse_TrustFolderMenu(t *testing.T) {
	cases := []string{
		"❯ 1. Yes, I trust this folder",
		"  Yes, I trust the folder",
		"trust this folder",
		"Quick safety check",
	}
	for _, line := range cases {
		got := claudeDisclaimerResponse(line)
		if got != "1\n" {
			t.Errorf("response(%q) = %q want %q", line, got, "1\n")
		}
	}
}

func TestClaudeDisclaimerResponse_LoadingDevelopmentChannels(t *testing.T) {
	cases := []string{
		"  WARNING: Loading development channels",
		"Loading development channels",
		"  ❯ 1. I am using this for local development",
		"I am using this for local development",
	}
	for _, line := range cases {
		got := claudeDisclaimerResponse(line)
		if got != "\n" {
			t.Errorf("response(%q) = %q want %q", line, got, "\n")
		}
	}
}

func TestClaudeDisclaimerResponse_UnrelatedReturnsEmpty(t *testing.T) {
	// Lines that should NOT trip the auto-accept (caller treats
	// "" as no-op).
	cases := []string{
		"",
		"echo hello",
		"Do you want to continue?",
		"[y/N]",
		"What's your favourite colour?",
		"Listening for channel messages from: server:datawatch-x",
	}
	for _, line := range cases {
		got := claudeDisclaimerResponse(line)
		if got != "" {
			t.Errorf("response(%q) = %q want empty (no-op)", line, got)
		}
	}
}

func TestClaudeDisclaimerResponse_CaseInsensitive(t *testing.T) {
	// Patterns are matched case-insensitively so a claude-code
	// release that capitalises differently still triggers.
	if got := claudeDisclaimerResponse("TRUST THIS FOLDER"); got != "1\n" {
		t.Errorf("uppercase: response = %q want %q", got, "1\n")
	}
	if got := claudeDisclaimerResponse("LOADING DEVELOPMENT CHANNELS"); got != "\n" {
		t.Errorf("uppercase: response = %q want %q", got, "\n")
	}
}
