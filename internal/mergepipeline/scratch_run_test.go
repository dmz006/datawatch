package mergepipeline

// An isolated merge run: the merge is built in a scratch worktree, and the
// project's main branch moves only after the remote is confirmed to hold the
// pushed commit. Every repository here is a sandbox with a local bare
// "remote"; nothing leaves the machine.

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

// noTagSteps is the default pipeline without its tag and deploy steps, with
// extra inserted just before the release-bump validator, where checks of the
// final candidate belong.
func noTagSteps(extra ...Step) Definition {
	var out []Step
	for _, s := range DefaultDefinition().Steps {
		switch s.Action {
		case ActionTagRelease, ActionDeployLatest:
			continue
		case ActionValidateReleaseBump:
			out = append(out, extra...)
		}
		out = append(out, s)
	}
	return Definition{Steps: out}
}

func check(name, command string) Step {
	return Step{Name: name, Kind: KindTest, Command: command}
}

// sandbox is a merge repository with a bare remote holding main and feature.
type sandbox struct{ dir, bare, notes string }

func newSandbox(t *testing.T) sandbox {
	t.Helper()
	dir := newMergeRepo(t)
	return sandbox{dir: dir, bare: withOrigin(t, dir), notes: t.TempDir()}
}

func (s sandbox) runner(t *testing.T, def Definition) *Runner {
	t.Helper()
	return &Runner{
		Resolver: resolverWithRecord(t, def), VersionFiles: testVersionFiles, Dir: s.dir, Project: "p",
		Ship: ExecShipRunner{}, Recorder: &memRecorder{}, Isolate: true, ScratchRoot: t.TempDir(),
	}
}

