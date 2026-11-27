//go:build !windows

package acceptance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", r.dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Env = append(cleanEnv(os.Environ()), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *gitRepo) write(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *gitRepo) commit(msg string) string {
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

type gitResolver struct {
	r    *gitRepo
	work string
}

func (g *gitResolver) Resolve(_ context.Context, _, effect string) (Subject, error) {
	c := g.work
	if ref, ok := strings.CutPrefix(effect, "ref:"); ok {
		c = g.r.git("rev-parse", ref)
	}
	return Subject{Repo: g.r.dir, Commit: c}, nil
}

func gitService(t *testing.T, res SubjectResolver, checks map[string]ProjectCheck) *Service {
	t.Helper()
	return must[*Service](t)(Open(Options{
		Path: filepath.Join(t.TempDir(), "a.json"), Mode: func() string { return ModeReport }, Resolver: res,
		Settings: func() Settings { return Settings{ProjectChecks: checks, CheckTimeout: 20 * time.Second} },
	}))
}

// helper.sh is the listed input; it passes only when code.txt says fixed.
const helper = `. ./code.txt
[ -f ./override.sh ] && . ./override.sh
if [ "$STATE" = fixed ]; then echo "PASS T"; else echo "FAIL T"; exit 1; fi
`

func TestRule3CleanCopyOfTheExactVersion(t *testing.T) {
	ctx := context.Background()
	r := newGitRepo(t)
	r.write("helper.sh", helper)
	r.write("code.txt", "STATE=broken\n")
	start := r.commit("start")
	res := &gitResolver{r: r, work: start}
	s := gitService(t, res, map[string]ProjectCheck{"unit": {Command: "sh helper.sh", Inputs: []string{"helper.sh"}, Expect: []Expect{{Pass: "PASS T", Fail: "FAIL T"}}}})
	l := must[*List](t)(s.Edit(ctx, EditInput{RequestID: "r1", Source: "x", Ask: oneAsk, Upsert: []Item{fix()}}, operator))
	l = must[*List](t)(s.Freeze(ctx, "r1", l.Hash(), operator, ""))
	if b := l.State["fix"].Baseline; b == nil || !b.Finished || len(b.FailSeen) != 1 {
		t.Fatalf("baseline %+v", b)
	}
	eval := func() ItemRead { return read(must[*List](t)(s.Evaluate(ctx, "r1")), "fix") }

	// Planted untracked and uncommitted helpers do not reach the check.
	r.write("override.sh", "STATE=fixed\n")
	if got := eval(); got.Result.Value != ResultNotMet {
		t.Fatalf("untracked helper reached the check: %+v", got)
	}
	r.write("code.txt", "STATE=fixed\n")
	if err := os.Remove(filepath.Join(r.dir, "override.sh")); err != nil {
		t.Fatal(err)
	}
	if got := eval(); got.Result.Value != ResultNotMet {
		t.Fatalf("an uncommitted edit counted: %+v", got)
	}
	// A weakened listed input reads cannot tell.
	r.write("helper.sh", "echo 'PASS T'\n")
	res.work = r.commit("weaken")
	if got := eval(); got.Result.Value != ResultCannotTell || !strings.Contains(got.Result.Reason, "helper.sh") {
		t.Fatalf("%+v", got)
	}
	r.write("helper.sh", helper)
	res.work = r.commit("fix")
	if got := eval(); got.Result.Value != ResultMet {
		t.Fatalf("%+v", got)
	}
	// The repository's own index and worktree are untouched.
	if st := r.git("status", "--porcelain"); st != "" {
		t.Fatalf("repository changed: %q", st)
	}
}

func TestRule3SavedScriptRunsAndAChangedScriptShows(t *testing.T) {
	ctx := context.Background()
	r := newGitRepo(t)
	r.write("verify.sh", "echo 'FAIL T'; exit 1\n")
	r.write("other.txt", "x")
	start := r.commit("start")
	res := &gitResolver{r: r, work: start}
	s := gitService(t, res, nil)
	it := Item{ID: "s", Statement: "s", From: oneAsk, Method: MethodCommand, Effect: "work",
		Check: &CheckSpec{ScriptPath: "verify.sh", Script: "echo 'FAIL T'; exit 1\n", Expect: []Expect{{Pass: "PASS T"}}}}
	l := must[*List](t)(s.Edit(ctx, EditInput{RequestID: "r1", Ask: oneAsk, Upsert: []Item{it}}, operator))
	must[*List](t)(s.Freeze(ctx, "r1", l.Hash(), operator, ""))
	r.write("verify.sh", "echo 'PASS T'\n")
	res.work = r.commit("rewrite the check")
	got := read(must[*List](t)(s.Evaluate(ctx, "r1")), "s")
	if got.Result.Value != ResultCannotTell || !strings.Contains(got.Result.Reason, "check changed") {
		t.Fatalf("%+v", got)
	}
}

