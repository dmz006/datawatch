// Failure escalation policy config: the autonomous.failure_escalation block.
//
// The config package cannot import internal/autonomous, so these structs
// mirror autonomous.FailureEscalationPolicy and its step type (the same
// arrangement as VerifierPolicyConfig). Overrides at the Automaton and task
// level live on the autonomous types; this block is the global default.
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// FailureEscalationDefaultMaxFailures is the failed-attempt count at which a
// task is held for operator review when nothing sets it.
const FailureEscalationDefaultMaxFailures = 3

// FailureEscalationMaxSteps bounds the length of the steps ladder.
const FailureEscalationMaxSteps = 8

// FailureEscalationConfig is the global failure escalation default
// (autonomous.failure_escalation). The fields carry no omitempty so the live
// config round-trip through JSON never elides a zero and leaves a previous
// stored value in place.
type FailureEscalationConfig struct {
	// Enabled turns failure escalation on. false restores the old behaviour
	// (retry up to auto_fix_retries, then mark the task failed). DefaultConfig
	// seeds true.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// MaxFailures is the number of failed attempts after which a task is held
	// for operator review. The minimum is 1; 0 reads as the default (3).
	MaxFailures int `yaml:"max_failures" json:"max_failures"`
	// Steps is the ladder of workers to try; empty means every attempt uses
	// the task's normally resolved worker.
	Steps []FailureEscalationStepConfig `yaml:"steps" json:"steps"`
}

// FailureEscalationStepConfig is one rung of the escalation ladder.
type FailureEscalationStepConfig struct {
	// Backend and Model name the worker; an empty Backend means the task's
	// normally resolved worker.
	Backend string `yaml:"backend" json:"backend"`
	Model   string `yaml:"model" json:"model"`
	// Effort is the effort level for this step; empty means the task's.
	Effort string `yaml:"effort" json:"effort"`
	// Attempts is how many consecutive failed attempts the step covers;
	// 0 reads as 1.
	Attempts int `yaml:"attempts" json:"attempts"`
}

// DefaultFailureEscalationConfig returns the seeded defaults: on, three
// failed attempts, no steps.
func DefaultFailureEscalationConfig() FailureEscalationConfig {
	return FailureEscalationConfig{Enabled: true, MaxFailures: FailureEscalationDefaultMaxFailures}
}

// Validate rejects negative values and an over-long ladder, naming the key.
func (c FailureEscalationConfig) Validate() error {
	if c.MaxFailures < 0 {
		return fmt.Errorf("autonomous.failure_escalation.max_failures: %d is invalid; use 1 or more (0 reads as %d)",
			c.MaxFailures, FailureEscalationDefaultMaxFailures)
	}
	if len(c.Steps) > FailureEscalationMaxSteps {
		return fmt.Errorf("autonomous.failure_escalation.steps: %d steps is too many; at most %d are allowed",
			len(c.Steps), FailureEscalationMaxSteps)
	}
	for i, s := range c.Steps {
		if s.Attempts < 0 {
			return fmt.Errorf("autonomous.failure_escalation.steps[%d].attempts: %d is invalid; use 1 or more (0 reads as 1)",
				i, s.Attempts)
		}
	}
	return nil
}

var (
	failureEscalationKeys     = map[string]bool{"enabled": true, "max_failures": true, "steps": true}
	failureEscalationStepKeys = map[string]bool{"backend": true, "model": true, "effort": true, "attempts": true}
)

// validateFailureEscalationYAML rejects unknown keys inside the
// autonomous.failure_escalation block (and inside each step) of raw config
// YAML, so a typo such as max_failure is an error instead of being ignored.
// The lenient struct decode cannot see unknown keys, hence the second pass.
func validateFailureEscalationYAML(data []byte) error {
	var doc struct {
		Autonomous struct {
			FailureEscalation *struct {
				Fields map[string]any `yaml:",inline"`
			} `yaml:"failure_escalation"`
		} `yaml:"autonomous"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil || doc.Autonomous.FailureEscalation == nil {
		// A parse failure is reported by the main decode; nothing to check.
		return nil
	}
	for k, v := range doc.Autonomous.FailureEscalation.Fields {
		if !failureEscalationKeys[k] {
			return fmt.Errorf("autonomous.failure_escalation.%s: unknown key; valid keys: enabled, max_failures, steps", k)
		}
		if k != "steps" {
			continue
		}
		steps, _ := v.([]any)
		for i, raw := range steps {
			step, _ := raw.(map[string]any)
			for sk := range step {
				if !failureEscalationStepKeys[sk] {
					return fmt.Errorf("autonomous.failure_escalation.steps[%d].%s: unknown key; valid keys: backend, model, effort, attempts", i, sk)
				}
			}
		}
	}
	return nil
}
