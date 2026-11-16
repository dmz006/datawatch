package planrecords

import (
	"strings"
	"testing"
)

// TestParseStateList is the unit face of the shared REST state query parser:
// no filter, one state, several states, and the refusals.
func TestParseStateList(t *testing.T) {
	rows := []string{"open", "planned", "in_progress", "blocked", "shipped", "done", "deferred", "cancelled"}
	findings := []string{"open", "triaged", "fixed", "wontfix", "duplicate"}

	// No filter.
	if words, err := ParseStateList("", "state", rows); err != nil || words != nil {
		t.Fatalf("empty value: %v %v", words, err)
	}
	if words, err := ParseStateList("   ", "", findings); err == nil {
		t.Fatalf("spaces-only single form must be refused, got %v", words)
	}
	if words, err := ParseStateList("open, ,", "state", rows); err != nil || len(words) != 1 || words[0] != "open" {
		t.Fatalf("dangling commas: %v %v", words, err)
	}

	// One state: a lone token is exact; it is not trimmed, the way the route
	// answered it before a list existed.
	words, err := ParseStateList("blocked", "state", rows)
	if err != nil || !sameWords(words, []string{"blocked"}) {
		t.Fatalf("single state: %v %v", words, err)
	}
	for _, raw := range []string{" blocked", "blocked ", "  open  "} {
		if _, err := ParseStateList(raw, "state", rows); err == nil {
			t.Fatalf("%q: a padded single state must be refused, not trimmed", raw)
		}
	}

	// Several states — the list form trims each part, drops the empty ones
	// and keeps each state once, in order.
	several := []struct {
		name  string
		raw   string
		words []string
	}{
		{"a list parses", "open, blocked", []string{"open", "blocked"}},
		{"spaces around every part are fine", " open , blocked ,  open", []string{"open", "blocked"}},
		{"empty parts vanish", "open,, blocked", []string{"open", "blocked"}},
		{"duplicates collapse in place", "planned, planned, open", []string{"planned", "open"}},
	}
	for _, tc := range several {
		t.Run(tc.name, func(t *testing.T) {
			words, err := ParseStateList(tc.raw, "state", rows)
			if err != nil {
				t.Fatalf("%v", err)
			}
			if !sameWords(words, tc.words) {
				t.Fatalf("words = %v, want %v", words, tc.words)
			}
		})
	}

	// An unknown state is an error, not an empty list: the message names the
	// parameter, the refused state and the valid ones.
	rejects := []struct {
		raw string
		tok string // the exact token the error quotes
	}{
		{"triaged", "triaged"},       // a finding state, not a registry-row state.
		{"  SHIPPED", "  SHIPPED"},   // case-sensitive, and unpadded as sent.
		{" ", " "},                   // spaces only.
		{"open, triaged", "triaged"}, // one unknown word poisons the list.
		{", open, bogus", "bogus"},   // an unknown word after valid ones too.
		{"open, bogus", "bogus"},
		{" open , bogus ", "bogus"}, // the pad only goes in the list form.
	}
	for _, tc := range rejects {
		words, err := ParseStateList(tc.raw, "state", rows)
		if err == nil {
			t.Fatalf("%q: wanted an error, got %v", tc.raw, words)
		}
		if !strings.Contains(err.Error(), "\""+tc.tok+"\"") {
			t.Fatalf("%q: error %q does not name the refused state %q", tc.raw, err, tc.tok)
		}
		for _, w := range rows {
			if !strings.Contains(err.Error(), w) {
				t.Fatalf("%q: error %q omits valid state %q", tc.raw, err, w)
			}
		}
		if !strings.Contains(err.Error(), "state") {
			t.Fatalf("%q: error %q should name the parameter", tc.raw, err)
		}
	}

	// The same value is judged against the vocabulary it is given.
	if words, err := ParseStateList("triaged", "state", findings); err != nil || !sameWords(words, []string{"triaged"}) {
		t.Fatalf("triaged in the finding vocabulary: %v %v", words, err)
	}
	if _, err := ParseStateList("shipped", "state", findings); err == nil {
		t.Fatal("shipped is not a finding state")
	}
}

func sameWords(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
