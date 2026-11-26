// The outgoing check: what a push would deliver is scanned from the remote's
// own answer, and the exact checked commit is what leaves. Every remote here
// is a local bare repository in a temp directory; every fake credential is
// assembled at runtime.

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var enforce = SecretScanPolicy{Mode: ScanEnforce}

func writeAndCommit(t *testing.T, ctx context.Context, work, name, content, msg string) string {
	t.Helper()
	p := filepath.Join(work, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ctx, work, "add", "-A")
	mustGit(t, ctx, work, "commit", "-m", msg)
	return strings.TrimSpace(mustGit(t, ctx, work, "rev-parse", "HEAD"))
}

// assertRefused fails unless res is a refusal of the wanted kind that left
// the remote ref where it was and never printed secret.
func assertRefused(t *testing.T, ctx context.Context, res PushResult, kind PushFailureKind, bare, branch, before, secret string) {
	t.Helper()
	if res.Pushed || res.Kind != kind {
		t.Fatalf("want a %s refusal, got %+v", kind, res)
	}
	if got := remoteRefSHA(t, ctx, bare, branch); got != before {
		t.Fatalf("remote %s moved from %q to %q", branch, before, got)
	}
	if secret != "" {
		assertNoSecretInResult(t, res, secret)
	}
}

func remoteHasRef(t *testing.T, ctx context.Context, bare, ref string) bool {
	t.Helper()
	return strings.TrimSpace(mustGit(t, ctx, bare, "for-each-ref", ref)) != ""
}

// A credential that exists only in the resolution of a merge conflict is in
// no ordinary commit diff, and must still be found.
func TestOutgoing_SecretAddedOnlyInConflictResolutionIsRefused(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	writeAndCommit(t, ctx, work, "c.txt", "base\n", "base file")
	mustGit(t, ctx, work, "push", "origin", "main")
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	writeAndCommit(t, ctx, work, "c.txt", "feature side\n", "feature edit")
	mustGit(t, ctx, work, "checkout", "main")
	writeAndCommit(t, ctx, work, "c.txt", "main side\n", "main edit")
	mustGit(t, ctx, work, "push", "origin", "main")
	mustGit(t, ctx, work, "checkout", "feature")
	if _, err := runGit(ctx, work, "merge", "main"); err == nil {
		t.Fatal("fixture: the merge was expected to conflict")
	}
	secret := fakeScanSecret()
	writeAndCommit(t, ctx, work, "c.txt", "resolved\nGITHUB_TOKEN=\""+secret+"\"\n", "merge main")

	res := PushToBranch(ctx, work, "origin", "feature", enforce)
	assertRefused(t, ctx, res, PushFailSecret, bare, "feature", "", secret)
	if len(res.SecretFindings) != 1 || res.SecretFindings[0].Path != "c.txt" || res.SecretFindings[0].Line != 2 {
		t.Fatalf("findings %+v, want one at c.txt:2", res.SecretFindings)
	}
}

// Lines a merge only carries over from a parent that the remote already has
// are not new, and must not block the push.
func TestOutgoing_MergeDoesNotRescanWhatTheRemoteHas(t *testing.T) {
	ctx := context.Background()
	_, work := testRepo(t, ctx)
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	writeAndCommit(t, ctx, work, "f.txt", "feature\n", "feature work")
	mustGit(t, ctx, work, "checkout", "main")
	writeAndCommit(t, ctx, work, "old.env", "GITHUB_TOKEN=\""+fakeScanSecret()+"\"\n", "already public")
	mustGit(t, ctx, work, "push", "origin", "main")
	mustGit(t, ctx, work, "checkout", "feature")
	mustGit(t, ctx, work, "merge", "--no-ff", "-m", "merge main", "main")

	res := PushToBranch(ctx, work, "origin", "feature", enforce)
	if !res.Pushed {
		t.Fatalf("a clean merge of public main must push: %+v", res)
	}
}

func TestOutgoing_SecretInCommitMessageIsRefused(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	secret := fakeScanSecret()
	writeAndCommit(t, ctx, work, "a.txt", "clean\n", "fix\n\nused "+secret+" to test")

	res := PushToBranch(ctx, work, "origin", "main", enforce)
	assertRefused(t, ctx, res, PushFailSecret, bare, "main", before, secret)
}

func TestOutgoing_SecretInFileNameIsRefused(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	secret := fakeScanSecret()
	writeAndCommit(t, ctx, work, secret+".txt", "", "add empty file")

	res := PushToBranch(ctx, work, "origin", "main", enforce)
	assertRefused(t, ctx, res, PushFailSecret, bare, "main", before, secret)
}

// The ref being pushed is what is scanned, whatever HEAD points at.
func TestOutgoing_PushedBranchIsScannedWhenHeadDiffers(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)
	mustGit(t, ctx, work, "checkout", "main")

	res := PushToBranch(ctx, work, "origin", "feature", enforce)
	assertRefused(t, ctx, res, PushFailSecret, bare, "feature", "", secret)
}

// A check that cannot run refuses the push.
func TestOutgoing_ScanErrorRefuses(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{ScanEnforce, ScanShadow} {
		bare, work := testRepo(t, ctx)
		before := remoteRefSHA(t, ctx, bare, "main")
		workCommit(t, ctx, work, "a.txt", "work")
		res := PushToBranch(ctx, work, "origin", "main", SecretScanPolicy{Mode: mode, Detectors: []string{"no_such_detector"}})
		assertRefused(t, ctx, res, PushFailureKind("check_failed"), bare, "main", before, "")
	}
}

// Local replace refs, attributes and tracking refs are writable from inside a
// checkout; none of them may hide content from the check.
func TestOutgoing_ReplaceRefCannotHideACommit(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	secret := fakeScanSecret()
	bad := commitSecretFile(t, ctx, work, secret)
	mustGit(t, ctx, work, "checkout", "-b", "decoy", before)
	clean := writeAndCommit(t, ctx, work, "env", "nothing here\n", "accidental credential")
	mustGit(t, ctx, work, "checkout", "main")
	mustGit(t, ctx, work, "replace", bad, clean)

	res := PushToBranch(ctx, work, "origin", "main", enforce)
	assertRefused(t, ctx, res, PushFailSecret, bare, "main", before, secret)
}

func TestOutgoing_BinaryAttributeCannotHideContent(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	if err := os.MkdirAll(filepath.Join(work, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".git", "info", "attributes"), []byte("* binary\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)

	res := PushToBranch(ctx, work, "origin", "main", enforce)
	assertRefused(t, ctx, res, PushFailSecret, bare, "main", before, secret)
}

func TestOutgoing_ForgedTrackingRefCannotHideACommit(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	secret := fakeScanSecret()
	bad := commitSecretFile(t, ctx, work, secret)
	mustGit(t, ctx, work, "update-ref", "refs/remotes/origin/forged", bad)

	res := PushToBranch(ctx, work, "origin", "main", enforce)
	assertRefused(t, ctx, res, PushFailSecret, bare, "main", before, secret)
}

// push.followTags must not publish an older local tag along with a branch.
func TestOutgoing_FollowTagsPublishesNoTag(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	mustGit(t, ctx, work, "config", "push.followTags", "true")
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	workCommit(t, ctx, work, "a.txt", "work")
	mustGit(t, ctx, work, "tag", "-a", "v9.9.9", "-m", "old local tag")

	if res := PushToBranch(ctx, work, "origin", "feature"); !res.Pushed {
		t.Fatalf("push: %+v", res)
	}
	if remoteHasRef(t, ctx, bare, "refs/tags") {
		t.Fatal("a tag was published with the branch push")
	}
}
