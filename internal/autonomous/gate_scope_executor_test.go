// Gate scope at the executor: the autonomous quality gate (executeOne)
// must run the scoped test command and scoped vet for non-final tasks,
// the full suite for final gate tasks and full scopes, fold new vet
// failures into the regression verdict, and keep the whole scope plan on
// the persisted gate result for later tracing.
//
// The fakes stand in for the two seams the executor already exposes:
// runQualityGateTests (which command actually runs where) and, for these
// tests, the plan-side GateScopeRunner (changed files, package listing,
// import graph). Everything between the fakes — PlanGateScope,
// ScopedTestCommand, the restricted baseline, EvaluateGate, the
// confirming re-run, and the gate_disagreement verdict — is the real
// code.

package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/pipeline"
)

const gsFullCmd = "go test ./..."

const (
	gsAppPkg   = "example.com/app"
	gsUtilPkg  = "example.com/util"
	gsSmokePkg = "example.com/smoke"
)

// gsSmokePatterns is the set of smoke package patterns the tests may feed
// (per-PRD or global) — each resolves to the one fake smoke package.
var gsSmokePatterns = []string{"pkg/smoke/*.sh"}

// gsFakeRunner is a deterministic pipeline.GateScopeRunner.
type gsFakeRunner struct {
	mu           sync.Mutex
	changedFiles []string
	packages     map[string][]string // pattern -> import paths
	importers    map[string][]string // pkg -> imports
	lastBase     string
	changedCalls int
}

func (f *gsFakeRunner) ChangedFiles(_ string, base string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastBase = base
	f.changedCalls++
	return f.changedFiles, nil
}

func (f *gsFakeRunner) ListPackages(_ string, patterns ...string) ([]string, error) {
	var out []string
	for _, p := range patterns {
		if pkgs, ok := f.packages[p]; ok {
			out = append(out, pkgs...)
			continue
		}
		return nil, os.ErrNotExist
	}
	return out, nil
}

func (f *gsFakeRunner) PackageImports(_ string) (map[string][]string, error) {
	return f.importers, nil
}

// gsDefaultRunner covers the fake project: two touched packages
// (pkg/app, pkg/util), app imports util, plus the smoke set.
func gsDefaultRunner() *gsFakeRunner {
	r := &gsFakeRunner{
		changedFiles: []string{"pkg/app/app.go", "pkg/util/util.go"},
		packages: map[string][]string{
			"./pkg/app":  {gsAppPkg},
			"./pkg/util": {gsUtilPkg},
		},
		importers: map[string][]string{
			gsAppPkg:  {gsUtilPkg},
			gsUtilPkg: {},
		},
	}
	for _, p := range append(gsSmokePatterns, pipeline.DefaultGateSmokePackages...) {
		r.packages[p] = []string{gsSmokePkg}
	}
	return r
}

// gsTester records every command the gate runs and answers from an
// exact-command map — that is how the tests assert which command the
// executor actually ran. Commands not in the map answer the fallback.
type gsTester struct {
	mu       sync.Mutex
	runs     []pipeline.TestResult
	cmds     []string
	byCmd    map[string]pipeline.TestResult
	fallback pipeline.TestResult
	// real, when set, answers commands with no canned result by actually
	// executing them — used for the confirming re-run, which must hit the
	// real module.
	real func(cmd, dir string, timeout int, roots []string) pipeline.TestResult
}

// gsPkArgs splits a `go <subcmd> <args...>` command into its subcommand and
// sorted non-flag arguments — used to compare command package sets
// independently of the plan's package order.
func gsPkArgs(cmd string) (string, []string) {
	f := strings.Fields(cmd)
	if len(f) < 2 || f[0] != "go" {
		return cmd, nil
	}
	var out []string
	for _, a := range f[2:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		out = append(out, a)
	}
	sort.Strings(out)
	return f[1], out
}

func (g *gsTester) do(cmd, dir string, timeout int, roots []string) pipeline.TestResult {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r2, ok := g.byCmd[cmd]; ok {
		g.record(r2, cmd)
		return r2
	}
	// The gate plan's package order is plan-internal; match by subcommand
	// plus package argument set, so the same scoped command answers
	// regardless of order.
	ws, args := gsPkArgs(cmd)
	for k, r2 := range g.byCmd {
		ks, kk := gsPkArgs(k)
		if ks == ws && reflect.DeepEqual(kk, args) { //nolint:govet // map lookup, order irrelevant
			g.record(r2, cmd)
			return r2
		}
	}
	fb := g.fallback
	if g.real != nil {
		fb = g.real(cmd, dir, timeout, roots)
	}
	g.record(fb, cmd)
	return fb
}

