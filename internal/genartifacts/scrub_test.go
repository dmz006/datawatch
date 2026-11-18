package genartifacts

import "testing"

func TestBareSessionID(t *testing.T) {
	cases := map[string]string{
		"ab12": "ab12", "host-ab12": "ab12", "host:ab12": "ab12",
		"two-part-ab12": "ab12", "dead-1234": "1234", "bee-ab12": "ab12",
		"a:b-c": "c", "host-": "", "s-aa": "aa",
	}
	for in, want := range cases {
		if got := bareSessionID(in); got != want {
			t.Errorf("bareSessionID(%q)=%q want %q", in, got, want)
		}
	}
	if got := bareSessionIDs([]string{"x-ab", "y-"}); len(got) != 1 || got[0] != "ab" {
		t.Errorf("bareSessionIDs=%v", got)
	}
}

func TestScrubNote(t *testing.T) {
	cases := map[string]string{
		"no paths here":             "no paths here",
		"see /a/b/c.txt now":        "see c.txt now",
		`quoted "/home/<user>/x/y.log"`:  `quoted "y.log"`,
		"embedded (/var/tmp/z) end": "embedded (z) end",
		"a/b relative stays":        "a/b relative stays",
	}
	for in, want := range cases {
		if got := scrubNote(in); got != want {
			t.Errorf("scrubNote(%q)=%q want %q", in, got, want)
		}
	}
}
