// project_policy_wire_test.go — the reader side of the per-project policy
// tier: each wired Manager reader must honor automaton > project > daemon,
// and a PRD with no project policy must behave exactly as before (daemon
// reads untouched).
package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func testPrd() *PRD { return &PRD{ID: "prd-test"} }

// setPol wires a pre-resolved policy straight into the manager, tagging the
// fields a test cares about with the given source as the resolver would.
func setPol(t *testing.T, m *Manager, pol config.EffectivePolicy) {
	t.Helper()
	fn := func(*PRD) config.EffectivePolicy { return pol }
	m.SetEffectivePolicy(fn)
}

func projectStr(v string) config.Field[string] {
	return config.Field[string]{Value: v, Source: config.PolicySourceProject}
}
func projectInt(v int) config.Field[int] {
	return config.Field[int]{Value: v, Source: config.PolicySourceProject}
}
func projectBool(v bool) config.Field[bool] {
	return config.Field[bool]{Value: v, Source: config.PolicySourceProject}
}
func projectFloat(v float64) config.Field[float64] {
	return config.Field[float64]{Value: v, Source: config.PolicySourceProject}
}

func TestReaderSideProjectTier(t *testing.T) {
	m := &Manager{}
	m.SetConfig(Config{
		PlanReviewMode:     "warn",
		DefaultAutoPR:      false,
		AutoFixRetries:     3,
		MaxConcurrentTasks: 2,
		Automation: AutomationConfig{
			UncommittedWorkPolicy: "warn",
			UncommittedWorkScope:  "task_files",
		},
		// Explicit daemon offs so a PASS below means the project tier won.
		Parallel: config.AutonomousParallelConfig{ReadOnlyTasks: boolPtr(false), WorktreePerTask: boolPtr(false)},
	})
	prd := testPrd()
	setPol(t, m, config.EffectivePolicy{
		PlanReviewMode:          projectStr("block"),
		ScopeLint:               projectStr("off"),
		PlanCoverageCheck:       projectStr("off"),
		PlanCoverageMinRatio:    projectFloat(0.5),
		AutoFixRetries:          projectInt(1),
		MaxConcurrentTasks:      projectInt(5),
		DefaultAutoPR:           projectBool(true),
		UncommittedWorkPolicy:   projectStr("hold"),
		UncommittedWorkScope:    projectStr("worktree"),
		ParallelReadOnlyTasks:   projectBool(true),
		ParallelWorktreePerTask: projectBool(true),
	})

	if got := m.effectivePlanReviewMode(prd); got != "block" {
		t.Errorf("plan review mode = %q, want block", got)
	}
	if got := m.effectiveScopeLint(prd); got != "off" {
		t.Errorf("scope lint = %q, want off", got)
	}
	settings, err := m.effectivePlanCoverage(prd)
	if err != nil || settings.Mode != config.PlanCoverageOff || settings.MinRatio != 0.5 {
		t.Errorf("plan coverage = %+v (err %v), want off/0.5", settings, err)
	}
	if got := m.effectiveDefaultAutoPR(prd); got != true {
		t.Errorf("default auto-pr = %v, want true", got)
	}
	if got := m.effectiveSpawnRetries(prd); got != 1 {
		t.Errorf("spawn retries = %d, want 1", got)
	}
	if got := m.resolveTaskConcurrency(prd); got != 5 {
		t.Errorf("task concurrency = %d, want 5", got)
	}
	if got := m.effectiveUncommittedWorkPolicy(prd); got != "hold" {
		t.Errorf("uncommitted work policy = %q, want hold", got)
	}
	if got := m.effectiveUncommittedWorkScope(prd); got != "worktree" {
		t.Errorf("uncommitted work scope = %q, want worktree", got)
	}
	if !m.effectiveReadOnlyTasks(prd) || !m.effectiveWorktreePerTask(prd) {
		t.Errorf("parallel switches not both on: ro=%v wt=%v", m.effectiveReadOnlyTasks(prd), m.effectiveWorktreePerTask(prd))
	}
}

// The Automaton's own override beats the project tier.
func TestReaderSideAutomatonTierWins(t *testing.T) {
	m := &Manager{}
	m.SetConfig(Config{MaxConcurrentTasks: 2})
	prd := &PRD{ID: "prd-test", MaxConcurrentTasks: 7, RetryCeilingTotal: 9}
	setPol(t, m, config.EffectivePolicy{
		MaxConcurrentTasks: config.Field[int]{Value: 5, Source: config.PolicySourceProject},
		RetryCeilingTotal:  config.Field[int]{Value: 9, Source: config.PolicySourceAutomaton},
	})
	if got := m.resolveTaskConcurrency(prd); got != 7 {
		t.Errorf("task concurrency = %d, want 7 (automaton)", got)
	}
	if got := m.resolveRetryCeiling(prd); got != 9 {
		t.Errorf("retry ceiling = %d, want 9 (automaton)", got)
	}
}

