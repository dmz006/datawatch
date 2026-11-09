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
	"github.com/dmz006/datawatch/internal/config"
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
	putFile(t, dir, "README.md", "# Project\n\n**Current release: v1.2.3 (2026-01-01).** Older notes.\n")
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
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: runRepo, Project: "p", Ship: ship, Recorder: rec}
	res, err := r.Run(context.Background(), RunRequest{Title: "Add the feature", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// The pipeline is the built-in one minus the validator, which the script
	// does not have, so it is handed over as a record; the steps it shares
	// with the script must leave the same repository behind.
	if res.Run.Result != autonomous.MergeRunOK || res.Run.Source != string(SourceRecord) {
		t.Fatalf("result %q source %q", res.Run.Result, res.Run.Source)
	}

	// the same content, the same history shape, the same worktree. The one
	// deliberate difference is the README: the pipeline's set-readme-release-line
	// step rewrites its Current release: line, which the script does not touch.
	if a, b := treeWithoutReadme(t, scriptRepo), treeWithoutReadme(t, runRepo); a != b {
		t.Errorf("tree differs outside README.md:\nscript:\n%s\nrunner:\n%s", a, b)
	}
	if got := readRepoText(t, runRepo, "README.md"); !strings.Contains(got, "**Current release: v1.2.4 (") {
		t.Errorf("runner README does not carry the release line:\n%s", got)
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
	if want := len(definitionWithoutReleaseValidator().Steps); len(res.Run.Steps) != want {
		t.Errorf("recorded %d steps, want %d", len(res.Run.Steps), want)
	}
	for _, s := range res.Run.Steps {
		if s.End == nil || s.Source != string(SourceRecord) || s.Start.IsZero() {
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
	// The rebase step runs first in the default pipeline and would settle this
	// branch before the merge saw it, so this test drops it to reach the merge.
	def := Definition{Steps: DefaultDefinition().Steps[1:]}
	r := &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship, Recorder: rec, Resolver: resolverWithRecord(t, def)}
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
	r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: ship}
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
		Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()),
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

// ---- release gate and release approval, run end to end with fakes ----
//
// Each test runs a real pipeline (one check, then tag and push) through a fake
// ShipRunner and a fake question store. The mode for each size comes from
// config.MergePipelineConfig.EffectiveReleaseApproval, as the daemon wires it.

// releaseRunFor builds a run for project and size whose approval mode is the
// one cfg resolves for them, with the fake question store wired both ways.
func releaseRunFor(t *testing.T, cfg config.MergePipelineConfig, project, size string, qs *fakeReleaseQuestions, ship *fakeShip) *Runner {
	t.Helper()
	r := newGateRun(t, newMergeRepo(t), gatePipeline(false, false), ship)
	r.Resolver = resolverForProjects(t, gatePipeline(false, false), project)
	r.Project = project
	r.ReleaseSize = size
	r.ReleaseApproval = cfg.EffectiveReleaseApproval(project, size)
	r.Questions = qs
	r.ReleaseAnswers = qs
	return r
}

// resolverForProjects is resolverWithRecord for any number of project names,
// so each project runs the same gate pipeline rather than the built-in default.
func resolverForProjects(t *testing.T, def Definition, projects ...string) *Resolver {
	t.Helper()
	rs, err := NewRecordStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range projects {
		if _, err := rs.Set(p, def); err != nil {
			t.Fatal(err)
		}
	}
	return &Resolver{Records: rs, ProjectDir: func(string) (string, bool) { return "", false }}
}

func releaseRunOnce(t *testing.T, r *Runner) autonomous.MergePipelineRun {
	t.Helper()
	res, err := r.Run(context.Background(), RunRequest{Title: "release", Branches: []string{"feature"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return res.Run
}

// auto ships when the checks pass, with no question.
func TestReleaseGateAutoShipsWhenChecksPass(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	run := releaseRunOnce(t, releaseRunFor(t, config.MergePipelineConfig{}, "p", config.ReleaseSizePatch, qs, ship))
	if stepOutcome(run, "tests") != autonomous.MergeStepOK {
		t.Fatalf("tests = %q; want ok", stepOutcome(run, "tests"))
	}
	if stepOutcome(run, "tag") != autonomous.MergeStepOK || stepOutcome(run, "push") != autonomous.MergeStepOK {
		t.Fatalf("tag/push = %q/%q; want both ok", stepOutcome(run, "tag"), stepOutcome(run, "push"))
	}
	if calls := shipCalls(ship); len(calls) != 2 {
		t.Fatalf("ship calls = %q; want tag and push", calls)
	}
	if qs.raisedCount() != 0 {
		t.Fatalf("auto must not raise a question; raised %d", qs.raisedCount())
	}
}

// auto does not ship when a check fails: the leaving steps are held and
// nothing reaches the ShipRunner.
func TestReleaseGateAutoDoesNotShipWhenACheckFails(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	r := releaseRunFor(t, config.MergePipelineConfig{}, "p", config.ReleaseSizePatch, qs, ship)
	r.Resolver = resolverWithRecord(t, gatePipeline(true, true))
	run := releaseRunOnce(t, r)
	if stepOutcome(run, "tests") != autonomous.MergeStepFailed {
		t.Fatalf("tests = %q; want failed", stepOutcome(run, "tests"))
	}
	for _, step := range []string{"tag", "push"} {
		if got := stepOutcome(run, step); got != autonomous.MergeStepSkipped {
			t.Fatalf("%s = %q; want skipped", step, got)
		}
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("ship calls = %q; want none", calls)
	}
	if qs.raisedCount() != 0 {
		t.Fatalf("a failed check must not raise a question; raised %d", qs.raisedCount())
	}
}

// manual runs every preparing step, ships nothing, and the record says a
// person ships the release.
func TestReleaseApprovalManualPreparesAndShipsNothing(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	cfg := config.MergePipelineConfig{ReleaseApproval: config.ReleaseApprovalModes{Minor: config.ReleaseApprovalManual}}
	run := releaseRunOnce(t, releaseRunFor(t, cfg, "p", config.ReleaseSizeMinor, qs, ship))
	if stepOutcome(run, "tests") != autonomous.MergeStepOK {
		t.Fatalf("preparing step tests = %q; want ok", stepOutcome(run, "tests"))
	}
	for _, step := range []string{"tag", "push"} {
		if got := stepOutcome(run, step); got != autonomous.MergeStepSkipped {
			t.Fatalf("%s = %q; want skipped", step, got)
		}
		if d := stepDetail(run, step); !strings.Contains(d, "prepared; a person ships this release") {
			t.Fatalf("%s detail = %q; want the person-ships wording", step, d)
		}
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("ship calls = %q; want none", calls)
	}
	if qs.raisedCount() != 0 {
		t.Fatalf("manual must not ask; raised %d", qs.raisedCount())
	}
}

// ask raises exactly one question that carries the merge plan, and ships
// nothing while the question is unanswered.
func TestReleaseApprovalAskRaisesOneQuestionWithThePlan(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	r := releaseRunFor(t, config.MergePipelineConfig{}, "p", config.ReleaseSizeMajor, qs, ship)
	run := releaseRunOnce(t, r)
	if qs.raisedCount() != 1 {
		t.Fatalf("raised %d questions; want exactly one", qs.raisedCount())
	}
	_, q := qs.open(t)
	for _, want := range []string{"Release plan for p", "Size: major", "- tests: passed"} {
		if !strings.Contains(q.Text, want) {
			t.Fatalf("question text = %q; want it to contain %q", q.Text, want)
		}
	}
	if q.AskedBy != ReleaseQuestionAsker {
		t.Fatalf("asked_by = %q; want %q", q.AskedBy, ReleaseQuestionAsker)
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("unanswered ask shipped: %q", calls)
	}
	for _, step := range []string{"tag", "push"} {
		if got := stepOutcome(run, step); got != autonomous.MergeStepSkipped {
			t.Fatalf("%s = %q; want skipped while unanswered", step, got)
		}
	}
}

// A second run while the question is open finds it: no second question, and
// still no ship.
func TestReleaseApprovalAskSecondRunFindsOpenQuestion(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	r := releaseRunFor(t, config.MergePipelineConfig{}, "p", config.ReleaseSizeMajor, qs, ship)
	releaseRunOnce(t, r)
	releaseRunOnce(t, r)
	if qs.raisedCount() != 1 {
		t.Fatalf("raised %d questions over two runs; want exactly one", qs.raisedCount())
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("ship calls = %q; want none", calls)
	}
}

// ask ships after the operator says yes. The fake finds the answered question
// on the re-run, which is what the gate needs from a store.
//
// Known gap, not covered here: the real autonomous question store dedupes only
// open questions (questions_store.go, raise: have.State == QuestionOpen). A
// re-run after a yes therefore raises a new question and holds instead of
// shipping. Fixing that is a store change, outside these tests.
func TestReleaseApprovalAskShipsAfterYes(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestionsDedupingAnswered()
	r := releaseRunFor(t, config.MergePipelineConfig{}, "p", config.ReleaseSizeMajor, qs, ship)
	releaseRunOnce(t, r)
	qid := qs.answer(t, releaseOptionShip)
	run := releaseRunOnce(t, r)
	if stepOutcome(run, "tag") != autonomous.MergeStepOK || stepOutcome(run, "push") != autonomous.MergeStepOK {
		t.Fatalf("tag/push = %q/%q; want both ok after yes", stepOutcome(run, "tag"), stepOutcome(run, "push"))
	}
	if !strings.HasPrefix(stepDetail(run, "tag"), "approved by the operator (operator question "+qid+")") {
		t.Fatalf("tag detail = %q; want the approval note with question %s", stepDetail(run, "tag"), qid)
	}
	if calls := shipCalls(ship); len(calls) != 2 {
		t.Fatalf("ship calls = %q; want tag and push", calls)
	}
}

// ask does not ship after the operator says no; the record names the question.
func TestReleaseApprovalAskDoesNotShipAfterNo(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	r := releaseRunFor(t, config.MergePipelineConfig{}, "p", config.ReleaseSizeMajor, qs, ship)
	releaseRunOnce(t, r)
	qs.answer(t, releaseOptionHold)
	run := releaseRunOnce(t, r)
	for _, step := range []string{"tag", "push"} {
		if got := stepOutcome(run, step); got != autonomous.MergeStepSkipped {
			t.Fatalf("%s = %q; want skipped after no", step, got)
		}
		if !strings.Contains(stepDetail(run, step), "(operator question ") {
			t.Fatalf("%s detail = %q; want it to name the operator question", step, stepDetail(run, step))
		}
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("ship calls = %q; want none after no", calls)
	}
}

// ask does not ship after the question is withdrawn.
func TestReleaseApprovalAskDoesNotShipAfterWithdraw(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	r := releaseRunFor(t, config.MergePipelineConfig{}, "p", config.ReleaseSizeMajor, qs, ship)
	releaseRunOnce(t, r)
	qs.withdraw(t)
	run := releaseRunOnce(t, r)
	if stepOutcome(run, "tag") != autonomous.MergeStepSkipped {
		t.Fatalf("tag = %q; want skipped after withdraw", stepOutcome(run, "tag"))
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("ship calls = %q; want none after withdraw", calls)
	}
}

// An unknown mode behaves as ask: one question, nothing ships, and the record
// reports the mode as ask.
func TestReleaseApprovalUnknownModeBehavesAsAsk(t *testing.T) {
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	r := releaseRunFor(t, config.MergePipelineConfig{}, "p", config.ReleaseSizePatch, qs, ship)
	r.ReleaseApproval = "yolo"
	run := releaseRunOnce(t, r)
	if qs.raisedCount() != 1 {
		t.Fatalf("unknown mode raised %d questions; want one, as ask does", qs.raisedCount())
	}
	if d := stepDetail(run, "tag"); !strings.Contains(d, "release gate (mode ask, size patch)") {
		t.Fatalf("tag detail = %q; want it reported as ask", d)
	}
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("unknown mode shipped: %q", calls)
	}
}

// The default per size, through config: patch and minor ship with no question,
// major asks and holds.
func TestReleaseApprovalDefaultsPerSizeInARun(t *testing.T) {
	var cfg config.MergePipelineConfig
	for _, size := range []string{config.ReleaseSizePatch, config.ReleaseSizeMinor} {
		ship := &fakeShip{}
		qs := newFakeReleaseQuestions()
		releaseRunOnce(t, releaseRunFor(t, cfg, "p", size, qs, ship))
		if calls := shipCalls(ship); len(calls) != 2 {
			t.Fatalf("default %s ship calls = %q; want tag and push", size, calls)
		}
		if qs.raisedCount() != 0 {
			t.Fatalf("default %s asked; raised %d", size, qs.raisedCount())
		}
	}
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	releaseRunOnce(t, releaseRunFor(t, cfg, "p", config.ReleaseSizeMajor, qs, ship))
	if calls := shipCalls(ship); len(calls) != 0 {
		t.Fatalf("default major shipped without a yes: %q", calls)
	}
	if qs.raisedCount() != 1 {
		t.Fatalf("default major raised %d questions; want one", qs.raisedCount())
	}
}

// A project override changes one size only: project p sets major to auto, so
// its major ships, while the global major stays ask for project q.
func TestReleaseApprovalProjectOverrideChangesOneSize(t *testing.T) {
	cfg := config.MergePipelineConfig{
		Projects: map[string]config.MergePipelineOverride{
			"p": {ReleaseApproval: config.ReleaseApprovalModes{Major: config.ReleaseApprovalAuto}},
		},
	}
	ship := &fakeShip{}
	qs := newFakeReleaseQuestions()
	releaseRunOnce(t, releaseRunFor(t, cfg, "p", config.ReleaseSizeMajor, qs, ship))
	if calls := shipCalls(ship); len(calls) != 2 {
		t.Fatalf("project p major ship calls = %q; want tag and push", calls)
	}
	if qs.raisedCount() != 0 {
		t.Fatalf("project p major is auto; raised %d", qs.raisedCount())
	}

	otherShip := &fakeShip{}
	otherQs := newFakeReleaseQuestions()
	releaseRunOnce(t, releaseRunFor(t, cfg, "q", config.ReleaseSizeMajor, otherQs, otherShip))
	if calls := shipCalls(otherShip); len(calls) != 0 {
		t.Fatalf("project q major shipped without a yes: %q", calls)
	}
	if otherQs.raisedCount() != 1 {
		t.Fatalf("project q major is the global ask; raised %d", otherQs.raisedCount())
	}
}

// A project override of minor to manual holds that project's minor, while
// another project's minor still ships.
func TestReleaseApprovalProjectManualMinorHoldsWhileOtherProjectShips(t *testing.T) {
	cfg := config.MergePipelineConfig{
		Projects: map[string]config.MergePipelineOverride{
			"p": {ReleaseApproval: config.ReleaseApprovalModes{Minor: config.ReleaseApprovalManual}},
		},
	}
	held := &fakeShip{}
	heldRun := releaseRunOnce(t, releaseRunFor(t, cfg, "p", config.ReleaseSizeMinor, newFakeReleaseQuestions(), held))
	if calls := shipCalls(held); len(calls) != 0 {
		t.Fatalf("project p minor is manual; ship calls = %q", calls)
	}
	if stepOutcome(heldRun, "tag") != autonomous.MergeStepSkipped {
		t.Fatalf("project p tag = %q; want skipped", stepOutcome(heldRun, "tag"))
	}

	shipped := &fakeShip{}
	releaseRunOnce(t, releaseRunFor(t, cfg, "q", config.ReleaseSizeMinor, newFakeReleaseQuestions(), shipped))
	if calls := shipCalls(shipped); len(calls) != 2 {
		t.Fatalf("project q minor (global auto) ship calls = %q; want tag and push", calls)
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
	first := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: gate, Locks: locks}

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
	second := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: locks, Recorder: rec}
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
	first := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: locks}
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
		r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: after, Recorder: rec}
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
		r := &Runner{Resolver: resolverWithRecord(t, definitionWithoutReleaseValidator()), VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Locks: after}
		res, err := r.Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}, AutomatonID: "a1"})
		if err != nil || res.Run.Result != autonomous.MergeRunOK {
			t.Fatalf("the same Automaton should resume its run: %q err %v", res.Run.Result, err)
		}
		if _, held := after.Status(dir); held {
			t.Error("the resumed run must release the lock when it ends")
		}
	})
}

