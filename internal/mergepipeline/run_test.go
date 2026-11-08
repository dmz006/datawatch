package mergepipeline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
)

// ---- helpers ----

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(scrubbedEnv(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func putFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newMergeRepo makes a temporary repository with a version-carrying layout,
// the project's own merge scripts, and one feature branch that adds a file.
func newMergeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	runGit(t, dir, "config", "user.email", "t@example.invalid")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	ver := "package x\n\nvar Version = \"1.2.3\"\n"
	putFile(t, dir, "cmd/datawatch/main.go", ver)
	putFile(t, dir, "internal/server/api.go", ver)
	putFile(t, dir, "CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n\n## v1.2.3 — older\n")
	putFile(t, dir, "shared.txt", "line one\n")
	for _, s := range []string{"check-merge-version.sh", "merge-branches-bump-version.sh", "set-release-version.sh"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "scripts", s))
		if err != nil {
			t.Fatal(err)
		}
		putFile(t, dir, "scripts/"+s, string(data))
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "base")
	runGit(t, dir, "checkout", "-q", "-b", "feature")
	putFile(t, dir, "feature.txt", "new\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "feature work")
	runGit(t, dir, "checkout", "-q", "main")
	return dir
}

// addConflictingBranch makes a branch and a main commit that edit the same
// line of shared.txt differently.
func addConflictingBranch(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "checkout", "-q", "-b", "clash")
	putFile(t, dir, "shared.txt", "branch side\n")
	runGit(t, dir, "commit", "-q", "-am", "clash side")
	runGit(t, dir, "checkout", "-q", "main")
	putFile(t, dir, "shared.txt", "main side\n")
	runGit(t, dir, "commit", "-q", "-am", "main side")
}

type fakeShip struct {
	mu    sync.Mutex
	calls []string
	fail  string // a call name that returns an error
}

func (f *fakeShip) note(c string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if f.fail == c {
		return errors.New(c + " refused")
	}
	return nil
}
func (f *fakeShip) Tag(_ context.Context, _, tag, _ string) error { return f.note("tag " + tag) }
func (f *fakeShip) Push(_ context.Context, _, b, tag string) error {
	return f.note("push " + b + " " + tag)
}
func (f *fakeShip) Deploy(_ context.Context, _ string) error { return f.note("deploy") }
func (f *fakeShip) Hook(_ context.Context, _ string, s Step) (string, error) {
	return "hook ran", f.note("hook " + s.Name)
}
func (f *fakeShip) FileIssue(_ context.Context, _, repo, title, _ string) error {
	return f.note("issue " + repo + " " + title)
}

type memRecorder struct {
	mu   sync.Mutex
	runs map[string]autonomous.MergePipelineRun
	n    int
}

func (m *memRecorder) RecordMergeRun(r autonomous.MergePipelineRun) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs == nil {
		m.runs = map[string]autonomous.MergePipelineRun{}
	}
	m.runs[r.ID] = r
	m.n++
}
func (m *memRecorder) only(t *testing.T) autonomous.MergePipelineRun {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(m.runs))
	}
	for _, r := range m.runs {
		return r
	}
	return autonomous.MergePipelineRun{}
}

func resolverWithRecord(t *testing.T, def Definition) *Resolver {
	t.Helper()
	rs, err := NewRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rs.Set("p", def); err != nil {
		t.Fatal(err)
	}
	return &Resolver{Records: rs, ProjectDir: func(string) (string, bool) { return "", false }}
}

func stepResults(r autonomous.MergePipelineRun) map[string]string {
	out := map[string]string{}
	for _, s := range r.Steps {
		out[s.Step] = s.Result
	}
	return out
}

// ---- the default pipeline equals today's merge helper ----

