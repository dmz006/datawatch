package gitguard

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Tests for the repository-scoped guard together with the ownership rules.

// guardFiles returns the sorted names in a guard directory.
func guardFiles(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

// wantGuardFiles is every hook plus, for an owned guard, the record.
func wantGuardFiles(owned bool) string {
	names := append(append([]string{}, guardHooks...), passThroughHooks...)
	if owned {
		names = append(names, guardOwnerFile)
	}
	sort.Strings(names)
	return strings.Join(names, " ")
}

// assertScopeTable checks the refuse and allow table of g: refused in own,
// allowed in other.
func assertScopeTable(t *testing.T, g *CommitGuard, own, other string) {
	t.Helper()
	for _, op := range refusedOps(t) {
		assertRefused(t, g, own, op...)
	}
	for _, op := range [][]string{{"status", "--short"}, {"add", "f.txt"}, {"log", "--oneline"}, {"diff", "--cached"}} {
		mustGit(t, own, g.Env(), op...)
	}
	for _, op := range [][]string{
		{"commit", "-q", "--allow-empty", "-m", "x"}, {"branch", "b1"}, {"tag", "t1"},
		{"switch", "-q", "-c", "s1"}, {"stash"}, {"update-ref", "refs/heads/p1", "HEAD"},
	} {
		mustGit(t, other, g.Env(), op...)
	}
}

func TestOwnedRepoGuardKeepsTheScopeTableThroughAdoption(t *testing.T) {
	scopeEnv(t)
	t.Setenv("TMPDIR", t.TempDir())
	own := historyRepo(t, filepath.Join(t.TempDir(), "own"))
	g, err := NewOwnedRepoCommitGuard("/data/a", "h-1", own)
	if err != nil {
		t.Fatal(err)
	}
	if g.Repository() == "" {
		t.Fatal("owned guard has no repository of its own")
	}
	if got, want := guardFiles(t, g.Dir()), wantGuardFiles(true); got != want {
		t.Fatalf("guard files:\n%s\nwant:\n%s", got, want)
	}
	assertScopeTable(t, g, own, historyRepo(t, filepath.Join(t.TempDir(), "other")))

	if err := g.RecordTmuxSocket("/sock/a"); err != nil {
		t.Fatal(err)
	}
	// Recording the socket leaves no temporary file behind.
	if got, want := guardFiles(t, g.Dir()), wantGuardFiles(true); got != want {
		t.Fatalf("guard files after recording the socket:\n%s\nwant:\n%s", got, want)
	}
	// A restarted daemon: the same folder and token, still scoped. The
	// project folder it passes does not rescope an existing guard.
	elsewhere := historyRepo(t, filepath.Join(t.TempDir(), "elsewhere"))
	adopted, err := AdoptOwnedRepoCommitGuard("/data/a", "h-1", elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	if !adopted.Adopted() || adopted.Dir() != g.Dir() || adopted.Token() != g.Token() {
		t.Fatalf("adopted %s, want the existing guard %s", adopted.Dir(), g.Dir())
	}
	assertScopeTable(t, adopted, own, elsewhere)
	if err := adopted.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(g.Dir()); !os.IsNotExist(err) {
		t.Fatalf("own guard not removed by Cleanup: %v", err)
	}
}

func TestAdoptRepoGuardFollowsTheOwnerRules(t *testing.T) {
	scopeEnv(t)
	t.Setenv("TMPDIR", t.TempDir())
	own := historyRepo(t, filepath.Join(t.TempDir(), "own"))
	other := historyRepo(t, filepath.Join(t.TempDir(), "other"))
	theirs, err := NewOwnedRepoCommitGuard("/data/a", "h-1", own)
	if err != nil {
		t.Fatal(err)
	}

	// Another daemon, and a caller with no owner, never take a recorded
	// folder. Each gets a new scoped guard.
	otherDaemon, err := AdoptOwnedRepoCommitGuard("/data/b", "h-1", own)
	if err != nil {
		t.Fatal(err)
	}
	noOwner, err := AdoptRepoCommitGuard("h-1", own)
	if err != nil {
		t.Fatal(err)
	}
	for name, g := range map[string]*CommitGuard{"another owner": otherDaemon, "no owner": noOwner} {
		if g.Dir() == theirs.Dir() || g.Adopted() {
			t.Fatalf("%s took the recorded folder", name)
		}
		assertRefused(t, g, own, "tag", "nope")
		mustGit(t, other, g.Env(), "tag", "-f", "ok")
		if err := g.Cleanup(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(theirs.Dir()); err != nil {
		t.Fatalf("the owner's folder is gone: %v", err)
	}

	// A new guard made at adoption is recorded as the daemon's.
	made, err := AdoptOwnedRepoCommitGuard("/data/a", "h-2", own)
	if err != nil {
		t.Fatal(err)
	}
	if rec, ok, _ := readGuardRecord(made.Dir()); !ok || rec.Owner != "/data/a" || rec.SessionID != "h-2" {
		t.Fatalf("new guard record = %+v ok=%v", rec, ok)
	}

	// A scoped folder with no record is adopted and never removed.
	old, err := NewRepoCommitGuard("h-3", own)
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"/data/a", ""} {
		got, err := AdoptOwnedRepoCommitGuard(owner, "h-3", own)
		if err != nil {
			t.Fatal(err)
		}
		if got.Dir() != old.Dir() || got.Token() != old.Token() {
			t.Fatalf("owner %q did not adopt the unrecorded guard", owner)
		}
		if err := got.Cleanup(); err != nil {
			t.Fatal(err)
		}
		if err := got.RecordTmuxSocket("/sock/a"); err != nil {
			t.Fatal(err)
		}
		if got, want := guardFiles(t, old.Dir()), wantGuardFiles(false); got != want {
			t.Fatalf("unrecorded guard changed:\n%s\nwant:\n%s", got, want)
		}
	}
	ReapDeadGuardDirs("/data/a", func(id, _ string) bool { return id == "h-3" })
	if _, err := os.Stat(old.Dir()); err != nil {
		t.Fatalf("unrecorded guard reaped: %v", err)
	}
}
