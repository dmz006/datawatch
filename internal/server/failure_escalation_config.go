// Failure escalation config surfaces — the autonomous.failure_escalation block
// on GET /api/config and PUT /api/config.
//
// The block is the global default: whether escalation is on, how many failed
// attempts hold a task for review, and the ladder of workers to try first. The
// PUT path checks every key with the same rules the Automaton and task
// overrides use (autonomous.ValidateFailureEscalationPolicy), and refuses the
// whole PUT before anything is applied. The steps key takes either a JSON array
// of step objects or the compact text backend:model[@attempts],...
package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/config"
)

// PUT /api/config keys of the failure escalation block.
const (
	keyFailureEscalationEnabled     = "autonomous.failure_escalation.enabled"
	keyFailureEscalationMaxFailures = "autonomous.failure_escalation.max_failures"
	keyFailureEscalationSteps       = "autonomous.failure_escalation.steps"
)

// failureEscalationConfigForDisplay renders the global default for
// GET /api/config as {enabled, max_failures, steps}. A zero max_failures shows
// the default, and steps is always an array, never null.
func failureEscalationConfigForDisplay(c config.FailureEscalationConfig) map[string]any {
	maxFailures := c.MaxFailures
	if maxFailures < 1 {
		maxFailures = config.FailureEscalationDefaultMaxFailures
	}
	steps := make([]config.FailureEscalationStepConfig, 0, len(c.Steps))
	steps = append(steps, c.Steps...)
	return map[string]any{
		"enabled":      c.Enabled,
		"max_failures": maxFailures,
		"steps":        steps,
	}
}

// validateFailureEscalationPatch checks the failure escalation keys of a PUT
// /api/config body before anything is applied. A key that is absent is not
// checked. The error names the key and the valid values, and a bad value
// refuses the whole PUT.
func validateFailureEscalationPatch(patch map[string]any) error {
	var p autonomous.FailureEscalationPolicy
	if v, ok := patch[keyFailureEscalationEnabled]; ok {
		b, isBool := failureEscalationBool(v)
		if !isBool {
			return fmt.Errorf("%s: %v is invalid; valid values: true, false", keyFailureEscalationEnabled, v)
		}
		p.Enabled = &b
	}
	if v, ok := patch[keyFailureEscalationMaxFailures]; ok {
		n, isInt := failureEscalationInt(v)
		if !isInt || n < 1 {
			return fmt.Errorf("%s: %v is invalid; use a whole number of 1 or more (the default is %d)",
				keyFailureEscalationMaxFailures, v, config.FailureEscalationDefaultMaxFailures)
		}
		p.MaxFailures = n
	}
	if v, ok := patch[keyFailureEscalationSteps]; ok {
		steps, err := parseFailureEscalationStepsValue(v)
		if err != nil {
			return err
		}
		p.Steps = autonomousFailureEscalationSteps(steps)
	}
	if err := autonomous.ValidateFailureEscalationPolicy(&p); err != nil {
		return fmt.Errorf("autonomous.%v", err)
	}
	return nil
}

// applyFailureEscalationPatch writes one failure escalation key into the
// daemon config. It reports whether key belongs to the block. Values were
// checked by validateFailureEscalationPatch before the PUT reached this point,
// so a value that does not parse here is left unchanged.
func applyFailureEscalationPatch(cfg *config.Config, key string, v any) bool {
	fe := &cfg.Autonomous.FailureEscalation
	switch key {
	case keyFailureEscalationEnabled:
		if b, ok := failureEscalationBool(v); ok {
			fe.Enabled = b
		}
	case keyFailureEscalationMaxFailures:
		if n, ok := failureEscalationInt(v); ok {
			fe.MaxFailures = n
		}
	case keyFailureEscalationSteps:
		if steps, err := parseFailureEscalationStepsValue(v); err == nil {
			fe.Steps = steps
		}
	default:
		return false
	}
	return true
}

// parseFailureEscalationStepsValue reads the steps value of a PUT: a JSON array
// of step objects, or the compact text backend:model[@attempts],... An empty
// string or null clears the ladder. Unknown keys inside a step object are an
// error, the same as in the YAML block.
func parseFailureEscalationStepsValue(v any) ([]config.FailureEscalationStepConfig, error) {
	var steps []autonomous.FailureEscalationStep
	switch x := v.(type) {
	case nil:
	case string:
		parsed, err := autonomous.ParseFailureEscalationSteps(x)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", keyFailureEscalationSteps, err)
		}
		steps = parsed
	case []any:
		raw, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", keyFailureEscalationSteps, err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&steps); err != nil {
			return nil, fmt.Errorf("%s: must be an array of objects with the keys backend, model, effort and attempts: %w",
				keyFailureEscalationSteps, err)
		}
	default:
		return nil, fmt.Errorf("%s: must be a JSON array of step objects, or compact text such as claude-code:claude-opus-5-5@2,pi-prompt:ollama/qwen3.8:27b",
			keyFailureEscalationSteps)
	}
	out := make([]config.FailureEscalationStepConfig, 0, len(steps))
	for _, s := range steps {
		out = append(out, config.FailureEscalationStepConfig{
			Backend:  s.Backend,
			Model:    s.Model,
			Effort:   string(s.Effort),
			Attempts: s.Attempts,
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// autonomousFailureEscalationSteps converts config steps to the autonomous
// type the policy validator reads.
func autonomousFailureEscalationSteps(steps []config.FailureEscalationStepConfig) []autonomous.FailureEscalationStep {
	out := make([]autonomous.FailureEscalationStep, 0, len(steps))
	for _, s := range steps {
		out = append(out, autonomous.FailureEscalationStep{
			Backend:  s.Backend,
			Model:    s.Model,
			Effort:   autonomous.Effort(s.Effort),
			Attempts: s.Attempts,
		})
	}
	return out
}

// failureEscalationBool reads a boolean config value: a JSON boolean, or the
// text "true" or "false" (the form the chat configure verb sends).
func failureEscalationBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}

// failureEscalationInt reads a whole-number config value: a JSON number or a
// string of digits.
func failureEscalationInt(v any) (int, bool) {
	if n, ok := toInt(v); ok {
		return n, true
	}
	if s, ok := v.(string); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return n, true
		}
	}
	return 0, false
}

// errFailureEscalationNotSupported is returned when the Automaton manager does
// not offer the failure escalation operations (a test double, for example).
var errFailureEscalationNotSupported = errors.New("failure escalation is not available on this daemon")
