package gitguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for the repository scope of the commit guard. Every repository here is
// a throwaway under t.TempDir().

const refusalText = "the harness commits after validation"

// scopeEnv gives the test a git environment of its own: no user or system
// config, a fixed identity, and no guard variables inherited from the caller.
func scopeEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_PARAMETERS",
		"GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", CommitTokenEnv} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
}

func mustGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	out, err := guardGit(t, dir, env, args...)
	if err != nil {
		t.Errorf("git %v in %s: %v: %s", args, dir, err, out)
	}
	return out
}

func mustWrite(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil { // #nosec G306 -- test hooks must be executable
		t.Fatal(err)
	}
}

// historyRepo makes a repository at dir with two commits on main, a spare
// branch and an uncommitted change.
func historyRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, nil, "init", "-q", "-b", "main")
	mustWrite(t, filepath.Join(dir, "f.txt"), "one\n", 0o600)
	mustGit(t, dir, nil, "add", "f.txt")
	mustGit(t, dir, nil, "commit", "-q", "-m", "one")
	mustWrite(t, filepath.Join(dir, "f.txt"), "two\n", 0o600)
	mustGit(t, dir, nil, "commit", "-q", "-am", "two")
	mustGit(t, dir, nil, "branch", "spare")
	mustWrite(t, filepath.Join(dir, "f.txt"), "dirty\n", 0o600)
	return dir
}

func scopedGuard(t *testing.T, projectDir string) *CommitGuard {
	t.Helper()
	g, err := NewRepoCommitGuard("scope-sess", projectDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Cleanup() })
	return g
}

func refState(t *testing.T, dir string) string {
	t.Helper()
	return mustGit(t, dir, nil, "for-each-ref", "--format=%(refname) %(objectname)") +
		mustGit(t, dir, nil, "rev-parse", "HEAD")
}

// assertRefused runs git under the guard and expects the guard's refusal and
// no change to any ref.
func assertRefused(t *testing.T, g *CommitGuard, dir string, args ...string) {
	t.Helper()
	before := refState(t, dir)
	out, err := guardGit(t, dir, g.Env(), args...)
	if err == nil {
		t.Errorf("git %v in %s was not refused: %s", args, dir, out)
	} else if !strings.Contains(out, refusalText) {
		t.Errorf("git %v in %s failed, but not by the guard: %s", args, dir, out)
	}
	if after := refState(t, dir); after != before {
		t.Errorf("git %v in %s changed refs:\nbefore %safter %s", args, dir, before, after)
	}
}

// refusedOps are operations the guard refused before it was scoped to a
// repository. Inside the session's repository the list must stay refused.
func refusedOps(t *testing.T) [][]string {
	t.Helper()
	source := historyRepo(t, filepath.Join(t.TempDir(), "source"))
	return [][]string{
		{"commit", "--allow-empty", "-m", "x"},
		{"commit", "--allow-empty", "--no-verify", "-m", "x"},
		{"commit", "-am", "x"},
		{"stash"},
		{"branch", "newbranch"},
		{"branch", "-D", "spare"},
		{"switch", "-c", "newswitch"},
		{"checkout", "-b", "newcheckout"},
		{"tag", "v1"},
		{"reset", "--hard", "HEAD~1"},
		{"update-ref", "refs/heads/plumbed", "HEAD"},
		{"worktree", "add", "-b", "wtbranch", filepath.Join(t.TempDir(), "more")},
		{"fetch", source, "main:refs/heads/fetched"},
	}
}

func TestRepoGuardRefusesInOwnRepositoryAndLinkedWorktree(t *testing.T) {
	scopeEnv(t)
	root := t.TempDir()
	own := historyRepo(t, filepath.Join(root, "own"))
	linked := filepath.Join(root, "linked")
	mustGit(t, own, nil, "worktree", "add", "-q", "-b", "linkedbranch", linked)
	mustWrite(t, filepath.Join(linked, "f.txt"), "dirty linked\n", 0o600)
	g := scopedGuard(t, own)
	for _, dir := range []string{own, linked} {
		for _, op := range refusedOps(t) {
			assertRefused(t, g, dir, op...)
		}
		// What never touched a ref is still allowed.
		for _, op := range [][]string{{"status", "--short"}, {"add", "f.txt"}, {"log", "--oneline"}, {"diff", "--cached"}} {
			mustGit(t, dir, g.Env(), op...)
		}
	}
	// The same repository named from somewhere else.
	elsewhere := t.TempDir()
	for _, args := range [][]string{
		{"-C", own, "tag", "from-c"},
		{"--git-dir=" + filepath.Join(own, ".git"), "tag", "from-git-dir"},
	} {
		if out, err := guardGit(t, elsewhere, g.Env(), args...); err == nil || !strings.Contains(out, refusalText) {
			t.Errorf("git %v: err=%v out=%s", args, err, out)
		}
	}
}