// state is everything a run may not change when it does not ship: the
// project's refs, checkout and worktrees, and every ref on the remote.
func (s sandbox) state(t *testing.T) string {
	t.Helper()
	return strings.Join([]string{
		"local:\n" + runGit(t, s.dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads", "refs/tags", "refs/datawatch-merge-tags"),
		"head: " + runGit(t, s.dir, "symbolic-ref", "HEAD") + " " + runGit(t, s.dir, "rev-parse", "HEAD"),
		"status: " + runGit(t, s.dir, "status", "--porcelain", "--untracked-files=all"),
		"worktrees:\n" + runGit(t, s.dir, "worktree", "list", "--porcelain"),
		"remote:\n" + runGit(t, s.bare, "for-each-ref", "--format=%(refname) %(objectname)"),
	}, "\n")
}

func (s sandbox) assertUnchanged(t *testing.T, before string) {
	t.Helper()
	if after := s.state(t); after != before {
		t.Errorf("the project or the remote changed:\nBEFORE\n%s\nAFTER\n%s", before, after)
	}
}

func (s sandbox) note(name string) string { return filepath.Join(s.notes, name) }

func (s sandbox) read(name string) string {
	b, _ := os.ReadFile(s.note(name))
	return strings.TrimSpace(string(b))
}

func mergeFeature(t *testing.T, r *Runner) (RunResult, error) {
	t.Helper()
	return r.Run(context.Background(), RunRequest{Title: "Add the feature", Branches: []string{"feature"}})
}

func TestIsolatedFailedCheckLeavesEverythingUnchanged(t *testing.T) {
	s := newSandbox(t)
	before := s.state(t)
	res, err := mergeFeature(t, s.runner(t, noTagSteps(check("unit tests", "exit 1"))))
	if err == nil || stepResults(res.Run)["unit tests"] != autonomous.MergeStepFailed {
		t.Fatalf("err=%v steps=%+v", err, res.Run.Steps)
	}
	s.assertUnchanged(t, before)
}

func TestIsolatedHeldReleaseIsNeverCarriedByALaterRun(t *testing.T) {
	s := newSandbox(t)
	runGit(t, s.dir, "checkout", "-q", "-b", "other")
	putFile(t, s.dir, "other.txt", "other\n")
	runGit(t, s.dir, "add", "-A")
	runGit(t, s.dir, "commit", "-q", "-m", "other work")
	runGit(t, s.dir, "checkout", "-q", "main")
	before := s.state(t)

	held := s.runner(t, noTagSteps())
	held.ReleaseSize, held.ReleaseApproval = "patch", "manual"
	notify := &fakeNotify{}
	held.Notifier = notify
	held.PostShip = PostShipSettings{Enabled: true, NotifyTemplate: "shipped",
		Hooks: []PostShipHook{{Name: "notify", Builtin: "notify", Enabled: true}}}
	res, err := mergeFeature(t, held)
	if err != nil || res.Pushed || stepResults(res.Run)["push-release"] != autonomous.MergeStepSkipped {
		t.Fatalf("held run: err=%v pushed=%v steps=%+v", err, res.Pushed, res.Run.Steps)
	}
	if _, ran := stepResults(res.Run)["notify"]; ran {
		t.Error("a post-ship hook ran for a release that was never pushed")
	}
	s.assertUnchanged(t, before)

	next := s.runner(t, noTagSteps())
	if _, err := next.Run(context.Background(), RunRequest{Title: "Other", Branches: []string{"other"}}); err != nil {
		t.Fatalf("later run: %v", err)
	}
	files := runGit(t, s.bare, "ls-tree", "--name-only", "refs/heads/main")
	if !strings.Contains(files, "other.txt") || strings.Contains(files, "feature.txt") {
		t.Errorf("the later push carried the held work, or lost its own: %s", files)
	}
}

func TestIsolatedRefusedPushLeavesNoTrace(t *testing.T) {
	s := newSandbox(t)
	hook := filepath.Join(s.bare, "hooks", "pre-receive")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\necho refused here >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.invalid")
	runGit(t, s.dir, "tag", "v1.2.3")
	before := s.state(t)
	// The default pipeline tags before it pushes.
	r := s.runner(t, DefaultDefinition())
	res, err := mergeFeature(t, r)
	if err == nil || stepResults(res.Run)["tag-release"] != autonomous.MergeStepOK || stepResults(res.Run)["push-release"] != autonomous.MergeStepFailed {
		t.Fatalf("err=%v steps=%+v", err, res.Run.Steps)
	}
	s.assertUnchanged(t, before)
}

func TestIsolatedRunPushesTheCheckedCommitThenMovesMain(t *testing.T) {
	s := newSandbox(t)
	// main gets a commit after feature forked, so the run has to rebase.
	putFile(t, s.dir, "later.txt", "later\n")
	runGit(t, s.dir, "add", "-A")
	runGit(t, s.dir, "commit", "-q", "-m", "later on main")
	runGit(t, s.dir, "push", "-q", "origin", "main")
	base := runGit(t, s.dir, "rev-parse", "main")
	branchBefore := runGit(t, s.dir, "rev-parse", "feature")

	def := noTagSteps(check("unit tests", "git rev-parse HEAD > '"+s.note("checked")+"' && "+
		"git rev-parse refs/heads/main > '"+s.note("main-during")+"' && "+
		"git rev-parse refs/heads/feature > '"+s.note("branch-during")+"' && pwd > '"+s.note("pwd")+"'"))
	r := s.runner(t, def)
	res, err := mergeFeature(t, r)
	if err != nil || !res.Pushed {
		t.Fatalf("run: %v pushed=%v %+v", err, res.Pushed, res.Run.Steps)
	}
	if s.read("main-during") != base || s.read("branch-during") != branchBefore {
		t.Errorf("main or the branch moved before the push: main %s (base %s), branch %s (was %s)",
			s.read("main-during"), base, s.read("branch-during"), branchBefore)
	}
	if s.read("pwd") == s.dir || s.read("pwd") == "" {
		t.Errorf("the check ran in the project's own checkout: %q", s.read("pwd"))
	}
	checked := s.read("checked")
	remote := runGit(t, s.bare, "rev-parse", "refs/heads/main")
	local := runGit(t, s.dir, "rev-parse", "main")
	if checked == "" || remote != checked || local != checked {
		t.Errorf("checked %s, remote main %s, local main %s: all three must be one commit", checked, remote, local)
	}
	// The rebased branch moved with the push, so it is on main.
	runGit(t, s.dir, "merge-base", "--is-ancestor", "feature", "main")
	if runGit(t, s.dir, "status", "--porcelain") != "" || getFile(t, s.dir, "feature.txt") == "" {
		t.Error("the project's checkout was not brought up to the pushed commit")
	}
	if left := runGit(t, s.dir, "for-each-ref", "--format=%(refname)", "refs/heads/datawatch-merge", "refs/datawatch-merge-tags"); left != "" {
		t.Errorf("temporary refs left: %s", left)
	}
	if n := strings.Count(runGit(t, s.dir, "worktree", "list", "--porcelain"), "worktree "); n != 1 {
		t.Errorf("worktrees = %d, want only the project", n)
	}
}

func TestIsolatedRunRefusesAnUnpushedCommitOnLocalMain(t *testing.T) {
	s := newSandbox(t)
	putFile(t, s.dir, "stray.txt", "never pushed\n")
	runGit(t, s.dir, "add", "-A")
	runGit(t, s.dir, "commit", "-q", "-m", "stray commit nobody checked")
	stray := runGit(t, s.dir, "rev-parse", "main")
	before := s.state(t)
	res, err := mergeFeature(t, s.runner(t, noTagSteps()))
	if err == nil || !strings.Contains(err.Error(), stray[:12]) || !strings.Contains(res.Run.Reason, stray[:12]) {
		t.Fatalf("err=%v reason=%q, want a refusal naming %s", err, res.Run.Reason, stray[:12])
	}
	s.assertUnchanged(t, before)
}

func TestIsolatedMovedRemoteRunsTheChecksAgain(t *testing.T) {
	s := newSandbox(t)
	other := filepath.Join(t.TempDir(), "other")
	runGit(t, filepath.Dir(other), "clone", "-q", s.bare, other)
	runGit(t, other, "config", "user.email", "t@example.invalid")
	runGit(t, other, "config", "user.name", "Test")
	runGit(t, other, "config", "commit.gpgsign", "false")
	putFile(t, other, "elsewhere.txt", "x\n")
	runGit(t, other, "add", "-A")
	runGit(t, other, "commit", "-q", "-m", "elsewhere")
	// The first check run moves the remote under the run.
	cmd := "echo ran >> '" + s.note("runs") + "'; if [ \"$(wc -l < '" + s.note("runs") + "')\" = 1 ]; then git -C '" + other + "' push -q origin main; fi"
	res, err := mergeFeature(t, s.runner(t, noTagSteps(check("unit tests", cmd))))
	if err != nil || !res.Pushed {
		t.Fatalf("run: %v %+v", err, res.Run.Steps)
	}
	if got := len(strings.Fields(s.read("runs"))); got != 2 {
		t.Errorf("the check ran %d time(s), want 2", got)
	}
	files := runGit(t, s.bare, "ls-tree", "--name-only", "refs/heads/main")
	if !strings.Contains(files, "elsewhere.txt") || !strings.Contains(files, "feature.txt") {
		t.Errorf("remote main lost work: %s", files)
	}
	if runGit(t, s.dir, "rev-parse", "main") != runGit(t, s.bare, "rev-parse", "refs/heads/main") {
		t.Error("local main is not at the pushed commit")
	}
}

func TestIsolatedCheckCannotChangeWhatIsPushed(t *testing.T) {
	s := newSandbox(t)
	before := s.state(t)
	cmd := "echo sneaked > sneaked.txt && git add sneaked.txt && git commit -q -m sneaked"
	res, err := mergeFeature(t, s.runner(t, noTagSteps(check("unit tests", cmd))))
	if err == nil || stepResults(res.Run)["unit tests"] != autonomous.MergeStepFailed {
		t.Fatalf("err=%v steps=%+v", err, res.Run.Steps)
	}
	s.assertUnchanged(t, before)
}

func TestIsolatedRunStopsWhenNoLongerAllowed(t *testing.T) {
	for name, set := range map[string]func(r *Runner, tip string){
		"the switch or an operator pause": func(r *Runner, _ string) {
			r.StillAllowed = func(context.Context) error { return errors.New("merging was switched off") }
		},
		"the Automaton is held during the checks": func(r *Runner, tip string) {
			r.Selection = &onceSelection{tip: tip}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			before := s.state(t)
			r := s.runner(t, noTagSteps())
			set(r, runGit(t, s.dir, "rev-parse", "feature"))
			res, err := mergeFeature(t, r)
			if err == nil || res.Pushed || stepResults(res.Run)["push-release"] != autonomous.MergeStepFailed {
				t.Fatalf("err=%v pushed=%v steps=%+v", err, res.Pushed, res.Run.Steps)
			}
			s.assertUnchanged(t, before)
		})
	}
}

