package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustGit runs a git command and fails the test on error, returning
// the combined output.
func mustGit(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()
	out, err := runGit(ctx, dir, args...)
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return out
}

// testRepo sets up a real bare remote plus a fresh work clone with a
// single initial commit already pushed to main, and returns both
// directories. Everything lives under t.TempDir().
func testRepo(t *testing.T, ctx context.Context) (bare, work string) {
	t.Helper()
	root := t.TempDir()
	bare = filepath.Join(root, "bare.git")
	work = filepath.Join(root, "work")

	mustGit(t, ctx, root, "init", "-b", "main", "--bare", bare)
	mustGit(t, ctx, root, "clone", bare, work)
	mustGit(t, ctx, work, "config", "user.name", "Test")
	mustGit(t, ctx, work, "config", "user.email", "test@example.com")

	_ = os.WriteFile(filepath.Join(work, "initial.txt"), []byte("initial\n"), 0o644)
	mustGit(t, ctx, work, "add", "initial.txt")
	mustGit(t, ctx, work, "commit", "-m", "initial")

	// Seed the remote's default branch so the bare has main.
	mustGit(t, ctx, work, "push", "origin", "main")
	return bare, work
}

// workCommit adds a commit to the given clone and returns its SHA.
func workCommit(t *testing.T, ctx context.Context, work, name, msg string) string {
	t.Helper()
	_ = os.WriteFile(filepath.Join(work, name), []byte(name+"\n"), 0o644)
	mustGit(t, ctx, work, "add", name)
	mustGit(t, ctx, work, "commit", "-m", msg)
	return strings.TrimSpace(mustGit(t, ctx, work, "rev-parse", "HEAD"))
}

// remoteRefSHA returns the bare's SHA for a branch ("": absent).
func remoteRefSHA(t *testing.T, ctx context.Context, bare, branch string) string {
	t.Helper()
	out := mustGit(t, ctx, bare, "ls-remote", bare, "refs/heads/"+branch)
	f := strings.Fields(strings.TrimSpace(out))
	if len(f) < 1 {
		return ""
	}
	return f[0]
}

func TestPushPolicyParse(t *testing.T) {
	cases := []struct {
		in     string
		want   PushPolicy
		wantOK bool
	}{
		{"", PushBranch, true},
		{"branch", PushBranch, true},
		{"  BRANCH  ", PushBranch, true},
		{"never", PushNever, true},
		{"NEVER", PushNever, true},
		{"default_branch", PushDefaultBranch, true},
		{"Default_Branch", PushDefaultBranch, true},
		{"force", "", false},
		{"rebase", "", false},
		{"   ", PushBranch, true}, // whitespace normalises to the empty default,
	}
	for _, c := range cases {
		got, ok := ParsePolicy(c.in)
		if got != c.want || ok != c.wantOK {
			t.Errorf("ParsePolicy(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

func TestPushToBranch_NewBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, work := testRepo(t, ctx)

	mustGit(t, ctx, work, "checkout", "-b", "feature")
	sha := workCommit(t, ctx, work, "feature.txt", "feature work")

	res := PushToBranch(ctx, work, "origin", "feature")
	if !res.Pushed {
		t.Fatalf("want pushed, got %+v", res)
	}
	if res.SHA != sha {
		t.Errorf("SHA = %q, want %q", res.SHA, sha)
	}

	// The bare must now have the branch at the same SHA.
	bare := remoteRefSHA(t, ctx, filepath.Join(filepath.Dir(work), "bare.git"), "feature")
	if bare != sha {
		t.Errorf("remote feature = %q, want %q", bare, sha)
	}

	// Upstream tracking must be set for a later plain `git push`.
	rem := strings.TrimSpace(mustGit(t, ctx, work, "config", "branch.feature.remote"))
	if rem != "origin" {
		t.Errorf("branch.feature.remote = %q, want origin", rem)
	}
	merge := strings.TrimSpace(mustGit(t, ctx, work, "config", "branch.feature.merge"))
	if merge != "refs/heads/feature" {
		t.Errorf("branch.feature.merge = %q, want refs/heads/feature", merge)
	}
}

func TestPushToBranch_SecondIsNoop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, work := testRepo(t, ctx)

	mustGit(t, ctx, work, "checkout", "-b", "feature")
	workCommit(t, ctx, work, "feature.txt", "feature work")

	if res := PushToBranch(ctx, work, "origin", "feature"); !res.Pushed {
		t.Fatalf("first push: want pushed, got %+v", res)
	}
	if res := PushToBranch(ctx, work, "origin", "feature"); !res.NothingToPush {
		t.Fatalf("second push: want nothing to push, got %+v", res)
	}
}

func TestPushToBranch_DivergedIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bare, work := testRepo(t, ctx)

	// Local and remote both advance past their common base: work
	// commits locally; a second clone commits on top of the remote
	// tip and pushes it.
	workCommit(t, ctx, work, "local.txt", "local work")
	localSha := mustGit(t, ctx, work, "rev-parse", "HEAD")
	localSha = strings.TrimSpace(localSha)

	root := filepath.Dir(work)
	work2 := filepath.Join(root, "work2")
	mustGit(t, ctx, root, "clone", bare, work2)
	mustGit(t, ctx, work2, "config", "user.name", "Other")
	mustGit(t, ctx, work2, "config", "user.email", "other@example.com")
	remoteSha := workCommit(t, ctx, work2, "remote.txt", "remote work")
	mustGit(t, ctx, work2, "push", "origin", "main")
	if localSha == remoteSha {
		t.Fatal("test setup: local and remote tips must differ")
	}

	// The never-force guarantee: this push must be refused and the
	// bare's main must stay exactly where the other clone left it.
	res := PushToBranch(ctx, work, "origin", "main")
	if res.Kind != PushFailNonFastForward {
		t.Fatalf("want non_fast_forward, got %+v", res)
	}
	if got := remoteRefSHA(t, ctx, bare, "main"); got != remoteSha {
		t.Errorf("remote main moved: got %q, want %q (never force!)", got, remoteSha)
	}
	if res.Reason == "" {
		t.Error("want an operator-facing Reason")
	}
}

func TestPushToBranch_MissingRemote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, work := testRepo(t, ctx)

	res := PushToBranch(ctx, work, "upstream", "main")
	if res.Kind != PushFailNoRemote {
		t.Fatalf("want no_remote, got %+v", res)
	}
	if res.Pushed || res.NothingToPush {
		t.Fatalf("must not report success: %+v", res)
	}
	if res.Reason == "" {
		t.Error("want a Reason for the operator")
	}
}

func TestPushToDefaultBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bare, work := testRepo(t, ctx)

	// Fast-forward case: commit on top of the default branch and
	// push it to the remote's default branch.
	sha := workCommit(t, ctx, work, "default.txt", "head work")
	res := PushToDefaultBranch(ctx, work, "origin")
	if !res.Pushed {
		t.Fatalf("want pushed, got %+v", res)
	}
	if res.Ref != "main" {
		t.Errorf("Ref = %q, want main", res.Ref)
	}
	if res.SHA != sha {
		t.Errorf("SHA = %q, want %q", res.SHA, sha)
	}
	if got := remoteRefSHA(t, ctx, bare, "main"); got != sha {
		t.Errorf("remote main = %q, want %q", got, sha)
	}

	// Idempotent: nothing new to push on a repeat call.
	if res := PushToDefaultBranch(ctx, work, "origin"); !res.NothingToPush {
		t.Errorf("second push: want nothing to push, got %+v", res)
	}
}

func TestPushToDefaultBranch_DivergedRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	bare, work := testRepo(t, ctx)

	workCommit(t, ctx, work, "local.txt", "local work")
	root := filepath.Dir(work)
	work2 := filepath.Join(root, "work2")
	mustGit(t, ctx, root, "clone", bare, work2)
	mustGit(t, ctx, work2, "config", "user.name", "Other")
	mustGit(t, ctx, work2, "config", "user.email", "other@example.com")
	remoteSha := workCommit(t, ctx, work2, "remote.txt", "remote work")
	mustGit(t, ctx, work2, "push", "origin", "main")

	res := PushToDefaultBranch(ctx, work, "origin")
	if res.Kind != PushFailNonFastForward {
		t.Fatalf("want non_fast_forward, got %+v", res)
	}
	if got := remoteRefSHA(t, ctx, bare, "main"); got != remoteSha {
		t.Errorf("remote main moved: got %q, want %q (never force!)", got, remoteSha)
	}
}

func TestUnpushedCommits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, work := testRepo(t, ctx)

	// Freshly cloned and fully pushed: nothing unpushed.
	count, branch, err := UnpushedCommits(ctx, work, "origin")
	if err != nil {
		t.Fatalf("UnpushedCommits: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}

	// Two more local commits → exactly two unpushed.
	workCommit(t, ctx, work, "one.txt", "one")
	workCommit(t, ctx, work, "two.txt", "two")
	count, branch, err = UnpushedCommits(ctx, work, "origin")
	if err != nil {
		t.Fatalf("UnpushedCommits: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want main", branch)
	}

	// An unconfigured remote has no refs: everything on HEAD counts
	// as unpushed (initial + two more = 3) rather than erroring.
	if count, _, err := UnpushedCommits(ctx, work, "upstream"); err != nil || count != 3 {
		t.Errorf("missing remote: got (%d, %v), want (3, nil)", count, err)
	}
}

func TestDefaultBranch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, work := testRepo(t, ctx)

	got, err := DefaultBranch(ctx, work, "origin")
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if got != "main" {
		t.Errorf("DefaultBranch = %q, want main", got)
	}
}
