package genartifacts

import (
	"strings"
	"testing"
)

// TestTrackedPathGeneratedFilesIgnoredMatchesEveryKind covers: each kind's
// default path with the sample id 4f2a matches, and the changelog draft and
// tracker rows paths, which share a directory, are told apart: each real
// name matches and a blend of the two suffixes is neither.
func TestTrackedPathGeneratedFilesIgnoredMatchesEveryKind(t *testing.T) {
	for _, kind := range Kinds() {
		tpl := DefaultPaths()[kind]
		if !strings.Contains(tpl, IDPlaceholder) {
			t.Fatalf("kind %s: default path %q has no IDPlaceholder", kind, tpl)
		}
		sample := strings.ReplaceAll(tpl, IDPlaceholder, "4f2a")
		if !MatchesTrackedPath(sample) {
			t.Errorf("MatchesTrackedPath(%q) = false, want true", sample)
		}
	}
}

// TestTrackedPathGeneratedFilesIgnoredRejectsMalformed covers: an id that
// contains a slash does not match; a path in another directory, a path with a
// different suffix, a path with an extra nested segment, and an absolute path
// do not match; the hash sidecar name of a tracked path does not match; and
// an empty, '..', or non-clean forward-slash form does not match.
func TestTrackedPathGeneratedFilesIgnoredRejectsMalformed(t *testing.T) {
	notMatch := []string{
		"",                                              // empty path
		"/docs/plans/reports/4f2a-gate.md",              // absolute path
		"docs/plans/reports/4f2a/inner-gate.md",         // extra nested segment
		"docs/plans/reports/4f2a/sub/gate.md",           // id that contains a slash
		"docs/plans/drafts/4f2a-gate.md",                // gate report in another kind's directory
		"docs/other/reports/4f2a-gate.md",               // another directory entirely
		"plans/reports/4f2a-gate.md",                    // missing a directory segment
		"docs/plans/reports/4f2a-gate.txt",              // different suffix
		"docs/plans/reports/4f2a-gate.md.bak",           // suffix extended
		"docs/plans/reports/.4f2a-gate.md.genhash",      // hash sidecar of a tracked path
		"../a/docs/plans/reports/4f2a-gate.md",          // contains ".."
		"docs//plans/reports/4f2a-gate.md",              // not clean forward-slash form
		"docs/plans/drafts/../drafts/4f2a-changelog.md", // contains ".."
	}
	for _, rel := range notMatch {
		if MatchesTrackedPath(rel) {
			t.Errorf("MatchesTrackedPath(%q) = true, want false", rel)
		}
	}
}

// TestTrackedPathGeneratedFilesIgnoredTellsApartSharedDirectoryDrafts
// covers: the changelog draft and the tracker rows paths, which share
// docs/plans/drafts, are both matched and each is rejected when the other's
// suffix is substituted.
func TestTrackedPathGeneratedFilesIgnoredTellsApartSharedDirectoryDrafts(t *testing.T) {
	checks := []struct {
		pat  string
		want bool
	}{
		{"docs/plans/drafts/4f2a-changelog.md", true},
		{"docs/plans/drafts/4f2a-tracker-rows.md", true},
		// Each draft's suffix is anchored, not a prefix:
		{"docs/plans/drafts/4f2a-changelog-rows.md", false},
		{"docs/plans/drafts/4f2a-registry-row.md", true}, // third resident of the same directory
		{"docs/plans/drafts/4f2a-registry-rows.md", false},
	}
	for _, c := range checks {
		if got := MatchesTrackedPath(c.pat); got != c.want {
			t.Errorf("MatchesTrackedPath(%q) = %v, want %v", c.pat, got, c.want)
		}
	}
}
