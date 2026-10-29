// Decision gate in the executor — keeping a task from starting while its spec
// names an operator decision that is not answered.
//
// Before a pending or queued task starts, the executor runs the gate rule
// (DecisionHold, decision_gate.go) on the task's spec. In mode on a task with
// holds is parked in TaskWaitingDecision with the reasons as its WaitReason,
// and its dependents wait behind it. A parked task whose holds have all
// cleared goes back to pending in the same scan and runs. In mode shadow the
// task runs and the Automaton decision log records, once, what would have
// held it. The text HOLD line (PRDHoldReason) is a separate rule and is not
// touched here.
//
// An executor with a task parked on a decision stays alive: it waits for the
// answer instead of returning (awaitDecisionAnswer), and API.OnQuestionChange
// wakes it when the decision is answered.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// decisionGateShadowKind is the Decision.Kind written in mode shadow for a
// task the gate would have held.
const decisionGateShadowKind = "decision_gate_shadow"

// decisionWaitPoll is the longest an executor with a task parked on a
// decision waits before it re-checks on its own. An answer wakes the executor
// at once; the poll covers a decision that is withdrawn, an operator who
// cancels the parked task, and an answer that lands between the check and the
// wait. A var so tests can shorten it.
var decisionWaitPoll = 30 * time.Second

// SetDecisionGate wires the decision gate: the question store the gate reads
// decisions from, and a function returning the live autonomous.questions
// config (read on every scan, so a config write takes effect without a
// restart). With no config function the gate is off.
func (m *Manager) SetDecisionGate(store *QuestionStore, cfg func() config.AutonomousQuestionsConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisionStore = store
	m.decisionGateCfg = cfg
}

// decisionGateSettings returns the gate's question store and whether it is
// enabled and in mode on. A gate that is not wired is off.
func (m *Manager) decisionGateSettings() (store *QuestionStore, enabled, modeOn bool) {
	m.mu.Lock()
	store, cfgFn := m.decisionStore, m.decisionGateCfg
	m.mu.Unlock()
	if cfgFn == nil {
		return store, false, false
	}
	q := cfgFn()
	return store, q.EffectiveDecisionGate(), q.EffectiveDecisionGateMode() == config.DecisionGateModeOn
}

// decisionGateHold runs the gate on t and reports whether t must not start. It
// only looks at a task that is pending, queued or already waiting on a
// decision; running and finished tasks are never checked. A task found held
// is parked in TaskWaitingDecision. A parked task whose holds have cleared (or
// whose gate was turned off or set to shadow) is returned to pending and
// reported as not held.
func (m *Manager) decisionGateHold(prd *PRD, t *Task) bool {
	var status TaskStatus
	var spec string
	m.store.WithLock(func() { status, spec = normTaskStatus(t.Status), t.Spec })
	switch status {
	case TaskPending, TaskQueued, TaskWaitingDecision:
	default:
		return false
	}
	store, enabled, modeOn := m.decisionGateSettings()
	var holds []DecisionHoldReason
	if enabled {
		holds = DecisionHold(store, spec)
	}
	if len(holds) > 0 && modeOn {
		m.holdTaskForDecision(prd, t, holds)
		return true
	}
	if status == TaskWaitingDecision {
		m.releaseDecisionHold(prd, t)
	}
	if len(holds) > 0 {
		m.recordDecisionShadow(prd, t, holds)
	}
	return false
}

// decisionHoldSentence joins the hold reasons into the one plain sentence
// shown as the task's wait reason.
func decisionHoldSentence(holds []DecisionHoldReason) string {
	reasons := make([]string, len(holds))
	for i, h := range holds {
		reasons[i] = h.Reason
	}
	return strings.Join(reasons, "; ")
}

// holdTaskForDecision parks t in TaskWaitingDecision with the holds as its
// wait reason, saves it and announces the status. Nothing happens when t is
// already parked for the same reason. The one operator alert is raised only
// when t enters the hold, not on each later scan.
func (m *Manager) holdTaskForDecision(prd *PRD, t *Task, holds []DecisionHoldReason) {
	reason := decisionHoldSentence(holds)
	var old TaskStatus
	changed := false
	m.store.WithLock(func() {
		old = t.Status
		if t.Status != TaskWaitingDecision || t.WaitReason != reason {
			t.Status = TaskWaitingDecision
			t.WaitReason = reason
			changed = true
		}
	})
	if !changed {
		return
	}
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] prd=%s task=%s save waiting_decision: %v", prd.ID, t.ID, err)
	}
	m.emitTaskStatus(prd, t, old, nil, nil)
	m.EmitPRDUpdate(prd.ID)
	if old != TaskWaitingDecision {
		m.raiseDecisionHoldAlert(prd, t, reason)
	}
}

