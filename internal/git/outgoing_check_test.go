// The project's own check-outgoing target: read and run from the base commit,
// handed a manifest of exactly what would leave. All remotes are local bare
// repositories.

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandboxCheck refuses a push whose manifest holds the word FORBIDDEN in an
// added line, a commit, a tag or a file name, or an intruder address. When the
// project directory holds .move-to it also moves the feature branch, as a
// worker still committing during the check would.
const sandboxCheck = `#!/bin/sh
cd "$OUTGOING_MANIFEST_DIR" || exit 2
if [ -f "$OUTGOING_PROJECT_DIR/.move-to" ]; then
	git -C "$OUTGOING_PROJECT_DIR" update-ref refs/heads/feature "$(cat "$OUTGOING_PROJECT_DIR/.move-to")"
fi
if grep -rqs FORBIDDEN added.txt files.tsv refs.tsv commits tags; then
	echo "forbidden word in the outgoing content"
	exit 1
fi
if grep -qs 'intruder@' identities.tsv; then
	echo "unlisted author address"
	exit 1
fi
if [ -f "$OUTGOING_PROJECT_DIR/.must-exist" ] || [ ! -f "$OUTGOING_PROJECT_DIR/.want-main-tree" ]; then
	exit 0
fi
`

const sandboxMakefile = "check-outgoing:\n\t@sh scripts/check.sh\n"

// checkedRepo is a work clone whose remote main declares the check, with a
// feature branch checked out.
func checkedRepo(t *testing.T, ctx context.Context) (bare, work string) {
	t.Helper()
	bare, work = testRepo(t, ctx)
	writeAndCommit(t, ctx, work, "scripts/check.sh", sandboxCheck, "add check script")
	writeAndCommit(t, ctx, work, "Makefile", sandboxMakefile, "add check target")
	mustGit(t, ctx, work, "push", "origin", "main")
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	return bare, work
}

func TestProjectCheck_CleanPushPasses(t *testing.T) {
	ctx := context.Background()
	bare, work := checkedRepo(t, ctx)
	sha := writeAndCommit(t, ctx, work, "a.txt", "clean\n", "clean work")

	res := PushToBranch(ctx, work, "origin", "feature")
	if !res.Pushed || remoteRefSHA(t, ctx, bare, "feature") != sha {
		t.Fatalf("clean push: %+v", res)
	}
}