// ---- rebase onto the main branch ----

// rebaseOnlyRunner runs a pipeline whose only step is rebase-onto-main.
func rebaseOnlyRunner(t *testing.T, dir string) *Runner {
	t.Helper()
	def := Definition{Steps: []Step{{Name: "rebase-onto-main", Kind: KindCustom, Action: ActionRebaseOntoMain, OnFailure: OnFailureStop}}}
	return &Runner{VersionFiles: testVersionFiles, Dir: dir, Project: "p", Ship: &fakeShip{}, Resolver: resolverWithRecord(t, def)}
}

func TestRebaseOntoMainRebasesABehindBranchAndKeepsItsCommits(t *testing.T) {
	dir := newMergeRepo(t)
	putFile(t, dir, "main-only.txt", "main moved\n")
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "main moves ahead")
	mainBefore := runGit(t, dir, "rev-parse", "main")

	res, err := rebaseOnlyRunner(t, dir).Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: %v %q", err, res.Run.Result)
	}
	if runGit(t, dir, "rev-parse", "main") != mainBefore {
		t.Error("the main branch must not move")
	}
	if runGit(t, dir, "merge-base", "main", "feature") != mainBefore {
		t.Error("feature must now sit on top of main")
	}
	if subj := runGit(t, dir, "log", "--format=%s", "main..feature"); subj != "feature work" {
		t.Errorf("feature commits not kept: %q", subj)
	}
	if got := runGit(t, dir, "show", "feature:feature.txt"); got != "new" {
		t.Errorf("branch content lost: %q", got)
	}
	if d := stepDetail(res.Run, "rebase-onto-main"); !strings.Contains(d, "rebased onto main: feature") {
		t.Errorf("detail %q", d)
	}
	if st := runGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("tree dirty: %q", st)
	}
	if wt := runGit(t, dir, "worktree", "list", "--porcelain"); strings.Count(wt, "worktree ") != 1 {
		t.Errorf("throwaway worktree left behind:\n%s", wt)
	}
}