func (g *gsTester) record(r pipeline.TestResult, cmd string) {
	g.runs = append(g.runs, r)
	g.cmds = append(g.cmds, cmd)
}

func (g *gsTester) commands() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, len(g.cmds))
	copy(out, g.cmds)
	return out
}

// ran reports whether a command matching c (argument-order-insensitive —
// the gate plan's package order is plan-internal) was issued.
func (g *gsTester) ran(cmd string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	ws, args := gsPkArgs(cmd)
	for _, c := range g.cmds {
		if c == cmd {
			return true
		}
		ks, kk := gsPkArgs(c)
		if ks == ws && reflect.DeepEqual(kk, args) {
			return true
		}
	}
	return false
}

// gsOK / gsVetFailing build test and vet run results (gsVetFailing in
// the "# pkg" header form VetFailuresFromOutput understands).
func gsOK(cmd string) pipeline.TestResult {
	return pipeline.TestResult{Command: cmd, ExitCode: 0, TestsFound: 2, PassCount: 2}
}

func gsVetFailing(cmd string, pkgs ...string) pipeline.TestResult {
	var b strings.Builder
	for _, p := range pkgs {
		b.WriteString("# " + p + "\n" + p + "/x.go:1:2:5: unreachable code (vet)\n")
	}
	return pipeline.TestResult{Command: cmd, ExitCode: 1, Output: b.String()}
}

func installGateScopeGate(t *testing.T, runner *gsFakeRunner, tester *gsTester) {
	t.Helper()
	oldRun, oldGate := runQualityGateTests, gateScopeRunner
	if tester.real == nil {
		tester.real = oldRun // unknown commands actually execute (e.g. the confirming re-run)
	}
	runQualityGateTests = tester.do
	gateScopeRunner = runner
	t.Cleanup(func() { runQualityGateTests, gateScopeRunner = oldRun, oldGate })
}

type gsWorkerFn func(_ context.Context, _ SpawnRequest) (SpawnResult, error)

// gsRun is the harness: manager + PRD with one story, gates configured,
// then one executor pass with a stub worker/verifier.
func gsRun(t *testing.T, runner *gsFakeRunner, tester *gsTester, testCmd, scope string, smoke []string, cfg Config, spawn gsWorkerFn, stories []Story) *PRD {
	t.Helper()
	installGateScopeGate(t, runner, tester)
	dir := t.TempDir()
	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(m.Stop)
	allowTempProjectRoots(m)
	st := m.Store()
	prd, err := st.CreatePRD("gate scope executor prd", t.TempDir(), "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := st.SetStories(prd.ID, stories); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found")
	}
	prd.Status = PRDApproved
	if _, err := m.SetPRDQualityGates(prd.ID, true, testCmd, 30, false, scope, smoke); err != nil {
		t.Fatalf("SetPRDQualityGates: %v", err)
	}
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	spawnFn := SpawnFn(func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		if spawn != nil {
			return spawn(context.Background(), SpawnRequest{})
		}
		return SpawnResult{SessionID: "gs-sess", PreTaskSHA: "gs-base-sha"}, nil
	})
	verifyFn := VerifyFn(func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_ = m.Run(ctx, prd.ID, spawnFn, verifyFn)

	fresh, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("final GetPRD: not found in manager store")
	}
	return fresh
}