func TestDefaultPipelineMatchesMergeHelperScript(t *testing.T) {
	scriptRepo := newMergeRepo(t)
	cmd := exec.Command("bash", "scripts/merge-branches-bump-version.sh", "Add the feature", "feature")
	cmd.Dir = scriptRepo
	cmd.Env = append(scrubbedEnv(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}

	runRepo := newMergeRepo(t)
	ship := &fakeShip{}
	rec := &memRecorder{}
	r := &Runner{VersionFiles: testVersionFiles, Dir: runRepo, Project: "p", Ship: ship, Recorder: rec}
	res, err := r.Run(context.Background(), RunRequest{Title: "Add the feature", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Run.Result != autonomous.MergeRunOK || res.Run.Source != string(SourceDefault) {
		t.Fatalf("result %q source %q", res.Run.Result, res.Run.Source)
	}

	// the same content, the same history shape, the same worktree
	if a, b := runGit(t, scriptRepo, "rev-parse", "HEAD^{tree}"), runGit(t, runRepo, "rev-parse", "HEAD^{tree}"); a != b {
		t.Errorf("tree differs: script %s, runner %s", a, b)
	}
	if a, b := runGit(t, scriptRepo, "log", "--format=%s", "--first-parent"), runGit(t, runRepo, "log", "--format=%s", "--first-parent"); a != b {
		t.Errorf("history differs:\nscript:\n%s\nrunner:\n%s", a, b)
	}
	if a, b := runGit(t, scriptRepo, "status", "--porcelain"), runGit(t, runRepo, "status", "--porcelain"); a != b || a != "" {
		t.Errorf("status differs or dirty: %q vs %q", a, b)
	}
	if !strings.Contains(getFile(t, runRepo, "CHANGELOG.md"), "## v1.2.4 — Add the feature") {
		t.Error("changelog heading missing")
	}

	// the leaving-the-machine steps went to the fake only
	want := []string{"tag v1.2.4", "push main v1.2.4", "deploy"}
	if strings.Join(ship.calls, "|") != strings.Join(want, "|") {
		t.Errorf("ship calls %v, want %v", ship.calls, want)
	}
	// the check-internal-refs step is skipped: the temporary repo has no Makefile
	got := stepResults(res.Run)
	if got["check-internal-refs"] != autonomous.MergeStepSkipped || got["merge-branches"] != autonomous.MergeStepOK {
		t.Errorf("step results %v", got)
	}
	if len(res.Run.Steps) != len(DefaultDefinition().Steps) {
		t.Errorf("recorded %d steps, want %d", len(res.Run.Steps), len(DefaultDefinition().Steps))
	}
	for _, s := range res.Run.Steps {
		if s.End == nil || s.Source != "default" || s.Start.IsZero() {
			t.Errorf("step %s record incomplete: %+v", s.Step, s)
		}
	}
	if saved := rec.only(t); saved.End == nil || len(saved.Steps) != len(res.Run.Steps) {
		t.Error("recorder did not receive the finished run")
	}
}

func getFile(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDefaultPipelineRefusesWhatTheScriptRefuses(t *testing.T) {
	dir := newMergeRepo(t)
	runGit(t, dir, "checkout", "-q", "feature")
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}}
	if _, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}}); err == nil {
		t.Fatal("run on a non-main branch must fail")
	}
	runGit(t, dir, "checkout", "-q", "main")
	putFile(t, dir, "dirty.txt", "x")
	if _, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}}); err == nil {
		t.Fatal("run on a dirty tree must fail")
	}
	if _, err := r.Run(context.Background(), RunRequest{Title: " ", Branches: []string{"feature"}}); err == nil {
		t.Fatal("empty title must fail")
	}
	if _, err := r.Run(context.Background(), RunRequest{Title: "t"}); err == nil {
		t.Fatal("no branches must fail")
	}
}

// ---- conflicts ----

func TestConflictStopsTheRunAndIsRecorded(t *testing.T) {
	dir := newMergeRepo(t)
	addConflictingBranch(t, dir)
	ship := &fakeShip{}
	rec := &memRecorder{}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship, Recorder: rec}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"clash"}})
	if err == nil || res.Run.Result != autonomous.MergeRunFailed {
		t.Fatalf("want a failed run, got %q err %v", res.Run.Result, err)
	}
	if got := stepResults(res.Run); got["merge-branches"] != autonomous.MergeStepFailed || len(got) != 3 {
		t.Errorf("steps %v: later steps must not run", got)
	}
	if len(ship.calls) != 0 {
		t.Errorf("nothing may ship after a conflict: %v", ship.calls)
	}
	if st := runGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("the merge must be aborted, tree has %q", st)
	}
}

