package autonomous

import (
	"fmt"
	"time"
)

// Decision kind and audit action recorded when the verifier policy picks the
// backend for one verification.
const (
	verifierPolicyDecisionKind = "verifier_policy"
	VerifierPolicyAuditAction  = "autonomous.verifier_policy"

	judgePolicyDecisionKind = "judge_policy"
	JudgePolicyAuditAction  = "autonomous.judge_policy"
)

// HeldBackends returns the subset of backends that are currently backed off
// (a usage limit hit, or an operator hold), read from the same per-backend
// back-off registry the usage_backoff status reports. Nil when none is held
// or no registry is wired.
func (m *Manager) HeldBackends(backends []string) map[string]bool {
	m.mu.Lock()
	bo := m.backoff
	m.mu.Unlock()
	if bo == nil {
		return nil
	}
	var held map[string]bool
	for _, b := range backends {
		if blocked, _ := bo.Blocked(b); blocked {
			if held == nil {
				held = map[string]bool{}
			}
			held[b] = true
		}
	}
	return held
}

// RecordVerifierPolicyChoice stores the verifier policy's pick on the task,
// appends a verifier_policy decision to the PRD, and writes an audit entry.
func (m *Manager) RecordVerifierPolicyChoice(prdID, taskID, backend, model, reason string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	now := time.Now()
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			if t.ID != taskID {
				continue
			}
			t.VerifierPolicyChoice = &VerifierPolicyChoice{Backend: backend, Model: model, Reason: reason, At: now}
			t.UpdatedAt = now
			found = true
		}
	}
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: verifierPolicyDecisionKind, Backend: backend, Model: model, Actor: "autonomous",
		Note: fmt.Sprintf("role=%s task=%s backend=%s model=%s reason=%s", PolicyRoleVerifier, taskID, backend, model, reason),
	})
	prd.UpdatedAt = now
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	m.capAudit.Record(AuditActorAutonomous, VerifierPolicyAuditAction, taskID, map[string]any{
		"prd_id": prdID, "task_id": taskID, "backend": backend, "model": model, "reason": reason,
		"role": string(PolicyRoleVerifier),
	})
	return nil
}

// RecordJudgePolicyChoice appends a judge_policy decision to the PRD and writes
// an audit entry, both carrying the judge role so a judge pick is told apart
// from a verifier pick. A judge may run for the PRD as a whole (plan review), so
// taskID is optional; unlike the verifier's pick it is not stored on a task.
func (m *Manager) RecordJudgePolicyChoice(prdID, taskID, backend, model, reason string) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	now := time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: judgePolicyDecisionKind, Backend: backend, Model: model, Actor: "autonomous",
		Note: fmt.Sprintf("role=%s task=%s backend=%s model=%s reason=%s", PolicyRoleJudge, taskID, backend, model, reason),
	})
	prd.UpdatedAt = now
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	m.capAudit.Record(AuditActorAutonomous, JudgePolicyAuditAction, prdID, map[string]any{
		"prd_id": prdID, "task_id": taskID, "backend": backend, "model": model, "reason": reason,
		"role": string(PolicyRoleJudge),
	})
	return nil
}
