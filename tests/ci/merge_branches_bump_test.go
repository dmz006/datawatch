package ci

import (
	"strings"
	"testing"
)

// TestMergeBranchesBumpsOnce merges two branches that each change only
// app/core.go into main with merge-branches-bump-version.sh. Neither branch
// touches the version, so the script must merge both and set the release
// version once: 1.0.1, in both version files, with one changelog heading and
// one bump commit. Everything runs inside a throwaway repository under
// t.TempDir(); no tmux, service or process is touched.
func TestMergeBranchesBumpsOnce(t *testing.T) {
	r := newMergeRepo(t, "check-merge-version.sh", "set-release-version.sh", "merge-branches-bump-version.sh")

	base := r.head()

	// The two edits sit apart in app/core.go (a comment after the package
	// clause and a comment after the function), so the second merge applies
	// cleanly instead of conflicting.
	r.git("checkout", "-b", "alpha", base)
	r.writeFile("app/core.go", "package app\n\nfunc Core() int { return 1 }\n\n// alpha\n")
	r.commitAll("alpha: note in core")
	alphaTip := r.head()

	r.git("checkout", "-b", "beta", base)
	r.writeFile("app/core.go", "package app\n\n// beta\nfunc Core() int { return 1 }\n")
	r.commitAll("beta: note in core")
	betaTip := r.head()

	r.git("checkout", "main")

	out, err := r.runScript("merge-branches-bump-version.sh", "merged feature work", "alpha", "beta")
	if err != nil {
		t.Fatalf("merge-branches-bump-version.sh failed: %v\n%s", err, out)
	}

	// Both version files on main carry the bumped version.
	if got := r.versionAt("main"); got != "1.0.1" {
		t.Fatalf("cmd/datawatch/main.go on main: want version 1.0.1, got %s", got)
	}
	apiSrc := r.git("show", "main:internal/server/api.go")
	if !strings.Contains(apiSrc, `var Version = "1.0.1"`) {
		t.Fatalf("internal/server/api.go on main does not carry version 1.0.1:\n%s", apiSrc)
	}

	// CHANGELOG.md has exactly one v1.0.1 heading.
	changelog := r.git("show", "main:CHANGELOG.md")
	headings := 0
	for _, line := range strings.Split(changelog, "\n") {
		if strings.HasPrefix(line, "## v1.0.1 ") {
			headings++
		}
	}
	if headings != 1 {
		t.Fatalf("CHANGELOG.md: want exactly one line starting with '## v1.0.1 ', got %d:\n%s", headings, changelog)
	}

	// main has exactly one bump commit for v1.0.1.
	bumps := 0
	for _, subject := range strings.Split(r.git("log", "--format=%s", "main"), "\n") {
		if strings.HasPrefix(subject, "chore(release): v1.0.1") {
			bumps++
		}
	}
	if bumps != 1 {
		t.Fatalf("main: want exactly one commit starting 'chore(release): v1.0.1', got %d\n%s", bumps, r.git("log", "--oneline", "main"))
	}

	// Both branch tips were merged into main.
	for name, tip := range map[string]string{"alpha": alphaTip, "beta": betaTip} {
		if _, err := r.gitErr("merge-base", "--is-ancestor", tip, "main"); err != nil {
			t.Fatalf("tip of %s (%s) is not reachable from main: %v", name, tip, err)
		}
	}
}