func TestRebaseOntoMainLeavesBranchAlreadyOnMainAlone(t *testing.T) {
	dir := newMergeRepo(t)
	before := runGit(t, dir, "rev-parse", "feature")

	res, err := rebaseOnlyRunner(t, dir).Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: %v %q", err, res.Run.Result)
	}
	if after := runGit(t, dir, "rev-parse", "feature"); after != before {
		t.Errorf("branch moved from %s to %s", before, after)
	}
	if d := stepDetail(res.Run, "rebase-onto-main"); !strings.Contains(d, "already on main: feature") {
		t.Errorf("detail %q", d)
	}
}

func TestRebaseOntoMainConflictFailsAndLeavesBranchUntouched(t *testing.T) {
	dir := newMergeRepo(t)
	addConflictingBranch(t, dir)
	before := runGit(t, dir, "rev-parse", "clash")

	res, err := rebaseOnlyRunner(t, dir).Run(context.Background(), RunRequest{Title: "t", Branches: []string{"clash"}})
	if err == nil || res.Run.Result != autonomous.MergeRunFailed {
		t.Fatalf("want a failed run, got %q err %v", res.Run.Result, err)
	}
	if !strings.Contains(err.Error(), `"clash"`) {
		t.Errorf("error does not name the branch: %v", err)
	}
	if d := stepDetail(res.Run, "rebase-onto-main"); !strings.HasPrefix(d, `rebase of "clash"`) {
		t.Errorf("detail should lead with the failed branch: %q", d)
	}
	if after := runGit(t, dir, "rev-parse", "clash"); after != before {
		t.Errorf("conflicting branch moved from %s to %s", before, after)
	}
	if st := runGit(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("rebase must be aborted, tree has %q", st)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "rebase-merge")); err == nil {
		t.Error("a rebase is still in progress")
	}
	if wt := runGit(t, dir, "worktree", "list", "--porcelain"); strings.Count(wt, "worktree ") != 1 {
		t.Errorf("throwaway worktree left behind:\n%s", wt)
	}
}