// No policy wired: every reader serves the daemon value — the historical
// behaviour, byte for byte.
func TestReaderSideUnwiredServesDaemon(t *testing.T) {
	m := &Manager{}
	m.SetConfig(Config{
		PlanReviewMode:     "warn",
		DefaultAutoPR:      false,
		AutoFixRetries:     4,
		MaxConcurrentTasks: 2,
		Automation: AutomationConfig{
			UncommittedWorkPolicy: "warn",
			UncommittedWorkScope:  "task_files",
		},
		// read_only_tasks explicitly off: the daemon reader must serve false.
		Parallel: config.AutonomousParallelConfig{ReadOnlyTasks: boolPtr(false)},
	})
	prd := testPrd()
	if got := m.effectivePlanReviewMode(prd); got != "warn" {
		t.Errorf("plan review mode = %q, want warn", got)
	}
	if got := m.effectiveDefaultAutoPR(prd); got != false {
		t.Errorf("default auto-pr = %v, want false", got)
	}
	if got := m.effectiveSpawnRetries(prd); got != 4 {
		t.Errorf("spawn retries = %d, want 4", got)
	}
	if got := m.resolveTaskConcurrency(prd); got != 2 {
		t.Errorf("task concurrency = %d, want 2", got)
	}
	if got := m.effectiveUncommittedWorkPolicy(prd); got != "warn" {
		t.Errorf("uncommitted work policy = %q, want warn", got)
	}
	if m.readOnlyTaskSkipsLock(prd, &Task{ReadOnly: true}) {
		t.Error("read-only skip = true, want false (daemon says off)")
	}
}

// A field the resolver tagged "daemon" must never win over the reader's own
// daemon read: the gate is on the source tag, not the value.
func TestReaderSideDaemonSourceIsIgnored(t *testing.T) {
	m := &Manager{}
	m.SetConfig(Config{MaxConcurrentTasks: 2})
	setPol(t, m, config.EffectivePolicy{
		MaxConcurrentTasks: config.Field[int]{Value: 99, Source: config.PolicySourceDaemon},
	})
	if got := m.resolveTaskConcurrency(testPrd()); got != 2 {
		t.Errorf("task concurrency = %d, want 2 (daemon source must not win)", got)
	}
}

func TestResolveRetryPolicyProjectTier(t *testing.T) {
	m := &Manager{}
	m.SetConfig(Config{RetryPolicy: map[string]int{"content": 3}, AutoFixRetries: 8})
	prd := testPrd()

	// Project tier names the category: it wins before the daemon map.
	setPol(t, m, config.EffectivePolicy{
		RetryPolicy: config.Field[map[string]int]{Value: map[string]int{"content": 1}, Source: config.PolicySourceProject},
	})
	if got := m.resolveRetryPolicy(prd, "content"); got != 1 {
		t.Errorf("content budget = %d, want 1 (project)", got)
	}
	// Project tier's map omits the category: fall through to the daemon map.
	setPol(t, m, config.EffectivePolicy{
		RetryPolicy: config.Field[map[string]int]{Value: map[string]int{"infra": 1}, Source: config.PolicySourceProject},
	})
	if got := m.resolveRetryPolicy(prd, "content"); got != 3 {
		t.Errorf("content budget = %d, want 3 (daemon map)", got)
	}
	// Unwired: daemon map, then the AutoFixRetries fallback for content.
	m.SetEffectivePolicy(nil)
	if got := m.resolveRetryPolicy(prd, "content"); got != 3 {
		t.Errorf("content budget = %d, want 3 (daemon unwired)", got)
	}
	if got := m.resolveRetryPolicy(prd, "policy"); got != 8 {
		t.Errorf("policy budget = %d, want 8 (auto-fix fallback)", got)
	}
}

func TestResumeInfraEscalateAfterProjectTier(t *testing.T) {
	m := &Manager{}
	m.SetConfig(Config{InfraRetryEscalateAfter: 2})
	setPol(t, m, config.EffectivePolicy{
		InfraRetryEscalateAfter: projectInt(4),
	})
	if got := m.resolveInfraEscalateAfter(testPrd()); got != 4 {
		t.Errorf("infra escalate-after = %d, want 4 (project)", got)
	}
}

func TestResolveFailureEscalationProjectTier(t *testing.T) {
	daemonSteps := []FailureEscalationStep{{Backend: "a", Model: "da", Attempts: 1}}
	m := &Manager{}
	m.SetConfig(Config{FailureEscalation: FailureEscalationPolicy{MaxFailures: 3, Steps: daemonSteps}})

	polSteps := []config.FailureEscalationStepPolicy{{Backend: "b", Model: "pb", Effort: "high", Attempts: 2}}
	setPol(t, m, config.EffectivePolicy{
		FailureEscalation: config.Field[config.FailureEscalationResolved]{
			Value:  config.FailureEscalationResolved{Enabled: true, MaxFailures: 5, Steps: polSteps},
			Source: config.PolicySourceProject,
		},
	})
	eff := m.resolveFailureEscalation(testPrd(), &Task{})
	if len(eff.Steps) != 1 || eff.Steps[0].Backend != "b" || eff.Steps[0].Model != "pb" {
		t.Fatalf("steps = %+v, want the project step", eff.Steps)
	}
	// The project tier did not state max_failures beyond its step, and it did
	// state 5 — it wins over the daemon's 3.
	if eff.MaxFailures != 5 {
		t.Errorf("max failures = %d, want 5", eff.MaxFailures)
	}

	// Unwired: daemon policy untouched.
	m.SetEffectivePolicy(nil)
	eff = m.resolveFailureEscalation(testPrd(), &Task{})
	if len(eff.Steps) != 1 || eff.Steps[0].Backend != "a" {
		t.Fatalf("unwired steps = %+v, want the daemon step", eff.Steps)
	}
	if eff.MaxFailures != 3 {
		t.Errorf("unwired max failures = %d, want 3", eff.MaxFailures)
	}
}

