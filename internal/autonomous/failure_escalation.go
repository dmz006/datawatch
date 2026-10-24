// Failure escalation policy — data model, config and override resolution.
//
// A task that keeps failing is retried on an escalating ladder of workers and
// then held for the operator instead of burning retries forever. This file
// holds the types, the per-field override resolver and the validation and
// compact-text helpers; the executor behaviour that consumes them lives in
// the executor files. See docs/plans/ for the design.
//
// Counting rule: attempts are counted as failed attempts since the last
// operator action (Task.EscalationFailures). Step i covers Attempts
// consecutive failed attempts (default 1) and the last step covers every
// remaining attempt. A step with an empty Backend means "the task's normally
// resolved worker", and an empty Steps list means every attempt uses it.
// After MaxFailures failed attempts (default 3) the task is held in
// TaskReviewNeeded. Enabled defaults to true; false restores the old
// behaviour (retry up to autonomous.auto_fix_retries, then mark the task
// failed).
package autonomous

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Defaults and limits for the failure escalation policy.
const (
	// defaultFailureEscalationMaxFailures is the failed-attempt count at
	// which a task is held for operator review when nothing sets it.
	defaultFailureEscalationMaxFailures = 3
	// maxFailureEscalationSteps bounds the length of a Steps ladder.
	maxFailureEscalationSteps = 8
)

// Escalation source labels reported in EscalationSources.
const (
	escalationSourceTask   = "task"
	escalationSourcePRD    = "prd"
	escalationSourceGlobal = "global"
)

// ErrTaskNotInReview is returned by ResolveTaskReview when the task is not
// held in TaskReviewNeeded. Callers test for it with errors.Is.
var ErrTaskNotInReview = errors.New("task is not awaiting review")

// FailureEscalationPolicy is one level (global, Automaton or task) of the
// failure escalation policy. A nil, zero or empty field means "not set at
// this level"; each field is resolved independently, so a task that sets only
// MaxFailures still inherits the Automaton's Steps.
type FailureEscalationPolicy struct {
	// Enabled turns escalation on or off. Nil = inherit.
	Enabled *bool `json:"enabled,omitempty"`
	// MaxFailures is the number of failed attempts after which the task is
	// held for operator review. 0 = inherit; the minimum is 1.
	MaxFailures int `json:"max_failures,omitempty"`
	// Steps is the ladder of workers to try, in order. Empty = inherit.
	Steps []FailureEscalationStep `json:"steps,omitempty"`
}

// FailureEscalationStep is one rung of the escalation ladder.
type FailureEscalationStep struct {
	// Backend and Model name the worker for this step. An empty Backend
	// means the task's normally resolved worker.
	Backend string `json:"backend,omitempty"`
	Model   string `json:"model,omitempty"`
	// Effort is the effort level for this step. Empty = the task's effort.
	Effort Effort `json:"effort,omitempty"`
	// Attempts is how many consecutive failed attempts this step covers.
	// 0 reads as 1.
	Attempts int `json:"attempts,omitempty"`
}

