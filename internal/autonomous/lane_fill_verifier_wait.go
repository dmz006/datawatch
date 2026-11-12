// Lane fill's view of a lane blocked on verification. An occupied lane whose
// running tasks are all waiting on a verifier slot is held without
// advancing, and the lane status must say so — while one lane is held, the
// planner must not keep starting work on it. This file computes that
// fact-only view from the capacity ledger's wait queue and the Automaton's
// tasks. It changes nothing about how lanes or slots are counted.
package autonomous

import (
	"strings"

	"github.com/dmz006/datawatch/internal/capacity"
)

// verifierWaitRunningTask is the set of task statuses that count as "running"
// for the check: the task holds the Automaton's lane and can be blocked on
// its verification. Queued tasks have not started, and tasks parked in
// waiting_capacity are stalled on something else, so neither can hold the
// lane in validation.
var verifierWaitRunningTask = map[TaskStatus]bool{
	TaskInProgress:   true,
	TaskRunningTests: true,
	TaskVerifying:    true,
}

// taskVerifierWait answers for one running task: is it blocked waiting on a
// verifier slot, and if so with what reason. A verifier-slot wait is
// registered in the capacity ledger's wait queue under the task's verification
// holder id — the id shapes the code uses when it records one:
//
//   - "verify:<taskID>" (VerifyHolder) — the task's verifier is queued for
//     the capacity slot, and the task is stalled in verification;
//   - "judge:<taskID>" (JudgeHolder with this task as the unit) — the
//     guardrail judge that must pass to finish the task's verification is
//     queued for a capacity slot;
//   - "<taskID>" — the task's own (re-)admission is queued while the slot of
//     its verification is still held.
//
// A waiter for another id — a judge on another unit (story, the Automaton),
// another task's verifier, or a session lease — can never block verification
// of this task, so it is not a verifier-slot wait. The second result is the
// waiter's text reason, or "" when the task is not waiting.
func taskVerifierWait(taskID string, waiters map[string]capacity.Waiter) (bool, string) {
	for _, h := range []string{VerifyHolder(taskID), JudgeHolder(taskID), taskID} {
		w, ok := waiters[h]
		if !ok {
			continue
		}
		if reason := strings.TrimSpace(w.ReadReason); reason != "" {
			return true, reason
		}
		if reason := strings.TrimSpace(w.Reason); reason != "" {
			return true, reason
		}
		return true, "waiting for a capacity slot to finish verification"
	}
	return false, ""
}

// prdVerifierWait reports whether the Automaton's lane is occupied and all
// of its running tasks are blocked waiting on a verifier slot — that is, the
// lane is held without advancing. It requires at least one running task.
//
// The second result is the first non-empty waiter's reason found (readable
// form when available), for the status to surface.
func (m *Manager) prdVerifierWait(prdID string) (bool, string) {
	if m == nil || m.store == nil {
		return false, ""
	}
	p, ok := m.store.SnapshotPRD(prdID)
	if !ok {
		return false, ""
	}
	running := []string{}
	for si := range p.Story {
		s := &p.Story[si]
		for ti := range s.Tasks {
			t := &s.Tasks[ti]
			if verifierWaitRunningTask[t.Status] {
				running = append(running, t.ID)
			}
		}
	}
	if len(running) == 0 {
		return false, ""
	}
	waiters := map[string]capacity.Waiter{}
	if led := m.Capacity(); led != nil {
		for _, w := range led.Snapshot().Waiting {
			waiters[w.Holder] = w
		}
	}
	reason := ""
	for _, id := range running {
		ok, r := taskVerifierWait(id, waiters)
		if !ok {
			return false, ""
		}
		if reason == "" && r != "" {
			reason = r
		}
	}
	if r := m.ReadableWaitSentence(reason); r != "" {
		reason = r
	}
	return true, reason
}

// laneFillVerifierWait annotates a lane's occupant when the Automaton it
// holds is occupied and its running tasks are all blocked on a verifier slot.
// It fills WaitingOnVerifier and WaitReason. Lane counting is not changed.
func (c *Coordinator) laneFillVerifierWait(o LaneOccupant) LaneOccupant {
	if waiting, reason := c.m.prdVerifierWait(o.PRDID); waiting {
		o.WaitingOnVerifier = true
		o.WaitReason = reason
	}
	return o
}