func TestRepoGuardAllowsOtherRepositories(t *testing.T) {
	scopeEnv(t)
	own := historyRepo(t, filepath.Join(t.TempDir(), "own"))
	g := scopedGuard(t, own)
	env := g.Env()
	root := t.TempDir()

	other := filepath.Join(root, "other")
	mustGit(t, root, env, "init", "-q", "-b", "main", other)
	mustWrite(t, filepath.Join(other, "f.txt"), "one\n", 0o600)
	mustGit(t, other, env, "add", "f.txt")
	mustGit(t, other, env, "commit", "-q", "-m", "one")
	mustGit(t, other, env, "commit", "-q", "--allow-empty", "-m", "two")
	mustWrite(t, filepath.Join(other, "f.txt"), "dirty\n", 0o600)
	mustGit(t, other, env, "stash")
	mustGit(t, other, env, "stash", "pop", "-q")
	mustGit(t, other, env, "branch", "b1")
	mustGit(t, other, env, "switch", "-q", "-c", "b2")
	mustGit(t, other, env, "checkout", "-q", "-b", "b3")
	mustGit(t, other, env, "tag", "v1")
	mustGit(t, other, env, "reset", "-q", "--hard", "HEAD~1")
	mustGit(t, other, env, "update-ref", "refs/heads/plumbed", "HEAD")
	mustGit(t, other, env, "worktree", "add", "-q", "-b", "wtb", filepath.Join(root, "otherwt"))
	mustGit(t, filepath.Join(root, "otherwt"), env, "commit", "-q", "--allow-empty", "-m", "in worktree")

	clone := filepath.Join(root, "clone")
	mustGit(t, root, env, "clone", "-q", other, clone)
	mustGit(t, other, env, "branch", "later")
	mustGit(t, clone, env, "fetch", "-q", "origin")
	// A clone of the project is another repository.
	ownClone := filepath.Join(root, "own-clone")
	mustGit(t, root, env, "clone", "-q", own, ownClone)
	mustGit(t, ownClone, env, "commit", "-q", "--allow-empty", "-m", "in a clone of the project")

	bare := filepath.Join(root, "bare.git")
	mustGit(t, root, env, "init", "-q", "--bare", bare)
	mustGit(t, other, env, "push", "-q", bare, "HEAD:refs/heads/pushed")
	mustGit(t, bare, env, "update-ref", "refs/heads/in-bare", "refs/heads/pushed")

	sep := filepath.Join(root, "sep")
	mustGit(t, root, env, "init", "-q", "--separate-git-dir", filepath.Join(root, "sep.git"), sep)
	mustGit(t, sep, env, "commit", "-q", "--allow-empty", "-m", "separate git dir")
}

