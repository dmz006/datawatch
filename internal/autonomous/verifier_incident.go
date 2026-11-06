package autonomous

import (
	"fmt"
	"time"
)

// Decision kind and audit action recorded when a task's verifier went idle
// and is reported as a verifier incident rather than a worker failure.
const (
	verifierIncidentDecisionKind = "verifier_incident"
	VerifierIncidentAuditAction  = "autonomous.verifier_incident"
)

// RecordVerifierIncident reports that a task's verifier session went idle as
// a verifier incident, not a worker failure. It appends a
// VerifierIncidentEvent to the task itself (the task id, the verifier
// backend and model that went idle, and how long the verifier was idle),
// writes a verifier_incident decision on the Automaton's decision log, and
// records an audit entry. An idle verifier is the verifier's own incident;
// an idle-worker nudge, a delivery failure or a crashed worker are the
// worker-failure kinds it must not be confused with.
func (m *Manager) RecordVerifierIncident(prdID, taskID, backend, model string, idle time.Duration) error {
	now := time.Now()
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	found := false
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if t.ID != taskID {
					continue
				}
				t.VerifierIncidentEvents = append(t.VerifierIncidentEvents, VerifierIncidentEvent{
					At:      now,
					TaskID:  taskID,
					Backend: backend,
					Model:   model,
					Idle:    idle,
				})
				t.UpdatedAt = now
				prd.Decisions = append(prd.Decisions, Decision{
					At:              now,
					Kind:            verifierIncidentDecisionKind,
					Backend:         backend,
					Model:           model,
					Actor:           "autonomous",
					FailureCategory: "infra",
					Note: fmt.Sprintf(
						"Automaton verifier incident: task=%s backend=%s model=%s idle=%v — the verifier went idle; reported as a verifier incident, not a worker failure",
						taskID, backend, model, idle),
				})
				prd.UpdatedAt = now
				found = true
				return
			}
		}
	})
	if !found {
		return fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	m.capAudit.Record(AuditActorAutonomous, VerifierIncidentAuditAction, taskID, map[string]any{
		"prd_id": prdID, "task_id": taskID, "backend": backend, "model": model, "idle": idle.String(),
	})
	return nil
}
