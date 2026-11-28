package pilog

import (
	"regexp"
	"strings"
	"testing"
)

// testIDRule states pi's acceptance rule for --session-id (non-empty,
// alphanumerics plus '-', '_' and '.', alphanumeric first and last)
// independently of sessionIDPattern, so the validator and the rule are
// checked against each other.
var testIDRule = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// assertTranslatedID reports all the failures of a translated id: it must
// satisfy pi's rule and fit the 113-character budget a translated id has
// (at most 100 base characters + '-' + 12 hex).
func assertTranslatedID(t *testing.T, id string) {
	t.Helper()
	if id == "" {
		t.Fatalf("SessionID returned an empty id")
	}
	if !testIDRule.MatchString(id) {
		t.Errorf("id %q must satisfy pi's --session-id rule", id)
	}
	if len(id) > 113 {
		t.Errorf("id %q is %d chars; a translated id must fit in at most 113", id, len(id))
	}
}

// TestSessionIDLongNameWithSpaces: a 300-character name with spaces must
// translate to an id pi accepts that fits the 113-character budget. The live
// case was a 285-character spaced name: pi accepted the id and then failed
// with ENAMETOOLONG writing its log file, so the base must be cut, not just
// sanitized.
func TestSessionIDLongNameWithSpaces(t *testing.T) {
	name := strings.Repeat("the quick brown fox ", 15) // 20 * 15 = exactly 300 chars
	if len(name) != 300 {
		t.Fatalf("test setup: name is %d chars, want 300", len(name))
	}
	if got := SessionID(name); got == name {
		t.Fatalf("SessionID must translate a name with spaces, not pass it through")
	}
	id := SessionID(name)
	assertTranslatedID(t, id)
}

// TestSessionIDLongNamesDifferingOnlyAtEnd: two long names whose cleaned
// bases are identical (both cut to the same 100 characters) must still map
// to DIFFERENT ids — the sha256 fingerprint of the original name is what
// distinguishes them, since the cut removes the differing tail.
func TestSessionIDLongNamesDifferingOnlyAtEnd(t *testing.T) {
	prefix := strings.Repeat("word ", 59) // 295 chars
	a := prefix + "alpha"
	b := prefix + "omega"
	if len(a) != 300 || len(b) != 300 {
		t.Fatalf("test setup: names are %d and %d chars, want 300 and 300", len(a), len(b))
	}
	ia := SessionID(a)
	ib := SessionID(b)
	if ia == ib {
		t.Errorf("SessionID of two long names differing only at the end both map to %q; the original-name fingerprint must distinguish them", ia)
	}
	assertTranslatedID(t, ia)
	assertTranslatedID(t, ib)
	if SessionID(a) != ia || SessionID(b) != ib {
		t.Errorf("SessionID must be deterministic: %q / %q then %q / %q", ia, ib, SessionID(a), SessionID(b))
	}
}

// TestSessionIDLongAlreadyValidUnchanged: a name that already satisfies pi's
// rule stays byte-for-byte unchanged, even when it is far over 100
// characters — long already-valid ids are ones pi already accepts and must
// not be rewritten by the fix.
func TestSessionIDLongAlreadyValidUnchanged(t *testing.T) {
	name := strings.Repeat("a", 300) + "-x905" // 305 valid chars
	if !testIDRule.MatchString(name) {
		t.Fatalf("test setup: %q must satisfy pi's rule", name)
	}
	if got := SessionID(name); got != name {
		t.Errorf("SessionID of an already-valid long name = %q (%d chars); an already-valid id must be returned unchanged", got, len(got))
	}
}