// TaskAttempt is the record of one failed, stalled or cancelled attempt of a
// task, kept in Task.Attempts for the operator's review report.
type TaskAttempt struct {
	Number    int       `json:"number"`
	Backend   string    `json:"backend,omitempty"`
	Model     string    `json:"model,omitempty"`
	Effort    Effort    `json:"effort,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
	// DurationSeconds is the wall-clock length of the attempt.
	DurationSeconds int `json:"duration_seconds,omitempty"`
	// Outcome is "failed", "stalled" or "cancelled".
	Outcome         string   `json:"outcome,omitempty"`
	FailureCategory string   `json:"failure_category,omitempty"`
	FailureSummary  string   `json:"failure_summary,omitempty"`
	VerifierSummary string   `json:"verifier_summary,omitempty"`
	VerifierIssues  []string `json:"verifier_issues,omitempty"`
	ChangedFiles    []string `json:"changed_files,omitempty"`
	// SnapshotRef names the saved state of the work tree after the attempt.
	SnapshotRef string `json:"snapshot_ref,omitempty"`
	// Escalated is true when this attempt ran on an escalation step rather
	// than the task's normally resolved worker.
	Escalated bool `json:"escalated,omitempty"`
}

// ReviewSummary is written when a task is held in TaskReviewNeeded.
type ReviewSummary struct {
	HeldAt       time.Time `json:"held_at,omitempty"`
	FailureCount int       `json:"failure_count,omitempty"`
	TotalSeconds int       `json:"total_seconds,omitempty"`
	// Text is a plain-text per-attempt report.
	Text string `json:"text,omitempty"`
}

// ReviewResolution is the operator's decision on a task held for review.
// Action is "retry" or "cancel". A retry may carry a changed spec, a changed
// worker (task-level backend, model and effort) or a narrowed scope (changed
// files and/or spec).
type ReviewResolution struct {
	Action  string   `json:"action"`
	Spec    string   `json:"spec,omitempty"`
	Backend string   `json:"backend,omitempty"`
	Model   string   `json:"model,omitempty"`
	Effort  Effort   `json:"effort,omitempty"`
	Files   []string `json:"files,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// EscalationSources reports, per field, which level supplied the effective
// value: "task", "prd" or "global".
type EscalationSources struct {
	Enabled     string `json:"enabled"`
	MaxFailures string `json:"max_failures"`
	Steps       string `json:"steps"`
}

// EffectiveFailureEscalation is the resolved policy for one task.
type EffectiveFailureEscalation struct {
	Enabled     bool                    `json:"enabled"`
	MaxFailures int                     `json:"max_failures"`
	Steps       []FailureEscalationStep `json:"steps,omitempty"`
	Sources     EscalationSources       `json:"sources"`
}

// defaultFailureEscalationPolicy is the built-in global default: escalation
// on, three failed attempts, and no steps (every attempt uses the normally
// resolved worker).
func defaultFailureEscalationPolicy() FailureEscalationPolicy {
	enabled := true
	return FailureEscalationPolicy{Enabled: &enabled, MaxFailures: defaultFailureEscalationMaxFailures}
}

// normalizeFailureEscalationPolicy fills the global policy's unset fields
// with the built-in defaults: a nil Enabled reads as true and a MaxFailures
// below 1 reads as 3. Steps are copied so the result never aliases the input.
func normalizeFailureEscalationPolicy(p FailureEscalationPolicy) FailureEscalationPolicy {
	def := defaultFailureEscalationPolicy()
	if p.Enabled == nil {
		p.Enabled = def.Enabled
	} else {
		v := *p.Enabled
		p.Enabled = &v
	}
	if p.MaxFailures < 1 {
		p.MaxFailures = def.MaxFailures
	}
	p.Steps = cloneEscalationSteps(p.Steps)
	return p
}

func cloneEscalationSteps(steps []FailureEscalationStep) []FailureEscalationStep {
	if len(steps) == 0 {
		return nil
	}
	return append([]FailureEscalationStep(nil), steps...)
}

// escalationLevel pairs one override level's policy with its source label.
type escalationLevel struct {
	policy *FailureEscalationPolicy
	source string
}

// resolveFailureEscalation returns the effective failure escalation policy
// for task t of prd. Each field is resolved independently: task override
// (Task.FailureEscalation) > Automaton override (PRD.FailureEscalation) >
// global default (Config.FailureEscalation). Sources records which level
// supplied each field. Either argument may be nil.
func (m *Manager) resolveFailureEscalation(prd *PRD, t *Task) EffectiveFailureEscalation {
	m.mu.Lock()
	global := m.cfg.FailureEscalation
	m.mu.Unlock()
	return resolveFailureEscalationFrom(global, prd, t)
}

// resolveFailureEscalationFrom is resolveFailureEscalation with the global
// default passed in. Callers that already hold m.mu (the Run preflight) use
// it so they do not re-acquire the lock.
func resolveFailureEscalationFrom(global FailureEscalationPolicy, prd *PRD, t *Task) EffectiveFailureEscalation {
	var levels []escalationLevel
	if t != nil && t.FailureEscalation != nil {
		levels = append(levels, escalationLevel{t.FailureEscalation, escalationSourceTask})
	}
	if prd != nil && prd.FailureEscalation != nil {
		levels = append(levels, escalationLevel{prd.FailureEscalation, escalationSourcePRD})
	}

	var eff EffectiveFailureEscalation
	var haveEnabled, haveMax, haveSteps bool
	for _, l := range levels {
		if !haveEnabled && l.policy.Enabled != nil {
			eff.Enabled, eff.Sources.Enabled, haveEnabled = *l.policy.Enabled, l.source, true
		}
		if !haveMax && l.policy.MaxFailures > 0 {
			eff.MaxFailures, eff.Sources.MaxFailures, haveMax = l.policy.MaxFailures, l.source, true
		}
		if !haveSteps && len(l.policy.Steps) > 0 {
			eff.Steps, eff.Sources.Steps, haveSteps = cloneEscalationSteps(l.policy.Steps), l.source, true
		}
	}
	if haveEnabled && haveMax && haveSteps {
		return eff
	}

	g := normalizeFailureEscalationPolicy(global)
	if !haveEnabled {
		eff.Enabled, eff.Sources.Enabled = *g.Enabled, escalationSourceGlobal
	}
	if !haveMax {
		eff.MaxFailures, eff.Sources.MaxFailures = g.MaxFailures, escalationSourceGlobal
	}
	if !haveSteps {
		eff.Steps, eff.Sources.Steps = g.Steps, escalationSourceGlobal
	}
	return eff
}

// validateFailureEscalationPolicy reports whether p is acceptable at any
// level. A nil policy is valid. MaxFailures and every step's Attempts must
// not be negative, a ladder has at most 8 steps, a step's Effort must be a
// known effort, a step with a Model needs a Backend, and a step naming both
// must pass validateModelForBackend. Errors name the offending step index
// and the valid values.
func validateFailureEscalationPolicy(p *FailureEscalationPolicy) error {
	if p == nil {
		return nil
	}
	if p.MaxFailures < 0 {
		return fmt.Errorf("failure_escalation.max_failures: %d is invalid; use 1 or more (0 inherits the next level)", p.MaxFailures)
	}
	if len(p.Steps) > maxFailureEscalationSteps {
		return fmt.Errorf("failure_escalation.steps: %d steps is too many; at most %d are allowed", len(p.Steps), maxFailureEscalationSteps)
	}
	for i, s := range p.Steps {
		if s.Attempts < 0 {
			return fmt.Errorf("failure_escalation.steps[%d].attempts: %d is invalid; use 1 or more (0 reads as 1)", i, s.Attempts)
		}
		if !validVerificationEffort(string(s.Effort)) {
			return fmt.Errorf("failure_escalation.steps[%d].effort: %q is invalid; valid values: low, medium, high, max, quick, normal, thorough, or empty", i, s.Effort)
		}
		if s.Model != "" && s.Backend == "" {
			return fmt.Errorf("failure_escalation.steps[%d]: model %q needs a backend; leave both empty to use the task's normally resolved worker", i, s.Model)
		}
		if err := validateModelForBackend(s.Backend, s.Model); err != nil {
			return fmt.Errorf("failure_escalation.steps[%d]: %w", i, err)
		}
	}
	return nil
}

// ParseFailureEscalationSteps parses the compact text form of a step ladder:
//
//	backend:model[@attempts],backend:model[@attempts],...
//
// Each comma-separated item is split at its FIRST colon into backend and
// model, because a model name may itself contain colons (qwen3.8:27b). An
// optional trailing @N is the step's attempts and must be 1 or more. The
// single token "-", optionally "-@N", is a step that uses the normally
// resolved worker. A backend with an empty model ("claude-code:") uses that
// backend's default model. Effort is only expressible in the JSON form.
// Empty text yields no steps. Whitespace around items is ignored.
func ParseFailureEscalationSteps(text string) ([]FailureEscalationStep, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	var steps []FailureEscalationStep
	for i, raw := range strings.Split(text, ",") {
		item := strings.TrimSpace(raw)
		if item == "" {
			return nil, fmt.Errorf("failure escalation steps: step %d is empty; use backend:model[@attempts] or - for the normal worker", i)
		}
		var step FailureEscalationStep
		if at := strings.LastIndex(item, "@"); at >= 0 {
			n, err := strconv.Atoi(item[at+1:])
			if err != nil || n < 1 {
				return nil, fmt.Errorf("failure escalation steps: step %d %q has an invalid @attempts suffix; use a whole number of 1 or more", i, item)
			}
			step.Attempts = n
			item = item[:at]
		}
		if item != "-" {
			colon := strings.Index(item, ":")
			if colon < 0 {
				return nil, fmt.Errorf("failure escalation steps: step %d %q needs the form backend:model, or - for the normal worker", i, item)
			}
			step.Backend, step.Model = strings.TrimSpace(item[:colon]), strings.TrimSpace(item[colon+1:])
			if step.Backend == "" {
				return nil, fmt.Errorf("failure escalation steps: step %d %q has no backend before the first colon", i, item)
			}
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// FormatFailureEscalationSteps renders steps in the compact text form read by
// ParseFailureEscalationSteps: backend:model, with @attempts appended when a
// step sets Attempts, and - for a step on the normally resolved worker.
// Effort has no text form and is not shown.
func FormatFailureEscalationSteps(steps []FailureEscalationStep) string {
	parts := make([]string, 0, len(steps))
	for _, s := range steps {
		item := "-"
		if s.Backend != "" {
			item = s.Backend + ":" + s.Model
		}
		if s.Attempts > 0 {
			item += "@" + strconv.Itoa(s.Attempts)
		}
		parts = append(parts, item)
	}
	return strings.Join(parts, ",")
}