// gsRunRealModule is gsRun with a real project dir, so the real confirming
// re-run (which executes in the project dir) can prove its scoping.
func gsRunRealModule(t *testing.T, runner *gsFakeRunner, tester *gsTester, projDir, testCmd, scope string, cfg Config, spawn gsWorkerFn, stories []Story) *PRD {
	t.Helper()
	installGateScopeGate(t, runner, tester)
	dir := t.TempDir()
	m, err := NewManager(dir, cfg, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(m.Stop)
	allowTempProjectRoots(m)
	st := m.Store()
	prd, err := st.CreatePRD("gate scope real module prd", projDir, "claude-code", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if err := st.SetStories(prd.ID, stories); err != nil {
		t.Fatalf("SetStories: %v", err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD not found")
	}
	prd.Status = PRDApproved
	if _, err := m.SetPRDQualityGates(prd.ID, true, testCmd, 30, false, scope, nil); err != nil {
		t.Fatalf("SetPRDQualityGates: %v", err)
	}
	if err := st.SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	spawnFn := SpawnFn(func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		if spawn != nil {
			return spawn(context.Background(), SpawnRequest{})
		}
		return SpawnResult{SessionID: "gs-sess", PreTaskSHA: "gs-base-sha"}, nil
	})
	verifyFn := VerifyFn(func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok", VerifiedAt: time.Now()}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	_ = m.Run(ctx, prd.ID, spawnFn, verifyFn)
	fresh, ok := m.Store().GetPRD(prd.ID)
	if !ok {
		t.Fatal("final GetPRD: not found")
	}
	return fresh
}

func gsTask(t *testing.T, prd *PRD, wantIndex int) *Task {
	t.Helper()
	seen := -1
	for _, s := range prd.Story {
		for i := range s.Tasks {
			seen++
			if seen == wantIndex {
				return &s.Tasks[i]
			}
		}
	}
	t.Fatalf("task #%d not found on PRD", wantIndex)
	return nil
}

func gsPlan(t *testing.T, tk *Task) *pipeline.GateScopePlan {
	t.Helper()
	if tk.QualityGateResult == nil {
		t.Fatalf("task has no QualityGateResult")
	}
	if tk.QualityGateResult.ScopePlan == nil {
		t.Fatalf("QualityGateResult.ScopePlan is nil — the scope plan must be persisted on the gate result")
	}
	return tk.QualityGateResult.ScopePlan
}

func gsWrite(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

func gsStories(t *testing.T) []Story {
	t.Helper()
	// Two tasks: task 0 is the scoped (non-finale) gate, task 1 exists only
	// so task 0 is not "the last remaining task". Under MaxConcurrentTasks=1
	// (the default) they run in order; task 1 is the finale.
	return []Story{{Title: "S", Tasks: []Task{{Title: "work", Spec: "s"}, {Title: "tail", Spec: "s"}}}}
}

func gsFileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// --- 1. touched-plus-smoke narrows the gate command ---

func TestGateScope_TouchedPlusSmokeRunsScopedCommand(t *testing.T) {
	runner := gsDefaultRunner()
	smoke := gsSmokePatterns
	scopedCmd, ok, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	if !ok {
		t.Fatalf("fixture: ScopedTestCommand should be rewritable")
	}
	vetCmd := pipeline.ScopedVetCommand([]string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:      gsOK(gsFullCmd),                         // baseline (and any full run)
		scopedCmd:      gsOK(scopedCmd),                         // the scoped gate run
		"go vet ./...": gsVetFailing("go vet ./...", gsUtilPkg), // vet baseline: pre-existing
		vetCmd:         gsOK(vetCmd),                            // current scoped vet: clean
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouchedPlusSmoke, smoke,
		Config{Enabled: true, AutoFixRetries: 0}, nil, gsStories(t))

	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("task 0 = %s (err=%q), want completed (all runs green); task 1 = %+v", tk.Status, tk.Error, gsTask(t, prd, 1))
	}
	if runner.lastBase != "gs-base-sha" || runner.changedCalls == 0 {
		t.Fatalf("scope plan was not computed from the task base SHA: lastBase=%q changedCalls=%d", runner.lastBase, runner.changedCalls)
	}
	if !tester.ran(scopedCmd) {
		t.Fatalf("scoped gate command %q was never run; commands: %v", scopedCmd, tester.commands())
	}
	plan := gsPlan(t, tk)
	if plan.Scope != pipeline.GateScopeTouchedPlusSmoke || plan.ScopeSource != "automaton" {
		t.Fatalf("plan.Scope=%q source=%q, want touched-plus-smoke from the per-PRD setting", plan.Scope, plan.ScopeSource)
	}
	if len(plan.Packages) != 3 {
		t.Fatalf("plan packages = %v, want the touched + importer + smoke set %v", plan.Packages, []string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	}
	if !strings.Contains(tk.QualityGateResult.Summary, "touched-plus-smoke") {
		t.Fatalf("gate summary should describe the scope, got %q", tk.QualityGateResult.Summary)
	}
}

// --- 2. touched scope omits the smoke set ---

func TestGateScope_TouchedOmitsSmoke(t *testing.T) {
	runner := gsDefaultRunner()
	touchedCmd, ok, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg})
	if !ok {
		t.Fatalf("fixture: should be rewritable")
	}
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:      gsOK(gsFullCmd),
		touchedCmd:     gsOK(touchedCmd),
		"go vet ./...": gsOK("go vet ./..."),
		pipeline.ScopedVetCommand([]string{gsAppPkg, gsUtilPkg}): gsOK("v"),
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouched, nil,
		Config{Enabled: true, AutoFixRetries: 0}, nil, gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("task 0 = %s (err=%q), want completed", tk.Status, tk.Error)
	}
	for _, c := range tester.commands() {
		if strings.Contains(c, gsSmokePkg) {
			t.Fatalf("touched scope must omit the smoke package; command %q includes it (all: %v)", c, tester.commands())
		}
	}
	plan := gsPlan(t, tk)
	if plan.Scope != pipeline.GateScopeTouched {
		t.Fatalf("plan.Scope = %q, want %q", plan.Scope, pipeline.GateScopeTouched)
	}
}

// --- 3. full scope runs the configured command untouched (no vet) ---

func TestGateScope_FullRunsConfiguredCommandUnchanged(t *testing.T) {
	runner := gsDefaultRunner()
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{gsFullCmd: gsOK(gsFullCmd)}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeFull, nil,
		Config{Enabled: true, AutoFixRetries: 0}, nil,
		[]Story{{Title: "S", Tasks: []Task{{Title: "t", Spec: "s"}}}})
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("task = %s (err=%q), want completed", tk.Status, tk.Error)
	}
	cmds := tester.commands()
	for _, c := range cmds {
		if c != gsFullCmd {
			t.Fatalf("full scope must run only the configured command; saw %q (all: %v)", c, cmds)
		}
	}
	plan := gsPlan(t, tk)
	if !plan.Full || len(plan.Packages) != 0 {
		t.Fatalf("plan = %+v, want Full with no package list", plan)
	}
}