func TestConflictPolicyIsApplied(t *testing.T) {
	dir := newMergeRepo(t)
	runGit(t, dir, "checkout", "-q", "-b", "adds")
	putFile(t, dir, "shared.txt", "line one\nbranch added\n")
	runGit(t, dir, "commit", "-q", "-am", "branch adds")
	runGit(t, dir, "checkout", "-q", "main")
	putFile(t, dir, "shared.txt", "line one\nmain added\n")
	runGit(t, dir, "commit", "-q", "-am", "main adds")

	def := Definition{Steps: []Step{{Name: "merge", Kind: KindShip, Action: ActionMergeBranches}}}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: resolverWithRecord(t, def),
		ConflictSettings: func() ConflictSettings { return ConflictSettings{Policy: PolicyMechanical} }}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"adds"}})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("an additive conflict under the mechanical policy should merge: %v %q", err, res.Run.Result)
	}
	body := getFile(t, dir, "shared.txt")
	if !strings.Contains(body, "branch added") || !strings.Contains(body, "main added") {
		t.Errorf("both sides should be kept, got %q", body)
	}
}

// ---- failure handling, kinds and sources ----

func TestFailingStepStopsUnlessContinue(t *testing.T) {
	dir := newMergeRepo(t)
	def := Definition{Steps: []Step{
		{Name: "bad-continue", Kind: KindCustom, Command: "echo oops; exit 3", OnFailure: OnFailureContinue},
		{Name: "after", Kind: KindCustom, Command: "echo fine"},
		{Name: "bad-stop", Kind: KindLint, Command: "exit 1"},
		{Name: "never", Kind: KindCustom, Command: "echo no"},
	}}
	rec := &memRecorder{}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: resolverWithRecord(t, def), Recorder: rec}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err == nil || res.Run.Result != autonomous.MergeRunFailed {
		t.Fatalf("want failed run, got %q err %v", res.Run.Result, err)
	}
	want := map[string]string{"bad-continue": "failed", "after": "ok", "bad-stop": "failed"}
	got := stepResults(res.Run)
	if len(got) != 3 {
		t.Fatalf("steps %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("step %s: %q, want %q", k, got[k], v)
		}
	}
	if res.Run.Source != string(SourceRecord) {
		t.Errorf("source %q", res.Run.Source)
	}
	if res.Run.Steps[0].OnFailure != "continue" {
		t.Errorf("on_failure not recorded: %+v", res.Run.Steps[0])
	}

	def2 := Definition{Steps: []Step{
		{Name: "soft", Kind: KindCustom, Command: "exit 2", OnFailure: OnFailureContinue},
		{Name: "next", Kind: KindCustom, Command: "true"},
	}}
	r2 := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: resolverWithRecord(t, def2)}
	res2, err := r2.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res2.Run.Result != autonomous.MergeRunCompletedWithFailures {
		t.Fatalf("want completed_with_failures, got %q err %v", res2.Run.Result, err)
	}
}

func TestShipKindsOnlyRunWhenListedAndOnlyThroughTheRunner(t *testing.T) {
	dir := newMergeRepo(t)
	// no tag, deploy or hook step listed: the fake sees nothing
	ship := &fakeShip{}
	def := Definition{Steps: []Step{{Name: "merge", Kind: KindShip, Action: ActionMergeBranches}}}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: resolverWithRecord(t, def), Ship: ship}
	if _, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}}); err != nil {
		t.Fatal(err)
	}
	if len(ship.calls) != 0 {
		t.Fatalf("unlisted steps ran: %v", ship.calls)
	}

	// listed hook, deploy and tag command steps go to the fake, not to sh
	marker := filepath.Join(t.TempDir(), "ran")
	dir2 := newMergeRepo(t)
	def2 := Definition{Steps: []Step{
		{Name: "tag-cmd", Kind: KindTag, Command: "touch " + marker},
		{Name: "deploy-cmd", Kind: KindDeploy, Command: "touch " + marker},
		{Name: "notify", Kind: KindPostShipHook, Command: "touch " + marker},
	}}
	ship2 := &fakeShip{}
	r2 := &Runner{VersionFiles: testVersionFiles, Dir: dir2, Project: "p", Resolver: resolverWithRecord(t, def2), Ship: ship2}
	if _, err := r2.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(ship2.calls, "|") != "hook tag-cmd|hook deploy-cmd|hook notify" {
		t.Errorf("calls %v", ship2.calls)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a leaving-the-machine step ran a command directly")
	}

	// with no runner wired such a step fails instead of running
	r3 := &Runner{VersionFiles: testVersionFiles, Dir: newMergeRepo(t), Project: "p", Resolver: resolverWithRecord(t, def2)}
	res3, err := r3.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err == nil || res3.Run.Steps[0].Result != autonomous.MergeStepFailed {
		t.Fatalf("want failure without a ship runner, got %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the command ran without a runner")
	}
}

