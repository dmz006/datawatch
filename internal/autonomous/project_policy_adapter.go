// project_policy_adapter.go — adapts a PRD's own override fields into the
// config-level AutomatonOverrides, the third (highest) tier of
// config.ResolveProjectPolicy.
package autonomous

import "github.com/dmz006/datawatch/internal/config"

// PolicyOverrides returns this Automaton's override fields as the
// automaton tier of the policy resolver: retry policy, the auto-retry
// ceiling, max concurrent tasks, the failure escalation policy, and the
// auto-PR default. A field that is zero/nil/absent on this PRD is left
// unset in the result so the tier below (project, then daemon) supplies it.
//
// Zero-value semantics follow the PRD's own fields: RetryCeilingTotal and
// MaxConcurrentTasks 0 mean "unset" (the PRD's existing resolution applies
// 0 = fallback), so they map to nil here; AutoPR is a bool with omitempty
// on the PRD, so only an explicit `auto_pr: true` carries intent (an
// explicit false is indistinguishable from absent there and therefore never
// overrides the lower tiers).
func (p *PRD) PolicyOverrides() config.AutomatonOverrides {
	if p == nil {
		return config.AutomatonOverrides{}
	}
	var (
		retryCeiling  *int
		concurrentMax *int
		autoPR        *bool
	)
	if p.RetryCeilingTotal > 0 {
		v := p.RetryCeilingTotal
		retryCeiling = &v
	}
	if p.MaxConcurrentTasks > 0 {
		v := p.MaxConcurrentTasks
		concurrentMax = &v
	}
	if p.Git.AutoPR {
		v := true
		autoPR = &v
	}
	return config.AutomatonOverrides{
		RetryPolicy:        p.RetryPolicy,
		RetryCeilingTotal:  retryCeiling,
		MaxConcurrentTasks: concurrentMax,
		FailureEscalation:  escalationPolicyToLevel(p.FailureEscalation),
		DefaultAutoPR:      autoPR,
	}
}

// escalationPolicyToLevel converts the PRD-level escalation policy (pointer
// fields, "set" = non-nil / non-zero) into the config-level tier. A nil
// policy returns nil (= the tier is absent).
func escalationPolicyToLevel(pol *FailureEscalationPolicy) *config.FailureEscalationLevel {
	if pol == nil {
		return nil
	}
	level := &config.FailureEscalationLevel{Enabled: pol.Enabled}
	if pol.MaxFailures > 0 {
		v := pol.MaxFailures
		level.MaxFailures = &v
	}
	if len(pol.Steps) > 0 {
		level.Steps = make([]config.FailureEscalationStepPolicy, 0, len(pol.Steps))
		for _, s := range pol.Steps {
			level.Steps = append(level.Steps, config.FailureEscalationStepPolicy{
				Backend:  s.Backend,
				Model:    s.Model,
				Effort:   string(s.Effort),
				Attempts: s.Attempts,
			})
		}
	}
	return level
}