// onceSelection selects the branch the first time and refuses afterwards, as
// the rule does when a hold is put on while the checks run.
type onceSelection struct {
	tip   string
	calls int
}

func (o *onceSelection) SelectMergeRun(context.Context, []string, string) (autonomous.MergeSelectionEntry, error) {
	o.calls++
	if o.calls > 1 {
		return autonomous.MergeSelectionEntry{}, errors.New("the Automaton is held")
	}
	return autonomous.MergeSelectionEntry{AutomatonID: "a1", Eligible: true, Tip: o.tip}, nil
}

func TestIsolatedRunWithoutATagStepPushesMainOnly(t *testing.T) {
	s := newSandbox(t)
	// The newest tag is far behind: most point releases carry no tag.
	runGit(t, s.dir, "tag", "v1.0.0")
	// A stale local tag with the next version's name must not travel.
	runGit(t, s.dir, "config", "push.followTags", "true")
	res, err := mergeFeature(t, s.runner(t, noTagSteps()))
	if err != nil || !res.Pushed {
		t.Fatalf("run: %v %+v", err, res.Run.Steps)
	}
	if tags := runGit(t, s.bare, "tag", "--list"); tags != "" {
		t.Errorf("remote tags = %q, want none", tags)
	}
	if tags := runGit(t, s.dir, "tag", "--list"); tags != "v1.0.0" {
		t.Errorf("local tags = %q, want only v1.0.0", tags)
	}
	if res.Run.Validator == nil || !res.Run.Validator.Pass {
		t.Errorf("validator: %+v", res.Run.Validator)
	}
}

