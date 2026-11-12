// project_policy.go — the per-project policy tier: the override block a
// project entry can carry between the daemon-wide autonomous.* defaults
// and the per-Automaton (PRD) override fields.
//
// The config package cannot import internal/autonomous (autonomous imports
// config), so this file is the config-level type that layer needs: the
// struct, its validation, and the three-tier resolver all live here.
// internal/autonomous adapts a PRD's own override fields into
// AutomatonOverrides before calling ResolveProjectPolicy.
//
// Precedence, highest first:
//
//	automaton (the specific Automaton's own override fields)
//	  > project (the entry's policy block)
//	  > daemon  (autonomous.* defaults; ask_on_change from usage.burn_guard)
//
// Every field of ProjectPolicy is a pointer or map; nil (key absent) means
// "inherit the daemon value". A set field carries a concrete value and must
// be valid: unknown values are validation errors, never silent fall backs.
package config

import (
	"fmt"
	"strings"
)

// PolicySource names which tier supplied one resolved value.
type PolicySource string

const (
	// PolicySourceAutomaton: the specific Automaton's own override field.
	PolicySourceAutomaton PolicySource = "automaton"
	// PolicySourceProject: the project's policy block set it.
	PolicySourceProject PolicySource = "project"
	// PolicySourceDaemon: no higher tier set it; the daemon default holds.
	PolicySourceDaemon PolicySource = "daemon"
)

// Field is one resolved policy value with the tier that supplied it.
type Field[T any] struct {
	Value  T
	Source PolicySource
}

// Plan-review modes (autonomous.plan_review_mode). The autonomous package
// defines the same spellings for the same key; both sides must accept
// off|warn|block.
const (
	PlanReviewPolicyOff   = "off"
	PlanReviewPolicyWarn  = "warn"
	PlanReviewPolicyBlock = "block"
)

// RetryPolicyCategories are the failure categories a retry_policy budget
// map may name. A category outside this set is a validation error so a
// typo cannot quietly go unspent.
var RetryPolicyCategories = []string{"infra", "content", "scope", "policy"}

