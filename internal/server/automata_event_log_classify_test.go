package server

import (
	"strings"
	"testing"
)

func TestAutomataEventErrorClassifyHarmlessPermission(t *testing.T) {
	raw := "stat /home/dmz/.datawatch/worktrees/auto-9: permission denied (removed worktree)"
	got := automataEventErrorClassify(raw, "worktree")

	if !got.Quiet {
		t.Errorf("Quiet = false, want true for harmless permission error")
	}
	if got.Severity != AutomataSeverityInfo {
		t.Errorf("Severity = %q, want info", got.Severity)
	}
	if !strings.Contains(got.Reason, "permission denied") || !strings.Contains(got.Reason, "removed worktree") {
		t.Errorf("Reason = %q, want the raw reason kept visible", got.Reason)
	}
	if !strings.Contains(got.Summary, "Harmless") {
		t.Errorf("Summary = %q, want it to say harmless", got.Summary)
	}
	if strings.Contains(got.Summary, "\n") || strings.Contains(got.Reason, "\n") {
		t.Errorf("text must be one line: summary=%q reason=%q", got.Summary, got.Reason)
	}
}

func TestAutomataEventErrorClassifyRealPermission(t *testing.T) {
	raw := "open /etc/datawatch/secrets.env: permission denied"
	got := automataEventErrorClassify(raw, "config")

	if got.Quiet {
		t.Errorf("Quiet = true, want false for real permission error")
	}
	if got.Severity != AutomataSeverityError {
		t.Errorf("Severity = %q, want error", got.Severity)
	}
	if !strings.Contains(got.NextAction, "Grant the daemon user access") {
		t.Errorf("NextAction = %q, want concrete access fix", got.NextAction)
	}
	if got.NextAction == "" || got.Reason == "" || got.Summary == "" {
		t.Errorf("all fields must be set: %+v", got)
	}
}

func TestAutomataEventErrorClassifyUnknownError(t *testing.T) {
	raw := "connection reset by peer"
	got := automataEventErrorClassify(raw, "federation")

	if got.Quiet {
		t.Errorf("Quiet = true, want false for unknown error")
	}
	if got.Severity != AutomataSeverityError {
		t.Errorf("Severity = %q, want error", got.Severity)
	}
	if !strings.Contains(got.NextAction, "Open the daemon log") {
		t.Errorf("NextAction = %q, want concrete next step", got.NextAction)
	}
	if !strings.Contains(got.Summary, "federation") || !strings.Contains(got.Summary, raw) {
		t.Errorf("Summary = %q, want source and raw text", got.Summary)
	}
}

func TestAutomataEventErrorClassifyEmptyString(t *testing.T) {
	got := automataEventErrorClassify("   ", "")

	if got.Quiet {
		t.Errorf("Quiet = true, want false for empty error")
	}
	if got.Severity != AutomataSeverityError {
		t.Errorf("Severity = %q, want error", got.Severity)
	}
	if !strings.Contains(got.Summary, "source") {
		t.Errorf("Summary = %q, want the default source name", got.Summary)
	}
	if got.NextAction == "" {
		t.Errorf("NextAction empty, want a concrete step")
	}
}

func TestAutomataEventErrorClassifyNeverSaysPRD(t *testing.T) {
	got := automataEventErrorClassify("PRD 7 failed: connection refused", "automaton")
	for _, s := range []string{got.Summary, got.Reason, got.NextAction} {
		if strings.Contains(s, "PRD") {
			t.Errorf("operator text %q still says PRD", s)
		}
	}
	if !strings.Contains(got.Summary, "Automaton") {
		t.Errorf("Summary = %q, want Automaton wording", got.Summary)
	}
}

func TestAutomataEventErrorClassifyLongSummaryCapped(t *testing.T) {
	raw := strings.Repeat("x", 500)
	got := automataEventErrorClassify(raw, "temp")
	if n := len([]rune(got.Summary)); n > automataErrorSummaryLimit+len("Error in temp: ") {
		t.Errorf("summary length %d exceeds cap", n)
	}
}
