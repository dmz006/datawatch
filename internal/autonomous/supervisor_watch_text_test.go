// Supervisor watch text — operator-facing wording tests.
package autonomous

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var (
	supervisorWatchPRDRe     = regexp.MustCompile(`(?i)\bPRD\b`)
	supervisorWatchTrackerRe = regexp.MustCompile(`(?i)\bA?BL\d+\b|\b[A-Z]{2,4}-\d{3,}\b|\b\d{7,}\b`)
)

// TestSupervisorWatchTextEveryKindHasSingleLineText: every stable kind
// produces a non-empty, single-line text that names the Automaton or
// Automata wherever a work item is meant.
func TestSupervisorWatchTextEveryKindHasSingleLineText(t *testing.T) {
	for _, kind := range SupervisorWatchKinds {
		t.Run(string(kind), func(t *testing.T) {
			text, err := SupervisorWatchText(kind, "the-subject")
			if err != nil {
				t.Fatalf("SupervisorWatchText(%q): %v", kind, err)
			}
			if strings.TrimSpace(text) == "" {
				t.Fatalf("text is empty for kind %q", kind)
			}
			if strings.ContainsAny(text, "\r\n") {
				t.Fatalf("text for %q is not a single line: %q", kind, text)
			}
			// Work items must name the Automaton or the Automata; the
			// node, lane, checkout and CI kinds still name the Automaton
			// fleet they serve.
			lower := strings.ToLower(text)
			if !strings.Contains(lower, "automaton") && !strings.Contains(lower, "automata") {
				t.Fatalf("text for %q must name the Automaton or Automata: %q", kind, text)
			}
		})
	}
}

// TestSupervisorWatchTextNoPRDNoTrackerID: no line contains the word
// PRD, and none contains a tracker id — not in any kind's own wording
// and not when a caller-supplied Automaton id or run id is inlined.
func TestSupervisorWatchTextNoPRDNoTrackerID(t *testing.T) {
	subjects := map[string]string{
		"automaton id": "aut-7f3e8066",
		"node name":    "edge-node-3",
		"run id":       "424242",
	}
	for _, kind := range SupervisorWatchKinds {
		for label, subject := range subjects {
			text, err := SupervisorWatchText(kind, subject)
			if err != nil {
				t.Fatalf("SupervisorWatchText(%q, %q): %v", kind, label, err)
			}
			if supervisorWatchPRDRe.MatchString(text) {
				t.Fatalf("text for %q (subject %q) contains the word PRD: %q", kind, label, text)
			}
			if supervisorWatchTrackerRe.MatchString(text) {
				t.Fatalf("text for %q (subject %q) carries a tracker id: %q", kind, label, text)
			}
		}
	}
}

// TestSupervisorWatchTextUnknownKind: an unknown kind is an error, so a
// typo can never produce a finding that reads like nothing happened.
func TestSupervisorWatchTextUnknownKind(t *testing.T) {
	if _, err := SupervisorWatchText("not_a_real_kind", "the-subject"); err == nil {
		t.Fatal("SupervisorWatchText(unknown kind) succeeded, want an error")
	}
	if _, err := SupervisorWatchText("", "the-subject"); err == nil {
		t.Fatal("SupervisorWatchText(empty kind) succeeded, want an error")
	}
	// A blank subject still renders as one line of English.
	text, err := SupervisorWatchText(SupervisorWatchTaskFailed, "  ")
	if err != nil {
		t.Fatalf("SupervisorWatchText(blank subject): %v", err)
	}
	if strings.ContainsAny(text, "\r\n") || strings.TrimSpace(text) == "" {
		t.Fatalf("blank-subject text is not a single non-empty line: %q", text)
	}
}

// TestSupervisorWatchTextWiring: a finding upserted with its text still
// carries a well-formed single line after a round trip through the
// store — the record and the text stay in agreement.
func TestSupervisorWatchTextWiring(t *testing.T) {
	store, err := NewSupervisorWatchStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSupervisorWatchStore: %v", err)
	}
	kind, subject := SupervisorWatchAutomatonNotMerged, "auto-done"
	text, err := SupervisorWatchText(kind, subject)
	if err != nil {
		t.Fatalf("SupervisorWatchText: %v", err)
	}
	f := testSupervisorWatchFinding(kind, subject, text, time.Time{})
	out, err := store.Upsert(f)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if out.Text != text {
		t.Fatalf("stored Text = %q, want %q", out.Text, text)
	}
	if supervisorWatchPRDRe.MatchString(out.Text) || supervisorWatchTrackerRe.MatchString(out.Text) {
		t.Fatalf("stored Text is not operator-safe: %q", out.Text)
	}
}
