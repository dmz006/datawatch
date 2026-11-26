// One test per safeguard of the outgoing check that no other test would miss.

package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A push never takes submodule commits along, whatever the repository asks.
func TestOutgoing_SubmoduleRecursionIsOff(t *testing.T) {
	ctx := context.Background()
	t.Setenv("GIT_ALLOW_PROTOCOL", "file")
	subBare, subSeed := testRepo(t, ctx)
	_, work := testRepo(t, ctx)
	mustGit(t, ctx, work, "-c", "protocol.file.allow=always", "submodule", "add", subBare, "sub")
	mustGit(t, ctx, work, "commit", "-m", "add submodule")
	mustGit(t, ctx, work, "push", "origin", "main")
	_ = subSeed
	sub := filepath.Join(work, "sub")
	mustGit(t, ctx, sub, "config", "user.email", "t@example.com")
	mustGit(t, ctx, sub, "config", "user.name", "t")
	subBefore := remoteRefSHA(t, ctx, subBare, "main")
	workCommit(t, ctx, sub, "inner.txt", "unpushed submodule work")
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	mustGit(t, ctx, work, "add", "sub")
	mustGit(t, ctx, work, "commit", "-m", "bump submodule")
	mustGit(t, ctx, work, "config", "push.recurseSubmodules", "on-demand")

	if res := PushWorkBranch(ctx, work, "origin", "feature"); !res.Pushed {
		t.Fatalf("push: %+v", res)
	}
	if got := remoteRefSHA(t, ctx, subBare, "main"); got != subBefore {
		t.Fatalf("the submodule remote moved from %s to %s", subBefore, got)
	}
}

// The project's check never sees a variable that names a credential.
func TestProjectCheck_GetsNoCredentialVariables(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"DW_TEST_API_TOKEN", "DW_TEST_CLIENT_SECRET", "DW_TEST_DB_PASSWORD"} {
		t.Setenv(name, "value-"+strings.ToLower(name))
	}
	t.Setenv("DW_TEST_ORDINARY", "kept")
	_, work := testRepo(t, ctx)
	writeAndCommit(t, ctx, work, "Makefile",
		"check-outgoing:\n\t@test -z \"$$DW_TEST_API_TOKEN$$DW_TEST_CLIENT_SECRET$$DW_TEST_DB_PASSWORD\"\n\t@test \"$$DW_TEST_ORDINARY\" = kept\n", "check")
	mustGit(t, ctx, work, "push", "origin", "main")
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	workCommit(t, ctx, work, "a.txt", "work")

	if res := PushWorkBranch(ctx, work, "origin", "feature"); !res.Pushed {
		t.Fatalf("the check saw a credential variable (or lost an ordinary one): %+v", res)
	}
}

// A grafts file cannot take a commit out of what is checked.
func TestOutgoing_GraftsCannotHideACommit(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	secret := fakeScanSecret()
	commitSecretFile(t, ctx, work, secret)
	tip := writeAndCommit(t, ctx, work, "env", "removed again\n", "remove it")
	info := filepath.Join(work, ".git", "info")
	if err := os.MkdirAll(info, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pretend the tip sits directly on what the remote has.
	if err := os.WriteFile(filepath.Join(info, "grafts"), []byte(tip+" "+before+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := PushToBranch(ctx, work, "origin", "main", enforce)
	assertRefused(t, ctx, res, PushFailSecret, bare, "main", before, secret)
}

// More than the check can take is refused, not waved through.
func TestOutgoing_LimitsRefuse(t *testing.T) {
	ctx := context.Background()
	for name, lower := range map[string]func(){
		"commits": func() { outgoingMaxCommits = 2 },
		"bytes":   func() { outgoingMaxDiffBytes = 1500 },
	} {
		t.Run(name, func(t *testing.T) {
			commits, bytes := outgoingMaxCommits, outgoingMaxDiffBytes
			t.Cleanup(func() { outgoingMaxCommits, outgoingMaxDiffBytes = commits, bytes })
			bare, work := testRepo(t, ctx)
			before := remoteRefSHA(t, ctx, bare, "main")
			for _, f := range []string{"a", "b", "c"} {
				writeAndCommit(t, ctx, work, f, strings.Repeat("a plain line\n", 200), "add "+f)
			}
			lower()
			res := PushToBranch(ctx, work, "origin", "main", enforce)
			assertRefused(t, ctx, res, PushFailCheck, bare, "main", before, "")
		})
	}
}

// When the remote's default branch has moved past what this clone has, its
// commit is fetched and its check is the one that runs.
func TestProjectCheck_FetchesAMissingBase(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	mustGit(t, ctx, work, "checkout", "-b", "feature")
	writeAndCommit(t, ctx, work, "a.txt", "FORBIDDEN\n", "work")
	other := filepath.Join(t.TempDir(), "other")
	mustGit(t, ctx, work, "clone", bare, other)
	mustGit(t, ctx, other, "config", "user.email", "o@example.com")
	mustGit(t, ctx, other, "config", "user.name", "o")
	writeAndCommit(t, ctx, other, "scripts/check.sh", sandboxCheck, "add check script")
	writeAndCommit(t, ctx, other, "Makefile", sandboxMakefile, "add check target")
	mustGit(t, ctx, other, "push", "origin", "HEAD:main")

	res := PushWorkBranch(ctx, work, "origin", "feature")
	assertRefused(t, ctx, res, PushFailCheck, bare, "feature", "", "")
	if !strings.Contains(res.Reason, "forbidden word") {
		t.Errorf("the new base's check did not run: %q", res.Reason)
	}
}

// Several refs go all or nothing: a remote that refuses the tag keeps the
// branch where it was.
func TestOutgoing_BranchAndTagAreAtomic(t *testing.T) {
	ctx := context.Background()
	bare, work := testRepo(t, ctx)
	before := remoteRefSHA(t, ctx, bare, "main")
	hook := "#!/bin/sh\ncase \"$1\" in refs/tags/*) exit 1 ;; esac\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bare, "hooks", "update"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	workCommit(t, ctx, work, "a.txt", "work")
	mustGit(t, ctx, work, "tag", "-a", "v1.0.0", "-m", "release")

	res := PushCheckedRefs(ctx, work, "origin", []OutgoingRef{
		{Src: "refs/heads/main", Dst: "refs/heads/main"}, {Src: "refs/tags/v1.0.0", Dst: "refs/tags/v1.0.0"},
	})

	if res.Kind == "" || remoteRefSHA(t, ctx, bare, "main") != before || remoteHasRef(t, ctx, bare, "refs/tags") {
		t.Fatalf("want nothing to move, got %+v", res)
	}
}