func TestProjectCheck_RefusesEveryPartThatWouldLeave(t *testing.T) {
	ctx := context.Background()
	for name, make := range map[string]func(work string){
		"added line": func(w string) { writeAndCommit(t, ctx, w, "a.txt", "FORBIDDEN\n", "work") },
		"commit message": func(w string) {
			writeAndCommit(t, ctx, w, "a.txt", "clean\n", "work\n\nFORBIDDEN trailer")
		},
		"file name": func(w string) { writeAndCommit(t, ctx, w, "FORBIDDEN.txt", "", "work") },
		"added then removed": func(w string) {
			writeAndCommit(t, ctx, w, "a.txt", "FORBIDDEN\n", "work")
			writeAndCommit(t, ctx, w, "a.txt", "clean\n", "clean it up")
		},
		"author address": func(w string) {
			if err := os.WriteFile(filepath.Join(w, "a.txt"), []byte("clean\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mustGit(t, ctx, w, "add", "-A")
			mustGit(t, ctx, w, "commit", "-m", "work", "--author", "x <intruder@host>")
		},
	} {
		t.Run(name, func(t *testing.T) {
			bare, work := checkedRepo(t, ctx)
			make(work)
			res := PushToBranch(ctx, work, "origin", "feature")
			assertRefused(t, ctx, res, PushFailCheck, bare, "feature", "", "")
		})
	}
}

// A branch that rewrites the check, or removes it, is still judged by the base
// commit's copy.
func TestProjectCheck_BranchCannotWeakenItsOwnCheck(t *testing.T) {
	ctx := context.Background()
	for name, weaken := range map[string]func(work string){
		"script rewritten": func(w string) { writeAndCommit(t, ctx, w, "scripts/check.sh", "exit 0\n", "relax") },
		"target rewritten": func(w string) { writeAndCommit(t, ctx, w, "Makefile", "check-outgoing:\n\t@true\n", "relax") },
		"target removed":   func(w string) { writeAndCommit(t, ctx, w, "Makefile", "build:\n\t@true\n", "drop") },
		"uncommitted edit": func(w string) {
			if err := os.WriteFile(filepath.Join(w, "scripts/check.sh"), []byte("exit 0\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"local main moved": func(w string) {
			mustGit(t, ctx, w, "checkout", "main")
			writeAndCommit(t, ctx, w, "scripts/check.sh", "exit 0\n", "relax on local main")
			mustGit(t, ctx, w, "checkout", "feature")
		},
	} {
		t.Run(name, func(t *testing.T) {
			bare, work := checkedRepo(t, ctx)
			weaken(work)
			if err := os.WriteFile(filepath.Join(work, "bad.txt"), []byte("FORBIDDEN\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mustGit(t, ctx, work, "add", "bad.txt")
			mustGit(t, ctx, work, "commit", "-m", "work")
			res := PushToBranch(ctx, work, "origin", "feature")
			assertRefused(t, ctx, res, PushFailCheck, bare, "feature", "", "")
		})
	}
}

// A check declared locally but missing from the remote's default branch has no
// trusted copy, so nothing is pushed.
func TestProjectCheck_DeclaredButNotInBaseRefuses(t *testing.T) {
	ctx := context.Background()
	for name, where := range map[string]string{"on the pushed branch": "feature", "on local main": "main"} {
		t.Run(name, func(t *testing.T) {
			bare, work := testRepo(t, ctx)
			mustGit(t, ctx, work, "checkout", "-b", "feature")
			workCommit(t, ctx, work, "a.txt", "work")
			mustGit(t, ctx, work, "checkout", where)
			writeAndCommit(t, ctx, work, "Makefile", sandboxMakefile, "declare the check")
			res := PushToBranch(ctx, work, "origin", "feature")
			assertRefused(t, ctx, res, PushFailCheck, bare, "feature", "", "")
			if !strings.Contains(res.Reason, "no trusted copy") {
				t.Errorf("reason %q", res.Reason)
			}
		})
	}
}

// A check that cannot run refuses.
func TestProjectCheck_ThatCannotRunRefuses(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	writeAndCommit(t, ctx, work, "Makefile", "check-outgoing:\n\t@sh scripts/not-there.sh\n", "broken check")
	mustGit(t, ctx, work, "push", "origin", "main")
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	workCommit(t, ctx, work, "a.txt", "work")

	res := PushToBranch(ctx, work, "origin", "feature")
	assertRefused(t, ctx, res, PushFailCheck, bare, "feature", "", "")
}

// What is sent is the commit that was checked, even when the branch moves while
// the check runs.
func TestProjectCheck_PushesTheCheckedCommit(t *testing.T) {
	ctx := context.Background()
	bare, work := checkedRepo(t, ctx)
	checked := writeAndCommit(t, ctx, work, "a.txt", "clean\n", "checked work")
	unchecked := writeAndCommit(t, ctx, work, "b.txt", "FORBIDDEN\n", "later work")
	mustGit(t, ctx, work, "reset", "--hard", checked)
	if err := os.WriteFile(filepath.Join(work, ".move-to"), []byte(unchecked), 0o600); err != nil {
		t.Fatal(err)
	}

	res := PushToBranch(ctx, work, "origin", "feature")
	if !res.Pushed || res.SHA != checked {
		t.Fatalf("push: %+v", res)
	}
	if got := remoteRefSHA(t, ctx, bare, "feature"); got != checked {
		t.Fatalf("remote feature = %s, want the checked commit %s", got, checked)
	}
	if got := strings.TrimSpace(mustGit(t, ctx, work, "rev-parse", "feature")); got != unchecked {
		t.Fatalf("fixture: the branch should have moved during the check, at %s", got)
	}
}

// In a linked worktree the check is pointed at the main working tree, where
// files that are not committed live.
func TestProjectCheck_LinkedWorktreeUsesTheMainTree(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	writeAndCommit(t, ctx, work, "Makefile", "check-outgoing:\n\t@test -f \"$$OUTGOING_PROJECT_DIR/.private\"\n", "check")
	mustGit(t, ctx, work, "push", "origin", "main")
	if err := os.WriteFile(filepath.Join(work, ".private"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	mustGit(t, ctx, work, "worktree", "add", "-b", "feature", wt)
	workCommit(t, ctx, wt, "a.txt", "work")

	if res := PushWorkBranch(ctx, wt, "origin", "feature"); !res.Pushed {
		t.Fatalf("push from a worktree: %+v", res)
	}
	if !remoteHasRef(t, ctx, bare, "refs/heads/feature") {
		t.Fatal("feature not on the remote")
	}
}

// The check's output reaches the refusal with secrets removed.
func TestProjectCheck_OutputIsRedacted(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	writeAndCommit(t, ctx, work, "Makefile", "check-outgoing:\n\t@cat \"$$OUTGOING_MANIFEST_DIR/added.txt\"; exit 1\n", "chatty check")
	mustGit(t, ctx, work, "push", "origin", "main")
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)
	logBuf := captureLog(t)

	res := PushToBranch(ctx, work, "origin", "feature", enforce)
	assertRefused(t, ctx, res, PushFailCheck, bare, "feature", "", secret)
	if strings.Contains(logBuf.String(), secret) {
		t.Fatal("the log holds the secret")
	}
}

func TestOutgoing_SecretInTagMessageIsRefused(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	secret := fakeScanSecret()
	mustGit(t, ctx, work, "tag", "-a", "v1.0.0", "-m", "release notes "+secret)

	err := PushTag(ctx, work, "origin", "v1.0.0", enforce)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("want a redacted refusal, got %v", err)
	}
	if remoteHasRef(t, ctx, bare, "refs/tags") {
		t.Fatal("the tag was pushed")
	}
}

func TestOutgoing_DaemonPolicyCoversCallersWithoutOne(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	commitSecretFile(t, ctx, work, fakeScanSecret())
	SetDaemonScanPolicy(func() SecretScanPolicy { return enforce })
	t.Cleanup(func() { SetDaemonScanPolicy(nil) })

	if err := PushBranchRef(ctx, work, "origin", "main"); err == nil {
		t.Fatal("want a refusal from the daemon policy")
	}
	if got := remoteRefSHA(t, ctx, bare, "main"); got != before {
		t.Fatal("remote moved")
	}
}

func TestPushWorkBranch_NeverMovesADefaultBranch(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	workCommit(t, ctx, work, "a.txt", "work")
	res := PushWorkBranch(ctx, work, "origin", "main")
	assertRefused(t, ctx, res, PushFailProtected, bare, "main", before, "")

	// A default branch with another name, learnt from the remote itself.
	mustGit(t, ctx, work, "push", "origin", "main:refs/heads/trunk")
	mustGit(t, ctx, bare, "symbolic-ref", "HEAD", "refs/heads/trunk")
	mustGit(t, ctx, work, "checkout", "-b", "trunk")
	workCommit(t, ctx, work, "b.txt", "more work")
	trunkBefore := remoteRefSHA(t, ctx, bare, "trunk")
	res = PushWorkBranch(ctx, work, "origin", "trunk")
	assertRefused(t, ctx, res, PushFailProtected, bare, "trunk", trunkBefore, "")
}

func TestOutgoing_TwoPushAddressesRefuse(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	other := filepath.Join(t.TempDir(), "other.git")
	mustGit(t, ctx, work, "init", "--bare", other)
	mustGit(t, ctx, work, "remote", "set-url", "--add", "--push", "origin", bare)
	mustGit(t, ctx, work, "remote", "set-url", "--add", "--push", "origin", other)
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	workCommit(t, ctx, work, "a.txt", "work")

	res := PushWorkBranch(ctx, work, "origin", "feature")
	assertRefused(t, ctx, res, PushFailCheck, bare, "feature", "", "")
	if remoteHasRef(t, ctx, other, "refs/heads") {
		t.Fatal("pushed to the second address")
	}
}

// When the push address differs from the fetch address, what the push address
// has is what counts.
func TestOutgoing_ChecksAgainstThePushAddress(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	target := filepath.Join(t.TempDir(), "target.git")
	mustGit(t, ctx, work, "init", "--bare", target)
	secret := fakeScanSecret()
	// The fetch remote already has the commit; the push target does not.
	commitSecretFile(t, ctx, work, secret)
	mustGit(t, ctx, work, "push", "origin", "main")
	mustGit(t, ctx, work, "remote", "set-url", "--push", "origin", target)
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	workCommit(t, ctx, work, "a.txt", "work")

	res := PushWorkBranch(ctx, work, "origin", "feature", enforce)
	if res.Kind != PushFailSecret || remoteHasRef(t, ctx, target, "refs/heads") {
		t.Fatalf("want a refusal with nothing on the target, got %+v (origin %s)", res, bare)
	}
}

func TestCheckOutgoing_SendsNothing(t *testing.T) {
	ctx := context.Background()
	bare, work := checkedRepo(t, ctx)
	writeAndCommit(t, ctx, work, "a.txt", "clean\n", "clean work")
	refs := []OutgoingRef{{Src: "HEAD", Dst: "refs/heads/feature"}}
	if res := CheckOutgoing(ctx, work, "origin", refs, enforce); res.Kind != "" || res.Pushed {
		t.Fatalf("clean check: %+v", res)
	}
	writeAndCommit(t, ctx, work, "b.txt", "FORBIDDEN\n", "bad work")
	if res := CheckOutgoing(ctx, work, "origin", refs, enforce); res.Kind != PushFailCheck {
		t.Fatalf("dirty check: %+v", res)
	}
	if remoteHasRef(t, ctx, bare, "refs/heads/feature") {
		t.Fatal("a check pushed")
	}
}
