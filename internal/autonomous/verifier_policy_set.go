package autonomous

import (
	"fmt"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// applyVerifierPolicyOverride writes a presence-aware change onto an
// override and returns the value to store: a nil mode or backends leaves that
// field unchanged; an empty mode or an empty list clears it. The result is nil
// once both fields are cleared, so the level inherits again.
//
// A non-empty mode must be one of config.ValidVerifierPolicyModes and is
// stored in its canonical form. Backend entries are trimmed; an empty entry
// is rejected rather than dropped, so a stray comma never silently shrinks
// the list.
func applyVerifierPolicyOverride(cur *VerifierPolicyOverride, mode *string, backends *[]string) (*VerifierPolicyOverride, error) {
	next := VerifierPolicyOverride{}
	if cur != nil {
		next = VerifierPolicyOverride{Mode: cur.Mode, Backends: append([]string(nil), cur.Backends...)}
	}
	if mode != nil {
		m, err := config.NormalizeVerifierPolicyMode(*mode)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(*mode) == "" {
			m = ""
		}
		next.Mode = m
	}
	if backends != nil {
		for _, b := range *backends {
			if strings.TrimSpace(b) == "" {
				return nil, fmt.Errorf("verifier_policy.backends: entries must not be empty")
			}
		}
		next.Backends = config.NormalizeVerifierPolicyBackends(*backends)
	}
	if next.Mode == "" && len(next.Backends) == 0 {
		return nil, nil
	}
	return &next, nil
}

// verifierPolicyNote renders an override for the decision log.
func verifierPolicyNote(o *VerifierPolicyOverride) string {
	if o == nil {
		return "mode= backends="
	}
	return fmt.Sprintf("mode=%s backends=%s", o.Mode, strings.Join(o.Backends, ","))
}

// SetPRDVerifierPolicy overrides the verifier policy mode and backend list
// for a whole Automaton (a task-level override still wins). Presence-aware
// like SetPRDVerificationLLM: a nil argument leaves that field unchanged, an
// empty mode or an empty list clears it. Same lock rule as
// SetPRDVerificationLLM.
func (m *Manager) SetPRDVerifierPolicy(prdID string, mode *string, backends *[]string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := verificationLLMLocked(prd); err != nil {
		return nil, err
	}
	next, err := applyVerifierPolicyOverride(prd.VerifierPolicy, mode, backends)
	if err != nil {
		return nil, err
	}
	prd.VerifierPolicy = next
	now := time.Now()
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "set_prd_verifier_policy", Actor: actor,
		Note: verifierPolicyNote(next),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetTaskVerifierPolicy overrides the verifier policy mode and backend list
// for one task, the most specific level. Presence-aware like
// SetTaskVerificationLLM. Same accepted-while rule as SetTaskVerificationLLM
// (autonomous.task_override_statuses).
func (m *Manager) SetTaskVerifierPolicy(prdID, taskID string, mode *string, backends *[]string, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if err := m.taskOverrideLocked(prd, prdID); err != nil {
		return nil, err
	}
	task, _ := findTaskInPRD(prd, taskID)
	if task == nil {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	next, err := applyVerifierPolicyOverride(task.VerifierPolicy, mode, backends)
	if err != nil {
		return nil, err
	}
	task.VerifierPolicy = next
	now := time.Now()
	task.UpdatedAt = now
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "set_task_verifier_policy", Actor: actor,
		Note: fmt.Sprintf("task=%s %s", taskID, verifierPolicyNote(next)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// normalizeVerifierPolicyConfig validates the global policy written through
// a config set and stores it in canonical form: a non-empty mode lower-cased,
// backend entries trimmed. An empty backend entry or a negative number is an
// error, so a bad value is never stored.
func normalizeVerifierPolicyConfig(p *config.VerifierPolicyConfig) error {
	if err := p.Validate(); err != nil {
		return err
	}
	for _, b := range p.Backends {
		if strings.TrimSpace(b) == "" {
			return fmt.Errorf("verifier_policy.backends: entries must not be empty")
		}
	}
	if strings.TrimSpace(p.Mode) != "" {
		p.Mode, _ = config.NormalizeVerifierPolicyMode(p.Mode)
	} else {
		p.Mode = ""
	}
	p.Backends = config.NormalizeVerifierPolicyBackends(p.Backends)
	return nil
}
