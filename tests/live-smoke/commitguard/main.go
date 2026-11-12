// Command commitguard is a live smoke check for the harness commit guard. It
// drives the real gitguard package and the real ProjectGit harness commit
// path against throwaway git repositories under a temp directory, using the
// system git. Nothing is mocked and the operator home is never touched.
//
// Usage: env -u TMUX -u TMUX_TMPDIR go run ./tests/live-smoke/commitguard
// Exit 0 when every check holds.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/gitguard"
	"github.com/dmz006/datawatch/internal/session"
)

var failed bool

func check(ok bool, format string, a ...any) {
	status := "PASS"
	if !ok {
		status = "FAIL"
		failed = true
	}
	fmt.Printf("%s: %s\n", status, fmt.Sprintf(format, a...))
}

// baseEnv isolates git from the operator's global configuration.
func baseEnv(home string) []string {
	return append(os.Environ(),
		"HOME="+home,
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=smoke", "GIT_AUTHOR_EMAIL=smoke@example.com",
		"GIT_COMMITTER_NAME=smoke", "GIT_COMMITTER_EMAIL=smoke@example.com",
	)
}

func git(dir string, env []string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = env
	out, err := c.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func newRepo(root, name string, env []string) string {
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		fatal(err)
	}
	if out, err := git(dir, env, "init", "-q"); err != nil {
		fatal(fmt.Errorf("init: %v %s", err, out))
	}
	return dir
}

func commitCount(dir string, env []string) int {
	out, err := git(dir, env, "rev-list", "--count", "--all")
	if err != nil {
		return 0
	}
	var n int
	_, _ = fmt.Sscanf(out, "%d", &n)
	return n
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "setup error:", err)
	os.Exit(2)
}

// validate mirrors the one-shot gate: every command must exit 0.
func validate(dir string, env []string, cmds ...string) bool {
	for _, c := range cmds {
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = dir
		cmd.Env = env
		if err := cmd.Run(); err != nil {
			return false
		}
	}
	return true
}

func main() {
	root, err := os.MkdirTemp("", "commitguard-smoke-")
	if err != nil {
		fatal(err)
	}
	defer func() { _ = os.RemoveAll(root) }()
	// Guard directories are created under the OS temp dir; point it at the
	// throwaway root so nothing else is touched or reaped.
	tmp := filepath.Join(root, "tmp")
	home := filepath.Join(root, "home")
	for _, d := range []string{tmp, home} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			fatal(err)
		}
	}
	if err := os.Setenv("TMPDIR", tmp); err != nil {
		fatal(err)
	}
	env := baseEnv(home)

	fmt.Println("== case 1: worker commit with guard env is rejected")
	repo1 := newRepo(root, "pass", env)
	g1, err := gitguard.NewCommitGuard("smoke-pass")
	if err != nil {
		fatal(err)
	}
	check(strings.HasPrefix(g1.Dir(), tmp), "guard dir %s is outside the repository", g1.Dir())
	_ = os.WriteFile(filepath.Join(repo1, "work.txt"), []byte("work\n"), 0o600)
	if out, err := git(repo1, env, "add", "work.txt"); err != nil {
		fatal(fmt.Errorf("add: %v %s", err, out))
	}
	workerEnv := append(append([]string{}, env...), g1.Env()...)
	out, err := git(repo1, workerEnv, "commit", "-q", "-m", "worker")
	fmt.Printf("observed: %s\n", out)
	check(err != nil && strings.Contains(out, "the harness commits after validation"), "worker git commit rejected with the harness message")
	out, err = git(repo1, workerEnv, "commit", "-q", "--no-verify", "-m", "worker")
	fmt.Printf("observed (--no-verify): %s\n", out)
	check(err != nil, "worker git commit --no-verify still rejected")
	check(commitCount(repo1, env) == 0, "no commit exists after worker attempts")

	fmt.Println("== case 2: harness commit with token after passing validation")
	if validate(repo1, env, "true") {
		pg := session.NewProjectGit(repo1)
		pg.SetCommitEnv(append(append([]string{}, env...), g1.CommitEnv()...))
		sha, err := pg.CommitSessionWork("smoke-pass", "guarded work", "smoke summary", nil)
		fmt.Printf("observed: sha=%s err=%v\n", sha, err)
		head, _ := git(repo1, env, "rev-parse", "HEAD")
		check(err == nil && sha != "" && sha == head, "harness commit succeeded, HEAD=%s", head)
		check(commitCount(repo1, env) == 1, "exactly one commit exists")
	} else {
		check(false, "validation unexpectedly failed")
	}

	fmt.Println("== case 3: failed validation leaves no commit")
	repo3 := newRepo(root, "fail", env)
	g3, err := gitguard.NewCommitGuard("smoke-fail")
	if err != nil {
		fatal(err)
	}
	_ = os.WriteFile(filepath.Join(repo3, "work.txt"), []byte("work\n"), 0o600)
	pg3 := session.NewProjectGit(repo3)
	committed := false
	if validate(repo3, env, "false") {
		pg3.SetCommitEnv(append(append([]string{}, env...), g3.CommitEnv()...))
		_, _ = pg3.CommitSessionWork("smoke-fail", "guarded work", "", nil)
		committed = true
	}
	check(!committed, "token was never applied because validation failed")
	check(commitCount(repo3, env) == 0, "no commit exists after failed validation")
	_, _ = git(repo3, env, "add", "work.txt")
	out, err = git(repo3, append(append([]string{}, env...), g3.Env()...), "commit", "-q", "-m", "late")
	fmt.Printf("observed: %s\n", out)
	check(err != nil, "a later commit without the token is still rejected")

	fmt.Println("== case 4: cleanup")
	for name, g := range map[string]*gitguard.CommitGuard{"pass": g1, "fail": g3} {
		dir := g.Dir()
		check(g.Cleanup() == nil, "%s: cleanup returned no error", name)
		_, statErr := os.Stat(dir)
		check(os.IsNotExist(statErr), "%s: guard directory %s removed", name, filepath.Base(dir))
	}
	for _, r := range []string{repo1, repo3} {
		v, _ := git(r, env, "config", "--local", "--get-all", "core.hooksPath")
		check(v == "", "%s: no core.hooksPath in repository config", filepath.Base(r))
		check(gitguard.ScrubHooksPath(r, "") == nil, "%s: scrub is a no-op", filepath.Base(r))
	}
	left, _ := filepath.Glob(filepath.Join(tmp, "datawatch-commitguard-*"))
	check(len(left) == 0, "no guard directories remain under the temp dir (%d)", len(left))

	if failed {
		fmt.Println("VERDICT: FAIL")
		os.Exit(1)
	}
	fmt.Println("VERDICT: PASS")
}
