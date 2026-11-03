// project_policy_wire.go — the reader side of the per-project policy tier:
// where the Manager consults config.ResolveProjectPolicy (wired in by the
// daemon with SetEffectivePolicy) instead of reading only the daemon's
// autonomous.* defaults. Every helper uses a resolved value only when a
// higher tier (project or Automaton) actually supplied it; a PRD with no
// project policy, or a key no higher tier set, falls through to the
// historical read, so absent policy is byte-identical to before.
package autonomous

import (
	"github.com/dmz006/datawatch/internal/config"
)

// PolicyFn resolves the effective policy of one PRD with the daemon tier,
// the PRD's project tier, and the PRD's own Automaton overrides
// (PRD.PolicyOverrides) already merged. fn must be cheap and must not take
// Manager.mu: the helpers below call it lock-free, and none of the sites
// that call the helpers holds m.mu across the call.
type PolicyFn func(prd *PRD) config.EffectivePolicy

// SetEffectivePolicy wires the three-tier policy resolver into the
// Manager (nil unwires it). The daemon calls it once at startup; readers
// pick the resolver up on their next field read. It also feeds the
// resolveTaskConcurrency project hook from the same resolver, so the
// Automaton > project > daemon precedence there is exactly the resolver's.
func (m *Manager) SetEffectivePolicy(fn PolicyFn) {
	if m == nil {
		return
	}
	if fn == nil {
		m.effectivePolicy.Store(nil)
		m.projectPolicyConcurrency = nil
		return
	}
	copied := fn
	m.effectivePolicy.Store(&copied)
	m.projectPolicyConcurrency = func(prd *PRD) (int, bool) {
		pol, ok := m.effectivePolicyOf(prd)
		if !ok || !policyProjectTier(pol.MaxConcurrentTasks.Source) {
			return 0, false
		}
		if v := pol.MaxConcurrentTasks.Value; v > 0 {
			return v, true
		}
		return 0, false
	}
}

// effectivePolicyOf returns the resolved policy of prd and whether a
// resolver is wired at all. ok=false means "unwired": callers must fall
// through to the historical read.
func (m *Manager) effectivePolicyOf(prd *PRD) (config.EffectivePolicy, bool) {
	if m == nil {
		return config.EffectivePolicy{}, false
	}
	p := m.effectivePolicy.Load()
	if p == nil || *p == nil {
		return config.EffectivePolicy{}, false
	}
	return (*p)(prd), true
}

// policyProjectTier reports whether the resolved field value came from the
// project or the Automaton tier — anything but the daemon default.
func policyProjectTier(src config.PolicySource) bool {
	return src == config.PolicySourceProject || src == config.PolicySourceAutomaton
}

// effectiveRetryPolicyEntry looks up one category in the project/ Automaton
// retry policy. It reports found=false unless a higher tier set a retry
// policy that actually names the category: a key absent from a higher tier's
// map must fall through to the tier below (the resolver replaces the
// daemon's map wholesale when a tier sets one, it never merges maps).
func (m *Manager) effectiveRetryPolicyEntry(prd *PRD, cat string) (int, bool) {
	pol, ok := m.effectivePolicyOf(prd)
	if !ok || !policyProjectTier(pol.RetryPolicy.Source) {
		return 0, false
	}
	v, found := pol.RetryPolicy.Value[cat]
	return v, found
}

// effectivePlanReviewMode is the plan review mode in force for prd: a
// project/ Automaton tier sets it when it does; else the daemon's
// autonomous.plan_review_mode. The value is normalized either way, as the
// historical read was.
func (m *Manager) effectivePlanReviewMode(prd *PRD) string {
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.PlanReviewMode.Source) {
		return normalizePlanReviewMode(pol.PlanReviewMode.Value)
	}
	return normalizePlanReviewMode(m.Config().PlanReviewMode)
}

// effectiveScopeLint is the scope-lint mode in force for prd: a
// project/ Automaton tier sets it when it does; else the daemon's
// autonomous.scope_lint (default enforce).
func (m *Manager) effectiveScopeLint(prd *PRD) string {
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.ScopeLint.Source) {
		return pol.ScopeLint.Value
	}
	return m.scopeLintMode()
}