// ProjectPolicy is the per-project autonomous policy override block
// (projects.<name>.policy). A nil *ProjectPolicy, or a key absent in one,
// leaves that value inherited from the daemon tier; ResolveProjectPolicy
// never reads a zero value of a set field as "unset" — set means set and
// must be valid.
//
// Daemon-wide scope stays daemon-wide on purpose: timeouts, cadence and
// backend selection (session and lane timeouts, poll and idle-alert
// intervals, the gpu and claude backend/model picks, the default backend,
// worktree base and dir) remain autonomous.* keys and are not overridable
// per project.
type ProjectPolicy struct {
	// --- plan checks ---

	// PlanCoverageCheck: off | warn | block.
	PlanCoverageCheck *string `yaml:"plan_coverage_check,omitempty" json:"plan_coverage_check,omitempty"`
	// PlanCoverageMinRatio: 0 < r <= 1 (1 requires full coverage).
	PlanCoverageMinRatio *float64 `yaml:"plan_coverage_min_ratio,omitempty" json:"plan_coverage_min_ratio,omitempty"`
	// PlanReviewMode: off | warn | block.
	PlanReviewMode *string `yaml:"plan_review_mode,omitempty" json:"plan_review_mode,omitempty"`
	// ScopeLint: off | warn | enforce.
	ScopeLint *string `yaml:"scope_lint,omitempty" json:"scope_lint,omitempty"`

	// --- retry and escalation ---

	// RetryPolicy maps a category (infra|content|scope|policy) to its
	// auto-retry budget. Non-nil replaces the daemon's map wholesale; a
	// category absent from it falls through (including the
	// content/policy→AutoFixRetries fallback, applied downstream).
	RetryPolicy map[string]int `yaml:"retry_policy,omitempty" json:"retry_policy,omitempty"`
	// RetryCeilingTotal caps auto-retries summed across a whole Automaton's
	// lifecycle.
	RetryCeilingTotal *int `yaml:"retry_ceiling_total,omitempty" json:"retry_ceiling_total,omitempty"`
	// AutoFixRetries is the per-verifier-failure re-prompt budget (and the
	// content/policy fallback).
	AutoFixRetries *int `yaml:"auto_fix_retries,omitempty" json:"auto_fix_retries,omitempty"`
	// InfraRetryEscalateAfter is the infra-failure count after which a task
	// stops auto-retrying and is held.
	InfraRetryEscalateAfter *int `yaml:"infra_retry_escalate_after,omitempty" json:"infra_retry_escalate_after,omitempty"`
	// FailureEscalation: the hold-for-review policy; each sub-field is
	// resolved independently across tiers.
	FailureEscalation *FailureEscalationLevel `yaml:"failure_escalation,omitempty" json:"failure_escalation,omitempty"`

	// --- resources ---

	// MemoryLimit: the per-project memory ceiling override; a positive MB
	// number, the word "none", or unset for the default.
	MemoryLimit *string `yaml:"memory_limit,omitempty" json:"memory_limit,omitempty"`

	// --- git defaults ---

	// DefaultAutoPR turns auto-PR on or off for this project's Automata.
	DefaultAutoPR *bool `yaml:"default_auto_pr,omitempty" json:"default_auto_pr,omitempty"`

	// --- lane limits ---

	// MaxConcurrentTasks: at least 1.
	MaxConcurrentTasks *int `yaml:"max_concurrent_tasks,omitempty" json:"max_concurrent_tasks,omitempty"`
	// PlanTierMaxInflight: per-plan-tier inflight caps; non-nil replaces
	// the daemon's map.
	PlanTierMaxInflight map[string]int `yaml:"plan_tier_max_inflight,omitempty" json:"plan_tier_max_inflight,omitempty"`
	// LaneFillGPULanes / LaneFillClaudeLanes: 0 turns the lane kind off.
	LaneFillGPULanes    *int `yaml:"lane_fill_gpu_lanes,omitempty" json:"lane_fill_gpu_lanes,omitempty"`
	LaneFillClaudeLanes *int `yaml:"lane_fill_claude_lanes,omitempty" json:"lane_fill_claude_lanes,omitempty"`

	// --- recent autonomous keys the coordinator reads ---

	// UncommittedWorkPolicy: hold | warn | off (what the post-verification
	// uncommitted-work check does; daemon-side key
	// autonomous.automation.uncommitted_work.policy).
	UncommittedWorkPolicy *string `yaml:"uncommitted_work_policy,omitempty" json:"uncommitted_work_policy,omitempty"`
	// UncommittedWorkScope: task_files | worktree (which files that check
	// covers; daemon-side key autonomous.automation.uncommitted_work.scope).
	UncommittedWorkScope *string `yaml:"uncommitted_work_scope,omitempty" json:"uncommitted_work_scope,omitempty"`
	// BurnGuardAskOnChange: true asks one open question per guard rung
	// change, false alerts only (daemon-side key usage.burn_guard.ask_on_change).
	BurnGuardAskOnChange *bool `yaml:"burn_guard_ask_on_change,omitempty" json:"burn_guard_ask_on_change,omitempty"`
	// ParallelReadOnlyTasks: read-only tasks skip the tree lock
	// (daemon-side key autonomous.parallel.read_only_tasks).
	ParallelReadOnlyTasks *bool `yaml:"parallel_read_only_tasks,omitempty" json:"parallel_read_only_tasks,omitempty"`
	// ParallelWorktreePerTask: each parallel task runs in its own worktree
	// (daemon-side key autonomous.parallel.worktree_per_task).
	ParallelWorktreePerTask *bool `yaml:"parallel_worktree_per_task,omitempty" json:"parallel_worktree_per_task,omitempty"`

	// PlatformContextEnabled overrides the global platform_context.enabled for
	// this project's sessions (nil = use the global value).
	PlatformContextEnabled *bool `yaml:"platform_context_enabled,omitempty" json:"platform_context_enabled,omitempty"`

	// ProjectGateCommand overrides autonomous.project_gate_command for this
	// project (nil = inherit the daemon value; an empty string explicitly
	// unsets it for the project). Same validity rules as the daemon key.
	ProjectGateCommand *string `yaml:"project_gate_command,omitempty" json:"project_gate_command,omitempty"`
}