// --- 4. TaskKindFinalGate tag forces the full suite ---

func TestGateScope_FinalGateTagForcesFull(t *testing.T) {
	runner := gsDefaultRunner()
	scopedCmd, _, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:      gsOK(gsFullCmd),
		scopedCmd:      gsOK(scopedCmd), // must NOT be used by a final gate task
		"go vet ./...": gsOK("go vet ./..."),
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouchedPlusSmoke, gsSmokePatterns,
		Config{Enabled: true, AutoFixRetries: 0}, nil,
		[]Story{{Title: "S", Tasks: []Task{{Title: "final", Spec: "s", Kind: TaskKindFinalGate}}}})
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("task = %s (err=%q), want completed", tk.Status, tk.Error)
	}
	if tester.ran(scopedCmd) {
		t.Fatalf("final gate task must run the full command, not the scoped one %q (all: %v)", scopedCmd, tester.commands())
	}
	if !tester.ran(gsFullCmd) {
		t.Fatalf("final gate task must have run the full command %q (all: %v)", gsFullCmd, tester.commands())
	}
	plan := gsPlan(t, tk)
	if !plan.Full || !strings.Contains(plan.FullReason, "final gate task") {
		t.Fatalf("plan = %+v, want Full with the final-gate reason", plan)
	}
}

// --- 5. last remaining task (no tag) is also the final gate ---

func TestGateScope_LastRemainingTaskWithoutTagRunsFull(t *testing.T) {
	runner := gsDefaultRunner()
	scopedCmd, ok, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	if !ok {
		t.Fatalf("fixture: should be rewritable")
	}
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:      gsOK(gsFullCmd),
		scopedCmd:      gsOK(scopedCmd),
		"go vet ./...": gsOK("go vet ./..."),
		pipeline.ScopedVetCommand([]string{gsAppPkg, gsUtilPkg, gsSmokePkg}): gsOK("v"),
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouchedPlusSmoke, gsSmokePatterns,
		Config{Enabled: true, AutoFixRetries: 0}, nil,
		[]Story{{Title: "S", Tasks: []Task{{Title: "first", Spec: "s"}, {Title: "last", Spec: "s"}}}})
	first, last := gsTask(t, prd, 0), gsTask(t, prd, 1)
	if first.Status != TaskCompleted || last.Status != TaskCompleted {
		t.Fatalf("tasks = %s / %s (err=%q / %q), both want completed", first.Status, last.Status, first.Error, last.Error)
	}
	firstPlan := gsPlan(t, first)
	if firstPlan.Full || firstPlan.NothingToRun || len(firstPlan.Packages) != 3 {
		t.Fatalf("first plan = %+v, want a scoped plan with the narrow package set", firstPlan)
	}
	lastPlan := gsPlan(t, last)
	if !lastPlan.Full || !strings.Contains(lastPlan.FullReason, "final gate task") || len(lastPlan.Packages) != 0 {
		t.Fatalf("last plan = %+v, want Full (final-gate reason, no package list)", lastPlan)
	}
}

