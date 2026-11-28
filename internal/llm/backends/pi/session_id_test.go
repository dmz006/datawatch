package pi

import (
	"regexp"
	"strings"
	"testing"
)

// piIDRule is pi's acceptance rule for --session-id (non-empty, alphanumerics
// plus '-', '_' and '.', alphanumeric first and last). It is stated
// independently of piSessionIDPattern so the validator and the rule are
// checked against each other.
var piIDRule = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// TestPiSessionIDAlreadyValidUnchanged: session IDs pi already accepts must
// be returned untouched, so sessions that work today keep their ID.
func TestPiSessionIDAlreadyValidUnchanged(t *testing.T) {
	for _, id := range []string{"a", "abc", "local-node-c905", "sess_judge_1", "a.b-c_d9"} {
		if got := piSessionID(id); got != id {
			t.Errorf("piSessionID(%q) = %q; an already-valid ID must be returned unchanged", id, got)
		}
	}
}

// TestPiSessionIDRejectedNamesBecomeValid: names pi would refuse (spaces,
// colons, slashes, non-ASCII runes, leading/trailing punctuation, control
// characters, blank) must all translate to an ID pi accepts.
func TestPiSessionIDRejectedNamesBecomeValid(t *testing.T) {
	names := []string{
		"Pi: fix two page bugs", // the production incident name that refused to launch
		"a/b",                   // slash
		"",                      // empty (launch paths skip the flag, but the value must still be safe)
		" ",                     // blank only
		"-leading",              // leading dash
		"trailing-",             // trailing dash
		"_.",                    // no alphanumeric at all
		"café ☕ naïve",          // non-ASCII
		"tab\there",             // control character
		"dash and dot .end",
	}
	for _, name := range names {
		got := piSessionID(name)
		if got == "" {
			t.Errorf("piSessionID(%q) must be non-empty", name)
			continue
		}
		if !piIDRule.MatchString(got) {
			t.Errorf("piSessionID(%q) = %q; must satisfy pi's --session-id rule", name, got)
		}
	}
}

// TestPiSessionIDStableAcrossLaunches: the same session name must always map
// to the same ID, or a resume would not reattach to the original session.
func TestPiSessionIDStableAcrossLaunches(t *testing.T) {
	const name = "Pi: fix two page bugs"
	first := piSessionID(name)
	for i := 0; i < 10; i++ {
		if got := piSessionID(name); got != first {
			t.Fatalf("piSessionID(%q) must be deterministic: got %q then %q", name, first, got)
		}
	}
}

// TestPiSessionIDNoSanitizationCollision: names that differ only in
// characters the sanitizer rewrites must still map to DIFFERENT IDs.
func TestPiSessionIDNoSanitizationCollision(t *testing.T) {
	type pair struct{ a, b string }
	for _, p := range []pair{
		{"a b", "ab"},  // space vs none
		{"a:b", "a-b"}, // colon vs dash
		{"a/b", "a.b"}, // slash vs dot
		{"x y z", "x-y"},
		{"a b", "a:b"},
		{"a:b", "a/b"},
	} {
		ia, ib := piSessionID(p.a), piSessionID(p.b)
		if ia == ib {
			t.Errorf("piSessionID(%q) == piSessionID(%q) == %q; names differing only in sanitized characters must not collide", p.a, p.b, ia)
		}
	}
}

// TestPiSessionIDAllBaseVariantsDistinct: the five spellings that sanitize
// to the same base plus the already-valid "plain" spelling must all be
// distinct IDs together.
func TestPiSessionIDAllBaseVariantsDistinct(t *testing.T) {
	names := []string{"a b", "a:b", "a/b", "a_b", "a.b", "ab"}
	seen := map[string]string{}
	for _, n := range names {
		id := piSessionID(n)
		if other, dup := seen[id]; dup {
			t.Errorf("piSessionID(%q) and piSessionID(%q) both map to %q", other, n, id)
		}
		seen[id] = n
	}
}

// sessionFlagValue extracts the single-quoted value of --session-id from a
// built command line, failing the test when the flag is missing.
func sessionFlagValue(t *testing.T, cmd, name string) string {
	t.Helper()
	const prefix = "--session-id '"
	i := strings.Index(cmd, prefix)
	if i == -1 {
		t.Fatalf("no --session-id flag for name %q in: %s", name, cmd)
	}
	rest := cmd[i+len(prefix):]
	if j := strings.IndexByte(rest, '\''); j != -1 {
		return rest[:j]
	}
	t.Fatalf("unterminated --session-id value for name %q in: %s", name, cmd)
	return ""
}

// TestInteractiveSessionFlagIsPiValidAndStable exercises the fix at the
// flag level: a session name pi would refuse must reach --session-id
// sanitized into a valid ID, and the launch and a resume of the same
// session must carry the same ID.
func TestInteractiveSessionFlagIsPiValidAndStable(t *testing.T) {
	const name = "Pi: fix two page bugs"

	launchCmd, err := buildInteractiveCmd("pi", "prov/model-a", name, "", "/proj", "cs-launch")
	if err != nil {
		t.Fatalf("buildInteractiveCmd (launch): %v", err)
	}
	value := sessionFlagValue(t, launchCmd, name)
	if value == name {
		t.Errorf("the session flag must not carry the raw name %q; pi refuses it before doing any work", name)
	}
	if !piIDRule.MatchString(value) {
		t.Errorf("session flag value %q must satisfy pi's --session-id rule", value)
	}

	resumeCmd, err := buildInteractiveCmd("pi", "prov/model-a", name, "", "/proj", "cs-resume")
	if err != nil {
		t.Fatalf("buildInteractiveCmd (resume): %v", err)
	}
	if got := sessionFlagValue(t, resumeCmd, name); got != value {
		t.Errorf("the same session must keep the same --session-id across launch and resume: launch %q, resume %q", value, got)
	}
}