func TestLaneFillApplyProjectCaps(t *testing.T) {
	m := &Manager{}
	m.SetLaneFillPolicyCaps(func() (int, int) { return 1, 2 })
	c := &Coordinator{m: m}
	cfg := LaneFillConfig{GPULanes: 8, ClaudeLanes: 8}
	counts := LaneFillLaneCounts{
		GPU:    LaneFillLaneCount{Configured: 8, Effective: 8, Source: LaneCountSourceConfig},
		Claude: LaneFillLaneCount{Configured: 8, Effective: 8, Source: LaneCountSourceCaps},
	}
	cfg, counts = c.laneFillApplyProjectCaps(cfg, counts)
	if counts.GPU.Effective != 1 || cfg.GPULanes != 1 {
		t.Errorf("gpu = %d (cfg %d), want 1", counts.GPU.Effective, cfg.GPULanes)
	}
	if counts.Claude.Effective != 2 || cfg.ClaudeLanes != 2 {
		t.Errorf("claude = %d (cfg %d), want 2", counts.Claude.Effective, cfg.ClaudeLanes)
	}

	// Never raises: a cap above the effective count is ignored.
	m.SetLaneFillPolicyCaps(func() (int, int) { return 50, 50 })
	cfg = LaneFillConfig{GPULanes: 2, ClaudeLanes: 2}
	counts = LaneFillLaneCounts{
		GPU:    LaneFillLaneCount{Configured: 2, Effective: 2, Source: LaneCountSourceConfig},
		Claude: LaneFillLaneCount{Configured: 2, Effective: 2, Source: LaneCountSourceConfig},
	}
	cfg, counts = c.laneFillApplyProjectCaps(cfg, counts)
	if counts.GPU.Effective != 2 || counts.Claude.Effective != 2 {
		t.Errorf("caps raised counts: gpu=%d claude=%d", counts.GPU.Effective, counts.Claude.Effective)
	}

	// Unwired: no clamp at all.
	c2 := &Coordinator{m: &Manager{}}
	cfg, counts = c2.laneFillApplyProjectCaps(cfg, counts)
	if counts.GPU.Effective != 2 || counts.Claude.Effective != 2 {
		t.Errorf("unwired clamped counts: gpu=%d claude=%d", counts.GPU.Effective, counts.Claude.Effective)
	}
}

// End to end: the daemon's real wiring shape — the PRD's project policy
// goes through config.ResolveProjectPolicy and the reader sees it, and the
// PRD's own override still beats the project.
func TestEffectivePolicyEndToEndResolver(t *testing.T) {
	m := &Manager{}
	m.SetConfig(Config{PlanReviewMode: "warn", RetryCeilingTotal: 0})
	fn := func(prd *PRD) config.EffectivePolicy {
		proj := &config.ProjectPolicy{PlanReviewMode: strPtr("block"), RetryCeilingTotal: intPtr(6)}
		auto := config.AutomatonOverrides{}
		if prd != nil {
			auto = prd.PolicyOverrides()
		}
		return config.ResolveProjectPolicy(config.DaemonPolicySource{
			Autonomous: config.AutonomousConfig{PlanReviewMode: "warn"},
		}, proj, auto)
	}
	m.SetEffectivePolicy(fn)
	if got := m.effectivePlanReviewMode(testPrd()); got != "block" {
		t.Errorf("plan review mode = %q, want block (resolver)", got)
	}
	if got := m.resolveRetryCeiling(testPrd()); got != 6 {
		t.Errorf("retry ceiling = %d, want 6 (resolver)", got)
	}
	// Automaton tier still on top: explicit PRD ceiling wins.
	if got := m.resolveRetryCeiling(&PRD{ID: "a", RetryCeilingTotal: 9}); got != 9 {
		t.Errorf("retry ceiling = %d, want 9 (automaton over project)", got)
	}
	// No project policy at all: the daemon value holds.
	fn = func(*PRD) config.EffectivePolicy {
		return config.ResolveProjectPolicy(config.DaemonPolicySource{
			Autonomous: config.AutonomousConfig{PlanReviewMode: "warn"},
		}, nil, config.AutomatonOverrides{})
	}
	m.SetEffectivePolicy(fn)
	if got := m.effectivePlanReviewMode(testPrd()); got != "warn" {
		t.Errorf("plan review mode = %q, want warn (no project policy)", got)
	}
}

func intPtr(v int) *int { return &v }