func TestShipFailureOnPushStopsBeforeDeploy(t *testing.T) {
	dir := newMergeRepo(t)
	ship := &fakeShip{fail: "push main v1.2.4"}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err == nil || stepResults(res.Run)["push-release"] != autonomous.MergeStepFailed {
		t.Fatalf("want push failure, got %v %v", err, stepResults(res.Run))
	}
	if _, ran := stepResults(res.Run)["deploy-latest"]; ran {
		t.Error("deploy must not run after a failed push")
	}
}

// ---- post-ship hooks ----

// postShipPipeline is a minimal pipeline whose push-release step succeeds, so
// the post-ship hooks (when enabled) are due.
func postShipPipeline(t *testing.T) *Resolver {
	t.Helper()
	def := Definition{Steps: []Step{
		{Name: "merge", Kind: KindShip, Action: ActionMergeBranches},
		{Name: "push-release", Kind: KindShip, Action: ActionPushRelease},
	}}
	return resolverWithRecord(t, def)
}

func TestPostShipHooksRunAfterTheShipInConfiguredOrder(t *testing.T) {
	dir := newMergeRepo(t)
	ship := &fakeShip{}
	r := &Runner{
		VersionFiles: testVersionFiles, Dir: dir, Project: "p",
		Resolver: postShipPipeline(t), Ship: ship,
		PostShip: PostShipSettings{
			Enabled:             true,
			CompanionRepository: "dmz006/companion",
			IssueTitleTemplate:  "Shipped {project} {version}",
			Hooks: []PostShipHook{
				{Name: "issue-first", Builtin: "parity_issue", Enabled: true},
				{Name: "cmd-second", Command: "echo second", Enabled: true},
				{Name: "off", Builtin: "parity_issue", Enabled: false}, // records nothing
			},
		}}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run %q err %v", res.Run.Result, err)
	}
	var names []string
	for _, s := range res.Run.Steps {
		names = append(names, s.Step)
	}
	if strings.Join(names, "|") != "merge|push-release|issue-first|cmd-second" {
		t.Fatalf("steps %v: hooks must run after the ship, in configured order; a disabled hook records nothing", names)
	}
	for _, s := range res.Run.Steps[2:] {
		if s.Kind != "post_ship_hook" || s.Source != "post_ship" || s.OnFailure != string(OnFailureContinue) || s.Result != autonomous.MergeStepOK {
			t.Errorf("hook step record %+v", s)
		}
	}
	// the built-in reached the fake ShipRunner, the command reached Ship.Hook,
	// nothing ran a shell directly
	want := "push main v1.2.3|issue dmz006/companion Shipped p 1.2.3|hook cmd-second"
	if strings.Join(ship.calls, "|") != want {
		t.Errorf("ship calls %v, want %v", ship.calls, want)
	}
}