func TestRule8DroppedChangeWithItsCommitStillOnMain(t *testing.T) {
	ctx := context.Background()
	for name, drop := range map[string]func(r *gitRepo){
		"merge -s ours": func(r *gitRepo) {
			r.git("checkout", "-q", "main")
			r.write("other.txt", "main moved")
			r.commit("main moved")
			r.git("merge", "-q", "-s", "ours", "--no-edit", "work")
			r.git("merge-base", "--is-ancestor", "work", "main")
		},
		"revert": func(r *gitRepo) {
			r.git("checkout", "-q", "main")
			r.git("merge", "-q", "--no-ff", "--no-edit", "work")
			r.git("revert", "--no-edit", "-m", "1", "HEAD")
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newGitRepo(t)
			r.write("feature.txt", "old\n")
			start := r.commit("start")
			r.git("checkout", "-q", "-b", "work")
			r.write("feature.txt", "new behaviour\n")
			work := r.commit("work")
			s := gitService(t, &gitResolver{r: r, work: start}, nil)
			item := func(id, effect string) Item {
				return Item{ID: id, Statement: id, From: oneAsk, Method: "state:file", Change: true, Effect: effect, State: &StateSpec{Path: "feature.txt", Contains: "new behaviour"}}
			}
			l := must[*List](t)(s.Edit(ctx, EditInput{RequestID: "r1", Ask: oneAsk, Upsert: []Item{item("onmain", "ref:main")}}, operator))
			must[*List](t)(s.Freeze(ctx, "r1", l.Hash(), operator, ""))
			drop(r)
			_ = work
			got := read(must[*List](t)(s.Evaluate(ctx, "r1")), "onmain")
			if got.Result.Value != ResultNotMet {
				t.Fatalf("dropped change reads %+v", got)
			}
		})
	}
}

func TestGitRunnerFaultsAreNotFinishedRuns(t *testing.T) {
	r := newGitRepo(t)
	r.write("a", "a")
	sub := Subject{Repo: r.dir, Commit: r.commit("a")}
	run := func(cmd string, d time.Duration) RunOutcome {
		return Git{}.Run(context.Background(), CheckSpec{Command: cmd}, sub, d)
	}
	if o := run("sleep 30", 500*time.Millisecond); o.Finished || !strings.Contains(o.Reason, "limit") {
		t.Fatalf("timeout %+v", o)
	}
	for _, c := range []string{"no-such-tool-xyz", "kill -9 $$"} {
		if o := run(c, 20*time.Second); o.Finished {
			t.Fatalf("%s: %+v", c, o)
		}
	}
	if o := run(`test -z "$TMUX$DATAWATCH_TOKEN" && cat a`, 20*time.Second); !o.Finished || o.ExitCode != 0 || o.Output != "a" {
		t.Fatalf("%+v", o)
	}
	if o := (Git{}).Run(context.Background(), CheckSpec{Command: "true"}, Subject{Repo: r.dir, Commit: "--help"}, time.Second); o.Finished {
		t.Fatalf("%+v", o)
	}
}

func TestGitReadFileRefusesLinksAndEscapes(t *testing.T) {
	r := newGitRepo(t)
	r.write("a", "a")
	if err := os.Symlink("/etc/hostname", filepath.Join(r.dir, "link")); err != nil {
		t.Fatal(err)
	}
	sub := Subject{Repo: r.dir, Commit: r.commit("a")}
	for _, p := range []string{"link", "../a", "/etc/hostname", ""} {
		if _, ok, err := (Git{}).ReadFile(context.Background(), sub, p); ok || err == nil {
			t.Fatalf("%q: ok=%v err=%v", p, ok, err)
		}
	}
	if _, ok, err := (Git{}).ReadFile(context.Background(), sub, "missing"); ok || err != nil {
		t.Fatalf("missing: %v %v", ok, err)
	}
}