// --- 6. non-rewritable command falls back to full with a reason ---

func TestGateScope_NonRewritableCommandFallsBackToFull(t *testing.T) {
	pipeCmd := "go test ./... | tee /tmp/gate.log"
	runner := gsDefaultRunner()
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		pipeCmd:        gsOK(pipeCmd),
		"go vet ./...": gsOK("go vet ./..."),
	}}
	prd := gsRun(t, runner, tester, pipeCmd, pipeline.GateScopeTouched, nil,
		Config{Enabled: true, AutoFixRetries: 0}, nil, gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("task 0 = %s (err=%q), want completed (fallback runs the full command)", tk.Status, tk.Error)
	}
	if !tester.ran(pipeCmd) {
		t.Fatalf("fallback must run the configured full command %q; commands: %v", pipeCmd, tester.commands())
	}
	plan := gsPlan(t, tk)
	if !plan.Full || plan.FallbackReason == "" {
		t.Fatalf("plan = %+v, want Full with a recorded fallback reason", plan)
	}
	if !strings.Contains(plan.FallbackReason, "unsafe") && !strings.Contains(plan.FallbackReason, "rewrit") {
		t.Fatalf("fallback reason should explain why: %q", plan.FallbackReason)
	}
}

// --- 7. baseline failure in an unrun package is neither regression nor
// improvement for the scoped gate (restricted baseline) ---

func TestGateScope_PreexistingFailureInUnrunPackageNotReported(t *testing.T) {
	runner := gsDefaultRunner()
	scopedCmd, _, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg})
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd: { // baseline: one failing test in a package the scope never runs
			Command: gsFullCmd, ExitCode: 1, TestsFound: 2, FailCount: 1,
			FailedTests: []string{"example.com/other.TestPreexisting"},
		},
		scopedCmd:      gsOK(scopedCmd),
		"go vet ./...": gsOK("go vet ./..."),
		pipeline.ScopedVetCommand([]string{gsAppPkg, gsUtilPkg}): gsOK("v"),
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouched, nil,
		Config{Enabled: true, AutoFixRetries: 0}, nil, gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("task 0 = %s (err=%q), want completed (gate STABLE: out-of-scope baseline failure is invisible to the scoped gate); task 1 = %+v", tk.Status, tk.Error, gsTask(t, prd, 1))
	}
	qg := tk.QualityGateResult
	if qg == nil {
		t.Fatal("no QualityGateResult")
	}
	if qg.Regression || qg.Improved {
		t.Fatalf("baseline failure outside the scope must not show up as regression or improvement: %+v", qg)
	}
	if strings.Contains(qg.Summary, "TestPreexisting") {
		t.Fatalf("restricted baseline leaked the out-of-scope failure: %q", qg.Summary)
	}
	plan := gsPlan(t, tk)
	if len(plan.ChangedFiles) == 0 {
		t.Fatalf("plan should record the changed files: %+v", plan)
	}
}

// --- 8. a NEW vet failure in scope is a named regression; under the
// "block" gate_disagreement policy it fails the task ---

func TestGateScope_VetOnlyRegressionBlocks(t *testing.T) {
	runner := gsDefaultRunner()
	scopedCmd, _, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	pkgs := []string{gsAppPkg, gsUtilPkg, gsSmokePkg}
	vetCmd := pipeline.ScopedVetCommand(pkgs)
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:      gsOK(gsFullCmd),
		scopedCmd:      gsOK(scopedCmd),
		"go vet ./...": gsVetFailing("go vet ./...", gsUtilPkg), // baseline: util already red
		vetCmd:         gsVetFailing(vetCmd, gsAppPkg),          // now: app is NEW-red
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouchedPlusSmoke, gsSmokePatterns,
		Config{Enabled: true, AutoFixRetries: 0, GateDisagreement: "block"}, nil, gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskFailed {
		t.Fatalf("task 0 = %s (err=%q), want failed (new vet failure under the block policy)", tk.Status, tk.Error)
	}
	qg := tk.QualityGateResult
	if qg == nil || !qg.Regression {
		t.Fatalf("expected a recorded regression, got %+v", qg)
	}
	names := strings.Join(qg.NewFailureNames, ",")
	if !strings.Contains(names, "vet:"+gsAppPkg) {
		t.Fatalf("the new vet failure must be a named regression; NewFailureNames=%v", qg.NewFailureNames)
	}
	if strings.Contains(names, "vet:"+gsUtilPkg) {
		t.Fatalf("the pre-existing vet failure must not be a regression; NewFailureNames=%v", qg.NewFailureNames)
	}
	if tk.GateDisagreement == nil || tk.GateDisagreement.Policy != "block" {
		t.Fatalf("GateDisagreement = %+v, want policy=block", tk.GateDisagreement)
	}
	// the vet run is its own run, separate from the test run
	if !tester.ran(vetCmd) {
		t.Fatalf("the scoped vet command %q must have run as its own run; commands: %v", vetCmd, tester.commands())
	}
}