// FailureEscalationLevel is one tier's view of the failure escalation
// (hold-for-review) policy. Each sub-field is independent: nil (or 0, or
// empty Steps) means "not set at this tier" and the tier below supplies it
// — the same per-field inheritance the autonomous package applies between
// its global/Automaton/task levels.
type FailureEscalationLevel struct {
	// Enabled turns escalation on or off. Nil = not set at this tier.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// MaxFailures is the failed-attempt count that holds a task for review.
	// Nil or 0 = not set at this tier; when set, 1 or more.
	MaxFailures *int `yaml:"max_failures,omitempty" json:"max_failures,omitempty"`
	// Steps is the escalation ladder. Empty = not set at this tier; a
	// ladder from one tier replaces the ladder below, never merges into it.
	Steps []FailureEscalationStepPolicy `yaml:"steps,omitempty" json:"steps,omitempty"`
}

// FailureEscalationStepPolicy is one rung of an escalation ladder in a
// policy tier.
type FailureEscalationStepPolicy struct {
	// Backend and Model name the worker for the rung; empty Backend means
	// the task's normally resolved worker.
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`
	// Model for the rung; empty = the task's model.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Effort for the rung; empty = the task's effort.
	Effort string `yaml:"effort,omitempty" json:"effort,omitempty"`
	// Attempts the rung covers; 0 reads as 1.
	Attempts int `yaml:"attempts,omitempty" json:"attempts,omitempty"`
}

// FailureEscalationResolved is the merged result the resolver returns for
// the failure escalation policy: one concrete value per field.
// MaxFailures 0 keeps the daemon's "0 reads as the seeded default" meaning
// so a consumer can apply it unchanged.
type FailureEscalationResolved struct {
	Enabled     bool
	MaxFailures int
	Steps       []FailureEscalationStepPolicy
}

// DaemonPolicySource is the daemon tier the resolver merges: the
// autonomous.* config plus the one usage-scoped key the coordinator reads
// (ask_on_change). BurnGuard is a value because the usage.burn_guard block
// is always present in a loaded config (nil reads as absent = false).
type DaemonPolicySource struct {
	Autonomous AutonomousConfig
	BurnGuard  BurnGuardConfig
}

// AutomatonOverrides is the automaton tier the resolver merges: a PRD's own
// override fields expressed at config scope. A nil field (or a key absent
// from RetryPolicy) means "not set on this Automaton". internal/autonomous
// adapts a PRD into it; the resolver applies automaton > project > daemon
// per field.
type AutomatonOverrides struct {
	RetryPolicy        map[string]int
	RetryCeilingTotal  *int
	MaxConcurrentTasks *int
	FailureEscalation  *FailureEscalationLevel
	DefaultAutoPR      *bool
}

// EffectivePolicy is what one Automaton of one project actually runs with:
// one value per policy key, each tagged with the tier that supplied it.
type EffectivePolicy struct {
	// Plan checks (values normalized to their canonical set).
	PlanCoverageCheck    Field[string]
	PlanCoverageMinRatio Field[float64]
	PlanReviewMode       Field[string]
	ScopeLint            Field[string]

	// Retry and escalation.
	RetryPolicy             Field[map[string]int]
	RetryCeilingTotal       Field[int]
	AutoFixRetries          Field[int]
	InfraRetryEscalateAfter Field[int]
	FailureEscalation       Field[FailureEscalationResolved]

	// Git default.
	DefaultAutoPR Field[bool]

	// Lane limits.
	MaxConcurrentTasks  Field[int]
	PlanTierMaxInflight Field[map[string]int]
	LaneFillGPULanes    Field[int]
	LaneFillClaudeLanes Field[int]

	// Recent autonomous keys.
	UncommittedWorkPolicy   Field[string]
	UncommittedWorkScope    Field[string]
	BurnGuardAskOnChange    Field[bool]
	ParallelReadOnlyTasks   Field[bool]
	ParallelWorktreePerTask Field[bool]

	// Project gate command (empty = unset).
	ProjectGateCommand Field[string]
}

// ValidateProjectPolicy checks that every field set in p is a value the key
// actually knows. An unknown value is a validation error: it must never
// silently mean its neighbour. keyPrefix is the setting path surfaced in
// errors; empty defaults to "policy" which reads well under
// projects.<name>.policy too.
func ValidateProjectPolicy(p *ProjectPolicy, keyPrefix string) error {
	if p == nil {
		return nil
	}
	keyPath := keyPrefix
	if keyPath == "" {
		keyPath = "policy"
	}
	var first error
	fatal := func(format string, args ...any) {
		if first == nil {
			first = fmt.Errorf(format, args...)
		}
	}

	if v := p.PlanCoverageCheck; v != nil {
		if _, err := NormalizePlanCoverageCheck(*v); err != nil {
			fatal("%s.plan_coverage_check: %v", keyPath, err)
		}
	}
	if v := p.PlanCoverageMinRatio; v != nil {
		if *v <= 0 || *v > 1 {
			fatal("%s.plan_coverage_min_ratio: %v is invalid; use a value in (0, 1]", keyPath, *v)
		}
	}
	if v := p.PlanReviewMode; v != nil {
		if mode := strings.ToLower(strings.TrimSpace(*v)); mode != PlanReviewPolicyOff && mode != PlanReviewPolicyWarn && mode != PlanReviewPolicyBlock {
			fatal("%s.plan_review_mode: %q is invalid; use off, warn or block", keyPath, *v)
		}
	}
	if v := p.ScopeLint; v != nil {
		if *v == "" {
			fatal("%s.scope_lint: must be one of off, warn, enforce", keyPath)
		} else if err := ValidateScopeLint(*v); err != nil {
			fatal("%s.scope_lint: %v", keyPath, err)
		}
	}

	for k, v := range p.RetryPolicy {
		known := false
		for _, c := range RetryPolicyCategories {
			if k == c {
				known = true
				break
			}
		}
		if !known {
			fatal("%s.retry_policy: unknown category %q (valid: %s)", keyPath, k, strings.Join(RetryPolicyCategories, ", "))
		}
		if v < 0 {
			fatal("%s.retry_policy[%s]: %d is invalid; the budget is 0 or more", keyPath, k, v)
		}
	}
	countField := func(name string, v *int, min int) {
		if v == nil {
			return
		}
		if *v < min {
			fatal("%s.%s: %d is invalid; use %d or more", keyPath, name, *v, min)
		}
	}
	countField("retry_ceiling_total", p.RetryCeilingTotal, 0)
	countField("auto_fix_retries", p.AutoFixRetries, 0)
	countField("infra_retry_escalate_after", p.InfraRetryEscalateAfter, 0)
	if p.FailureEscalation != nil {
		if err := ValidateProjectPolicyFailureEscalation(p.FailureEscalation, keyPath); err != nil {
			fatal("%v", err)
		}
	}
	countField("max_concurrent_tasks", p.MaxConcurrentTasks, 1)
	if v := p.MemoryLimit; v != nil {
		if err := ValidateMemoryLimit(*v); err != nil {
			fatal("%s.memory_limit: %v", keyPath, err)
		}
	}
	for k, v := range p.PlanTierMaxInflight {
		if v < 0 {
			fatal("%s.plan_tier_max_inflight[%s]: %d is invalid; use 0 (unlimited) or more", keyPath, k, v)
		}
	}
	for name, v := range map[string]*int{
		"lane_fill_gpu_lanes":    p.LaneFillGPULanes,
		"lane_fill_claude_lanes": p.LaneFillClaudeLanes,
	} {
		if v != nil && *v < 0 {
			fatal("%s.%s: %d is invalid; 0 turns the lane off, negatives are not", keyPath, name, *v)
		}
	}
	if v := p.UncommittedWorkPolicy; v != nil {
		if *v != UncommittedWorkPolicyHold && *v != UncommittedWorkPolicyWarn && *v != UncommittedWorkPolicyOff {
			fatal("%s.uncommitted_work_policy: %q is invalid; use hold, warn or off", keyPath, *v)
		}
	}
	if v := p.UncommittedWorkScope; v != nil {
		if *v != UncommittedWorkScopeTaskFiles && *v != UncommittedWorkScopeWorktree {
			fatal("%s.uncommitted_work_scope: %q is invalid; use task_files or worktree", keyPath, *v)
		}
	}
	if v := p.ProjectGateCommand; v != nil {
		if err := ValidateProjectGateCommand(keyPath, *v); err != nil {
			fatal("%v", err)
		}
	}
	return first
}

// Validate reports whether p is valid at the default setting path.
func (p *ProjectPolicy) Validate() error { return ValidateProjectPolicy(p, "") }

// ValidateProjectPolicyFailureEscalation checks one escalation level at the
// setting path under keyPath (keyPath.failure_escalation).
func ValidateProjectPolicyFailureEscalation(l *FailureEscalationLevel, keyPath string) error {
	if l == nil {
		return nil
	}
	if l.MaxFailures != nil && *l.MaxFailures < 0 {
		return fmt.Errorf("%s.failure_escalation.max_failures: %d is invalid; 0 means not set, otherwise use 1 or more",
			keyPath, *l.MaxFailures)
	}
	if len(l.Steps) > FailureEscalationMaxSteps {
		return fmt.Errorf("%s.failure_escalation.steps: %d steps is too many; at most %d are allowed",
			keyPath, len(l.Steps), FailureEscalationMaxSteps)
	}
	for i, s := range l.Steps {
		if s.Attempts < 0 {
			return fmt.Errorf("%s.failure_escalation.steps[%d].attempts: %d is invalid; 0 reads as 1",
				keyPath, i, s.Attempts)
		}
	}
	return nil
}

// --- daemon tier ------------------------------------------------------------

// daemonPolicyValues pulls the daemon tier out of the source: every value
// is already the daemon's effective read (its own defaults applied), so
// "daemon" always holds one concrete value per key.
type daemonPolicyValues struct {
	planCoverageCheck       string
	planCoverageMinRatio    float64
	planReviewMode          string
	scopeLint               string
	retryPolicy             map[string]int
	retryCeilingTotal       int
	autoFixRetries          int
	infraRetryEscalateAfter int
	failureEscalation       FailureEscalationResolved
	defaultAutoPR           bool
	maxConcurrentTasks      int
	planTierMaxInflight     map[string]int
	laneFillGPULanes        int
	laneFillClaudeLanes     int
	uncommittedWorkPolicy   string
	uncommittedWorkScope    string
	burnGuardAskOnChange    bool
	parallelReadOnlyTasks   bool
	parallelWorktreePerTask bool
	projectGateCommand      string
}

func newDaemonPolicyValues(src DaemonPolicySource) daemonPolicyValues {
	a := src.Autonomous
	v := daemonPolicyValues{}

	// The daemon's own canonical resolver applies its defaults
	// (empty mode → block; empty → 0 for the ratio).
	if eff, err := a.EffectivePlanCoverage(); err == nil {
		v.planCoverageCheck, v.planCoverageMinRatio = eff.Mode, eff.MinRatio
	} else {
		v.planCoverageCheck, v.planCoverageMinRatio = PlanCoverageBlock, 1.0
	}
	switch strings.ToLower(strings.TrimSpace(a.PlanReviewMode)) {
	case PlanReviewPolicyWarn:
		v.planReviewMode = PlanReviewPolicyWarn
	case PlanReviewPolicyBlock:
		v.planReviewMode = PlanReviewPolicyBlock
	default:
		v.planReviewMode = PlanReviewPolicyOff // "" is off by default; garbage stays off
	}

	v.scopeLint = a.ScopeLintMode()
	v.retryPolicy = a.RetryPolicy
	if v.retryPolicy == nil {
		v.retryPolicy = map[string]int{}
	}
	v.retryCeilingTotal = a.RetryCeilingTotal
	v.autoFixRetries = a.AutoFixRetries
	v.infraRetryEscalateAfter = a.InfraRetryEscalateAfter
	v.failureEscalation = FailureEscalationResolved{
		Enabled:     a.FailureEscalation.Enabled,
		MaxFailures: a.FailureEscalation.MaxFailures,
		Steps:       failureEscalationStepsFromConfig(a.FailureEscalation.Steps),
	}
	v.defaultAutoPR = a.DefaultAutoPR
	if v.maxConcurrentTasks = a.MaxConcurrentTasks; v.maxConcurrentTasks <= 0 {
		v.maxConcurrentTasks = 1 // the daemon reads 0 as "sequential"
	}
	v.planTierMaxInflight = a.PlanTierMaxInflight
	if v.planTierMaxInflight == nil {
		v.planTierMaxInflight = map[string]int{}
	}
	v.laneFillGPULanes = a.LaneFill.GPULanes
	v.laneFillClaudeLanes = a.LaneFill.ClaudeLanes
	automation := a.Automation
	v.uncommittedWorkPolicy = automation.EffectiveUncommittedWorkPolicy()
	v.uncommittedWorkScope = automation.EffectiveUncommittedWorkScope()
	v.burnGuardAskOnChange = src.BurnGuard.AskOnChange
	v.parallelReadOnlyTasks = policyPointerOr(a.Parallel.ReadOnlyTasks, true)
	v.parallelWorktreePerTask = policyPointerOr(a.Parallel.WorktreePerTask, true)
	v.projectGateCommand = strings.TrimSpace(a.ProjectGateCommand)
	return v
}

func failureEscalationStepsFromConfig(steps []FailureEscalationStepConfig) []FailureEscalationStepPolicy {
	if len(steps) == 0 {
		return nil
	}
	out := make([]FailureEscalationStepPolicy, 0, len(steps))
	for _, s := range steps {
		out = append(out, FailureEscalationStepPolicy(s))
	}
	return out
}

func policyPointerOr(v *bool, absentDefault bool) bool {
	if v == nil {
		return absentDefault
	}
	return *v
}

// --- resolver ----------------------------------------------------------------

// ResolveProjectPolicy merges the three tiers for one Automaton of one
// project. Precedence per key: automaton > project > daemon. Every returned
// value carries the tier that supplied it (automaton/project/daemon) so an
// operator can answer "where did this value come from?" per key.
//
// daemon is the daemon tier (autonomous.* defaults plus ask_on_change);
// project is the project entry's policy block — nil means a plain project
// with no overrides, so every key resolves from the daemon tier with source
// "daemon"; automaton is this specific Automaton's override fields.
func ResolveProjectPolicy(daemon DaemonPolicySource, project *ProjectPolicy, automaton AutomatonOverrides) EffectivePolicy {
	d := newDaemonPolicyValues(daemon)

	// stringField resolves one string-valued key across the project and
	// daemon tiers (no automaton-tier field of this kind exists on a PRD
	// today): the project value if set, else the daemon value, each
	// normalized to its canonical spelling.
	stringField := func(projectValue *string, daemonValue string, normalize func(string) string) Field[string] {
		if projectValue != nil {
			v := *projectValue
			if normalize != nil {
				v = normalize(v)
			}
			return Field[string]{Value: v, Source: PolicySourceProject}
		}
		return Field[string]{Value: daemonValue, Source: PolicySourceDaemon}
	}

	intField := func(daemonVal int, automatonValue, projectValue *int) Field[int] {
		switch {
		case automatonValue != nil:
			return Field[int]{Value: *automatonValue, Source: PolicySourceAutomaton}
		case projectValue != nil:
			return Field[int]{Value: *projectValue, Source: PolicySourceProject}
		default:
			return Field[int]{Value: daemonVal, Source: PolicySourceDaemon}
		}
	}
	boolField := func(daemonVal bool, automatonValue, projectValue *bool) Field[bool] {
		switch {
		case automatonValue != nil:
			return Field[bool]{Value: *automatonValue, Source: PolicySourceAutomaton}
		case projectValue != nil:
			return Field[bool]{Value: *projectValue, Source: PolicySourceProject}
		default:
			return Field[bool]{Value: daemonVal, Source: PolicySourceDaemon}
		}
	}

	policySource := func(automaton, project bool) PolicySource {
		if automaton {
			return PolicySourceAutomaton
		}
		if project {
			return PolicySourceProject
		}
		return PolicySourceDaemon
	}

	planCoverageMinRatioField := func() Field[float64] {
		if project != nil && project.PlanCoverageMinRatio != nil {
			return Field[float64]{Value: *project.PlanCoverageMinRatio, Source: PolicySourceProject}
		}
		return Field[float64]{Value: d.planCoverageMinRatio, Source: PolicySourceDaemon}
	}()
	planTierMaxInflightField := func() Field[map[string]int] {
		if project != nil && len(project.PlanTierMaxInflight) > 0 {
			return Field[map[string]int]{Value: project.PlanTierMaxInflight, Source: PolicySourceProject}
		}
		return Field[map[string]int]{Value: d.planTierMaxInflight, Source: PolicySourceDaemon}
	}()

	return EffectivePolicy{
		// Plan checks.
		PlanCoverageCheck: stringField(projectPolicyPointer(project, func(p *ProjectPolicy) *string { return p.PlanCoverageCheck }),
			d.planCoverageCheck, func(s string) string {
				if m, err := NormalizePlanCoverageCheck(s); err == nil {
					return m
				}
				return s
			}),
		PlanCoverageMinRatio: planCoverageMinRatioField,
		PlanReviewMode: stringField(projectPolicyPointer(project, func(p *ProjectPolicy) *string { return p.PlanReviewMode }),
			d.planReviewMode, normalizePolicyPlanReviewMode),
		ScopeLint: stringField(projectPolicyPointer(project, func(p *ProjectPolicy) *string { return p.ScopeLint }),
			d.scopeLint, nil),

		// Retry and escalation.
		RetryPolicy: Field[map[string]int]{
			Value: func() map[string]int {
				switch {
				case len(automaton.RetryPolicy) > 0:
					return automaton.RetryPolicy
				case project != nil && len(project.RetryPolicy) > 0:
					return project.RetryPolicy
				default:
					return d.retryPolicy
				}
			}(),
			Source: policySource(len(automaton.RetryPolicy) > 0, project != nil && len(project.RetryPolicy) > 0),
		},
		RetryCeilingTotal: intField(d.retryCeilingTotal,
			automaton.RetryCeilingTotal, projectPolicyPointer(project, func(p *ProjectPolicy) *int { return p.RetryCeilingTotal })),
		AutoFixRetries: intField(d.autoFixRetries,
			nil, projectPolicyPointer(project, func(p *ProjectPolicy) *int { return p.AutoFixRetries })),
		InfraRetryEscalateAfter: intField(d.infraRetryEscalateAfter,
			nil, projectPolicyPointer(project, func(p *ProjectPolicy) *int { return p.InfraRetryEscalateAfter })),
		FailureEscalation: policyFailureEscalation(d.failureEscalation,
			escalationLevelOrNil(project), automaton.FailureEscalation),

		// Git default.
		DefaultAutoPR: boolField(d.defaultAutoPR,
			automaton.DefaultAutoPR, projectPolicyPointer(project, func(p *ProjectPolicy) *bool { return p.DefaultAutoPR })),

		// Lane limits.
		MaxConcurrentTasks: intField(d.maxConcurrentTasks,
			automaton.MaxConcurrentTasks, projectPolicyPointer(project, func(p *ProjectPolicy) *int { return p.MaxConcurrentTasks })),
		PlanTierMaxInflight: planTierMaxInflightField,
		LaneFillGPULanes: intField(d.laneFillGPULanes,
			nil, projectPolicyPointer(project, func(p *ProjectPolicy) *int { return p.LaneFillGPULanes })),
		LaneFillClaudeLanes: intField(d.laneFillClaudeLanes,
			nil, projectPolicyPointer(project, func(p *ProjectPolicy) *int { return p.LaneFillClaudeLanes })),

		// Recent autonomous keys.
		UncommittedWorkPolicy: stringField(projectPolicyPointer(project, func(p *ProjectPolicy) *string { return p.UncommittedWorkPolicy }),
			d.uncommittedWorkPolicy, nil),
		UncommittedWorkScope: stringField(projectPolicyPointer(project, func(p *ProjectPolicy) *string { return p.UncommittedWorkScope }),
			d.uncommittedWorkScope, nil),
		BurnGuardAskOnChange: boolField(d.burnGuardAskOnChange,
			nil, projectPolicyPointer(project, func(p *ProjectPolicy) *bool { return p.BurnGuardAskOnChange })),
		ParallelReadOnlyTasks: boolField(d.parallelReadOnlyTasks,
			nil, projectPolicyPointer(project, func(p *ProjectPolicy) *bool { return p.ParallelReadOnlyTasks })),
		ParallelWorktreePerTask: boolField(d.parallelWorktreePerTask,
			nil, projectPolicyPointer(project, func(p *ProjectPolicy) *bool { return p.ParallelWorktreePerTask })),

		// Project gate command: the project's value (even empty, which
		// explicitly unsets it) beats the daemon value.
		ProjectGateCommand: stringField(projectPolicyPointer(project, func(p *ProjectPolicy) *string { return p.ProjectGateCommand }),
			d.projectGateCommand, strings.TrimSpace),
	}
}

// projectPolicyPointer safely reads one pointer field of a possibly-nil
// policy.
func projectPolicyPointer[T any](p *ProjectPolicy, get func(*ProjectPolicy) *T) *T {
	if p == nil {
		return nil
	}
	return get(p)
}

// normalizePolicyPlanReviewMode mirrors the daemon read: empty (off by
// default) or anything unrecognized → off; warn/block keep their canonical
// spelling (case and whitespace trimmed).
func normalizePolicyPlanReviewMode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case PlanReviewPolicyWarn:
		return PlanReviewPolicyWarn
	case PlanReviewPolicyBlock:
		return PlanReviewPolicyBlock
	default:
		return PlanReviewPolicyOff
	}
}

// escalationLevelOrNil reads the project tier's escalation level or nil.
func escalationLevelOrNil(p *ProjectPolicy) *FailureEscalationLevel {
	if p == nil {
		return nil
	}
	return p.FailureEscalation
}

// policyFailureEscalation merges the escalation policy per field: a tier
// that sets a sub-field supplies that sub-field and the tier below fills
// what it does not set, independently per field. The resolved value's
// source is the highest tier that sets at least one sub-field.
func policyFailureEscalation(daemon FailureEscalationResolved, project, automaton *FailureEscalationLevel) Field[FailureEscalationResolved] {
	proj, auto := escalationSetFields(project), escalationSetFields(automaton)

	resolved := FailureEscalationResolved{
		Enabled:     daemon.Enabled,
		MaxFailures: daemon.MaxFailures,
		Steps:       daemon.Steps,
	}
	if proj.Enabled {
		resolved.Enabled = *project.Enabled
	}
	if proj.MaxFailures {
		resolved.MaxFailures = *project.MaxFailures
	}
	if proj.Steps {
		resolved.Steps = project.Steps
	}
	if auto.Enabled {
		resolved.Enabled = *automaton.Enabled
	}
	if auto.MaxFailures {
		resolved.MaxFailures = *automaton.MaxFailures
	}
	if auto.Steps {
		resolved.Steps = automaton.Steps
	}

	source := PolicySourceDaemon
	switch {
	case auto.Enabled || auto.MaxFailures || auto.Steps:
		source = PolicySourceAutomaton
	case proj.Enabled || proj.MaxFailures || proj.Steps:
		source = PolicySourceProject
	}
	return Field[FailureEscalationResolved]{Value: resolved, Source: source}
}

// escalationSubfields records which sub-fields a level sets. A nil or zero
// MaxFailures is "not set" (0 keeps the daemon default meaning).
type escalationSubfields struct {
	Enabled     bool
	MaxFailures bool
	Steps       bool
}

func escalationSetFields(l *FailureEscalationLevel) escalationSubfields {
	if l == nil {
		return escalationSubfields{}
	}
	s := escalationSubfields{
		Enabled: l.Enabled != nil,
		Steps:   len(l.Steps) > 0,
	}
	if l.MaxFailures != nil && *l.MaxFailures > 0 {
		s.MaxFailures = true
	}
	return s
}