func TestFailingPostShipHookKeepsTheRunResultAndLetsLaterHooksRun(t *testing.T) {
	hooks := []PostShipHook{
		{Name: "leaks", Command: "echo secret=abc", Enabled: true},
		{Name: "still-runs", Builtin: "parity_issue", Enabled: true},
	}
	postShip := PostShipSettings{
		Enabled: true, Hooks: hooks,
		CompanionRepository: "dmz006/companion",
		IssueTitleTemplate:  "Shipped {project} {version}",
	}

	// a run the pipeline gave ok: the failing hook records a failed step
	// and the result stays ok
	dir := newMergeRepo(t)
	ship := &fakeShip{fail: "hook leaks"}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: postShipPipeline(t), Ship: ship, PostShip: postShip}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("a failing hook must not change the run result: %q err %v", res.Run.Result, err)
	}
	got := stepResults(res.Run)
	if got["leaks"] != autonomous.MergeStepFailed || got["still-runs"] != autonomous.MergeStepOK {
		t.Fatalf("steps %v: the failure stays on the hook's step and later hooks run", got)
	}
	failed := res.Run.Steps[2]
	if failed.Result != autonomous.MergeStepFailed || failed.Detail == "" || failed.End == nil {
		t.Errorf("failed hook step %+v", failed)
	}
	if strings.Contains(failed.Detail, "abc") || strings.Contains(failed.Detail, "secret=") {
		t.Errorf("detail leaks its input: %q", failed.Detail)
	}

	// a run the pipeline gave completed_with_failures keeps that value
	def := Definition{Steps: []Step{
		{Name: "soft", Kind: KindCustom, Command: "exit 1", OnFailure: OnFailureContinue},
		{Name: "merge", Kind: KindShip, Action: ActionMergeBranches},
		{Name: "push-release", Kind: KindShip, Action: ActionPushRelease},
	}}
	dir2 := newMergeRepo(t)
	ship2 := &fakeShip{fail: "hook leaks"}
	r2 := &Runner{VersionFiles: testVersionFiles, Dir: dir2, Project: "p", Resolver: resolverWithRecord(t, def), Ship: ship2, PostShip: postShip}
	res2, err := r2.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res2.Run.Result != autonomous.MergeRunCompletedWithFailures {
		t.Fatalf("want completed_with_failures with a failing hook, got %q err %v", res2.Run.Result, err)
	}
	if got2 := stepResults(res2.Run); got2["leaks"] != autonomous.MergeStepFailed || got2["still-runs"] != autonomous.MergeStepOK {
		t.Errorf("steps %v", got2)
	}
}

func TestPostShipHooksSkipWhenPushReleaseFailsOrTheBlockIsOff(t *testing.T) {
	hook := PostShipHook{Name: "should-not-run", Command: "echo x", Enabled: true}

	// push-release failed: the hooks are not due
	dir := newMergeRepo(t)
	ship := &fakeShip{fail: "push main v1.2.4"}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship,
		PostShip: PostShipSettings{Enabled: true, Hooks: []PostShipHook{hook}}}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err == nil || stepResults(res.Run)["push-release"] != autonomous.MergeStepFailed {
		t.Fatalf("want a failed push, got %q err %v", res.Run.Result, err)
	}
	if _, ran := stepResults(res.Run)["should-not-run"]; ran {
		t.Error("a hook step was recorded although push-release failed")
	}
	for _, c := range ship.calls {
		if strings.HasPrefix(c, "hook ") {
			t.Errorf("a hook ran although push-release failed: %v", ship.calls)
		}
	}

	// block not enabled: enabled hooks stay off
	dir2 := newMergeRepo(t)
	ship2 := &fakeShip{}
	r2 := &Runner{VersionFiles: testVersionFiles, Dir: dir2, Project: "p", Resolver: postShipPipeline(t), Ship: ship2,
		PostShip: PostShipSettings{Enabled: false, Hooks: []PostShipHook{hook}}}
	res2, err := r2.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res2.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run %q err %v", res2.Run.Result, err)
	}
	if _, ran := stepResults(res2.Run)["should-not-run"]; ran {
		t.Error("a hook step was recorded although the block is not enabled")
	}
	for _, c := range ship2.calls {
		if strings.HasPrefix(c, "hook ") {
			t.Errorf("a hook ran although the block is not enabled: %v", ship2.calls)
		}
	}
}

