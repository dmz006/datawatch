// Automation LLM setters: the operator-driven change of an Automation's
// execution, planning and verification defaults. Both are presence-aware (a nil
// pointer leaves a field unchanged, a pointer to "" clears it), validate on the
// stored copy inside the store's Update so check and write are one atomic step,
// and record one Decision and one audit record.

package autonomous

import (
	"fmt"
	"strings"
)

// Decision kinds and audit actions of the Automation LLM setters.
const (
	controlSetLLM             = "automation_set_llm"
	controlSetVerificationLLM = "automation_set_verification_llm"
)

// llmField is one presence-aware field of an LLM setter call.
type llmField struct {
	name string
	v    *string
}

// suppliedLLMFields returns the fields the call carried, as the Decision note
// ("name=value", an empty value showing a cleared field) and the audit details.
func suppliedLLMFields(fields []llmField) (string, map[string]any) {
	var parts []string
	details := map[string]any{}
	for _, f := range fields {
		if f.v == nil {
			continue
		}
		parts = append(parts, f.name+"="+*f.v)
		details[f.name] = *f.v
	}
	return strings.Join(parts, " "), details
}

// checkAutomationLLMPair refuses a backend/model pair that the Automation would
// hold after the call, or nil when the call touches neither field.
func checkAutomationLLMPair(id string, backend, model *string, curBackend, curModel string) error {
	if backend == nil && model == nil {
		return nil
	}
	if err := validateModelForBackend(pickString(backend, curBackend), pickString(model, curModel)); err != nil {
		return fmt.Errorf("automation %s: %w", id, err)
	}
	return nil
}

// setAutomationLLM runs one LLM setter: apply validates and writes the stored
// copy inside Update, then one Decision and one audit record are emitted.
// expectedVersion 0 skips the version check (the comm channel cannot know it).
func (m *Manager) setAutomationLLM(id, kind, actor string, expectedVersion int64, fields []llmField, apply func(a *Automation) error) (*Automation, error) {
	if actor == "" {
		actor = "operator"
	}
	note, supplied := suppliedLLMFields(fields)
	updated, err := m.automations.Update(id, expectedVersion, func(a *Automation) error {
		if err := checkControlStatus(a, []AutomationStatus{AutomationDraft, AutomationActive, AutomationPaused, AutomationBlocked}); err != nil {
			return err
		}
		if err := apply(a); err != nil {
			return err
		}
		controlDecision(a, kind, actor, note)
		return nil
	})
	if err != nil {
		return nil, err
	}
	details := map[string]any{"automation_id": id, "actor": actor, "version": updated.Version}
	for k, v := range supplied {
		details[k] = v
	}
	m.controlAudit(kind, details)
	return updated, nil
}

// SetAutomationLLM overrides the Automation-level execution backend, model and
// effort and the planning backend and model. Presence-aware: a nil pointer
// leaves the field unchanged, a pointer to "" clears it. Allowed while the
// Automation is draft, active, paused or blocked. The execution pair is checked
// against the pair the Automation will hold; the planning pair is stored as
// given. The verification fields are not touched (SetAutomationVerificationLLM).
func (m *Manager) SetAutomationLLM(id string, backend, model, effort, planningBackend, planningModel *string, actor string, expectedVersion int64) (*Automation, error) {
	fields := []llmField{
		{"backend", backend},
		{"model", model},
		{"effort", effort},
		{"planning_backend", planningBackend},
		{"planning_model", planningModel},
	}
	return m.setAutomationLLM(id, controlSetLLM, actor, expectedVersion, fields, func(a *Automation) error {
		if effort != nil && !validVerificationEffort(*effort) {
			return fmt.Errorf("invalid effort %q for Automation %s", *effort, a.ID)
		}
		if err := checkAutomationLLMPair(a.ID, backend, model, a.Backend, a.Model); err != nil {
			return err
		}
		if backend != nil {
			a.Backend = *backend
		}
		if model != nil {
			a.Model = *model
		}
		if effort != nil {
			a.Effort = Effort(*effort)
		}
		if planningBackend != nil {
			a.PlanningBackend = *planningBackend
		}
		if planningModel != nil {
			a.PlanningModel = *planningModel
		}
		return nil
	})
}

// SetAutomationVerificationLLM overrides the Automation-level verifier backend,
// model and effort, presence-aware like SetAutomationLLM and with the same
// status rule and version semantics. The execution and planning fields are not
// touched.
func (m *Manager) SetAutomationVerificationLLM(id string, backend, model, effort *string, actor string, expectedVersion int64) (*Automation, error) {
	fields := []llmField{
		{"verification_backend", backend},
		{"verification_model", model},
		{"verification_effort", effort},
	}
	return m.setAutomationLLM(id, controlSetVerificationLLM, actor, expectedVersion, fields, func(a *Automation) error {
		if effort != nil && !validVerificationEffort(*effort) {
			return fmt.Errorf("invalid verification effort %q for Automation %s", *effort, a.ID)
		}
		if err := checkAutomationLLMPair(a.ID, backend, model, a.VerificationBackend, a.VerificationModel); err != nil {
			return err
		}
		applyVerificationLLM(&a.VerificationBackend, &a.VerificationModel, &a.VerificationEffort, backend, model, effort)
		return nil
	})
}
