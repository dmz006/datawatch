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
//
// When the planning pair is part of the call, every member Automaton whose
// planning run is still sitting in the capacity queue is retargeted onto the
// pool the NEW pair resolves to (requeuePlanningForMembers); the folded
// outcome returns as the second value so the surfaces that drive this setter
// (REST / MCP / CLI / comm) can report it. The result is nil when the call did
// not touch the planning pair.
func (m *Manager) SetAutomationLLM(id string, backend, model, effort, planningBackend, planningModel *string, actor string, expectedVersion int64) (*Automation, *PlanningRequeueResult, error) {
	fields := []llmField{
		{"backend", backend},
		{"model", model},
		{"effort", effort},
		{"planning_backend", planningBackend},
		{"planning_model", planningModel},
	}
	// The planning pair the Automation held before this call: the "old" side of
	// the requeue report (the member runs were queued on this pair's pool).
	var oldPlanBackend, oldPlanModel string
	if cur, ok := m.automations.Get(id); ok {
		oldPlanBackend, oldPlanModel = cur.PlanningBackend, cur.PlanningModel
	}
	updated, err := m.setAutomationLLM(id, controlSetLLM, actor, expectedVersion, fields, func(a *Automation) error {
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
	if err != nil {
		return nil, nil, err
	}
	// A planning-pair change moves any queued member planning run onto the pool
	// the NEW pair resolves to; the folded outcome rides back for the surfaces
	// to report. A call that only touched the execution pair has nothing to
	// retarget and returns a nil planning result exactly as before.
	var res *PlanningRequeueResult
	if planningBackend != nil || planningModel != nil {
		res = m.requeuePlanningForMembers(updated, oldPlanBackend, oldPlanModel, actor)
	}
	return updated, res, nil
}

// requeuePlanningForMembers retargets every member Automaton whose planning run
// is still queued onto the pool the Automation's NEW planning pair resolves to,
// and folds the per-member outcomes into one result. Requeued outranks deferred,
// which outranks unchanged: the returned result carries the strongest outcome so
// the surfaces can say at a glance whether anything actually moved. Member PRDs
// that are not on the store are skipped. The reported pool union is the set of
// pools any member was retargeted from / onto.
func (m *Manager) requeuePlanningForMembers(a *Automation, oldBackend, oldModel, actor string) *PlanningRequeueResult {
	rank := map[PlanningOutcome]int{PlanningUnchanged: 0, PlanningDeferred: 1, PlanningRequeued: 2}
	best := &PlanningRequeueResult{Outcome: PlanningUnchanged, OldBackend: oldBackend, OldModel: oldModel}
	seen := map[string]bool{}
	unionOld, unionNew := map[string]bool{}, map[string]bool{}
	affected := 0
	for _, mem := range a.Members {
		prdID := mem.PRDID
		if prdID == "" || seen[prdID] {
			continue
		}
		seen[prdID] = true
		affected++
		// requeuePlanningRun handles a missing PRD itself (it is a member the
		// store no longer carries), so no separate lookup is needed here.
		res := m.requeuePlanningRun(prdID, actor, oldBackend, oldModel)
		for _, p := range res.OldPools {
			unionOld[p] = true
		}
		for _, p := range res.NewPools {
			unionNew[p] = true
		}
		if rank[res.Outcome] > rank[best.Outcome] {
			best = res
		}
	}
	best.OldPools = sortedCopyPools(append([]string(nil), keys(unionOld)...))
	best.NewPools = sortedCopyPools(append([]string(nil), keys(unionNew)...))
	best.Reason = fmt.Sprintf("%d member Automaton(s) considered", affected) + ": " + best.Reason
	return best
}

// keys returns the keys of a string-set in an unspecified order (the caller
// re-sorts via sortedCopyPools).
func keys(s map[string]bool) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	return out
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