func TestRepoGuardJudgesByGitCommonDirectory(t *testing.T) {
	scopeEnv(t)
	root := t.TempDir()
	own := historyRepo(t, filepath.Join(root, "own"))
	outside := historyRepo(t, filepath.Join(root, "outside"))
	nested := historyRepo(t, filepath.Join(own, "vendor", "nested"))
	if err := os.MkdirAll(filepath.Join(own, "sub", "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	linkToOwn := filepath.Join(root, "link-to-own")
	linkInOwn := filepath.Join(own, "link-to-outside")
	if err := os.Symlink(own, linkToOwn); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, linkInOwn); err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(root, "linked")
	mustGit(t, own, nil, "worktree", "add", "-q", "-b", "linkedbranch", linked)

	// A repository created inside the project under the guard is another one.
	t.Run("init inside the project", func(t *testing.T) {
		g := scopedGuard(t, own)
		inside := filepath.Join(own, "sub", "fresh")
		mustGit(t, own, g.Env(), "init", "-q", inside)
		mustGit(t, inside, g.Env(), "commit", "-q", "--allow-empty", "-m", "allowed")
		assertRefused(t, g, own, "tag", "still-refused")
	})

	// The session folder may be the repository, a symlink to it, a folder
	// inside it or one of its linked worktrees: the same repository each time.
	for name, project := range map[string]string{
		"repository": own, "symlink": linkToOwn, "subfolder": filepath.Join(own, "sub", "dir"),
		"linked worktree": linked, "symlinked subfolder": filepath.Join(linkToOwn, "sub"),
	} {
		t.Run(name, func(t *testing.T) {
			g := scopedGuard(t, project)
			for _, dir := range []string{own, linkToOwn, filepath.Join(own, "sub", "dir"), linked} {
				assertRefused(t, g, dir, "commit", "--allow-empty", "-m", "x")
				assertRefused(t, g, dir, "tag", "nope")
			}
			// A repository nested inside the project, and one reached
			// through a symlink inside the project, are other repositories.
			for _, dir := range []string{nested, linkInOwn, outside} {
				mustGit(t, dir, g.Env(), "commit", "-q", "--allow-empty", "-m", "allowed")
			}
		})
	}
}

func TestRepoGuardRefusesEverywhereWhenOwnRepositoryIsUnknown(t *testing.T) {
	scopeEnv(t)
	plain := t.TempDir()
	other := historyRepo(t, filepath.Join(t.TempDir(), "other"))
	for name, project := range map[string]string{"folder outside git": plain, "no folder": "", "missing folder": filepath.Join(plain, "gone")} {
		t.Run(name, func(t *testing.T) {
			assertRefused(t, scopedGuard(t, project), other, "commit", "--allow-empty", "-m", "x")
		})
	}
	t.Run("own repository moved away", func(t *testing.T) {
		root := t.TempDir()
		own := historyRepo(t, filepath.Join(root, "own"))
		g := scopedGuard(t, own)
		moved := filepath.Join(root, "moved")
		if err := os.Rename(own, moved); err != nil {
			t.Fatal(err)
		}
		assertRefused(t, g, moved, "commit", "--allow-empty", "-m", "x")
		assertRefused(t, g, other, "commit", "--allow-empty", "-m", "x")
	})
}

// A git variable in the daemon's own environment must not change which
// repository the guard takes as the session's.
func TestRepoGuardResolvesProjectDirWithoutCallerGitEnv(t *testing.T) {
	scopeEnv(t)
	own := historyRepo(t, filepath.Join(t.TempDir(), "own"))
	other := historyRepo(t, filepath.Join(t.TempDir(), "other"))
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_COMMON_DIR", filepath.Join(other, ".git"))
	g := scopedGuard(t, own)
	_ = os.Unsetenv("GIT_DIR")
	_ = os.Unsetenv("GIT_COMMON_DIR")
	assertRefused(t, g, own, "tag", "nope")
	mustGit(t, other, g.Env(), "tag", "ok")
}

// recordHook is a hook that appends its name, arguments and stdin size to log.
func recordHook(log string) string {
	return "#!/bin/sh\nn=$(wc -c)\necho \"${0##*/} $# stdin=$((n))\" >> '" + log + "'\nexit 0\n"
}

func hookLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	return string(b)
}

func TestRepoGuardKeepsOtherRepositoriesHooks(t *testing.T) {
	scopeEnv(t)
	own := historyRepo(t, filepath.Join(t.TempDir(), "own"))
	g := scopedGuard(t, own)

	for _, tc := range []struct{ name, hooksPath string }{
		{"default hooks folder", ""}, {"relative core.hooksPath", "myhooks"}, {"absolute core.hooksPath", "ABS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := historyRepo(t, filepath.Join(t.TempDir(), "other"))
			log := filepath.Join(t.TempDir(), "log")
			dir := filepath.Join(other, ".git", "hooks")
			switch tc.hooksPath {
			case "myhooks":
				dir = filepath.Join(other, "myhooks")
				mustGit(t, other, nil, "config", "core.hooksPath", "myhooks")
			case "ABS":
				dir = filepath.Join(t.TempDir(), "abs hooks")
				mustGit(t, other, nil, "config", "core.hooksPath", dir)
			}
			for _, n := range []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit", "reference-transaction"} {
				mustWrite(t, filepath.Join(dir, n), recordHook(log), 0o700)
			}
			mustGit(t, other, g.Env(), "commit", "-q", "--allow-empty", "-m", "x")
			got := hookLog(t, log)
			for _, want := range []string{"pre-commit 0", "prepare-commit-msg 2", "commit-msg 1", "post-commit 0", "reference-transaction 1"} {
				if !strings.Contains(got, want) {
					t.Errorf("hook %q did not run; log:\n%s", want, got)
				}
			}
			if strings.Contains(got, "reference-transaction 1 stdin=0") {
				t.Errorf("reference-transaction got no stdin; log:\n%s", got)
			}
			// The repository's own hook can still veto.
			mustWrite(t, filepath.Join(dir, "pre-commit"), "#!/bin/sh\necho own-veto >&2\nexit 1\n", 0o700)
			if out, err := guardGit(t, other, g.Env(), "commit", "--allow-empty", "-m", "y"); err == nil || !strings.Contains(out, "own-veto") {
				t.Errorf("the repository's own pre-commit did not veto: err=%v %s", err, out)
			}
		})
	}

	t.Run("global core.hooksPath", func(t *testing.T) {
		dir, log := t.TempDir(), filepath.Join(t.TempDir(), "log")
		cfg := filepath.Join(t.TempDir(), "gitconfig")
		mustWrite(t, cfg, "[core]\n\thooksPath = "+dir+"\n", 0o600)
		mustWrite(t, filepath.Join(dir, "pre-commit"), recordHook(log), 0o700)
		other := historyRepo(t, filepath.Join(t.TempDir(), "other"))
		t.Setenv("GIT_CONFIG_GLOBAL", cfg)
		mustGit(t, other, g.Env(), "commit", "-q", "--allow-empty", "-m", "x")
		if got := hookLog(t, log); !strings.Contains(got, "pre-commit 0") {
			t.Errorf("global hook did not run; log:\n%s", got)
		}
	})

	// A leftover value naming a guard folder must not make one guard run
	// another, or itself.
	t.Run("core.hooksPath naming a guard folder", func(t *testing.T) {
		other := historyRepo(t, filepath.Join(t.TempDir(), "other"))
		mustGit(t, other, nil, "config", "core.hooksPath", g.Dir())
		mustGit(t, other, g.Env(), "commit", "-q", "--allow-empty", "-m", "x")
	})
}

