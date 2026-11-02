package audit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestUIPromptDismiss_AuditActionsVerify: the three dismissal outcomes
// (dismissed / not_cleared / refused) are first-class audit actions with
// their own fixed CEF signatures (62/63/64), and the daemon's entry shape
// (Actor "autonomous", ui_prompt_* details) round-trips through
// FormatCEFLine with the values intact.
func TestUIPromptDismiss_AuditActionsVerify(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	rows := []struct {
		action   string
		sigID    int
		header   string
		severity int
		key      string // the dismiss key the outcome refers to
	}{
		{ActionUIPromptDismissed, 62, "UIPromptDismissed", 2, "esc"},
		{ActionUIPromptNotCleared, 63, "UIPromptNotCleared", 3, "esc"},
		{ActionUIPromptRefused, 64, "UIPromptRefused", 4, "enter"},
	}
	for _, r := range rows {
		e := Entry{
			Timestamp: now,
			Actor:     "autonomous",
			Action:    r.action,
			SessionID: "tmux:sess-001:full-id",
			Details: map[string]any{
				"ui_prompt_kind":            "autocompact-threshold",
				"ui_prompt_key":             r.key,
				"ui_prompt_selected_option": "Autocompact at 400k",
			},
		}
		line := FormatCEFLine(e)

		if !strings.Contains(line, "CEF:0|") {
			t.Fatalf("%s: line missing CEF header: %q", r.action, line)
		}
		sig := fmt.Sprintf("|%d|%s|%d|", r.sigID, r.header, r.severity)
		if !strings.Contains(line, sig) {
			t.Errorf("%s: CEF signature block missing:\n  want %q\n  got  %q", r.action, sig, line)
		}
		if !strings.Contains(line, "suser=autonomous") {
			t.Errorf("%s: actor missing: %q", r.action, line)
		}
		if !strings.Contains(line, "deviceCustomString2Label=session_id") {
			t.Errorf("%s: session slot missing: %q", r.action, line)
		}
		if !strings.Contains(line, "autocompact-threshold") {
			t.Errorf("%s: kind value missing: %q", r.action, line)
		}
		if !strings.Contains(line, "Autocompact at 400k") {
			t.Errorf("%s: selected option missing: %q", r.action, line)
		}
	}
	if ActionUIPromptDismissed == ActionUIPromptNotCleared ||
		ActionUIPromptNotCleared == ActionUIPromptRefused ||
		ActionUIPromptDismissed == ActionUIPromptRefused {
		t.Fatal("dismissible outcome actions must be distinct strings")
	}
}

// TestUIPromptDismiss_CEFDetailEscape: a detail value carries CEF-unsafe
// characters (backslash, equals, newline — realistic for a TUI pane
// snapshot) and the escape must keep the value intact in the line.
func TestUIPromptDismiss_CEFDetailEscape(t *testing.T) {
	e := Entry{
		Timestamp: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC),
		Actor:     "autonomous",
		Action:    ActionUIPromptRefused,
		Details:   map[string]any{"ui_prompt_key": "back\\slash =eq line1\nline2"},
	}
	line := FormatCEFLine(e)
	// backslash -> double backslash, '=' -> backslash '=', newline -> '\n'.
	if !strings.Contains(line, "back\\\\slash \\=eq line1\\nline2") {
		t.Fatalf("escape broke the detail value:\n%s", line)
	}
}