// --- 9. vet failure present at baseline is not "new" ---

func TestGateScope_VetPriorFailureNotRegression(t *testing.T) {
	runner := gsDefaultRunner()
	scopedCmd, _, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	vetCmd := pipeline.ScopedVetCommand([]string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:      gsOK(gsFullCmd),
		scopedCmd:      gsOK(scopedCmd),
		"go vet ./...": gsVetFailing("go vet ./...", gsAppPkg), // already red at baseline
		vetCmd:         gsVetFailing(vetCmd, gsAppPkg),
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouchedPlusSmoke, gsSmokePatterns,
		Config{Enabled: true, AutoFixRetries: 0, GateDisagreement: "block"}, nil, gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("task 0 = %s (err=%q), want completed (vet failure pre-existed at baseline)", tk.Status, tk.Error)
	}
	qg := tk.QualityGateResult
	if qg == nil || qg.Regression {
		t.Fatalf("expected no regression, got %+v", qg)
	}
}

// --- 10a. baseline vet unusable: warn only, never blocks ---

func TestGateScope_VetBaselineUnusableWarnOnly(t *testing.T) {
	runner := gsDefaultRunner()
	scopedCmd, _, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	vetCmd := pipeline.ScopedVetCommand([]string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:      gsOK(gsFullCmd),
		scopedCmd:      gsOK(scopedCmd),
		"go vet ./...": {Command: "go vet ./...", ExitCode: -1}, // baseline vet could not run
		vetCmd:         gsVetFailing(vetCmd, gsAppPkg),          // would be a regression — but vet comparison is skipped
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouchedPlusSmoke, gsSmokePatterns,
		Config{Enabled: true, AutoFixRetries: 0, GateDisagreement: "block"}, nil, gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("an unusable vet baseline must warn only: task 0 = %s (err=%q)", tk.Status, tk.Error)
	}
	qg := tk.QualityGateResult
	if qg == nil || qg.Regression {
		t.Fatalf("an unusable vet comparison never counts as a regression, got %+v", qg)
	}
}

// --- 10b. current vet run unusable: warn only, never blocks ---

func TestGateScope_VetCurrentUnusableWarnOnly(t *testing.T) {
	runner := gsDefaultRunner()
	scopedCmd, _, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	vetCmd := pipeline.ScopedVetCommand([]string{gsAppPkg, gsUtilPkg, gsSmokePkg})
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:      gsOK(gsFullCmd),
		scopedCmd:      gsOK(scopedCmd),
		"go vet ./...": gsOK("go vet ./..."),            // baseline clean
		vetCmd:         {Command: vetCmd, ExitCode: -1}, // current vet could not run
	}}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouchedPlusSmoke, gsSmokePatterns,
		Config{Enabled: true, AutoFixRetries: 0, GateDisagreement: "block"}, nil, gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("an unusable vet run must warn only: task 0 = %s (err=%q)", tk.Status, tk.Error)
	}
	qg := tk.QualityGateResult
	if qg == nil || qg.Regression {
		t.Fatalf("an unusable vet run never counts as a regression, got %+v", qg)
	}
}

// --- 11. per-PRD setting wins over the automaton global default ---

