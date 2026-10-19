package ci

import (
	"strings"
	"testing"
)

// setBothVersions rewrites the `var Version = "..."` line in both
// version-carrying files to ver, using the temp repo's writeFile so the
// change is only made inside the throwaway repository.
func setBothVersions(r *mergeRepo, ver string) {
	r.t.Helper()
	r.writeFile("cmd/datawatch/main.go", "package main\n\nvar Version = \""+ver+"\"\n")
	r.writeFile("internal/server/api.go", "package server\n\nvar Version = \""+ver+"\"\n")
}

// TestMergeVersionCheckAcceptsUntouchedBranch covers a branch that does not
// change the version. Main has already been bumped to 1.0.1 by an earlier
// merge, so the merge step will set the branch's version itself. The check
// must pass.
func TestMergeVersionCheckAcceptsUntouchedBranch(t *testing.T) {
	r := newMergeRepo(t, "check-merge-version.sh")

	base := r.head()

	setBothVersions(r, "1.0.1")
	r.commitAll("bump to 1.0.1 on main")

	r.git("checkout", "-b", "feature", base)
	r.writeFile("app/core.go", "package app\n\nfunc Core() int { return 2 }\n")
	r.commitAll("change core on feature")

	out, err := r.runScript("check-merge-version.sh", "feature")
	if err != nil {
		t.Fatalf("expected check to pass for untouched branch, got error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok:") {
		t.Fatalf("expected an ok: line in output, got:\n%s", out)
	}
}

// TestMergeVersionCheckAcceptsPinnedExpectedVersion covers a branch that
// already carries the expected next version, 1.0.1, while main is still at
// 1.0.0. The check must pass.
func TestMergeVersionCheckAcceptsPinnedExpectedVersion(t *testing.T) {
	r := newMergeRepo(t, "check-merge-version.sh")

	r.git("checkout", "-b", "feature")
	setBothVersions(r, "1.0.1")
	r.commitAll("pin version 1.0.1 on feature")

	if got := r.versionAt("main"); got != "1.0.0" {
		t.Fatalf("main should stay at 1.0.0, got %s", got)
	}

	out, err := r.runScript("check-merge-version.sh", "feature")
	if err != nil {
		t.Fatalf("expected check to pass for pinned expected version, got error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "already carries the expected version") {
		t.Fatalf("expected pinned-version ok line in output, got:\n%s", out)
	}
}

// TestMergeVersionCheckRefusesSecondBump covers two branches that each set
// their own version 1.0.1 from the same base. The first one has already
// been merged, so main is at 1.0.1 and the expected version for the second
// is 1.0.2. The check must refuse and name 1.0.2 in its output.
func TestMergeVersionCheckRefusesSecondBump(t *testing.T) {
	r := newMergeRepo(t, "check-merge-version.sh")

	base := r.head()

	r.git("checkout", "-b", "first")
	setBothVersions(r, "1.0.1")
	r.commitAll("first sets 1.0.1")

	r.git("checkout", "-b", "second", base)
	setBothVersions(r, "1.0.1")
	r.commitAll("second sets 1.0.1")

	r.git("checkout", "main")
	setBothVersions(r, "1.0.1")
	r.commitAll("merge of first: bump main to 1.0.1")

	out, err := r.runScript("check-merge-version.sh", "second")
	if err == nil {
		t.Fatalf("expected check to refuse second bump, but it passed:\n%s", out)
	}
	if !strings.Contains(out, "1.0.2") {
		t.Fatalf("expected output to name the expected version 1.0.2, got:\n%s", out)
	}
}
