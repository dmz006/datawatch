//go:build unix

package mergepipeline

// Nothing is released before the remote confirms the exact pushed commit, and
// a push whose confirmation was lost is repaired from the journal written
// before it. Sandboxes with local bare remotes only.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// realPusher pushes as the daemon does, with faults added around the push.
type realPusher struct {
	ExecShipRunner
	before  func()
	after   func() error // runs after a real push; its error is the push's
	fake    bool         // no push is made and fakeErr is returned
	fakeErr error
	deploys *int
}

func (p realPusher) PushCommit(ctx context.Context, dir, commit, branch, tag string) error {
	if p.before != nil {
		p.before()
	}
	if p.fake {
		return p.fakeErr
	}
	if err := p.ExecShipRunner.PushCommit(ctx, dir, commit, branch, tag); err != nil {
		return err
	}
	if p.after != nil {
		return p.after()
	}
	return nil
}

func (p realPusher) Deploy(context.Context, string) (ShipResult, error) {
	if p.deploys != nil {
		*p.deploys++
	}
	return ShipResult{Note: "deployed"}, nil
}

// sandboxLanding applies the conditions the daemon's landing record applies:
// the remote holds the commit and the commit holds the branch.
type sandboxLanding struct {
	s        sandbox
	recorded *[]string
	refuse   bool
}

func (l sandboxLanding) RecordMergeLanding(_ context.Context, id, _, commit string, _ time.Time) (*autonomous.PRD, error) {
	ancestor := func(dir, a, b string) bool {
		return exec.Command("git", "-C", dir, "merge-base", "--is-ancestor", a, b).Run() == nil
	}
	switch {
	case l.refuse:
		return nil, errors.New("the store cannot be written")
	case !ancestor(l.s.bare, commit, "refs/heads/main"):
		return nil, errors.New("the remote does not hold the commit")
	case !ancestor(l.s.dir, "refs/heads/feature", commit):
		return nil, errors.New("the commit does not hold the branch")
	}
	*l.recorded = append(*l.recorded, id+" "+commit)
	return &autonomous.PRD{}, nil
}

func releaseRunner(t *testing.T, s sandbox, def Definition, ship ShipRunner, landing Landing, notify *fakeNotify) *Runner {
	t.Helper()
	r := s.runner(t, def)
	r.Ship, r.Landing, r.Notifier = ship, landing, notify
	r.PostShip = PostShipSettings{Enabled: true, NotifyTemplate: "Shipped {project} v{version}",
		Hooks: []PostShipHook{{Name: "notify", Builtin: "notify", Enabled: true}}}
	return r
}

func releaseFeature(r *Runner) (RunResult, error) {
	return r.Run(context.Background(), RunRequest{Title: "Add the feature", Branches: []string{"feature"}, AutomatonID: "a1"})
}

func committerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.invalid")
}

// diverge moves main after the branch forked, so a run rebases the branch.
func diverge(t *testing.T, s sandbox) {
	t.Helper()
	putFile(t, s.dir, "later.txt", "later\n")
	runGit(t, s.dir, "add", "-A")
	runGit(t, s.dir, "commit", "-q", "-m", "later on main")
	runGit(t, s.dir, "push", "-q", "origin", "main")
}

func TestNoReleaseActionBeforeTheRemoteConfirmsTheCommit(t *testing.T) {
	for name, c := range map[string]struct {
		pusher     func(s sandbox, deploys *int) realPusher
		continueOn bool
	}{
		"success claimed, remote lacks the commit": {pusher: func(_ sandbox, d *int) realPusher { return realPusher{fake: true, deploys: d} }},
		"remote unreadable after the push": {pusher: func(s sandbox, d *int) realPusher {
			return realPusher{deploys: d, after: func() error { return os.Rename(s.bare, s.bare+".gone") }}
		}},
		"push refused, set to continue": {continueOn: true, pusher: func(_ sandbox, d *int) realPusher {
			return realPusher{fake: true, fakeErr: errors.New("refused"), deploys: d}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			committerEnv(t)
			def := DefaultDefinition()
			for i := range def.Steps {
				if c.continueOn && def.Steps[i].Action == ActionPushRelease {
					def.Steps[i].OnFailure = OnFailureContinue
				}
			}
			var landed []string
			deploys := 0
			notify := &fakeNotify{}
			r := releaseRunner(t, s, def, c.pusher(s, &deploys), sandboxLanding{s: s, recorded: &landed}, notify)
			main := runGit(t, s.dir, "rev-parse", "main")
			res, _ := releaseFeature(r)
			_ = os.Rename(s.bare+".gone", s.bare)
			if deploys != 0 || len(notify.sent()) != 0 || len(landed) != 0 {
				t.Errorf("release actions without a confirmed push: deploys=%d notices=%v landed=%v", deploys, notify.sent(), landed)
			}
			if res.Pushed || res.Run.Result == autonomous.MergeRunOK {
				t.Errorf("pushed=%v result=%q, want neither pushed nor ok", res.Pushed, res.Run.Result)
			}
			if got := runGit(t, s.dir, "rev-parse", "main"); got != main {
				t.Errorf("local main moved to %s", got)
			}
		})
	}
}

