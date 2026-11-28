package server

import (
	"regexp"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/pilog"
)

// progressIDRule states pi's acceptance rule for --session-id (non-empty,
// alphanumerics plus '-', '_' and '.', alphanumeric first and last) so the
// lookup's id is checked against the rule it must satisfy.
var progressIDRule = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// TestPiProgressLogIDUsesTranslatedID: the progress lookup must ask pilog
// for the log under exactly the id the launch path used (pilog.SessionID),
// not the raw session name — otherwise the id-matched rule silently falls
// back to the time/project-dir rule and can pick another pi log in the same
// folder (the review's VERDICT B fix 1).
func TestPiProgressLogIDUsesTranslatedID(t *testing.T) {
	name := "Pi: fix two page bugs"
	want := pilog.SessionID(name)
	if want == name {
		t.Fatalf("test setup: %q must be translated, not passed through", name)
	}
	if got := piProgressLogID(name); got != want {
		t.Errorf("piProgressLogID(%q) = %q, want pilog.SessionID = %q", name, got, want)
	}
	if got := piProgressLogID(name); got == name {
		t.Errorf("piProgressLogID(%q) must not return the raw name; LocateLogFor would match no log file by it", name)
	}
	if got := piProgressLogID(name); !progressIDRule.MatchString(got) {
		t.Errorf("piProgressLogID(%q) = %q must satisfy pi's --session-id rule", name, got)
	}
	// a long spaced name keeps the translated id inside the file-name budget
	long := strings.Repeat("x ", 150) // 300 chars with spaces
	if got := piProgressLogID(long); len(got) > 113 {
		t.Errorf("piProgressLogID of a 300-char spaced name is %d chars, want at most 113", len(got))
	}
	// an already-valid name passes through unchanged, as at launch
	if got := piProgressLogID("local-node-c905"); got != "local-node-c905" {
		t.Errorf("piProgressLogID(%q) = %q; an already-valid id must be returned unchanged", "local-node-c905", got)
	}
}

// TestPiProgressLogIDEmptyStaysEmpty: an empty name must stay "" at the
// lookup, so LocateLogFor keeps its plain time/project-dir rule for
// sessions that recorded no name (one-shot pi-prompt sessions) — empty must
// not be translated into an id that could match the wrong log file.
func TestPiProgressLogIDEmptyStaysEmpty(t *testing.T) {
	if got := piProgressLogID(""); got != "" {
		t.Errorf("piProgressLogID(empty) = %q; an empty name must stay empty, not be translated", got)
	}
}