// effectivePlanCoverage is the plan coverage gate in force for prd: the
// project/ Automaton tier's check or ratio when either is set (the resolver
// already carries the daemon value behind any key the tier leaves unset);
// else the historical daemon read. Errors are the same either way: the
// same config.EffectivePlanCoverage normalization runs on both paths.
func (m *Manager) effectivePlanCoverage(prd *PRD) (config.PlanCoverageSettings, error) {
	if pol, ok := m.effectivePolicyOf(prd); ok &&
		(policyProjectTier(pol.PlanCoverageCheck.Source) || policyProjectTier(pol.PlanCoverageMinRatio.Source)) {
		return config.EffectivePlanCoverage(pol.PlanCoverageCheck.Value, pol.PlanCoverageMinRatio.Value)
	}
	return m.EffectivePlanCoverage()
}

// effectiveUncommittedWorkPolicy is the uncommitted-work policy in force
// for prd (a project/ Automaton tier when set, else the daemon's).
func (m *Manager) effectiveUncommittedWorkPolicy(prd *PRD) string {
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.UncommittedWorkPolicy.Source) {
		return pol.UncommittedWorkPolicy.Value
	}
	return m.Config().Automation.EffectiveUncommittedWorkPolicy()
}

// effectiveUncommittedWorkScope is the uncommitted-work scope in force for
// prd (a project/ Automaton tier when set, else the daemon's).
func (m *Manager) effectiveUncommittedWorkScope(prd *PRD) string {
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.UncommittedWorkScope.Source) {
		return pol.UncommittedWorkScope.Value
	}
	return m.Config().Automation.EffectiveUncommittedWorkScope()
}

// effectiveWorktreePerTask is the per-task-worktree switch in force for
// prd (a project/ Automaton tier when set, else the daemon's).
func (m *Manager) effectiveWorktreePerTask(prd *PRD) bool {
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.ParallelWorktreePerTask.Source) {
		return pol.ParallelWorktreePerTask.Value
	}
	return EffectiveWorktreePerTask(m.Config().Parallel)
}

// effectiveReadOnlyTasks is the read-only task parallelism switch in force
// for prd (a project/ Automaton tier when set, else the daemon's).
func (m *Manager) effectiveReadOnlyTasks(prd *PRD) bool {
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.ParallelReadOnlyTasks.Source) {
		return pol.ParallelReadOnlyTasks.Value
	}
	return EffectiveReadOnlyTasks(m.Config().Parallel)
}

// effectiveFailureEscalation is the hold-for-review policy the project or
// Automaton tier set for prd (per-field across tiers, resolved by the
// daemon), or nil when no higher tier supplies anything — the historical
// "global, then PRD, then task" path is untouched in that case.
func (m *Manager) effectiveFailureEscalation(prd *PRD) *FailureEscalationPolicy {
	pol, ok := m.effectivePolicyOf(prd)
	if !ok || !policyProjectTier(pol.FailureEscalation.Source) {
		return nil
	}
	r := pol.FailureEscalation.Value
	enabled := r.Enabled
	return &FailureEscalationPolicy{
		Enabled:     &enabled,
		MaxFailures: r.MaxFailures,
		Steps:       failureEscalationStepsFromPolicy(r.Steps),
	}
}

// projectFailureEscalation merges the higher tier's policy over the daemon
// default: fields the higher tier resolved (explicitly) win, unset ones
// (Enabled nil / MaxFailures 0 / no steps) keep the daemon value.
func projectFailureEscalation(base FailureEscalationPolicy, pe *FailureEscalationPolicy) FailureEscalationPolicy {
	if pe.Enabled == nil && pe.MaxFailures == 0 && len(pe.Steps) == 0 {
		return base
	}
	g := base
	if pe.Enabled != nil {
		g.Enabled = pe.Enabled
	}
	if pe.MaxFailures > 0 {
		g.MaxFailures = pe.MaxFailures
	}
	if len(pe.Steps) > 0 {
		g.Steps = pe.Steps
	}
	return g
}