// A pipeline's own deploy, tag or hook command waits for the confirmed push
// too, wherever it is listed.
func TestReleaseCommandStepsWaitForTheConfirmedPush(t *testing.T) {
	for _, kind := range []Kind{KindDeploy, KindTag, KindPostShipHook} {
		t.Run(string(kind), func(t *testing.T) {
			s := newSandbox(t)
			def := noTagSteps(Step{Name: "early", Kind: kind, Command: "true"})
			def.Steps = append(def.Steps, Step{Name: "late", Kind: kind, Command: "true"})
			ship := &recordingPusher{}
			r := s.runner(t, def)
			r.Ship = ship
			res, err := mergeFeature(t, r)
			if err != nil || !res.Pushed {
				t.Fatalf("%v %+v", err, res.Run.Steps)
			}
			if got := strings.Join(ship.hooks, ","); got != "late" {
				t.Errorf("release commands run = %q, want only the one after the push", got)
			}
			if stepResults(res.Run)["early"] != autonomous.MergeStepSkipped {
				t.Errorf("early step: %+v", res.Run.Steps)
			}
		})
	}
}

type recordingPusher struct {
	ExecShipRunner
	hooks []string
}

func (p *recordingPusher) Hook(_ context.Context, _ string, s Step) (string, error) {
	p.hooks = append(p.hooks, s.Name)
	return "ran", nil
}

// assertReconciled is the state after a landed push has been repaired.
func assertReconciled(t *testing.T, s sandbox, landed []string, remoteMain string) {
	t.Helper()
	if len(landed) != 1 || !strings.HasPrefix(landed[0], "a1 "+remoteMain) {
		t.Errorf("landing records = %v, want one for %s", landed, remoteMain)
	}
	if got := runGit(t, s.bare, "rev-parse", "refs/heads/main"); got != remoteMain {
		t.Errorf("a second release was pushed: remote main %s, was %s", got, remoteMain)
	}
	if got := runGit(t, s.dir, "rev-parse", "main"); got != remoteMain {
		t.Errorf("local main = %s, want %s", got, remoteMain)
	}
	if err := exec.Command("git", "-C", s.dir, "merge-base", "--is-ancestor", "feature", remoteMain).Run(); err != nil {
		t.Errorf("the branch is not part of the pushed commit")
	}
	if local, remote := runGit(t, s.dir, "tag", "--list"), runGit(t, s.bare, "tag", "--list"); !strings.Contains(local, remote) {
		t.Errorf("local tags %q lack the remote's %q", local, remote)
	}
	if left := pushJournal(t, s); len(left) != 0 {
		t.Errorf("journal entries left: %v", left)
	}
	if n := strings.Count(runGit(t, s.dir, "worktree", "list", "--porcelain"), "worktree "); n != 1 {
		t.Errorf("worktrees = %d", n)
	}
}