// releaseDecisionHold returns a parked task to pending and clears its wait
// reason.
func (m *Manager) releaseDecisionHold(prd *PRD, t *Task) {
	var old TaskStatus
	m.store.WithLock(func() {
		old = t.Status
		t.Status = TaskPending
		t.WaitReason = ""
	})
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] prd=%s task=%s save pending after decision: %v", prd.ID, t.ID, err)
	}
	m.emitTaskStatus(prd, t, old, nil, nil)
	m.EmitPRDUpdate(prd.ID)
}

// recordDecisionShadow appends the decision_gate_shadow entry for a task the
// gate would have held in mode on. The entry carries the first hold's
// question id and state. The same task with the same reasons is recorded once:
// a later scan finds the entry on the log and appends nothing. The note names
// the task, because the entry has no task field and two tasks may name the
// same decision.
func (m *Manager) recordDecisionShadow(prd *PRD, t *Task, holds []DecisionHoldReason) {
	p, ok := m.store.GetPRD(prd.ID)
	if !ok {
		return
	}
	dec := Decision{
		At:            m.attemptNow(),
		Kind:          decisionGateShadowKind,
		Actor:         "autonomous",
		Note:          fmt.Sprintf("would hold: %s (task %s)", decisionHoldSentence(holds), t.ID),
		QuestionID:    holds[0].ID,
		DecisionState: holds[0].State,
	}
	appended := false
	m.store.WithLock(func() {
		for _, d := range p.Decisions {
			if d.Kind == dec.Kind && d.QuestionID == dec.QuestionID &&
				d.DecisionState == dec.DecisionState && d.Note == dec.Note {
				return
			}
		}
		p.Decisions = append(p.Decisions, dec)
		appended = true
	})
	if !appended {
		return
	}
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s save %s decision: %v", p.ID, dec.Kind, err)
	}
}

// raiseDecisionHoldAlert sends the one operator alert for a task parked on a
// decision, through the sink raiseReviewAlert uses. Nothing is sent when no
// alert sink is wired.
func (m *Manager) raiseDecisionHoldAlert(prd *PRD, t *Task, reason string) {
	m.mu.Lock()
	alert := m.alertSink
	m.mu.Unlock()
	if alert == nil {
		return
	}
	body := fmt.Sprintf(
		"Automaton: %s (%s)\nTask: %s (%s)\nWaiting on: %s\n"+
			"The task starts by itself once the decision is answered.",
		prd.ID, prd.Title, t.ID, t.Title, reason)
	alert("warn", "Automaton task waiting for a decision", body)
}

// decisionWaitPending reports whether the executor should wait for an answer
// instead of returning: a task of prd is parked on a decision, no task is held
// for operator review (that blocks the Automaton and ends the run), and the
// Automaton is still running.
func (m *Manager) decisionWaitPending(prd *PRD) bool {
	waiting := false
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				if prd.Story[si].Tasks[ti].Status == TaskWaitingDecision {
					waiting = true
				}
			}
		}
	})
	if !waiting || m.reviewHeldTaskID(prd) != "" {
		return false
	}
	st, ok := m.prdStatusOf(prd.ID)
	return ok && st == PRDRunning
}

// decisionRefTasks reports, for the unfinished tasks of prd, whether any spec
// names decision id and whether any of those tasks is parked on a decision.
func (m *Manager) decisionRefTasks(prd *PRD, id string) (referenced, waiting bool) {
	m.store.WithLock(func() {
		for si := range prd.Story {
			for ti := range prd.Story[si].Tasks {
				t := &prd.Story[si].Tasks[ti]
				if isTaskTerminal(t.Status) {
					continue
				}
				for _, ref := range DecisionRefs(t.Spec) {
					if ref == id {
						referenced = true
						waiting = waiting || t.Status == TaskWaitingDecision
					}
				}
			}
		}
	})
	return referenced, waiting
}

// prdStatusOf reads the stored status of an Automaton under the Manager mutex,
// the lock status writes hold.
func (m *Manager) prdStatusOf(id string) (PRDStatus, bool) {
	p, ok := m.store.GetPRD(id)
	if !ok {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return p.Status, true
}

// awaitDecisionAnswer blocks until the wake channel is nudged, the poll
// interval passes, or ctx ends. It reports false when ctx ended or there is no
// wake channel to wait on (a Run without a live executor), so the caller
// stops waiting.
func awaitDecisionAnswer(ctx context.Context, wake <-chan struct{}) bool {
	if wake == nil {
		return false
	}
	timer := time.NewTimer(decisionWaitPoll)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-wake:
		return true
	case <-timer.C:
		return true
	}
}
