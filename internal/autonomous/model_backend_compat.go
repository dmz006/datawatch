// Model/backend pairing checks for the autonomous planning and verification
// paths. A model name must be one the chosen backend can actually serve:
// pi-family backends take provider-qualified names, claude-code takes bare
// Claude model names, and every other backend is left to its own validation.

package autonomous

import (
	"errors"
	"fmt"
	"strings"
)

// ErrModelBackendMismatch is returned, wrapped, when a model name cannot be
// served by the backend it is paired with. Callers test for it with errors.Is.
var ErrModelBackendMismatch = errors.New("model does not pair with backend")

// claudeCodeBackendName is the Name() of the claude-code LLM backend.
const claudeCodeBackendName = "claude-code"

// validateModelForBackend reports an error when model cannot run on backend.
//
// An empty model is always valid. An empty backend is also valid for any
// model, because the effective backend is resolved later and Run enforces
// the pair again. For pi-family backends a bare Claude alias is refused,
// since pi resolves it across providers and exits with no output. For
// claude-code a provider-qualified name is refused, since it is a pi
// provider tag. All other backends are not checked here.
func validateModelForBackend(backend, model string) error {
	if model == "" || backend == "" {
		return nil
	}
	switch {
	case servesProviderModels(backend):
		if strings.Contains(model, "/") || !strings.HasPrefix(model, "claude-") {
			return nil
		}
		return fmt.Errorf("%w: model %q is a bare Claude alias, and backend %q resolves it across providers and exits with no output; use a provider-qualified name such as anthropic/claude-haiku-5-5, or the %s backend",
			ErrModelBackendMismatch, model, backend, claudeCodeBackendName)
	case backend == claudeCodeBackendName:
		if !strings.Contains(model, "/") {
			return nil
		}
		return fmt.Errorf("%w: model %q is a provider-qualified pi name, and backend %q cannot serve it; use a bare Claude model name such as claude-opus-5-5",
			ErrModelBackendMismatch, model, backend)
	default:
		return nil
	}
}

// preflightModelBackend checks every non-completed task of a PRD against the
// backend and model that dispatch would resolve for it, so Run can refuse to
// start a PRD that would spawn a mismatched pair. The resolution is the same
// resolveTaskBackendModel call the dispatch loop makes. It returns an error
// wrapping ErrModelBackendMismatch that names each offending task, or nil.
func (m *Manager) preflightModelBackend(prdID string, prd *PRD) error {
	var bad []string
	auto := m.AutomationForPRD(prd)
	for si := range prd.Story {
		story := &prd.Story[si]
		for ti := range story.Tasks {
			t := &story.Tasks[ti]
			if t.Status == TaskCompleted {
				continue
			}
			backend, model := resolveTaskBackendModel(t, story, prd, auto, m.cfg.ExecutionBackend)
			if err := validateModelForBackend(backend, model); err != nil {
				bad = append(bad, fmt.Sprintf("task %s (backend %q, model %q): %v", t.ID, backend, model, err))
			}
			// Every step of the task's effective failure escalation ladder
			// must pair too, because a later attempt may run on any of them.
			// m.mu is held here, so the global default is passed in rather
			// than read through resolveFailureEscalation.
			esc := resolveFailureEscalationFrom(m.cfg.FailureEscalation, prd, t)
			for si, step := range esc.Steps {
				if step.Backend == "" && step.Model != "" {
					bad = append(bad, fmt.Sprintf("task %s failure escalation step %d (model %q): a model needs a backend; leave both empty to use the normal worker", t.ID, si+1, step.Model))
					continue
				}
				if err := validateModelForBackend(step.Backend, step.Model); err != nil {
					bad = append(bad, fmt.Sprintf("task %s failure escalation step %d (backend %q, model %q): %v", t.ID, si+1, step.Backend, step.Model, err))
				}
			}
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("prd %q not started: %w in %d task(s): %s",
		prdID, ErrModelBackendMismatch, len(bad), strings.Join(bad, "; "))
}
