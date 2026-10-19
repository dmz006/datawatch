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

// TestMergeBranchesRefusesSecondBump covers two branches that each bump the
// version by hand from the same base commit, both to 1.0.1. The first merge
// is allowed: 1.0.1 is the version expected after main at 1.0.0. Once main
// is at 1.0.1, the second branch's 1.0.1 clashes with the expected 1.0.2, so
// merge-branches-bump-version.sh must refuse it before merging anything. Main
// keeps its head and both version files still read 1.0.1. Everything runs in
// a throwaway repository under t.TempDir(); no tmux, service or process is
// touched.
func TestMergeBranchesRefusesSecondBump(t *testing.T) {
	r := newMergeRepo(t, "check-merge-version.sh", "set-release-version.sh", "merge-branches-bump-version.sh")

	base := r.head()

	r.git("checkout", "-b", "first", base)
	setVersionLines(r, "1.0.1")
	r.commitAll("first: bump version to 1.0.1")

	r.git("checkout", "-b", "second", base)
	setVersionLines(r, "1.0.1")
	r.commitAll("second: bump version to 1.0.1")

	r.git("checkout", "main")

	out, err := r.runScript("merge-branches-bump-version.sh", "first change", "first")
	if err != nil {
		t.Fatalf("first merge: merge-branches-bump-version.sh failed: %v\n%s", err, out)
	}
	if got := r.versionAt("main"); got != "1.0.1" {
		t.Fatalf("after first merge: cmd/datawatch/main.go on main: want version 1.0.1, got %s", got)
	}

	mainBefore := r.head()

	out, err = r.runScript("merge-branches-bump-version.sh", "second change", "second")
	if err == nil {
		t.Fatalf("second merge: want non-zero exit for a second bump to 1.0.1, got success\n%s", out)
	}
	if !strings.Contains(out, "1.0.2") {
		t.Fatalf("second merge: combined output does not mention the expected version 1.0.2:\n%s", out)
	}
	if got := r.head(); got != mainBefore {
		t.Fatalf("second merge: main moved from %s to %s; a refused merge must change nothing", mainBefore, got)
	}

	mainMain := r.git("show", "main:cmd/datawatch/main.go")
	if !strings.Contains(mainMain, `var Version = "1.0.1"`) {
		t.Fatalf("cmd/datawatch/main.go on main does not read 1.0.1 after the refused merge:\n%s", mainMain)
	}
	apiSrc := r.git("show", "main:internal/server/api.go")
	if !strings.Contains(apiSrc, `var Version = "1.0.1"`) {
		t.Fatalf("internal/server/api.go on main does not read 1.0.1 after the refused merge:\n%s", apiSrc)
	}
}

// setVersionLines rewrites the var Version line in both version files of the
// temp repo to version. The files keep the same layout newMergeRepo gives
// them, and the change is left for the caller to commit.
func setVersionLines(r *mergeRepo, version string) {
	r.t.Helper()
	r.writeFile("cmd/datawatch/main.go", "package main\n\nvar Version = \""+version+"\"\n")
	r.writeFile("internal/server/api.go", "package server\n\nvar Version = \""+version+"\"\n")
}