func TestGateScope_ScopeOverridesGlobal(t *testing.T) {
	runner := gsDefaultRunner()
	pkgs := []string{gsAppPkg, gsUtilPkg, gsSmokePkg}
	scopedCmd, ok, _ := pipeline.ScopedTestCommand(gsFullCmd, pkgs)
	if !ok {
		t.Fatalf("fixture: should be rewritable")
	}
	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		gsFullCmd:                       gsOK(gsFullCmd),
		scopedCmd:                       gsOK(scopedCmd),
		"go vet ./...":                  gsOK("go vet ./..."),
		pipeline.ScopedVetCommand(pkgs): gsOK("v"),
	}}
	cfg := Config{Enabled: true, AutoFixRetries: 0}
	cfg.DefaultQualityGates = pipeline.QualityGateConfig{
		Enabled:     true,
		TestCommand: gsFullCmd,
		Scope:       pipeline.GateScopeTouched, // global says: omit the smoke set
	}
	prd := gsRun(t, runner, tester, gsFullCmd, pipeline.GateScopeTouchedPlusSmoke, gsSmokePatterns,
		cfg, nil, gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskCompleted {
		t.Fatalf("task 0 = %s (err=%q), want completed", tk.Status, tk.Error)
	}
	plan := gsPlan(t, tk)
	if plan.Scope != pipeline.GateScopeTouchedPlusSmoke || plan.ScopeSource != "automaton" {
		t.Fatalf("plan.Scope=%q source=%q, want the per-PRD touched-plus-smoke (source: PRD/automaton setting)", plan.Scope, plan.ScopeSource)
	}
	if !tester.ran(scopedCmd) {
		t.Fatalf("the PRD smoke package must be in the gated command; commands: %v", tester.commands())
	}
}

// --- 12. the store-side finale decision (tag vs last remaining) ---

func TestGateScope_IsFinaleGateTaskHelper(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir, Config{Enabled: true}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Stop()

	// tagged final — regardless of siblings
	taggedPRD := &PRD{ID: "p", Story: []Story{{Title: "S", Tasks: []Task{
		{ID: "a", Kind: TaskKindFinalGate, Status: TaskQueued},
		{ID: "b", Status: TaskQueued},
	}}}}
	if !m.isFinaleGateTask(taggedPRD, &taggedPRD.Story[0].Tasks[0]) {
		t.Fatal("a task tagged TaskKindFinalGate must be a final gate regardless of siblings")
	}

	// not last: a sibling is still pending
	bothPRD := &PRD{ID: "p", Story: []Story{{Title: "S", Tasks: []Task{
		{ID: "a", Status: TaskQueued}, {ID: "b", Status: TaskQueued},
	}}}}
	if m.isFinaleGateTask(bothPRD, &bothPRD.Story[0].Tasks[0]) {
		t.Fatal("a non-last task with a still-queued sibling must not be a final gate")
	}

	// last non-terminal task (others completed/failed)
	mixPRD := &PRD{ID: "p", Story: []Story{{Title: "S", Tasks: []Task{
		{ID: "a", Status: TaskInProgress},
		{ID: "b", Status: TaskCompleted},
		{ID: "c", Status: TaskFailed},
	}}}}
	if !m.isFinaleGateTask(mixPRD, &mixPRD.Story[0].Tasks[0]) {
		t.Fatal("the last non-terminal task (siblings completed/failed) must be a final gate")
	}

	// a non-final sibling (queued) defers the finale for everyone
	blockedPRD := &PRD{ID: "p", Story: []Story{{Title: "S", Tasks: []Task{
		{ID: "a", Status: TaskQueued},
		{ID: "b", Status: TaskBlocked},
	}}}}
	if m.isFinaleGateTask(blockedPRD, &blockedPRD.Story[0].Tasks[0]) {
		t.Fatal("task a has a non-terminal sibling b: not yet the final gate")
	}
	if m.isFinaleGateTask(blockedPRD, &blockedPRD.Story[0].Tasks[1]) {
		t.Fatal("task b has a non-terminal sibling a (queued): the final gate is still deferred")
	}

	// last non-terminal across multiple stories
	multiPRD := &PRD{ID: "p", Story: []Story{
		{Title: "S1", Tasks: []Task{{ID: "x", Status: TaskCompleted}}},
		{Title: "S2", Tasks: []Task{{ID: "y", Status: TaskQueued}}},
	}}
	if !m.isFinaleGateTask(multiPRD, &multiPRD.Story[1].Tasks[0]) {
		t.Fatal("the last non-terminal task across stories must be a final gate")
	}
}

// --- 13. a real regression in the scoped run still blocks, and the
// confirming re-run ran ONLY the new failing test ---