func TestRebaseOntoMainFetchesMainFromTheRemote(t *testing.T) {
	dir := newMergeRepo(t)
	bare := t.TempDir()
	runGit(t, bare, "init", "-q", "--bare", "-b", "main")
	runGit(t, dir, "remote", "add", "origin", bare)
	runGit(t, dir, "push", "-q", "origin", "main")

	res, err := rebaseOnlyRunner(t, dir).Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: %v %q", err, res.Run.Result)
	}
	if d := stepDetail(res.Run, "rebase-onto-main"); !strings.HasPrefix(d, "fetched main from origin") {
		t.Errorf("detail %q", d)
	}
}

func TestRebaseOntoMainSkipsFetchWithoutRemote(t *testing.T) {
	dir := newMergeRepo(t)
	res, err := rebaseOnlyRunner(t, dir).Run(context.Background(), RunRequest{Title: "t", Branches: []string{"feature"}})
	if err != nil || res.Run.Result != autonomous.MergeRunOK {
		t.Fatalf("run: %v %q", err, res.Run.Result)
	}
	if d := stepDetail(res.Run, "rebase-onto-main"); !strings.HasPrefix(d, "no remote; the main branch was not fetched") {
		t.Errorf("detail %q", d)
	}
}

// ---- steps run twice leave the same state ----

