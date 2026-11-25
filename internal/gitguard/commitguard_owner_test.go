package gitguard

import (
	"os"
	"path/filepath"
	"testing"
)

func ownerTestGuard(t *testing.T, owner, id string) *CommitGuard {
	t.Helper()
	g, err := NewOwnedCommitGuard(owner, id)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestReapDeadGuardDirsKeepsAnythingItCannotProve(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	kept := map[string]string{}

	kept["another owner"] = ownerTestGuard(t, "/data/b", "h-1").Dir()
	kept["no record"] = ownerTestGuard(t, "", "h-2").Dir()
	kept["not proven dead"] = ownerTestGuard(t, "/data/a", "h-alive").Dir()

	corrupt := ownerTestGuard(t, "/data/a", "h-3").Dir()
	if err := os.WriteFile(filepath.Join(corrupt, guardOwnerFile), []byte(`{"owner":"/data/a"`), 0o600); err != nil {
		t.Fatal(err)
	}
	kept["unreadable record"] = corrupt

	// A symlink is not a record, even when its target is a good one.
	linked := ownerTestGuard(t, "", "h-4").Dir()
	good := filepath.Join(ownerTestGuard(t, "/data/a", "h-4").Dir(), guardOwnerFile)
	if err := os.Symlink(good, filepath.Join(linked, guardOwnerFile)); err != nil {
		t.Fatal(err)
	}
	kept["symlinked record"] = linked

	// A record copied into a directory of another name.
	misnamed := filepath.Join(os.TempDir(), guardDirPrefix+"other-1")
	if err := os.Mkdir(misnamed, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(misnamed, guardOwnerFile), []byte(`{"owner":"/data/a","session_id":"h-5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	kept["name does not match record"] = misnamed

	dead := ownerTestGuard(t, "/data/a", "h-dead").Dir()

	// "h-4" is skipped by the predicate so its good target stays for the test.
	ReapDeadGuardDirs("/data/a", func(id, _ string) bool { return id != "h-alive" && id != "h-4" })

	for why, dir := range kept {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s: removed: %v", why, err)
		}
	}
	if _, err := os.Stat(dead); !os.IsNotExist(err) {
		t.Errorf("own dead guard kept: %v", err)
	}
}

func TestReapDeadGuardDirsWithoutOwnerOrProofRemovesNothing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	g := ownerTestGuard(t, "/data/a", "h-1")
	ReapDeadGuardDirs("", func(string, string) bool { return true })
	ReapDeadGuardDirs("/data/a", nil)
	if _, err := os.Stat(g.Dir()); err != nil {
		t.Fatalf("removed: %v", err)
	}
}

func TestAdoptCommitGuardPrefersOwnDirectory(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	ownerTestGuard(t, "", "h-1")
	ownerTestGuard(t, "/data/b", "h-1")
	mine := ownerTestGuard(t, "/data/a", "h-1")
	got, err := AdoptCommitGuard("/data/a", "h-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Dir() != mine.Dir() || got.Token() != mine.Token() {
		t.Fatalf("adopted %s, want %s", got.Dir(), mine.Dir())
	}
	if err := got.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mine.Dir()); !os.IsNotExist(err) {
		t.Fatalf("own adopted guard not cleaned up: %v", err)
	}
}

func TestCommitGuardRecordTmuxSocket(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	g := ownerTestGuard(t, "/data/a", "h-1")
	seen := func() string {
		got := "not asked"
		ReapDeadGuardDirs("/data/a", func(_, socket string) bool { got = socket; return false })
		return got
	}
	if got := seen(); got != "" {
		t.Fatalf("new guard has socket %q", got)
	}
	for _, socket := range []string{"", "/sock/a", "/sock/b"} {
		if err := g.RecordTmuxSocket(socket); err != nil {
			t.Fatal(err)
		}
	}
	if got := seen(); got != "/sock/a" {
		t.Fatalf("socket %q, want the first recorded", got)
	}
	again, err := AdoptCommitGuard("/data/a", "h-1")
	if err != nil {
		t.Fatal(err)
	}
	if !again.Adopted() || again.Dir() != g.Dir() || g.Adopted() {
		t.Fatalf("adopted flags: made %v, found %v in %s", g.Adopted(), again.Adopted(), again.Dir())
	}
	if left, err := os.ReadDir(g.Dir()); err != nil || len(left) != len(guardHooks)+1 {
		t.Fatalf("unexpected files in the guard: %v %v", left, err)
	}
}

// Recording a socket must not turn an unowned folder into an owned one.
func TestCommitGuardRecordTmuxSocketNeverClaimsOwnership(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	made := ownerTestGuard(t, "", "h-1")
	if err := made.RecordTmuxSocket("/sock/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(made.Dir(), guardOwnerFile)); !os.IsNotExist(err) {
		t.Fatalf("record created: %v", err)
	}
	found, err := AdoptCommitGuard("/data/a", "h-1")
	if err != nil {
		t.Fatal(err)
	}
	if !found.Adopted() || found.Dir() != made.Dir() {
		t.Fatalf("unrecorded guard not marked adopted: %s", found.Dir())
	}
	fresh, err := AdoptCommitGuard("/data/a", "h-2")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Adopted() {
		t.Fatal("new guard marked adopted")
	}
}
