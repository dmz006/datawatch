package autonomous

import (
	"fmt"
	"time"
)

// Decision kind, audit action and end reason for a verifier session that the
// run cap (autonomous.verifier_max_run_seconds) ended.
const (
	verifierRunCapDecisionKind = "verifier_run_cap"
	// VerifierRunCapAuditAction is the audit action written next to the
	// decision.
	VerifierRunCapAuditAction = "autonomous.verifier_run_cap"
	// VerifierMaxRunEndReason is the attempt end reason of a verification that
	// the run cap ended. The kill request of the one-shot runner carries no
	// reason field, so the reason is recorded on the attempt.
	VerifierMaxRunEndReason = "verifier_max_run"
)

// attemptEndVerifierMaxRun is the attempt end reason for a run-cap end.
const attemptEndVerifierMaxRun = VerifierMaxRunEndReason

// ReleaseVerifierRunCapLease frees the capacity lease held under
// VerifyHolder(taskID) and reports whether one was held. It is a no-op when no
// lease is held, so a run-cap end that comes after an earlier release does not
// release a slot a different holder took in the meantime.
func (m *Manager) ReleaseVerifierRunCapLease(taskID string) bool {
	led := m.Capacity()
	holder := VerifyHolder(taskID)
	if led == nil || !led.Held(holder) {
		return false
	}
	m.releaseCapacity(holder)
	return true
}

// HandleVerifierRunCap is what a run-cap end does before the retry decision:
// it releases the verifier's lease once, then writes the decision and audit
// record. released reports whether a lease was held and freed. The decision is
// written even when no lease was held.
func (m *Manager) HandleVerifierRunCap(prdID, taskID string, elapsed time.Duration, capSeconds int) (released bool, err error) {
	released = m.ReleaseVerifierRunCapLease(taskID)
	return released, m.RecordVerifierRunCap(prdID, taskID, elapsed, capSeconds)
}

// RecordVerifierRunCap notes on the PRD's decision log, and in the audit log,
// that the run cap ended the task's verifier session. The note carries the
// task id, the elapsed seconds and the cap in seconds.
func (m *Manager) RecordVerifierRunCap(prdID, taskID string, elapsed time.Duration, capSeconds int) error {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return fmt.Errorf("prd %q not found", prdID)
	}
	elapsedSeconds := int(elapsed.Seconds())
	m.store.WithLock(func() {
		prd.Decisions = append(prd.Decisions, Decision{
			At: time.Now(), Kind: verifierRunCapDecisionKind, Actor: "autonomous",
			Note: fmt.Sprintf("task=%s elapsed_seconds=%d cap_seconds=%d end_reason=%s",
				taskID, elapsedSeconds, capSeconds, VerifierMaxRunEndReason),
			FailureCategory: "infra",
		})
		prd.UpdatedAt = time.Now()
	})
	if err := m.store.SavePRD(prd); err != nil {
		return err
	}
	m.capAudit.Record(AuditActorAutonomous, VerifierRunCapAuditAction, taskID, map[string]any{
		"prd_id": prdID, "task_id": taskID, "elapsed_seconds": elapsedSeconds,
		"cap_seconds": capSeconds, "end_reason": VerifierMaxRunEndReason,
	})
	return nil
}