// The merge step runs twice on the same branch. The first run merges it; the
// second finds the branch already in main, adds nothing, and leaves the main
// head where the first run left it.
func TestMergeBranchesTwiceLeavesMainHeadUnchanged(t *testing.T) {
	dir := newMergeRepo(t)
	st := &runState{r: &Runner{Dir: dir}, m: &Merger{Dir: dir}, req: RunRequest{Branches: []string{"feature"}}}
	ctx := context.Background()

	note, err := st.mergeBranches(ctx)
	if err != nil {
		t.Fatalf("first merge: %v", err)
	}
	if !strings.Contains(note, "merged: feature") {
		t.Fatalf("first note = %q, want it to say merged: feature", note)
	}
	headAfterFirst := runGit(t, dir, "rev-parse", "HEAD")

	note, err = st.mergeBranches(ctx)
	if err != nil {
		t.Fatalf("second merge must succeed: %v", err)
	}
	if !st.nothingNew || !strings.Contains(note, "nothing new") {
		t.Fatalf("second note = %q (nothingNew=%v), want a nothing-new note", note, st.nothingNew)
	}
	if head := runGit(t, dir, "rev-parse", "HEAD"); head != headAfterFirst {
		t.Fatalf("main head moved from %s to %s on the second merge", headAfterFirst, head)
	}
}