func TestRepoGuardKeepsOwnRepositoryHooksWithoutWeakeningRefusal(t *testing.T) {
	scopeEnv(t)
	root := t.TempDir()
	own := historyRepo(t, filepath.Join(root, "own"))
	linked := filepath.Join(root, "linked")
	mustGit(t, own, nil, "worktree", "add", "-q", "-b", "linkedbranch", linked)
	log := filepath.Join(t.TempDir(), "log")
	hooks := filepath.Join(own, ".git", "hooks")
	// As in a project that sets core.hooksPath in its own config.
	mustGit(t, own, nil, "config", "core.hooksPath", hooks)
	for _, n := range []string{"pre-commit", "commit-msg", "reference-transaction", "pre-push", "post-index-change"} {
		mustWrite(t, filepath.Join(hooks, n), recordHook(log), 0o700)
	}
	g := scopedGuard(t, own)
	bare := filepath.Join(root, "bare.git")
	mustGit(t, root, nil, "init", "-q", "--bare", bare)

	for _, dir := range []string{own, linked} {
		hookLog(t, log)
		assertRefused(t, g, dir, "commit", "--allow-empty", "-m", "x")
		assertRefused(t, g, dir, "tag", "nope")
		got := hookLog(t, log)
		// The hooks that refuse never reach the project's hooks.
		for _, n := range []string{"pre-commit", "commit-msg", "reference-transaction"} {
			if strings.Contains(got, n+" ") {
				t.Errorf("project hook %s ran for a refused operation in %s:\n%s", n, dir, got)
			}
		}
		mustGit(t, dir, g.Env(), "push", "-q", bare, "HEAD:refs/heads/"+filepath.Base(dir))
		if got := hookLog(t, log); !strings.Contains(got, "pre-push 2") {
			t.Errorf("project pre-push did not run in %s:\n%s", dir, got)
		}
	}
	// A project pre-push veto still counts.
	mustWrite(t, filepath.Join(hooks, "pre-push"), "#!/bin/sh\necho own-veto >&2\nexit 1\n", 0o700)
	if out, err := guardGit(t, own, g.Env(), "push", bare, "HEAD:refs/heads/vetoed"); err == nil || !strings.Contains(out, "own-veto") {
		t.Errorf("the project's pre-push did not veto: err=%v %s", err, out)
	}

	// The harness commit runs no project hook, as before: nothing a worker
	// put in the project's hooks folder runs inside the daemon.
	hookLog(t, log)
	mustGit(t, own, g.CommitEnv(), "commit", "-q", "-am", "harness")
	if got := hookLog(t, log); got != "" {
		t.Errorf("project hooks ran for the harness commit:\n%s", got)
	}
}

func TestAdoptRepoCommitGuardKeepsScopeAndToken(t *testing.T) {
	scopeEnv(t)
	t.Setenv("TMPDIR", t.TempDir())
	// A path with shell metacharacters stays data in the hook script.
	own := historyRepo(t, filepath.Join(t.TempDir(), `we ird'" != "$(false)`+"`x`"))
	other := historyRepo(t, filepath.Join(t.TempDir(), "other"))
	g := scopedGuard(t, own)
	adopted, err := AdoptRepoCommitGuard("scope-sess", own)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Dir() != g.Dir() || adopted.Token() != g.Token() {
		t.Fatalf("adopted %s, want the existing guard %s", adopted.Dir(), g.Dir())
	}
	assertRefused(t, adopted, own, "tag", "nope")
	mustGit(t, other, adopted.Env(), "tag", "ok")
	mustGit(t, own, adopted.CommitEnv(), "tag", "harness")

	// With no folder left to adopt, the new guard is scoped as well.
	if err := g.Cleanup(); err != nil {
		t.Fatal(err)
	}
	fresh, err := AdoptRepoCommitGuard("scope-sess", own)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Cleanup() })
	assertRefused(t, fresh, own, "tag", "nope2")
	mustGit(t, other, fresh.Env(), "tag", "ok2")
}
