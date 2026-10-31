package main

import (
	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// effectiveAutonomousConfig is the single live read point the
// config-sensitive closures in main.go (decomposeFn, autonomousVerify,
// autonomousGuardrail, the BL221 scan-grader askFn) use on every
// invocation: the Manager's live Config() snapshot when the Manager
// exists, else the boot-time literal captured before NewManager.
//
// Runtime config sets — PUT /api/autonomous/config →
// autonomous.API.SetConfig, MCP autonomous_config_set, Manager.SetConfig
// — mutate only the Manager, never the boot-time literal a closure could
// have captured. Reading through the Manager per invocation is therefore
// what makes a config set take effect on the NEXT closure call with no
// restart and no reconstruction of the closures.
//
// Extracted out of main()'s inline closure so cmd/datawatch's unit tests
// (live_autonomous_config_test.go) exercise byte-for-byte the helper the
// closures call, instead of a copy of its logic.
func effectiveAutonomousConfig(ref *autonomouspkg.Manager, boot autonomouspkg.Config) autonomouspkg.Config {
	if ref != nil {
		return ref.Config()
	}
	return boot
}

// effectiveVerifierSelection projects one live config snapshot onto the
// fields the autonomousVerify and autonomousGuardrail closures hand to
// the verifier selection: the verification_backends pool, the global
// verification_backend, and the global verification_model. Per-unit
// (task/story/PRD) overrides are applied by the selection helpers
// themselves; global config must always come from a live snapshot taken
// at call time.
func effectiveVerifierSelection(cfg autonomouspkg.Config) (pool []string, backend, model string) {
	return cfg.VerificationBackends, cfg.VerificationBackend, cfg.VerificationModel
}

// effectiveVerifierPolicy projects one live config snapshot onto the
// verifier policy the selector reads. The returned policy owns its backend
// list.
func effectiveVerifierPolicy(cfg autonomouspkg.Config) config.VerifierPolicyConfig {
	return copyVerifierPolicy(cfg.VerifierPolicy)
}

// copyVerifierPolicy copies the policy so the backend list is not aliased
// between the daemon config and the manager's mirror.
func copyVerifierPolicy(p config.VerifierPolicyConfig) config.VerifierPolicyConfig {
	p.Backends = append([]string(nil), p.Backends...)
	return p
}

// failureEscalationFromConfig maps the YAML failure_escalation block onto the
// manager's policy type. The config package cannot import the autonomous
// package, so the two carry mirror structs; the steps are copied so the
// manager never aliases the daemon config's slice.
func failureEscalationFromConfig(c config.FailureEscalationConfig) autonomouspkg.FailureEscalationPolicy {
	enabled := c.Enabled
	p := autonomouspkg.FailureEscalationPolicy{Enabled: &enabled, MaxFailures: c.MaxFailures}
	for _, s := range c.Steps {
		p.Steps = append(p.Steps, autonomouspkg.FailureEscalationStep{
			Backend:  s.Backend,
			Model:    s.Model,
			Effort:   autonomouspkg.Effort(s.Effort),
			Attempts: s.Attempts,
		})
	}
	return p
}

// copyInfraRetry copies the infra_retry block so the manager's mirror does
// not alias the daemon config's slices or bool pointers. nil lists stay nil
// (unset reads the default) and non-nil empty lists stay non-nil (explicit).
func copyInfraRetry(c config.InfraRetryConfig) config.InfraRetryConfig {
	out := c
	if c.BackoffSeconds != nil {
		out.BackoffSeconds = append([]int{}, c.BackoffSeconds...)
	}
	if c.Categories != nil {
		out.Categories = append([]string{}, c.Categories...)
	}
	out.Enabled = copyBoolPtr(c.Enabled)
	out.RetryDependents = copyBoolPtr(c.RetryDependents)
	out.RaiseQuestion = copyBoolPtr(c.RaiseQuestion)
	return out
}

func copyBoolPtr(p *bool) *bool {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// cloneNotifyDefaultEvents copies the autonomous.notify.default_events
// slice, preserving nil (unset — resolution falls back to the built-in
// DefaultNotifyEvents set) vs a non-nil empty slice (explicitly turned
// off). Like the sibling append-based bridges, a nil input must stay nil;
// unlike them, a non-nil empty input must stay non-nil empty, which
// `append([]string(nil), in...)` would collapse to nil.
func cloneNotifyDefaultEvents(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// repoCIWatchFromYAML bridges the repo CI watch block
// (autonomous.automation.repo_ci_watch.*) from the loaded config into the
// Manager's boot-time config. Without it the Manager starts with a zero
// RepoCIWatch: the watch reads off after every restart until a config PUT
// happens to carry it again, even though the saved file says shadow or on.
//
// The security workflow list keeps its nil-versus-empty distinction: a nil
// list (unset) stays nil so the default applies, and a non-nil empty list
// (an explicit "none") stays non-nil empty.
func repoCIWatchFromYAML(in config.RepoCIWatchConfig) autonomouspkg.RepoCIWatchConfig {
	out := autonomouspkg.RepoCIWatchConfig{
		Mode:             in.Mode,
		PollSeconds:      in.PollSeconds,
		HungAfterMinutes: in.HungAfterMinutes,
		DeleteSuperseded: in.DeleteSuperseded,
	}
	if in.Repos != nil {
		out.Repos = append([]string{}, in.Repos...)
	}
	if in.SecurityWorkflows != nil {
		out.SecurityWorkflows = append([]string{}, in.SecurityWorkflows...)
	}
	if in.AlertMinutes != nil {
		alert := *in.AlertMinutes
		out.AlertMinutes = &alert
	}
	if in.TagCount != nil {
		tags := *in.TagCount
		out.TagCount = &tags
	}
	return out
}

// supervisorWatchFromYAML bridges the supervisor watch block
// (autonomous.automation.supervisor_watch.*) from the loaded config into
// the Manager's boot-time config. Without it the Manager starts with a
// zero SupervisorWatch and re-reads its one defaults after every restart,
// losing any operator-set value from the saved file until a config PUT
// happens to carry it again.
//
// The pointer fields keep their nil-versus-zero distinction: a nil
// (unset) threshold stays nil so the Effective* default applies, and an
// explicit 0 stays an explicit 0.
func supervisorWatchFromYAML(in config.SupervisorWatchConfig) autonomouspkg.SupervisorWatchConfig {
	out := autonomouspkg.SupervisorWatchConfig{
		Mode:                in.Mode,
		PollSeconds:         in.PollSeconds,
		ScriptOutputPath:    in.ScriptOutputPath,
		TmuxRestartBaseline: in.TmuxRestartBaseline,
		MainCheckout:        in.MainCheckout,
		WorkingModel:        in.WorkingModel,
	}
	if in.AlertMinutes != nil {
		alert := *in.AlertMinutes
		out.AlertMinutes = &alert
	}
	if in.SilentMinutes != nil {
		silent := *in.SilentMinutes
		out.SilentMinutes = &silent
	}
	return out
}
