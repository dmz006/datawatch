package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/llm"
)

// The commit guard of a session refuses in the repository of the session's
// project folder only. The real-tmux tests use the fixture of
// pane_env_tmux_test.go: a private tmux server and throwaway repositories.

func scopeTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
}

// scopeWorkerScript tries git in the project, in a linked worktree of it and
// in a repository it creates, and records "label=ok|refused|failed".
const scopeWorkerScript = `#!/bin/sh
out="$1"; project="$2"; second="$3"; scratch="$4"
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@example.com GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@example.com
: > "$out/results"
try() {
  label=$1; dir=$2; shift 2
  if (cd "$dir" && git "$@") > "$out/last" 2>&1; then r=ok
  elif grep -q 'harness commits after validation' "$out/last"; then r=refused
  else r="failed: $(tr '\n' ' ' < "$out/last")"; fi
  echo "$label=$r" >> "$out/results"
}
try project-commit "$project" commit -q --allow-empty -m worker
try project-ref "$project" branch "worker-$$"
try second-commit "$second" commit -q --allow-empty -m worker
try second-ref "$second" tag "worker-$$"
try other-init "$scratch" init -q other
try other-commit "$scratch/other" commit -q --allow-empty -m worker
try other-branch "$scratch/other" branch b
echo done > "$out/done"
`

var scopeWant = []string{
	"project-commit=refused", "project-ref=refused", "second-commit=refused", "second-ref=refused",
	"other-init=ok", "other-commit=ok", "other-branch=ok",
}

type scopeFixture struct {
	*paneEnvFixture
	main, linked, worker string
}

func newScopeFixture(t *testing.T) *scopeFixture {
	t.Helper()
	f := &scopeFixture{paneEnvFixture: newPaneEnvFixture(t)}
	root := filepath.Dir(f.out)
	f.main, f.linked, f.worker = f.proj, filepath.Join(filepath.Dir(f.proj), "linked"), filepath.Join(root, "scope-worker.sh")
	scopeTestGit(t, f.main, "init", "-q")
	scopeTestGit(t, f.main, "commit", "-q", "--allow-empty", "-m", "base")
	scopeTestGit(t, f.main, "worktree", "add", "-q", "-b", "linkedbranch", f.linked)
	if err := os.WriteFile(f.worker, []byte(scopeWorkerScript), 0o700); err != nil { // #nosec G306 -- test script
		t.Fatal(err)
	}
	return f
}

// command is the worker command line for a session working in project, with
// second the other checkout of the same repository.
func (f *scopeFixture) command(project, second string) string {
	f.t.Helper()
	scratch, err := os.MkdirTemp(filepath.Dir(f.out), "scratch")
	if err != nil {
		f.t.Fatal(err)
	}
	words := []string{"sh", f.worker, f.out, project, second, scratch}
	for i := 1; i < len(words); i++ {
		words[i] = shellSingleQuote(words[i])
	}
	return strings.Join(words, " ")
}

func (f *scopeFixture) assertResults() {
	f.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(f.out, "done")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("worker did not finish")
		}
		time.Sleep(50 * time.Millisecond)
	}
	b, err := os.ReadFile(filepath.Join(f.out, "results"))
	if err != nil {
		f.t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(b)), strings.Join(scopeWant, "\n"); got != want {
		f.t.Errorf("worker git results:\n%s\nwant:\n%s", got, want)
	}
	if err := os.Remove(filepath.Join(f.out, "done")); err != nil {
		f.t.Fatal(err)
	}
}

func TestPaneGuardRefusesOnlyInTheSessionRepository(t *testing.T) {
	for _, tc := range []struct {
		name     string
		inLinked bool // the session works in a linked worktree, as an isolated task does
	}{{"project is the main checkout", false}, {"project is a linked worktree", true}} {
		t.Run(tc.name, func(t *testing.T) {
			stubPaneCeiling(t, false)
			f := newScopeFixture(t)
			project, second := f.main, f.linked
			if tc.inLinked {
				project, second = f.linked, f.main
			}
			launch := func(ctx context.Context, _, tmuxSession, dir, _ string) error {
				return llm.SendKeys(ctx, tmuxSession, f.command(dir, second))
			}
			sess, err := f.m.Start(context.Background(), "do the work", "", project, &StartOptions{
				Backend: "pane-env-test", LaunchFn: launch, PRDID: "prd1",
			})
			if err != nil {
				t.Fatalf("start: %v", err)
			}
			f.assertResults()

			stored, ok := f.m.GetSession(sess.FullID)
			if !ok {
				t.Fatal("session not stored")
			}
			done := *stored
			done.State = StateComplete
			if err := f.m.SaveSession(&done); err != nil {
				t.Fatal(err)
			}
			f.m.SetLLMBackend("pane-env-test", launch)
			if _, err := f.m.Restart(context.Background(), sess.FullID); err != nil {
				t.Fatalf("restart: %v", err)
			}
			f.assertResults()
		})
	}
}

func TestSubprocessGuardRefusesOnlyInTheSessionRepository(t *testing.T) {
	f := newScopeFixture(t)
	f.m.SetSubprocessBaseURL("http://127.0.0.1:18443")
	sess, err := f.m.Start(context.Background(), f.command(f.main, f.linked), "", f.main, &StartOptions{
		Backend: "pane-env-test", Subprocess: true, PRDID: "prd1",
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	f.assertResults()
	// Let the runner finish before the fixture is removed.
	for i := 0; i < 200; i++ {
		if s, ok := f.m.GetSession(sess.FullID); ok && s.State != StateRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// A live session whose guard folder is gone gets a new guard at daemon
// adoption; it must guard the session's repository, not every repository.
func TestAdoptCommitGuardsScopesANewGuardToTheSessionRepository(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	project := guardTestRepo(t)
	st, err := NewStore(filepath.Join(t.TempDir(), "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	m := &Manager{hostname: "host", store: st, tmux: NewFakeTmux()}
	m.adoptCommitGuards([]*Session{{FullID: "host-scope1", PRDID: "p", ProjectDir: project, TmuxSession: "cs-scope"}})
	g := m.commitGuardFor("host-scope1")
	if g == nil {
		t.Fatal("no guard adopted")
	}
	t.Cleanup(func() { _ = g.Cleanup() })
	want, err := filepath.EvalSymlinks(filepath.Join(project, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	if g.Repository() != want {
		t.Errorf("guarded repository = %q, want %q", g.Repository(), want)
	}
}