func failureEscalationStepsFromPolicy(steps []config.FailureEscalationStepPolicy) []FailureEscalationStep {
	if len(steps) == 0 {
		return nil
	}
	out := make([]FailureEscalationStep, len(steps))
	for i, s := range steps {
		out[i] = FailureEscalationStep{
			Backend:  s.Backend,
			Model:    s.Model,
			Effort:   Effort(s.Effort),
			Attempts: s.Attempts,
		}
	}
	return out
}

// effectiveDefaultAutoPR is the AutoPR default for a PRD at creation: a
// project/ Automaton tier when set, else the daemon's
// autonomous.default_auto_pr.
func (m *Manager) effectiveDefaultAutoPR(prd *PRD) bool {
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.DefaultAutoPR.Source) {
		return pol.DefaultAutoPR.Value
	}
	m.mu.Lock()
	v := m.cfg.DefaultAutoPR
	m.mu.Unlock()
	return v
}

// effectiveSpawnRetries is the per-run auto-fix re-prompt budget in force
// for prd: a project/ Automaton tier when it sets auto_fix_retries, else
// the daemon's autonomous.auto_fix_retries (read raw, as the historical
// reader did — no default backstop, a negative value clamped by the
// caller).
func (m *Manager) effectiveSpawnRetries(prd *PRD) int {
	if pol, ok := m.effectivePolicyOf(prd); ok && policyProjectTier(pol.AutoFixRetries.Source) {
		return pol.AutoFixRetries.Value
	}
	m.mu.Lock()
	v := m.cfg.AutoFixRetries
	m.mu.Unlock()
	return v
}

// --- lane fill project caps ------------------------------------------------

// laneFillPolicyCapFn returns the (GPU, Claude) lane-count caps; 0 in a
// position means "no cap there" (or no project sets one).
type laneFillPolicyCapFn func() (gpu, claude int)

// SetLaneFillPolicyCaps wires the floor of the projects' lane_fill_*
// counts into the Manager (nil unwires it). The lane-fill lanes are a
// shared budget the per-project counts cap: when a project's policy sets a
// lower lane count, the daemon runs the shared budget at no more than that
// count, so its Automata fit.
func (m *Manager) SetLaneFillPolicyCaps(fn laneFillPolicyCapFn) {
	if m == nil {
		return
	}
	if fn == nil {
		m.laneFillCaps.Store(nil)
		return
	}
	copied := fn
	m.laneFillCaps.Store(&copied)
}

// laneFillCaps reads the current project lane caps; (0,0) means unwired or
// no project sets them.
func (m *Manager) laneFillProjectCaps() (gpu, claude int) {
	if m == nil {
		return 0, 0
	}
	fn := m.laneFillCaps.Load()
	if fn == nil || *fn == nil {
		return 0, 0
	}
	return (*fn)()
}

// laneFillApplyProjectCaps clamps the poll's shared lane counts to the
// projects' lower counts. It never raises a count. The count's reported
// source becomes the config (the clamp is a configured ceiling), with the
// reason saying so.
func (c *Coordinator) laneFillApplyProjectCaps(cfg LaneFillConfig, counts LaneFillLaneCounts) (LaneFillConfig, LaneFillLaneCounts) {
	if c == nil || c.m == nil {
		return cfg, counts
	}
	gpuCap, claudeCap := c.m.laneFillProjectCaps()
	if gpuCap > 0 && gpuCap < counts.GPU.Effective {
		counts.GPU.Effective = gpuCap
		counts.GPU.Source = LaneCountSourceConfig
		counts.GPU.Reason = "capped by a project's policy lane_fill_gpu_lanes"
		cfg.GPULanes = gpuCap
	}
	if claudeCap > 0 && claudeCap < counts.Claude.Effective {
		counts.Claude.Effective = claudeCap
		counts.Claude.Source = LaneCountSourceConfig
		counts.Claude.Reason = "capped by a project's policy lane_fill_claude_lanes"
		cfg.ClaudeLanes = claudeCap
	}
	return cfg, counts
}
