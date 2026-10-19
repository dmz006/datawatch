package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readWorkingFile returns the current content of path inside the temp repo,
// reading the working tree rather than a committed blob, so uncommitted
// edits made by the script are visible.
func readWorkingFile(t *testing.T, r *mergeRepo, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.root, path))
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// TestSetReleaseVersionWritesBothFiles runs the script with 1.0.1 and checks
// that both version files now read 1.0.1 and that app/core.go, which the
// script must not touch, is unchanged.
func TestSetReleaseVersionWritesBothFiles(t *testing.T) {
	r := newMergeRepo(t, "set-release-version.sh")

	coreBefore := readWorkingFile(t, r, "app/core.go")

	out, err := r.runScript("set-release-version.sh", "1.0.1", "test release")
	if err != nil {
		t.Fatalf("expected script to succeed, got error: %v\n%s", err, out)
	}

	for _, path := range []string{"cmd/datawatch/main.go", "internal/server/api.go"} {
		got := readWorkingFile(t, r, path)
		if !strings.Contains(got, "var Version = \"1.0.1\"\n") {
			t.Fatalf("%s should declare version 1.0.1, got:\n%s", path, got)
		}
	}

	if coreAfter := readWorkingFile(t, r, "app/core.go"); coreAfter != coreBefore {
		t.Fatalf("app/core.go changed; before:\n%s\nafter:\n%s", coreBefore, coreAfter)
	}
}

// TestSetReleaseVersionInsertsChangelogHeading runs the script with 1.0.1 and
// checks that CHANGELOG.md gains a "## v1.0.1 — test release" heading directly
// below the "## [Unreleased]" line, with a blank line on each side, and that
// the existing v1.0.0 heading still follows.
func TestSetReleaseVersionInsertsChangelogHeading(t *testing.T) {
	r := newMergeRepo(t, "set-release-version.sh")

	out, err := r.runScript("set-release-version.sh", "1.0.1", "test release")
	if err != nil {
		t.Fatalf("expected script to succeed, got error: %v\n%s", err, out)
	}

	lines := strings.Split(readWorkingFile(t, r, "CHANGELOG.md"), "\n")
	unreleased := -1
	for i, line := range lines {
		if line == "## [Unreleased]" {
			unreleased = i
			break
		}
	}
	if unreleased < 0 {
		t.Fatalf("CHANGELOG.md lost its ## [Unreleased] line:\n%s", strings.Join(lines, "\n"))
	}

	want := []string{"", "## v1.0.1 — test release", ""}
	for i, w := range want {
		idx := unreleased + 1 + i
		if idx >= len(lines) || lines[idx] != w {
			t.Fatalf("line %d below Unreleased should be %q; CHANGELOG.md:\n%s", i+1, w, strings.Join(lines, "\n"))
		}
	}

	if !strings.Contains(strings.Join(lines, "\n"), "## v1.0.0 — first release") {
		t.Fatalf("existing v1.0.0 heading was lost:\n%s", strings.Join(lines, "\n"))
	}
}

// TestSetReleaseVersionHeadingIsIdempotent runs the script twice with the same
// version and checks that CHANGELOG.md ends with exactly one heading for that
// version, and that the second run leaves the file byte-for-byte unchanged.
func TestSetReleaseVersionHeadingIsIdempotent(t *testing.T) {
	r := newMergeRepo(t, "set-release-version.sh")

	if out, err := r.runScript("set-release-version.sh", "1.0.1", "test release"); err != nil {
		t.Fatalf("first run should succeed, got error: %v\n%s", err, out)
	}
	afterFirst := readWorkingFile(t, r, "CHANGELOG.md")

	if out, err := r.runScript("set-release-version.sh", "1.0.1", "test release"); err != nil {
		t.Fatalf("second run should succeed, got error: %v\n%s", err, out)
	}
	afterSecond := readWorkingFile(t, r, "CHANGELOG.md")

	headings := 0
	for _, line := range strings.Split(afterSecond, "\n") {
		if strings.HasPrefix(line, "## v1.0.1 ") {
			headings++
		}
	}
	if headings != 1 {
		t.Fatalf("expected exactly one ## v1.0.1 heading, found %d:\n%s", headings, afterSecond)
	}
	if afterSecond != afterFirst {
		t.Fatalf("second run changed CHANGELOG.md; after first:\n%s\nafter second:\n%s", afterFirst, afterSecond)
	}
}
