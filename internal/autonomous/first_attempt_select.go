package autonomous

// autonomous.first_attempt — the cheaper worker a task's first attempt runs on.
//
// firstAttemptChoice picks the configured backend and model for the task's
// kind when the task has not failed yet. applyFirstAttempt is the one call the
// executor makes: it leaves the normally resolved worker alone when the task or
// its Automaton pinned one, when the burn guard routed the attempt, or when no
// pair is configured for the kind.

import (
	"fmt"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// firstAttemptChoice returns the backend and model that the first attempt of t
// runs on, taken from cfg.FirstAttempt for taskAttemptKind(t). ok is true only
// while firstAttemptEscalated is false for failedAttempts and
// cfg.FirstAttemptEscalateAfterFailures, and the kind names a non-empty
// backend. Otherwise ok is false and the caller keeps its own choice.
func firstAttemptChoice(cfg Config, t *Task, failedAttempts int) (backend, model, reason string, ok bool) {
	if firstAttemptEscalated(failedAttempts, cfg.FirstAttemptEscalateAfterFailures) {
		return "", "", "", false
	}
	kind := taskAttemptKind(t)
	choice, found := cfg.FirstAttempt[kind]
	if !found {
		return "", "", "", false
	}
	backend = strings.TrimSpace(choice.Backend)
	if backend == "" {
		return "", "", "", false
	}
	model = strings.TrimSpace(choice.Model)
	return backend, model, fmt.Sprintf("first attempt for %s tasks", kind), true
}

// attemptLLMKeepsSource reports whether an execution source is a choice the
// first-attempt pair must not replace: an operator pin at the task, story, PRD
// or Automaton level, or a route the burn guard applied to the backend.
func attemptLLMKeepsSource(source string) bool {
	switch source {
	case ExecutorSourceTask, ExecutorSourceStory, ExecutorSourcePRD,
		ExecutorSourceAutomation, ExecutorSourceBurnGuard:
		return true
	}
	return false
}

// applyFirstAttempt returns the worker an attempt runs on. It takes the backend,
// model and source already resolved for the attempt and replaces them with the
// first-attempt pair only when attemptLLMKeepsSource is false and
// firstAttemptChoice applies. reason is non-empty exactly when the pair was
// used, so the caller can log it.
func applyFirstAttempt(cfg Config, t *Task, failedAttempts int, backend, model, source string) (string, string, string, string) {
	if attemptLLMKeepsSource(source) {
		return backend, model, source, ""
	}
	fb, fm, reason, ok := firstAttemptChoice(cfg, t, failedAttempts)
	if !ok {
		return backend, model, source, ""
	}
	return fb, fm, ExecutorSourceFirstAttempt, reason
}

// CloneFirstAttempt returns a copy of the first_attempt map so the manager's
// Config does not share a map with the caller's config. A nil or empty input
// returns nil.
func CloneFirstAttempt(in map[string]config.FirstAttemptChoice) map[string]config.FirstAttemptChoice {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]config.FirstAttemptChoice, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
