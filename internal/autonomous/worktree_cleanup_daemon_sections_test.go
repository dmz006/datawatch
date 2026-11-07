// A work tree whose only uncommitted change is the daemon's own marked
// section is not dirty for cleanup purposes: the daemon rewrites that
// block at the next session start, so removing the work tree loses
// nothing. A work tree with a real edit is still skipped. Proved both at
// the core level (scripted git) and at the manager level (real git).
package autonomous

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// daemonSectionFixtureClean is what HEAD has: only operator content.
const daemonSectionFixtureClean = "# Task\n\nDo the thing.\n"

// daemonSectionFixtureDaemonOnly is the working copy: the operator content
// plus one daemon-owned section (a `# Memory & Knowledge (datawatch)`
// header block, which StripDaemonSections removes).
const daemonSectionFixtureDaemonOnly = daemonSectionFixtureClean +
	"\n# Memory & Knowledge (datawatch)\n\n- saved fact\n"

// daemonSectionFixtureRealEdit is the working copy: the operator content
// plus a change the daemon never owns.
const daemonSectionFixtureRealEdit = "# Task\n\nDo the thing.\n\n## My own note\n\noperator edit\n"

// ------------------------------------------------------------------- core level

func TestWorktreeCleanupDaemonSectionOnlyChangeIsNotDirty(t *testing.T) {
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, "CLAUDE.md"), []byte(daemonSectionFixtureDaemonOnly), 0644); err != nil {
		t.Fatal(err)
	}
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree /repo/primary\n\n"
	g.out[keyOf(wt, "status", "--porcelain")] = " M CLAUDE.md"
	g.out[keyOf(wt, "show", "HEAD:CLAUDE.md")] = daemonSectionFixtureClean
	g.out[keyOf(testRepo, "rev-list", "--count", "main.."+testBranch)] = "0"
	g.out[keyOf(testRepo, "cherry", "main", testBranch)] = ""
	g.out[keyOf(testRepo, "log", "main.."+testBranch, "--format=%s")] = ""
	g.out[keyOf(testRepo, "log", "main", "--format=%s")] = ""
	g.out[keyOf(testRepo, "worktree", "remove", wt)] = ""
	g.out[keyOf(testRepo, "branch", "-d", testBranch)] = ""
	report := SafeDeleteReport{
		Safe: []SafeDeleteEntry{{Branch: testBranch, Worktree: wt, Reason: "all commits on main"}},
		Held: []SafeDeleteHeld{},
	}
	res := runCore(g, eligibleProbe(), report, nil, nil)
	if len(res.Removed) != 1 || res.Removed[0].Branch != testBranch {
		t.Fatalf("a daemon-section-only change must not hold the work tree, got removed=%+v skipped=%+v failed=%+v",
			res.Removed, res.Skipped, res.Failed)
	}
	if skippedHas(res, cleanupSkipDirty) {
		t.Fatalf("the entry must not be classified dirty, got %+v", res.Skipped)
	}
}

func TestWorktreeCleanupDaemonSectionRealEditStillDirty(t *testing.T) {
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, "CLAUDE.md"), []byte(daemonSectionFixtureRealEdit), 0644); err != nil {
		t.Fatal(err)
	}
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree /repo/primary\n\n"
	g.out[keyOf(wt, "status", "--porcelain")] = " M CLAUDE.md"
	g.out[keyOf(wt, "show", "HEAD:CLAUDE.md")] = daemonSectionFixtureClean
	g.out[keyOf(testRepo, "rev-list", "--count", "main.."+testBranch)] = "0"
	report := SafeDeleteReport{
		Safe: []SafeDeleteEntry{{Branch: testBranch, Worktree: wt, Reason: "all commits on main"}},
		Held: []SafeDeleteHeld{},
	}
	res := runCore(g, eligibleProbe(), report, nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipDirty) {
		t.Fatalf("a work tree with a real edit must be skipped with %q, got %+v", cleanupSkipDirty, res)
	}
	if g.hasCall("worktree", "remove") || g.hasCall("branch", "-d") {
		t.Fatalf("no removal commands may run on a dirty work tree: %v", g.called)
	}
}

func TestWorktreeCleanupDaemonSectionAmongRealDirtStillDirty(t *testing.T) {
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, "CLAUDE.md"), []byte(daemonSectionFixtureDaemonOnly), 0644); err != nil {
		t.Fatal(err)
	}
	g := newScriptedGit()
	g.out[keyOf(testRepo, "worktree", "list", "--porcelain")] = "worktree /repo/primary\n\n"
	// The daemon section is fine, but an untracked stray is not.
	g.out[keyOf(wt, "status", "--porcelain")] = " M CLAUDE.md\n?? stray.txt\n"
	g.out[keyOf(wt, "show", "HEAD:CLAUDE.md")] = daemonSectionFixtureClean
	report := SafeDeleteReport{
		Safe: []SafeDeleteEntry{{Branch: testBranch, Worktree: wt, Reason: "all commits on main"}},
		Held: []SafeDeleteHeld{},
	}
	res := runCore(g, eligibleProbe(), report, nil, nil)
	if len(res.Removed) != 0 || !skippedHas(res, cleanupSkipDirty) {
		t.Fatalf("a daemon section plus a stray file must still be skipped with %q, got %+v", cleanupSkipDirty, res)
	}
}

