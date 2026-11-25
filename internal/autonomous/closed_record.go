// Closed records stay closed. No boot-time or periodic recovery (hold
// releases, task resets, boot resume, repair passes, answer routing, merge
// retries, lane fill) may touch a cancelled, rejected or archived Automaton,
// or one with a recorded closed outcome. Every such path asks the one check
// here, closedRecordReason.
//
// Only the operator reopens a closed record: the operator's own task reset,
// reset to draft, or force past the shipped-work rule. Each of those clears the
// outcome, so the record is open again for every path. A record that carries a
// closed outcome but reads running was reopened by something else; the daemon
// puts its closed status back and records why.
//
// The rule has no setting: it holds with no model cooperating, whatever
// autonomous.shipped_refusal and autonomous.closed_outcome_archive say.
package autonomous

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// Decision kinds written by the closed record rule.
const (
	// DecisionClosedRecordRestored: a record with a closed outcome read
	// running, and its closed status was put back.
	DecisionClosedRecordRestored = "closed_record_restored"
	// DecisionClosedOutcomeCleared: the operator reopened the record, so its
	// closed outcome no longer stands.
	DecisionClosedOutcomeCleared = "closed_outcome_cleared"
	// DecisionClosedRecordAnswerIgnored: an answered question belongs to a
	// closed record, so nothing was applied.
	DecisionClosedRecordAnswerIgnored = "closed_record_answer_ignored"
)

// closedRecordCaller is the caller name of the sessions the restore ends.
const closedRecordCaller = "autonomous.closed-record.restore"

// closedRecordReason is the one shared closed check: why recovery must leave
// the Automaton alone, or "" when the record is open. The caller holds the
// store lock or passes a snapshot.
//
//   - the status is cancelled, rejected or archived; or
//   - a closed outcome is recorded (delivered, superseded, dropped or held),
//     whatever the status reads, unless the operator reopened the record after
//     it.
func closedRecordReason(p *PRD) string {
	if p == nil {
		return ""
	}
	switch s := NormalizePRDStatus(p.Status); s {
	case PRDCancelled, PRDRejected, PRDArchived:
		return fmt.Sprintf("its status is %s", s)
	}
	if p.ClosedOutcome != "" && !operatorReopened(p) {
		return fmt.Sprintf("it carries the closed outcome %s", p.ClosedOutcome)
	}
	return ""
}

// operatorReopened reports whether the decision log shows an operator reopen
// newer than the last closing entry. A reopen clears the outcome, so this only
// matters for a record the operator reopened before it did: the restore must
// not undo that.
func operatorReopened(p *PRD) bool {
	for i := len(p.Decisions) - 1; i >= 0; i-- {
		switch d := p.Decisions[i]; d.Kind {
		case DecisionClosedOutcome, "cancel", "reject", "archive":
			return false
		default:
			if isOperatorReopen(d) {
				return true
			}
		}
	}
	return false
}

// isOperatorReopen reports whether d is one of the operator's own reopen steps.
// Reset to draft and the shipped-work force have no automatic caller. A task
// reset has many, so only the operator's counts.
func isOperatorReopen(d Decision) bool {
	switch d.Kind {
	case "reset_to_draft", DecisionClosedWorkOverride:
		return true
	case "reset_task", "requeue_task":
		return isOperatorActor(d.Actor)
	}
	return false
}

// closedReason is closedRecordReason read under the store lock. Call it with no
// store lock held.
func (m *Manager) closedReason(p *PRD) string {
	var why string
	m.readLocked(func() { why = closedRecordReason(p) })
	return why
}

// closedToRecovery reports whether recovery must leave the Automaton alone.
// Call it with no store lock held.
func (m *Manager) closedToRecovery(p *PRD) bool { return m.closedReason(p) != "" }

// closedToRecoveryID is closedToRecovery by id; an unknown id is not closed.
func (m *Manager) closedToRecoveryID(id string) bool {
	if m == nil || m.store == nil || id == "" {
		return false
	}
	p, ok := m.store.GetPRD(id)
	return ok && m.closedToRecovery(p)
}

// refuseClosedOutcome refuses to start or resume a record that carries a closed
// outcome while its status still reads open. The closed statuses themselves are
// refused by the status checks and by refuseClosedWork.
func (m *Manager) refuseClosedOutcome(prd *PRD) error {
	if m == nil || prd == nil {
		return nil
	}
	var text string
	m.readLocked(func() {
		if !closedWorkStatus(prd.Status) && closedRecordReason(prd) != "" {
			text = fmt.Sprintf("is closed with outcome %s although it reads %s: it never runs", prd.ClosedOutcome, prd.Status)
		}
	})
	if text == "" {
		return nil
	}
	m.recordClosedRefusal(prd.ID, text)
	return fmt.Errorf("automaton %q %s; to reopen it, reset it to draft or reset a task as the operator", prd.ID, text)
}

// clearClosedOutcomeLocked drops the closed outcome of a record the operator
// reopens, and records what it was. The caller holds the store lock.
func clearClosedOutcomeLocked(p *PRD, actor, why string, at time.Time) {
	if p.ClosedOutcome == "" {
		return
	}
	was := p.ClosedOutcome
	if p.DeliveredVia != "" {
		was += " via " + p.DeliveredVia
	}
	p.ClosedOutcome, p.DeliveredVia, p.ClosedOutcomeNote, p.ClosedOutcomeAt = "", "", "", nil
	p.Decisions = trimDecisions(append(p.Decisions, Decision{At: at, Kind: DecisionClosedOutcomeCleared, Actor: actor,
		Note: fmt.Sprintf("Closed outcome cleared (it was %s): %s.", was, why)}))
}