func pushJournal(t *testing.T, s sandbox) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(s.dir, ".git", "datawatch-merge-pushes", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPushIsJournalledBeforeItLeaves(t *testing.T) {
	s := newSandbox(t)
	diverge(t, s)
	var before []string
	var body []byte
	r := s.runner(t, noTagSteps())
	r.Ship = realPusher{before: func() {
		before = pushJournal(t, s)
		if len(before) == 1 {
			body, _ = os.ReadFile(before[0])
		}
	}}
	res, err := releaseFeature(r)
	if err != nil || !res.Pushed {
		t.Fatalf("%v %+v", err, res.Run.Steps)
	}
	remote := runGit(t, s.bare, "rev-parse", "refs/heads/main")
	for _, want := range []string{remote, `"a1"`, `"feature"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("journal before the push lacks %s: %s", want, body)
		}
	}
	if left := pushJournal(t, s); len(left) != 0 {
		t.Errorf("a finished push left its journal entry: %v", left)
	}
}

func TestRunKilledAfterItsPushIsReconciled(t *testing.T) {
	if dir := os.Getenv("DW_KP_REPO"); dir != "" {
		r := &Runner{Resolver: resolverWithRecord(t, DefaultDefinition()), VersionFiles: testVersionFiles, Dir: dir, Project: "p",
			Ship: realPusher{after: func() error {
				_ = syscall.Kill(os.Getpid(), syscall.SIGKILL) // end-reason-ok: the test kills its own child process
				time.Sleep(time.Minute)
				return nil
			}}, Isolate: true, ScratchRoot: os.Getenv("DW_KP_SCRATCH")}
		_, _ = releaseFeature(r)
		return
	}
	for name, how := range map[string]string{"by the next run": "run", "at the next start": "start"} {
		for _, rebased := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "", true: ", rebased branch"}[rebased], func(t *testing.T) {
				s := newSandbox(t)
				committerEnv(t)
				runGit(t, s.dir, "tag", "v1.2.3")
				if rebased {
					diverge(t, s)
				}
				mainBefore := runGit(t, s.dir, "rev-parse", "main")
				scratch := t.TempDir()
				child := exec.Command(os.Args[0], "-test.run=^TestRunKilledAfterItsPushIsReconciled$", "-test.timeout=60s")
				child.Env = append(os.Environ(), "DW_KP_REPO="+s.dir, "DW_KP_SCRATCH="+scratch, "TMPDIR="+t.TempDir())
				_ = child.Run()
				remote := runGit(t, s.bare, "rev-parse", "refs/heads/main")
				if remote == mainBefore || runGit(t, s.dir, "rev-parse", "main") != mainBefore {
					t.Fatal("fixture: the child should have pushed and then died before moving local main")
				}
				var landed []string
				r := releaseRunner(t, s, DefaultDefinition(), realPusher{}, sandboxLanding{s: s, recorded: &landed}, &fakeNotify{})
				r.ScratchRoot = scratch
				if how == "start" {
					if _, err := r.ReconcilePushes(context.Background()); err != nil {
						t.Fatal(err)
					}
				} else {
					res, _ := releaseFeature(r)
					if res.Pushed {
						t.Errorf("the next run released again: %+v", res.Run.Steps)
					}
				}
				assertReconciled(t, s, landed, remote)
			})
		}
	}
}

func TestPushThatLandsButReportsAnErrorIsReconciled(t *testing.T) {
	s := newSandbox(t)
	committerEnv(t)
	runGit(t, s.dir, "tag", "v1.2.3")
	diverge(t, s)
	var landed []string
	deploys := 0
	notify := &fakeNotify{}
	r := releaseRunner(t, s, DefaultDefinition(), realPusher{deploys: &deploys, after: func() error {
		return errors.New("connection reset after the remote accepted")
	}}, sandboxLanding{s: s, recorded: &landed}, notify)
	res, _ := releaseFeature(r)
	if !res.Pushed {
		t.Errorf("the remote holds the commit but the run says it was not pushed: %+v", res.Run.Steps)
	}
	assertReconciled(t, s, landed, runGit(t, s.bare, "rev-parse", "refs/heads/main"))
	if runGit(t, s.dir, "tag", "--list", "v1.2.4") == "" {
		t.Error("the local tag the remote has was deleted")
	}
}

func TestUnreadableRemoteKeepsTheTagUntilItCanBeAsked(t *testing.T) {
	s := newSandbox(t)
	committerEnv(t)
	runGit(t, s.dir, "tag", "v1.2.3")
	var landed []string
	r := releaseRunner(t, s, DefaultDefinition(), realPusher{after: func() error { return os.Rename(s.bare, s.bare+".gone") }},
		sandboxLanding{s: s, recorded: &landed}, &fakeNotify{})
	_, _ = releaseFeature(r)
	if len(pushJournal(t, s)) != 1 || runGit(t, s.dir, "tag", "--list", "v1.2.4") == "" {
		t.Errorf("with the remote unreadable the journal (%v) and the tag must be kept", pushJournal(t, s))
	}
	if err := os.Rename(s.bare+".gone", s.bare); err != nil {
		t.Fatal(err)
	}
	next := releaseRunner(t, s, DefaultDefinition(), realPusher{}, sandboxLanding{s: s, recorded: &landed}, &fakeNotify{})
	if res, _ := releaseFeature(next); res.Pushed {
		t.Errorf("released again: %+v", res.Run.Steps)
	}
	assertReconciled(t, s, landed, runGit(t, s.bare, "rev-parse", "refs/heads/main"))
}

// While a pushed release cannot be recorded, its work is not released again.
func TestNoSecondReleaseWhileTheFirstCannotBeRecorded(t *testing.T) {
	s := newSandbox(t)
	var landed []string
	first := releaseRunner(t, s, noTagSteps(), realPusher{}, sandboxLanding{s: s, recorded: &landed, refuse: true}, &fakeNotify{})
	if res, err := releaseFeature(first); err != nil || !res.Pushed {
		t.Fatalf("%v %+v", err, res.Run.Steps)
	}
	remote := runGit(t, s.bare, "rev-parse", "refs/heads/main")
	if len(pushJournal(t, s)) != 1 {
		t.Fatalf("an unrecorded push must stay journalled: %v", pushJournal(t, s))
	}
	again := releaseRunner(t, s, noTagSteps(), realPusher{}, sandboxLanding{s: s, recorded: &landed, refuse: true}, &fakeNotify{})
	res, err := releaseFeature(again)
	if err == nil || res.Pushed || !strings.Contains(err.Error(), "already pushed") {
		t.Errorf("err=%v pushed=%v, want a refusal saying it was already pushed", err, res.Pushed)
	}
	if got := runGit(t, s.bare, "rev-parse", "refs/heads/main"); got != remote {
		t.Errorf("pushed again: %s", got)
	}
	working := releaseRunner(t, s, noTagSteps(), realPusher{}, sandboxLanding{s: s, recorded: &landed}, &fakeNotify{})
	if _, err := working.ReconcilePushes(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertReconciled(t, s, landed, remote)
}

func (f *fakeNotify) sent() []string { return f.texts }