func TestGateScope_TouchedRegressionStillBlocks(t *testing.T) {
	// Real module: TestOldBroken fails at baseline; the worker "work"
	// adds TestNewBroken (also failing) in the same touched package.
	projDir := t.TempDir()
	gsWrite(t, projDir, map[string]string{
		"go.mod":       "module example.com\n\ngo 1.21\n",
		"util/util.go": "package util\n\nfunc Broken() bool { return true }\n",
		"util/old_test.go": "package util\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
			"func TestOldBroken(t *testing.T) {\n\tos.WriteFile(\"OLD_RERAN\", nil, 0o644)\n\tt.Fatal(\"old broken (pre-existing)\")\n}\n",
	})

	runner := &gsFakeRunner{
		changedFiles: []string{"util/util.go"},
		packages:     map[string][]string{"./util": {gsUtilPkg}},
		importers:    map[string][]string{gsUtilPkg: {}},
	}
	scopedCmd, _, _ := pipeline.ScopedTestCommand(gsFullCmd, []string{gsUtilPkg})
	scopedOut := "--- FAIL: TestOldBroken (0.00s)\n    old_test.go:7: old broken (pre-existing)\n" +
		"--- FAIL: TestNewBroken (0.00s)\n    new_test.go:8: new broken (worker)\n" +
		"FAIL\t" + gsUtilPkg + "\t0.002s\n"

	tester := &gsTester{byCmd: map[string]pipeline.TestResult{
		// baseline (= the module's real state before the worker "works"):
		// exactly TestOldBroken fails
		gsFullCmd: {
			Command: gsFullCmd, ExitCode: 1, TestsFound: 1, FailCount: 1,
			FailedTests: []string{gsUtilPkg + ".TestOldBroken"},
		},
		scopedCmd: {Command: scopedCmd, ExitCode: 1, TestsFound: 2, FailCount: 2, Output: scopedOut,
			FailedTests: []string{gsUtilPkg + ".TestOldBroken", gsUtilPkg + ".TestNewBroken"}},
	}}

	var broke bool
	spawn := func(_ context.Context, _ SpawnRequest) (SpawnResult, error) {
		if !broke {
			body := "package util\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n" +
				"func TestNewBroken(t *testing.T) {\n\tos.WriteFile(\"NEW_RERAN\", nil, 0o644)\n\tt.Fatal(\"new broken (worker)\")\n}\n"
			if err := os.WriteFile(filepath.Join(projDir, "util/new_test.go"), []byte(body), 0o644); err != nil {
				t.Errorf("worker: %v", err)
			}
			broke = true
		}
		return SpawnResult{SessionID: "gs-work"}, nil
	}

	prd := gsRunRealModule(t, runner, tester, projDir, gsFullCmd, pipeline.GateScopeTouched,
		Config{Enabled: true, AutoFixRetries: 0, GateDisagreement: "block"}, spawn,
		gsStories(t))
	tk := gsTask(t, prd, 0)
	if tk.Status != TaskFailed {
		t.Fatalf("under the block policy task 0 is %s (err=%q), want failed (new failure TestNewBroken)", tk.Status, tk.Error)
	}
	if tk.Error == "" || !strings.Contains(tk.Error, "gate_disagreement:") {
		t.Fatalf("task.Error = %q, want the gate_disagreement verdict prefix", tk.Error)
	}
	qg := tk.QualityGateResult
	if qg == nil || !qg.Regression {
		t.Fatalf("expected a recorded regression, got %+v", qg)
	}
	names := strings.Join(qg.NewFailureNames, ",")
	if !strings.Contains(names, "TestNewBroken") {
		t.Fatalf("the new failure must be named on the result: %v", qg.NewFailureNames)
	}
	if strings.Contains(names, "TestOldBroken") {
		t.Fatalf("the pre-existing failure must not be a new failure: %v", qg.NewFailureNames)
	}
	// The confirming re-run executed in the real module and ran only the
	// new test: NEW_RERAN exists (in the package dir, where go test runs),
	// OLD_RERAN does not.
	if !gsFileExists(filepath.Join(projDir, "util", "NEW_RERAN")) {
		t.Fatalf("the confirming re-run must have executed TestNewBroken (marker missing); commands: %v", tester.commands())
	}
	if gsFileExists(filepath.Join(projDir, "util", "OLD_RERAN")) {
		t.Fatalf("the confirming re-run must not have re-run TestOldBroken (marker present)")
	}
}