func TestTagStepLimitedToReleaseSizes(t *testing.T) {
	for size, want := range map[string]string{"patch": "", "minor": "v1.3.0", "major": "v2.0.0"} {
		t.Run(size, func(t *testing.T) {
			s := newSandbox(t)
			t.Setenv("GIT_COMMITTER_NAME", "Test")
			t.Setenv("GIT_COMMITTER_EMAIL", "t@example.invalid")
			def := DefaultDefinition()
			for i := range def.Steps {
				if def.Steps[i].Action == ActionTagRelease {
					def.Steps[i].TagSizes = []string{"minor", "major"}
				}
			}
			if err := def.Validate(); err != nil {
				t.Fatal(err)
			}
			r := s.runner(t, def)
			r.ReleaseSize = size
			res, err := mergeFeature(t, r)
			if err != nil || !res.Pushed {
				t.Fatalf("run: %v %+v", err, res.Run.Steps)
			}
			if got := runGit(t, s.bare, "tag", "--list"); got != want {
				t.Errorf("remote tags = %q, want %q", got, want)
			}
		})
	}
}

func TestTagSizesAndConflictRulesAreValidated(t *testing.T) {
	for name, def := range map[string]Definition{
		"unknown size":       {Steps: []Step{{Name: "t", Kind: KindTag, Action: ActionTagRelease, TagSizes: []string{"huge"}}}},
		"sizes on a command": {Steps: []Step{{Name: "t", Kind: KindTest, Command: "true", TagSizes: []string{"minor"}}}},
		"unknown rule": {Steps: []Step{{Name: "t", Kind: KindTest, Command: "true"}},
			ConflictRules: []ConflictRule{{Path: "a", Rule: "theirs"}}},
		"escaping path": {Steps: []Step{{Name: "t", Kind: KindTest, Command: "true"}},
			ConflictRules: []ConflictRule{{Path: "../a", Rule: ConflictRuleAsk}}},
		"bad pattern": {Steps: []Step{{Name: "t", Kind: KindTest, Command: "true"}},
			ConflictRules: []ConflictRule{{Path: "[", Rule: ConflictRuleAsk}}},
	} {
		if def.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestReleaseSizeAndApprovalComeFromTheAutomaton(t *testing.T) {
	for name, c := range map[string]struct {
		facts   ReleaseFacts
		version string
		pushed  bool
	}{
		"feature is minor":   {ReleaseFacts{Size: "minor", Approval: "auto"}, "1.3.0", true},
		"major asks":         {ReleaseFacts{Size: "major", Approval: "ask"}, "1.2.3", false},
		"none releases none": {ReleaseFacts{Size: "none", Approval: "auto"}, "1.2.3", true},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSandbox(t)
			r := s.runner(t, noTagSteps())
			r.ReleaseSize, r.ReleaseApproval = "patch", "auto"
			r.Questions = newFakeReleaseQuestions()
			r.Selection = &fixedSelection{tip: runGit(t, s.dir, "rev-parse", "feature")}
			var asked string
			r.ReleaseFor = func(id string) (ReleaseFacts, bool) { asked = id; return c.facts, true }
			res, err := mergeFeature(t, r)
			if err != nil || res.Pushed != c.pushed {
				t.Fatalf("run: %v pushed=%v %+v", err, res.Pushed, res.Run.Steps)
			}
			if asked != "a1" || res.Run.AutomatonID != "a1" {
				t.Errorf("facts asked for %q, run records %q, want a1", asked, res.Run.AutomatonID)
			}
			got := runGit(t, s.bare, "show", "refs/heads/main:"+testVersionFiles[0])
			if !strings.Contains(got, `"`+c.version+`"`) {
				t.Errorf("remote version file: %s, want %s", got, c.version)
			}
			hasWork := strings.Contains(runGit(t, s.bare, "ls-tree", "--name-only", "refs/heads/main"), "feature.txt")
			if hasWork != c.pushed {
				t.Errorf("work on the remote = %v, want %v", hasWork, c.pushed)
			}
		})
	}
}

