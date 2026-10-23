package autonomous

import (
	"fmt"
	"time"
)

// Decision kind and audit action recorded when one task's verification is
// moved to the fallback backend.
const (
	verifierEscalationDecisionKind = "verifier_escalation"
	VerifierEscalationAuditAction  = "autonomous.verifier_escalation"
)

// DecideVerifierBackend picks the backend for the next verification of one
// task. failures is the task's consecutive infra verifier failure count;
// threshold and fallback are the normalized config values. It returns the
// configured backend unchanged unless the count has reached the threshold and
// the fallback differs from the configured backend, in which case it returns
// the fallback and true.
func DecideVerifierBackend(failures, threshold int, configured, fallback string) (backend string, escalated bool) {
	if threshold <= 0 || failures < threshold || fallback == "" || fallback == configured {
		return configured, false
	}
	return fallback, true
}

// RecordVerifierOutcome updates the task's consecutive infra verifier failure
// count after a verification: an infra result adds one unless the run was on
// the fallback backend, and any other result, or the end of an escalated run,
// resets it to zero.
func (m *Manager) RecordVerifierOutcome(prdID, taskID string, infra, escalated bool) error {
	return m.mutateTask(prdID, taskID, func(t *Task) {
		if infra && !escalated {
			t.VerifierInfraFailures++
		} else {
			t.VerifierInfraFailures = 0
		}
	})
}

// RecordVerifierEscalation notes on the PRD's decision log, and in the audit
// log, that the task's next verification moved from one backend to another.
func (m *Manager) RecordVerifierEscalation(prdID, taskID, from, to string, count int) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: verifierEscalationDecisionKind, Backend: to, Actor: "autonomous",
		Note:            fmt.Sprintf("task=%s from=%s to=%s consecutive_infra_failures=%d", taskID, from, to, count),
		FailureCategory: "infra",
	})
	prd.UpdatedAt = time.Now()
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	m.capAudit.Record(AuditActorAutonomous, VerifierEscalationAuditAction, taskID, map[string]any{
		"prd_id": prdID, "task_id": taskID, "from_backend": from, "to_backend": to, "consecutive_infra_failures": count,
	})
	return nil
}

func (m *Manager) mutateTask(prdID, taskID string, fn func(*Task)) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				fn(&prd.Story[si].Tasks[ti])
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				prd.UpdatedAt = time.Now()
				return m.store.SavePRD(prd)
			}
		}
	}
	return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
}