// countChangelogHeadings returns how many lines of text are a heading for version v.
func countChangelogHeadings(text, v string) int {
	n := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "## v"+v+" ") {
			n++
		}
	}
	return n
}

// The changelog heading step runs twice for the same version. The second run
// finds the heading already there and leaves the file bytes exactly as the
// first run wrote them, with one heading for the version.
func TestInsertChangelogHeadingTwiceLeavesOneHeading(t *testing.T) {
	dir := newMergeRepo(t)
	r := &Runner{Dir: dir}

	if _, err := r.insertChangelog("1.2.4", "repeat release"); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	afterFirst := readRepoText(t, dir, "CHANGELOG.md")
	if n := countChangelogHeadings(afterFirst, "1.2.4"); n != 1 {
		t.Fatalf("after first insert %d headings for v1.2.4, want 1", n)
	}

	note, err := r.insertChangelog("1.2.4", "repeat release")
	if err != nil {
		t.Fatalf("second insert must succeed: %v", err)
	}
	if !strings.Contains(note, "already has a heading for v1.2.4") {
		t.Fatalf("second note = %q, want it to say the heading exists", note)
	}
	afterSecond := readRepoText(t, dir, "CHANGELOG.md")
	if afterSecond != afterFirst {
		t.Fatalf("second insert changed the changelog bytes:\nfirst:\n%s\nsecond:\n%s", afterFirst, afterSecond)
	}
	if n := countChangelogHeadings(afterSecond, "1.2.4"); n != 1 {
		t.Fatalf("after second insert %d headings for v1.2.4, want 1", n)
	}
}

// countBumpCommits returns how many commits in the history of dir carry the
// release bump subject for version v.
func countBumpCommits(t *testing.T, dir, v string) int {
	t.Helper()
	n := 0
	for _, subject := range strings.Split(runGit(t, dir, "log", "--format=%s"), "\n") {
		if subject == "chore(release): v"+v+" bump for merged branches" {
			n++
		}
	}
	return n
}