// -------------------------------------------------------- manager level (real git)

// daemonSectionFixtureRepo builds a base repository (default branch "trunk",
// recorded on the remote) with one committed file (CLAUDE.md), plus a linked
// worktree on an automaton/* branch with no commits ahead of trunk.
func daemonSectionFixtureRepo(t *testing.T) (baseRepo, worktree string) {
	t.Helper()
	isolateGitEnv(t)
	root := t.TempDir()
	bare := filepath.Join(root, "origin-bare")
	baseRepo = filepath.Join(root, "base")
	worktree = filepath.Join(root, "worktrees", "fixture")

	gitCmd(t, root, "init", "--bare", "--initial-branch=trunk", bare)
	gitCmd(t, root, "init", "--initial-branch=trunk", baseRepo)
	if err := os.WriteFile(filepath.Join(baseRepo, "CLAUDE.md"), []byte(daemonSectionFixtureClean), 0644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, baseRepo, "add", "CLAUDE.md")
	gitCmd(t, baseRepo, "commit", "-m", "first (on trunk)")
	gitCmd(t, baseRepo, "remote", "add", "origin", bare)
	gitCmd(t, baseRepo, "push", "-q", "origin", "trunk")
	gitCmd(t, baseRepo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/trunk")
	gitCmd(t, baseRepo, "worktree", "add", "-q", "-B", "automaton/fixture", worktree, "trunk")
	return baseRepo, worktree
}

func TestWorktreeCleanupDaemonSectionNotHeldByTheDirtyCheck(t *testing.T) {
	baseRepo, worktree := daemonSectionFixtureRepo(t)
	m, audit := buildManager(t, baseRepo, config.CleanupModeOn, 0)
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now().Add(-48*time.Hour))
	// The daemon rewrote its own section; nothing the operator wrote changed.
	if err := os.WriteFile(filepath.Join(worktree, "CLAUDE.md"), []byte(daemonSectionFixtureDaemonOnly), 0644); err != nil {
		t.Fatal(err)
	}
	m.ReleaseAutomatonWorktree(prd)
	// Real git's strict `git worktree remove` (never with force) still sees a
	// modified checkout, so the removal is refused there and the work tree
	// stays for inspection — but the release must not have been held by the
	// dirty check: no dirty skip, and a failed removal attempt at the remove
	// stage, which is the existing never-force gate for any dirty checkout.
	if !branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("a strict git removal must not have force-deleted a modified checkout")
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the work tree must still exist: %v", err)
	}
	outcome := audit.events("autonomous_worktree_cleanup_release")
	if len(outcome) != 1 {
		t.Fatalf("want one release outcome audit event, got %d: %+v", len(outcome), audit.items)
	}
	if skipped, ok := outcome[0]["skipped"].(map[string]int); ok {
		if skipped[cleanupSkipDirty] != 0 {
			t.Fatalf("a daemon-section-only change must not be held by the dirty check, got skipped=%v", skipped)
		}
	} else {
		t.Fatalf("release outcome should carry a skipped digest, got %#v", outcome[0])
	}
	failed, ok := outcome[0]["failed"].(int)
	if !ok || failed != 1 {
		t.Fatalf("the strict git removal refusal must be recorded as one failed attempt, got %#v", outcome[0])
	}
	// Note: the per-entry audit record is written before the removal
	// attempt (pre-existing ordering), so it may exist even though nothing
	// was removed; the branch and work tree still existing above is the
	// proof that no removal completed.
}

func TestWorktreeCleanupDaemonSectionLeavesRealEditAlone(t *testing.T) {
	baseRepo, worktree := daemonSectionFixtureRepo(t)
	m, audit := buildManager(t, baseRepo, config.CleanupModeOn, 0)
	prd := saveFixturePRD(t, m, "automaton/fixture", PRDCompleted, "", time.Now().Add(-48*time.Hour))
	// A real operator edit, not a daemon section.
	if err := os.WriteFile(filepath.Join(worktree, "CLAUDE.md"), []byte(daemonSectionFixtureRealEdit), 0644); err != nil {
		t.Fatal(err)
	}
	m.ReleaseAutomatonWorktree(prd)
	if !branchExists(t, baseRepo, "automaton/fixture") {
		t.Fatalf("a work tree with a real edit must be left alone")
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("the work tree must still exist: %v", err)
	}
	if n := len(audit.events(worktreeCleanupAuditAction)); n != 0 {
		t.Fatalf("no removal may be audited, got %d events", n)
	}
}