func TestBuiltInPostShipHooksReachTheirWiring(t *testing.T) {
	dir := newMergeRepo(t)
	putFile(t, dir, "registry.md", "| project | status |\n| p | open |\n| other | open |\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "registry")
	ship := &fakeShip{}
	nf := &fakeNotify{}

	r := &Runner{
		VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: postShipPipeline(t),
		Ship: ship, Notifier: nf,
		PostShip: PostShipSettings{
			Enabled:            true,
			RegistryFile:       "registry.md",
			RegistryRowPattern: "| p |",
			NotifyTemplate:     "shipped {project} {version}",
			Hooks: []PostShipHook{
				{Name: "row", Builtin: "registry_row", Enabled: true},
				{Name: "taghook", Builtin: "tag", Enabled: true},
				{Name: "say", Builtin: "notify", Enabled: true},
				{Name: "warp", Builtin: "warp_drive", Enabled: true},
			},
		}}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run %q err %v", res.Run.Result, err)
	}
	got := stepResults(res.Run)
	for name, want := range map[string]string{
		"row": autonomous.MergeStepOK, "taghook": autonomous.MergeStepOK, "say": autonomous.MergeStepOK,
		"warp": autonomous.MergeStepFailed, // unknown built-in: on its step, result unchanged
	} {
		if got[name] != want {
			t.Errorf("steps %v", got)
		}
	}
	if !strings.Contains(getFile(t, dir, "registry.md"), "| p | Shipped 1.2.3 |") {
		t.Errorf("registry not updated: %q", getFile(t, dir, "registry.md"))
	}
	if strings.Join(ship.calls, "|") != "push main v1.2.3|tag v1.2.3" {
		t.Errorf("ship calls %v", ship.calls)
	}
	if len(nf.texts) != 1 || nf.texts[0] != "shipped p 1.2.3" {
		t.Errorf("notifications %v", nf.texts)
	}
}

type fakeNotify struct {
	texts []string
}

func (f *fakeNotify) Notify(_ context.Context, text string) error {
	f.texts = append(f.texts, text)
	return nil
}

func TestFileSourceIsInEffectAndRecorded(t *testing.T) {
	dir := newMergeRepo(t)
	putFile(t, dir, DefaultFileName, "steps:\n  - name: say\n    kind: custom\n    command: echo hi\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "pipeline file")
	rs, _ := NewRecordStore(t.TempDir())
	_, _ = rs.Set("p", Definition{Steps: []Step{{Name: "rec", Kind: KindCustom, Command: "true"}}})
	res := &Resolver{Records: rs, ProjectDir: func(string) (string, bool) { return dir, true }}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: res}
	out, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Run.Source != "file" || len(out.Run.Steps) != 1 || out.Run.Steps[0].Source != "file" || out.Run.Steps[0].Step != "say" {
		t.Errorf("unexpected run %+v", out.Run)
	}
}

func TestNoPipelineCouldBeChosenIsRecordedAsFailed(t *testing.T) {
	dir := newMergeRepo(t)
	putFile(t, dir, DefaultFileName, "steps: [not valid")
	set := DefaultSettings()
	set.FallbackOnBadFile = FallbackNone
	res := &Resolver{ProjectDir: func(string) (string, bool) { return dir, true }, Settings: func(string) Settings { return set }}
	rec := &memRecorder{}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: res, Recorder: rec}
	if _, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}}); err == nil {
		t.Fatal("expected an error")
	}
	if got := rec.only(t); got.Result != autonomous.MergeRunFailed || got.End == nil {
		t.Errorf("run not recorded as failed: %+v", got)
	}
}

// ---- secrets ----

func TestSecretsAreRemovedFromRecords(t *testing.T) {
	dir := newMergeRepo(t)
	def := Definition{Steps: []Step{{Name: "leaky", Kind: KindCustom,
		Command: "echo 'token=abc123supersecret'; echo 'https://user:hunter2@example.invalid/x'; echo ghp_abcdefghijklmnopqrstuvwxyz0123456789; exit 1"}}}
	rec := &memRecorder{}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Resolver: resolverWithRecord(t, def), Recorder: rec}
	res, _ := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	d := res.Run.Steps[0].Detail
	for _, bad := range []string{"abc123supersecret", "hunter2", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"} {
		if strings.Contains(d, bad) {
			t.Errorf("detail leaks %q: %s", bad, d)
		}
	}
	if !strings.Contains(d, "[redacted]") {
		t.Errorf("expected redaction marker in %q", d)
	}
}