// The bump commit step runs twice after the version and changelog are written.
// The first run commits them; the second finds nothing staged and makes no
// commit, so the history holds exactly one bump commit.
func TestCommitReleaseBumpTwiceMakesOneCommit(t *testing.T) {
	dir := newMergeRepo(t)
	st := &runState{r: &Runner{Dir: dir, VersionFiles: testVersionFiles}, m: &Merger{Dir: dir}}
	ctx := context.Background()
	for _, a := range []string{ActionSetReleaseVersion, ActionInsertChangelog} {
		if _, err := st.action(ctx, Step{Action: a}); err != nil {
			t.Fatalf("%s: %v", a, err)
		}
	}
	countBefore := runGit(t, dir, "rev-list", "--count", "HEAD")

	if _, err := st.action(ctx, Step{Action: ActionCommitReleaseBump}); err != nil {
		t.Fatalf("first commit-release-bump: %v", err)
	}
	countAfterFirst := runGit(t, dir, "rev-list", "--count", "HEAD")
	if countAfterFirst == countBefore {
		t.Fatalf("first commit-release-bump made no commit; count stayed at %s", countBefore)
	}
	if n := countBumpCommits(t, dir, "1.2.4"); n != 1 {
		t.Fatalf("after first run %d bump commits, want 1", n)
	}

	note, err := st.action(ctx, Step{Action: ActionCommitReleaseBump})
	if err != nil {
		t.Fatalf("second commit-release-bump must succeed: %v", err)
	}
	if !strings.Contains(note, "nothing staged") {
		t.Fatalf("second note = %q, want it to say nothing staged", note)
	}
	if countAfterSecond := runGit(t, dir, "rev-list", "--count", "HEAD"); countAfterSecond != countAfterFirst {
		t.Fatalf("second run changed the commit count from %s to %s", countAfterFirst, countAfterSecond)
	}
	if n := countBumpCommits(t, dir, "1.2.4"); n != 1 {
		t.Fatalf("after second run %d bump commits, want 1", n)
	}
}

// The push step runs twice against a local bare remote. The first push sends
// main and the release tag; the second finds the remote already at HEAD and
// leaves every ref on the remote as the first push left it.
func TestPushReleaseTwiceLeavesRemoteUnchanged(t *testing.T) {
	dir := newMergeRepo(t)
	bare := withOrigin(t, dir)
	runGit(t, dir, "merge", "--no-ff", "-q", "-m", "merge feature", "feature")
	st := &runState{r: &Runner{Dir: dir, VersionFiles: testVersionFiles, Ship: ExecShipRunner{}},
		m: &Merger{Dir: dir}}
	ctx := context.Background()
	for _, a := range []string{ActionSetReleaseVersion, ActionInsertChangelog, ActionCommitReleaseBump, ActionTagRelease} {
		if _, err := st.action(ctx, Step{Action: a}); err != nil {
			t.Fatalf("%s: %v", a, err)
		}
	}

	if _, err := st.action(ctx, Step{Action: ActionPushRelease}); err != nil {
		t.Fatalf("first push-release: %v", err)
	}
	remoteAfterFirst := runGit(t, bare, "for-each-ref", "--format=%(refname) %(objectname)")
	head := runGit(t, dir, "rev-parse", "HEAD")
	if !strings.Contains(remoteAfterFirst, "refs/heads/main "+head) {
		t.Fatalf("remote main after first push = %q, want it at HEAD %s", remoteAfterFirst, head)
	}
	if !strings.Contains(remoteAfterFirst, "refs/tags/v1.2.4 ") {
		t.Fatalf("remote after first push has no v1.2.4 tag:\n%s", remoteAfterFirst)
	}

	if _, err := st.action(ctx, Step{Action: ActionPushRelease}); err != nil {
		t.Fatalf("second push-release must succeed: %v", err)
	}
	if remoteAfterSecond := runGit(t, bare, "for-each-ref", "--format=%(refname) %(objectname)"); remoteAfterSecond != remoteAfterFirst {
		t.Fatalf("second push changed the remote:\nfirst:\n%s\nsecond:\n%s", remoteAfterFirst, remoteAfterSecond)
	}
}
