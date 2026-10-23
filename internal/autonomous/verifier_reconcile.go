package autonomous

import (
	"fmt"
	"log"
	"sort"
	"time"
)

// Decision kind and audit action written by the verifier restart reconcile.
const (
	verifierReconcileKind   = "verifier_reconcile"
	verifierReconcileAction = "autonomous_verifier_reconcile"
)

// Reconcile action kinds.
const (
	verifierActionEnd   = "end"
	verifierActionAdopt = "adopt"
)

// VerifierSession is one live verifier one-shot session ("autonomous:verify")
// and the task it belongs to.
type VerifierSession struct {
	ID      string
	TaskID  string
	Started time.Time
}

// VerifierHost is the small slice of the session manager and capacity ledger
// the reconcile needs. End must go through the manager's normal end path for
// sessions.
type VerifierHost interface {
	// LiveVerifiers lists verifier sessions that have not finished.
	LiveVerifiers() []VerifierSession
	// End ends one session by id with a human-readable reason.
	End(sessionID, reason string) error
	// BindLease re-binds the verifier's capacity lease (holder) to sessionID.
	BindLease(holder, sessionID string)
}

// VerifierTaskInfo is what the reconcile needs to know about a task.
type VerifierTaskInfo struct {
	PRDID             string
	Status            TaskStatus
	VerifierSessionID string
}

// VerifierAction is one planned step of the reconcile.
type VerifierAction struct {
	Kind      string // verifierActionEnd | verifierActionAdopt
	SessionID string
	TaskID    string
	Reason    string
}

// PlanVerifierReconcile decides what to do with the live verifier sessions.
// It is pure. lookup reports a task's state; ok is false for an unknown task.
//   - a task that no longer needs verification: end all its verifiers;
//   - several verifiers for one task: keep the newest, end the rest;
//   - exactly one remaining verifier: adopt it (a no-op when already adopted).
func PlanVerifierReconcile(sessions []VerifierSession, lookup func(taskID string) (VerifierTaskInfo, bool)) []VerifierAction {
	byTask := map[string][]VerifierSession{}
	var order []string
	for _, s := range sessions {
		if s.TaskID == "" {
			continue
		}
		if _, seen := byTask[s.TaskID]; !seen {
			order = append(order, s.TaskID)
		}
		byTask[s.TaskID] = append(byTask[s.TaskID], s)
	}
	sort.Strings(order)

	var out []VerifierAction
	for _, taskID := range order {
		group := byTask[taskID]
		info, ok := lookup(taskID)
		if !ok || info.Status != TaskVerifying {
			why := "task is no longer awaiting verification (status " + string(info.Status) + ")"
			if !ok {
				why = "task no longer exists"
			}
			for _, s := range group {
				out = append(out, VerifierAction{Kind: verifierActionEnd, SessionID: s.ID, TaskID: taskID,
					Reason: "stale verifier after daemon restart: " + why})
			}
			continue
		}
		// Newest first; ties broken by id so the choice is deterministic.
		sort.SliceStable(group, func(i, j int) bool {
			if !group[i].Started.Equal(group[j].Started) {
				return group[i].Started.After(group[j].Started)
			}
			return group[i].ID > group[j].ID
		})
		keep := group[0]
		for _, s := range group[1:] {
			out = append(out, VerifierAction{Kind: verifierActionEnd, SessionID: s.ID, TaskID: taskID,
				Reason: "duplicate verifier after daemon restart: superseded by newer verifier " + keep.ID})
		}
		if info.VerifierSessionID != keep.ID {
			out = append(out, VerifierAction{Kind: verifierActionAdopt, SessionID: keep.ID, TaskID: taskID,
				Reason: "adopted the surviving verifier after daemon restart"})
		}
	}
	return out
}

// SetVerifierHost wires the host used by the startup verifier reconcile.
// Nil (the default) skips the pass.
func (m *Manager) SetVerifierHost(h VerifierHost) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verifierHost = h
}

// ReconcileVerifiers runs the plan against the host and the PRD store, once at
// startup after sessions are loaded and before verification resumes. Each
// action is written as a task decision and an audit event. It returns the
// actions it carried out and is idempotent: a second run finds nothing to do.
func (m *Manager) ReconcileVerifiers(host VerifierHost) []VerifierAction {
	if host == nil {
		return nil
	}
	lookup := func(taskID string) (VerifierTaskInfo, bool) {
		t, ok := m.store.TaskSnapshot(taskID)
		if !ok {
			return VerifierTaskInfo{}, false
		}
		return VerifierTaskInfo{PRDID: t.PRDID, Status: t.Status, VerifierSessionID: t.VerifierSessionID}, true
	}
	var done []VerifierAction
	for _, a := range PlanVerifierReconcile(host.LiveVerifiers(), lookup) {
		switch a.Kind {
		case verifierActionEnd:
			if err := host.End(a.SessionID, a.Reason); err != nil {
				log.Printf("[autonomous] verifier-reconcile: end session %s (task %s): %v", a.SessionID, a.TaskID, err)
				continue
			}
		case verifierActionAdopt:
			host.BindLease(VerifyHolder(a.TaskID), a.SessionID)
			m.store.MutateTask(a.TaskID, func(t *Task) { t.VerifierSessionID = a.SessionID })
		}
		info, _ := lookup(a.TaskID)
		m.recordVerifierReconcile(info.PRDID, a)
		done = append(done, a)
	}
	return done
}

// recordVerifierReconcile writes the task decision (when the task's PRD is
// known) and the audit event for one action.
func (m *Manager) recordVerifierReconcile(prdID string, a VerifierAction) {
	note := fmt.Sprintf("%s verifier session %s for task %s: %s", a.Kind, a.SessionID, a.TaskID, a.Reason)
	if prdID != "" {
		m.store.MutatePRD(prdID, func(p *PRD) bool {
			p.Decisions = append(p.Decisions, Decision{At: time.Now(), Kind: verifierReconcileKind, Actor: "autonomous", Note: note})
			return true
		})
	}
	m.mu.Lock()
	fn := m.auditFn
	m.mu.Unlock()
	if fn != nil {
		fn(verifierReconcileAction, map[string]any{
			"prd_id": prdID, "task_id": a.TaskID, "session_id": a.SessionID, "action": a.Kind, "reason": a.Reason,
		})
	}
}