// revivedClosedRecord reports whether the record carries a closed outcome that
// still stands but reads running. The caller holds the store lock.
func revivedClosedRecord(p *PRD) bool {
	if p == nil || p.IsTemplate || p.ClosedOutcome == "" {
		return false
	}
	s := NormalizePRDStatus(p.Status)
	return (s == PRDRunning || s == PRDActive) && closedRecordReason(p) != ""
}

// closedStatusToRestore reads the closed status the record had from its own
// log: the newest cancel, reject or archive since the operator last reopened
// it. A log with none of those gives completed for delivered work and
// cancelled for anything else.
func closedStatusToRestore(p *PRD) PRDStatus {
	unarchived := false
scan:
	for i := len(p.Decisions) - 1; i >= 0; i-- {
		switch d := p.Decisions[i]; d.Kind {
		case unarchiveDecisionKind:
			unarchived = true // the archive before this entry was undone
		case "archive":
			if !unarchived {
				return PRDArchived
			}
			unarchived = false
		case "reject":
			return PRDRejected
		case "cancel":
			return PRDCancelled
		default:
			if isOperatorReopen(d) {
				break scan
			}
		}
	}
	switch {
	case p.Archive != nil:
		return PRDArchived
	case p.ClosedOutcome == OutcomeDelivered:
		return PRDCompleted
	}
	return PRDCancelled
}

// RestoreClosedRecords puts back the closed status of every Automaton that
// carries a closed outcome but reads running, records a decision naming why,
// and ends the worker sessions the reopening started. It returns the ids it
// restored. It runs before boot resume and the boot reconcile, and on every
// poll.
func (m *Manager) RestoreClosedRecords() []string {
	if m == nil || m.store == nil {
		return nil
	}
	var ids []string
	prds := m.store.ListPRDs()
	m.readLocked(func() {
		for _, p := range prds {
			if revivedClosedRecord(p) {
				ids = append(ids, p.ID)
			}
		}
	})
	var out []string
	for _, id := range ids {
		if m.restoreClosedRecord(id) {
			out = append(out, id)
		}
	}
	return out
}

// restoreClosedRecord restores one record, re-checked under the store lock.
func (m *Manager) restoreClosedRecord(id string) bool {
	at := m.holdNow()
	var old, to PRDStatus
	var note string
	type liveTask struct{ task, session string }
	var live []liveTask
	if !m.store.MutatePRD(id, func(p *PRD) bool {
		if !revivedClosedRecord(p) {
			return false
		}
		old, to = p.Status, closedStatusToRestore(p)
		outcome := p.ClosedOutcome
		if via := strings.TrimSpace(p.DeliveredVia); via != "" {
			outcome += " via " + via
		}
		when := ""
		if p.ClosedOutcomeAt != nil {
			when = " recorded " + p.ClosedOutcomeAt.UTC().Format(time.RFC3339)
		}
		note = fmt.Sprintf("Status restored from %s to %s: the Automaton carries the closed outcome %s%s, no operator reopened it, and closed records stay closed.", old, to, outcome, when)
		p.Status = to
		p.BlockedReason = ""
		clearPlannerFields(p)
		p.Decisions = trimDecisions(append(p.Decisions, Decision{At: at, Kind: DecisionClosedRecordRestored, Actor: "autonomous", Note: note}))
		for si := range p.Story {
			for ti := range p.Story[si].Tasks {
				if t := &p.Story[si].Tasks[ti]; !isTaskTerminal(t.Status) {
					live = append(live, liveTask{task: t.ID, session: t.SessionID})
				}
			}
		}
		return true
	}) {
		return false
	}
	log.Printf("[autonomous] closed-record: prd=%s: %s", id, note)
	m.mu.Lock()
	kill := m.sessionKillerFn
	m.mu.Unlock()
	for _, t := range live {
		m.releaseCapacity(t.task)
		if kill == nil || t.session == "" {
			continue
		}
		why := fmt.Sprintf("Automaton %s is closed: task %s session ends with it", id, t.task)
		if err := kill(t.session, closedRecordCaller, why); err != nil {
			log.Printf("[autonomous] closed-record: prd=%s: ending session %s: %v", id, t.session, err)
		}
	}
	if p, ok := m.store.GetPRD(id); ok {
		m.emitPRDStatus(p, old, lastDecision(p, DecisionClosedRecordRestored), nil)
	}
	m.EmitPRDUpdate(id)
	return true
}

// ignoreAnswerForClosedRecord reports whether an answered question belongs to a
// closed record. Nothing is applied then, and the record says so once per
// question.
func (m *Manager) ignoreAnswerForClosedRecord(c QuestionChange) bool {
	if !m.closedToRecoveryID(c.Question.PRDID) {
		return false
	}
	at := m.holdNow()
	m.store.MutatePRD(c.Question.PRDID, func(p *PRD) bool {
		if d := lastDecision(p, DecisionClosedRecordAnswerIgnored); d != nil && d.QuestionID == c.Question.ID {
			return false
		}
		p.Decisions = trimDecisions(append(p.Decisions, Decision{At: at, Kind: DecisionClosedRecordAnswerIgnored, Actor: "autonomous",
			QuestionID: c.Question.ID,
			Note:       fmt.Sprintf("The answer was not applied: the Automaton is closed (%s).", closedRecordReason(p))}))
		return true
	})
	log.Printf("[autonomous] closed-record: prd=%s: answer to %s not applied", c.Question.PRDID, c.Question.ID)
	return true
}