type fixedSelection struct{ tip string }

func (f *fixedSelection) SelectMergeRun(context.Context, []string, string) (autonomous.MergeSelectionEntry, error) {
	return autonomous.MergeSelectionEntry{AutomatonID: "a1", Eligible: true, Tip: f.tip}, nil
}

func TestIsolatedChecksSeeNoHomeTmuxOrCredentials(t *testing.T) {
	s := newSandbox(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOCACHE", "/shared/gocache")
	for _, k := range []string{"TMUX", "TMUX_TMPDIR", "DATAWATCH_TOKEN", "DATAWATCH_SESSION_ID", "GH_TOKEN", "GITHUB_TOKEN",
		"ANTHROPIC_API_KEY", "DW_BASE", "MY_SECRET", "DB_PASSWORD", "SSH_AUTH_SOCK"} {
		t.Setenv(k, "leak-"+k)
	}
	// newMergeRepo's repository-local identity still works without HOME.
	r := s.runner(t, noTagSteps(check("unit tests", "env > '"+s.note("env")+"'")))
	if res, err := mergeFeature(t, r); err != nil {
		t.Fatalf("run: %v %+v", err, res.Run.Steps)
	}
	env := map[string]string{}
	for _, l := range strings.Split(s.read("env"), "\n") {
		k, v, _ := strings.Cut(l, "=")
		env[k] = v
		if strings.HasPrefix(v, "leak-") {
			t.Errorf("a check saw %s", k)
		}
	}
	if env["HOME"] == "" || env["HOME"] == home {
		t.Errorf("a check saw HOME=%q", env["HOME"])
	}
	if !strings.Contains(filepath.Base(env["TMUX_TMPDIR"]), "dw-merge-tmux-") {
		t.Errorf("TMUX_TMPDIR=%q is not private to the run", env["TMUX_TMPDIR"])
	}
	if _, err := os.Stat(env["TMUX_TMPDIR"]); !os.IsNotExist(err) {
		t.Errorf("the run's tmux folder was left behind: %v", err)
	}
	if env["GOCACHE"] != "/shared/gocache" || env["GOPATH"] != filepath.Join(home, "go") {
		t.Errorf("Go caches not shared: GOCACHE=%q GOPATH=%q", env["GOCACHE"], env["GOPATH"])
	}
	if env["MERGE_PROJECT_DIR"] != s.dir || len(env["MERGE_BASE_COMMIT"]) != 40 || env["MERGE_RELEASE_VERSION"] != "1.2.4" || env["MERGE_SHADOW"] != "" {
		t.Errorf("merge facts: project=%q base=%q version=%q shadow=%q",
			env["MERGE_PROJECT_DIR"], env["MERGE_BASE_COMMIT"], env["MERGE_RELEASE_VERSION"], env["MERGE_SHADOW"])
	}
}

func TestShadowRunChangesAndSendsNothing(t *testing.T) {
	s := newSandbox(t)
	runGit(t, s.dir, "tag", "v1.2.3")
	// main moved after the fork, so the run rebases; the remote is ahead of
	// the local tracking ref, which a shadow run must not move either.
	putFile(t, s.dir, "later.txt", "later\n")
	runGit(t, s.dir, "add", "-A")
	runGit(t, s.dir, "commit", "-q", "-m", "later on main")
	runGit(t, s.dir, "push", "-q", "origin", "main")
	before := s.state(t)
	tracking := runGit(t, s.dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes")

	def := DefaultDefinition()
	def.Steps = append(def.Steps[:9:9], append([]Step{
		check("unit tests", "echo \"$MERGE_SHADOW\" > '"+s.note("shadow")+"'"),
		{Name: "outward", Kind: KindShip, Command: "touch '" + s.note("outward") + "'"},
	}, def.Steps[9:]...)...)
	ship := &fakeShip{}
	model := &countingModel{}
	qs := newFakeReleaseQuestions()
	notify := &fakeNotify{}
	r := s.runner(t, def)
	r.Shadow, r.Isolate = true, false
	r.Ship, r.Model, r.Questions, r.Notifier = ship, model, qs, notify
	r.ReleaseSize, r.ReleaseApproval = "patch", "ask"
	r.PostShip = PostShipSettings{Enabled: true, NotifyTemplate: "x",
		Hooks: []PostShipHook{{Name: "notify", Builtin: "notify", Enabled: true}}}
	res, err := mergeFeature(t, r)
	if err != nil || res.Pushed {
		t.Fatalf("shadow run: %v pushed=%v %+v", err, res.Pushed, res.Run.Steps)
	}
	s.assertUnchanged(t, before)
	if got := runGit(t, s.dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/remotes"); got != tracking {
		t.Errorf("tracking refs moved:\n%s\n%s", tracking, got)
	}
	if len(ship.calls) != 0 || model.calls != 0 || qs.raised != 0 {
		t.Errorf("outward calls: ship=%v model=%d questions=%d", ship.calls, model.calls, qs.raised)
	}
	if _, err := os.Stat(s.note("outward")); !os.IsNotExist(err) {
		t.Error("a ship command ran in a shadow run")
	}
	if s.read("shadow") != "1" {
		t.Errorf("the check did not run as a shadow check: %q", s.read("shadow"))
	}
	if !res.Run.Shadow || len(res.Run.PushPlan) != 2 {
		t.Fatalf("run shadow=%v plan=%+v, want main and the tag", res.Run.Shadow, res.Run.PushPlan)
	}
	main := res.Run.PushPlan[0]
	if main.Ref != "refs/heads/main" || main.Old != runGit(t, s.bare, "rev-parse", "refs/heads/main") || len(main.New) != 40 || len(main.Commits) != 3 {
		t.Errorf("push plan for main: %+v (want the feature, merge and bump commits)", main)
	}
	if res.Run.PushPlan[1].Ref != "refs/tags/v1.2.4" {
		t.Errorf("push plan tag: %+v", res.Run.PushPlan[1])
	}
}

type countingModel struct{ calls int }

func (c *countingModel) Propose(context.Context, string) (string, error) {
	c.calls++
	return "", errors.New("must not be called")
}

// A run killed while a check runs leaves local main and the remote as they
// were, and the next run clears what it left behind and works.
func TestIsolatedKilledRunLeavesMainAndTheNextRunWorks(t *testing.T) {
	if dir := os.Getenv("DW_KILL_REPO"); dir != "" {
		def := DefaultDefinition()
		def.Steps = append(def.Steps[:9:9], append([]Step{check("unit tests",
			"touch '"+os.Getenv("DW_KILL_MARK")+"'; sleep 30")}, def.Steps[9:]...)...)
		r := &Runner{Resolver: resolverWithRecord(t, def), VersionFiles: testVersionFiles, Dir: dir, Project: "p",
			Ship: ExecShipRunner{}, Isolate: true, ScratchRoot: os.Getenv("DW_KILL_SCRATCH")}
		_, _ = mergeFeature(t, r)
		return
	}
	s := newSandbox(t)
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.invalid")
	runGit(t, s.dir, "tag", "v1.2.3")
	mainBefore := runGit(t, s.dir, "rev-parse", "main")
	featureBefore := runGit(t, s.dir, "rev-parse", "feature")
	remoteBefore := runGit(t, s.bare, "for-each-ref")

	scratch, mark := t.TempDir(), s.note("mark")
	child := exec.Command(os.Args[0], "-test.run=^TestIsolatedKilledRunLeavesMainAndTheNextRunWorks$", "-test.timeout=60s")
	child.Env = append(os.Environ(), "DW_KILL_REPO="+s.dir, "DW_KILL_SCRATCH="+scratch, "DW_KILL_MARK="+mark, "TMPDIR="+t.TempDir())
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(25 * time.Second)
	for {
		if _, err := os.Stat(mark); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			t.Fatal("the run never reached its check")
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
	_ = child.Wait()

	if runGit(t, s.dir, "rev-parse", "main") != mainBefore || runGit(t, s.dir, "rev-parse", "feature") != featureBefore ||
		runGit(t, s.bare, "for-each-ref") != remoteBefore || runGit(t, s.dir, "status", "--porcelain") != "" {
		t.Fatal("the killed run changed main, the branch, the checkout or the remote")
	}
	if runGit(t, s.dir, "tag", "--list", "v1.2.4") == "" {
		t.Fatal("fixture: the killed run should have left its tag behind")
	}

	r := s.runner(t, DefaultDefinition())
	r.ScratchRoot = scratch
	res, err := mergeFeature(t, r)
	if err != nil || !res.Pushed {
		t.Fatalf("the next run: %v %+v", err, res.Run.Steps)
	}
	if left := runGit(t, s.dir, "for-each-ref", "--format=%(refname)", "refs/heads/datawatch-merge", "refs/datawatch-merge-tags"); left != "" {
		t.Errorf("left over: %s", left)
	}
	if n := strings.Count(runGit(t, s.dir, "worktree", "list", "--porcelain"), "worktree "); n != 1 {
		t.Errorf("worktrees = %d", n)
	}
	if entries, _ := os.ReadDir(scratch); len(entries) != 0 {
		t.Errorf("scratch folders left: %v", entries)
	}
}