func TestRedactKeepsOrdinaryText(t *testing.T) {
	in := "merge of \"feature\" stopped: 3 files conflicted"
	if got := redact(in); got != in {
		t.Errorf("redact changed ordinary text: %q", got)
	}
}

// ---- the record reaches the merge status ----

func TestRunRecordsPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	rec := autonomous.NewMergeRunRecords(dir)
	run := autonomous.MergePipelineRun{ID: "a", Project: "p", Result: autonomous.MergeRunRunning,
		Steps: []autonomous.MergeStepRun{{Step: "s", Result: autonomous.MergeStepRunning}}}
	rec.RecordMergeRun(run)
	run.Result = autonomous.MergeRunOK
	run.Steps[0].Result = autonomous.MergeStepOK
	rec.RecordMergeRun(run)
	rec.RecordMergeRun(autonomous.MergePipelineRun{ID: "b"})

	again := autonomous.NewMergeRunRecords(dir).List(0)
	if len(again) != 2 || again[0].ID != "b" || again[1].Result != autonomous.MergeRunOK || again[1].Steps[0].Result != autonomous.MergeStepOK {
		t.Fatalf("reloaded %+v", again)
	}
}

// testVersionFiles are the version files of the temporary repository the
// tests build; production reads them from the operator's setting.
var testVersionFiles = []string{"cmd/datawatch/main.go", "internal/server/api.go"}

// With no version files named, the release-version step fails and says why
// rather than touching a guessed path.
func TestRunWithoutVersionFilesFailsTheBumpStep(t *testing.T) {
	dir := newMergeRepo(t)
	r := &Runner{Dir: dir, Project: "p", Ship: &fakeShip{}}
	_, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err == nil || !strings.Contains(err.Error(), "version_files") {
		t.Fatalf("want a version_files error, got %v", err)
	}
}

// ---- repository merge lock ----

// gateShip blocks the tag step until proceed is closed, so a test can start a
// second run while the first one holds the repository lock.
type gateShip struct {
	fakeShip
	entered chan struct{}
	proceed chan struct{}
}

func (g *gateShip) Tag(ctx context.Context, dir, tag, msg string) error {
	g.entered <- struct{}{}
	<-g.proceed
	return g.fakeShip.Tag(ctx, dir, tag, msg)
}

// customPipeline is a one-step pipeline that touches no git state, so a run
// that is allowed to start can finish without merging anything.
func customPipeline() Definition {
	return Definition{Steps: []Step{{Name: "noop", Kind: KindCustom, Command: "echo ok"}}}
}

func TestMergeLockRefusesSecondRunAndKeepsFirst(t *testing.T) {
	dir := newMergeRepo(t)
	locks, err := autonomous.NewMergeLocks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gate := &gateShip{fakeShip: fakeShip{}, entered: make(chan struct{}), proceed: make(chan struct{})}
	first := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: gate, Locks: locks}

	type outcome struct {
		res RunResult
		err error
	}
	firstDone := make(chan outcome, 1)
	go func() {
		res, err := first.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}, AutomatonID: "a1"})
		firstDone <- outcome{res, err}
	}()
	// Wait until the first run is inside its tag step, holding the lock. If it
	// ends first, the select reports that instead of blocking.
	select {
	case <-gate.entered:
	case o := <-firstDone:
		t.Fatalf("first run ended before the tag step: %v %q", o.err, o.res.Run.Result)
	}
	held, ok := locks.Status(dir)
	if !ok {
		t.Fatal("the first run should hold the lock while it tags")
	}

	rec := &memRecorder{}
	second := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: locks, Recorder: rec}
	res, err := second.Run(context.Background(), RunRequest{Title: "t2", Branches: []string{"feature"}, AutomatonID: "a2"})
	if err == nil || res.Run.Result != autonomous.MergeRunFailed {
		t.Fatalf("second run should be refused, got %q err %v", res.Run.Result, err)
	}
	if !strings.Contains(res.Run.Reason, held.RunID) || !strings.Contains(res.Run.Reason, "a1") {
		t.Errorf("refusal reason %q should name holder run %s and Automaton a1", res.Run.Reason, held.RunID)
	}
	if len(res.Run.Steps) != 0 {
		t.Errorf("a refused run must not run any step, got %d", len(res.Run.Steps))
	}
	if got := rec.only(t); got.Result != autonomous.MergeRunFailed || got.Reason != res.Run.Reason {
		t.Errorf("the refusal must be recorded, got result %q reason %q", got.Result, got.Reason)
	}
	if still, ok := locks.Status(dir); !ok || still.RunID != held.RunID {
		t.Errorf("the refusal must not change the holder: %+v ok=%v", still, ok)
	}

	close(gate.proceed)
	o := <-firstDone
	if o.err != nil || o.res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("the first run should be unaffected: %q err %v", o.res.Run.Result, o.err)
	}
	if o.res.Run.ID != held.RunID {
		t.Errorf("the lock holder %s should be the first run %s", held.RunID, o.res.Run.ID)
	}
}

