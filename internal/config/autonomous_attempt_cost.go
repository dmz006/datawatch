package config

// Settings for the cost of a task's first attempt and of its retries.
//
// autonomous.first_attempt maps a task kind to the backend and model its
// first attempt runs on. An empty or absent entry keeps today's choice.
// autonomous.first_attempt_escalate_after_failures says how many failed
// attempts it takes before the next attempt moves to the stronger choice
// (default 1: the first failure escalates). autonomous.retry_resume_max_
// context_tokens caps how much prior context a retry may resume with; 0
// means every retry starts a fresh session.
//
// ValidateAttemptCostSettings is the one shared validator. Every surface
// that accepts these keys calls it, so the same values are refused the same
// way everywhere, and each error names the key that is wrong.

import (
	"fmt"
	"sort"
	"strings"
)

// Attempt-cost task kinds accepted as keys of autonomous.first_attempt.
const (
	AttemptKindImplementation = "implementation"
	AttemptKindDocs           = "docs"
	AttemptKindNote           = "note"
	AttemptKindFinalGate      = "final_gate"
)

// Default values for the attempt-cost keys.
const (
	DefaultFirstAttemptEscalateAfterFailures = 1
	DefaultRetryResumeMaxContextTokens       = 150000
)

// Bounds enforced by ValidateAttemptCostSettings.
const (
	maxFirstAttemptEscalateAfterFailures = 20
	maxRetryResumeMaxContextTokens       = 2000000
)

// attemptKinds is the closed set of task kinds a first_attempt entry may name.
var attemptKinds = []string{
	AttemptKindImplementation,
	AttemptKindDocs,
	AttemptKindNote,
	AttemptKindFinalGate,
}

// attemptBackendsNeedingModel lists the backends that cannot run without an
// explicit model: the print-mode and local-engine backends that refuse to
// fall back to the host's own default model.
var attemptBackendsNeedingModel = map[string]bool{
	"pi":        true,
	"pi-prompt": true,
	"ollama":    true,
}

// FirstAttemptChoice is the backend and model one task kind's first attempt
// runs on. Both fields are optional individually; a backend that needs a
// model must name one (see ValidateAttemptCostSettings).
type FirstAttemptChoice struct {
	Backend string `yaml:"backend,omitempty" json:"backend,omitempty"`
	Model   string `yaml:"model,omitempty" json:"model,omitempty"`
}

// ValidateAttemptCostSettings checks the attempt-cost keys of cfg and returns
// the first problem found, naming the key. It refuses:
//   - a first_attempt entry whose kind is not implementation, docs, note or final_gate;
//   - a first_attempt backend that needs a model but names none;
//   - a negative first_attempt_escalate_after_failures, or one above 20;
//   - a negative retry_resume_max_context_tokens, or one above 2000000.
func ValidateAttemptCostSettings(cfg AutonomousConfig) error {
	kinds := make([]string, 0, len(cfg.FirstAttempt))
	for kind := range cfg.FirstAttempt {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	for _, kind := range kinds {
		if !isAttemptKind(kind) {
			return fmt.Errorf("autonomous.first_attempt: unknown task kind %q (valid kinds: %s)",
				kind, strings.Join(attemptKinds, ", "))
		}
		choice := cfg.FirstAttempt[kind]
		backend := strings.ToLower(strings.TrimSpace(choice.Backend))
		model := strings.TrimSpace(choice.Model)
		if attemptBackendsNeedingModel[backend] && model == "" {
			return fmt.Errorf("autonomous.first_attempt.%s.model: backend %q needs a model; set a model or clear the backend",
				kind, backend)
		}
	}
	if n := cfg.FirstAttemptEscalateAfterFailures; n < 0 || n > maxFirstAttemptEscalateAfterFailures {
		return fmt.Errorf("autonomous.first_attempt_escalate_after_failures: %d is invalid; use a whole number from 0 to %d",
			n, maxFirstAttemptEscalateAfterFailures)
	}
	if n := cfg.RetryResumeMaxContextTokens; n < 0 || n > maxRetryResumeMaxContextTokens {
		return fmt.Errorf("autonomous.retry_resume_max_context_tokens: %d is invalid; use a whole number from 0 to %d (0 = always start a fresh session)",
			n, maxRetryResumeMaxContextTokens)
	}
	return nil
}

// isAttemptKind reports whether kind is one of the accepted task kinds. The
// match is exact: keys are written in lower case and are not normalised.
func isAttemptKind(kind string) bool {
	for _, k := range attemptKinds {
		if kind == k {
			return true
		}
	}
	return false
}