func TestMergeLockFreedAfterRunEnds(t *testing.T) {
	dir := newMergeRepo(t)
	locks, err := autonomous.NewMergeLocks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: locks}
	if res, err := first.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}, AutomatonID: "a1"}); err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("first run: %q err %v", res.Run.Result, err)
	}
	if _, held := locks.Status(dir); held {
		t.Fatal("a finished run must release the lock")
	}

	def := customPipeline()
	second := &Runner{Dir: dir, Project: "p", Resolver: resolverWithRecord(t, def), Locks: locks}
	res, err := second.Run(context.Background(), RunRequest{Title: "t2", Branches: []string{"feature"}, AutomatonID: "a2"})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("second run should proceed once the lock is free: %q err %v", res.Run.Result, err)
	}
	if _, held := locks.Status(dir); held {
		t.Error("the second run must release the lock too")
	}
}

func TestMergeLockReleasedWhenRunFails(t *testing.T) {
	dir := newMergeRepo(t)
	locks, err := autonomous.NewMergeLocks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// No version files: the release-version step fails, so the run ends failed.
	r := &Runner{Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: locks}
	res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}, AutomatonID: "a1"})
	if err == nil || res.Run.Result != autonomous.MergeRunFailed {
		t.Fatalf("want a failed run, got %q err %v", res.Run.Result, err)
	}
	if _, held := locks.Status(dir); held {
		t.Error("a failed run must release the lock")
	}
}

// A lock left held by a daemon that stopped is taken back by the same
// Automaton and branches after a restart, and refused for a different one.
func TestMergeLockAfterRestart(t *testing.T) {
	dir := newMergeRepo(t)
	lockDir := t.TempDir()
	before, err := autonomous.NewMergeLocks(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := before.Acquire(dir, autonomous.MergeLockHolder{RunID: "run-before", AutomatonID: "a1", Branches: []string{"feature"}}); err != nil {
		t.Fatal(err)
	}
	// The daemon restarts: a new store opened from the same directory sees
	// the lock the old daemon left.
	after, err := autonomous.NewMergeLocks(lockDir)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("different Automaton is refused", func(t *testing.T) {
		rec := &memRecorder{}
		r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: after, Recorder: rec}
		res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}, AutomatonID: "a2"})
		if err == nil || res.Run.Result != autonomous.MergeRunFailed {
			t.Fatalf("a different Automaton should be refused, got %q err %v", res.Run.Result, err)
		}
		if !strings.Contains(res.Run.Reason, "run-before") || !strings.Contains(res.Run.Reason, "a1") {
			t.Errorf("refusal reason %q should name run-before and Automaton a1", res.Run.Reason)
		}
		if st, held := after.Status(dir); !held || st.RunID != "run-before" {
			t.Errorf("the old holder must keep the lock, got %+v held=%v", st, held)
		}
	})

	t.Run("same Automaton and branches take it back", func(t *testing.T) {
		r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: after}
		res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}, AutomatonID: "a1"})
		if err != nil || res.Run.Result != autonomous.MergeRunOK {
			t.Fatalf("the same Automaton should resume its run: %q err %v", res.Run.Result, err)
		}
		if _, held := after.Status(dir); held {
			t.Error("the resumed run must release the lock when it ends")
		}
	})
}
