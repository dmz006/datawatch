// Worker questions across a daemon restart. A task parked on a blocking
// question keeps its live worker through the restart, and a worker adopted by
// the new daemon parks its task on a new blocking question as a freshly
// spawned worker does. Without both, the operator's answer has no worker to
// reach.

package autonomous

import "log"

// decisionParkPending reports whether the task is parked on a question whose
// decision can still be typed into its worker: the question exists and has not
// expired, and no resume for it has been recorded beyond waiting.
func (m *Manager) decisionParkPending(t Task) bool {
	if t.Status != TaskWaitingCapacity {
		return false
	}
	qid := AwaitDecisionQuestionID(t.WaitReason)
	if qid == "" {
		return false
	}
	if dr := t.DecisionResume; dr != nil && dr.QuestionID == qid && dr.Delivery != DecisionWaiting {
		return false
	}
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return false
	}
	q, ok := store.Get(qid)
	return ok && q.State != QuestionExpired
}

// keepDecisionPark is the boot reset's answer for a task parked on a question
// whose worker outlived the daemon: the task stays parked on that worker,
// marked adopted so the executor waits for it, and takes no capacity lease.
// It reports false when the worker is gone, never started, or no later
// decision could resume it; the task is then requeued.
func (m *Manager) keepDecisionPark(prd *PRD, t *Task, aliveFn, agentStartedFn func(string) bool) bool {
	snap, ok := m.store.TaskSnapshot(t.ID)
	if !ok || snap.SessionID == "" || aliveFn == nil || !aliveFn(snap.SessionID) || !m.decisionParkPending(snap) {
		return false
	}
	if started := stableAgentStarted(agentStartedFn); started != nil && !started(snap.SessionID) {
		return false
	}
	m.store.WithLock(func() { t.AdoptedSession = true })
	if err := m.store.SaveTask(t); err != nil {
		log.Printf("[autonomous] boot-resume: task %s kept parked, but saving it failed: %v", t.ID, err)
	}
	log.Printf("[autonomous] boot-resume: task %s stays parked on decision %s with its live session %s for prd=%s",
		t.ID, AwaitDecisionQuestionID(snap.WaitReason), snap.SessionID, prd.ID)
	return true
}

// adoptedState reads, in one step, whether the task holds an adopted worker
// that is running, or one that is parked on a decision. A resume may move the
// task from the second to the first at any moment.
func (m *Manager) adoptedState(t *Task) (running, parked bool) {
	m.store.WithLock(func() {
		running = t.AdoptedSession && t.Status == TaskInProgress
		parked = t.AdoptedSession && t.Status == TaskWaitingCapacity && IsAwaitDecisionWaitReason(t.WaitReason)
	})
	return running, parked
}

// adoptedDecision is the question handling of one adopted worker's wait.
type adoptedDecision struct {
	m    *Manager
	prd  *PRD
	task *Task
	// handled is the last open question the task was parked on; hold alone
	// uses it. A park that something else released is not repeated.
	handled string
}

// hold parks the task on its worker's open blocking question, once per
// question, and reports whether the worker is waiting for a decision, so the
// wait must not nudge or stall it.
func (d *adoptedDecision) hold() bool {
	if d == nil {
		return false
	}
	q, open := d.m.OpenBlockingQuestionForTask(d.task.ID)
	snap, ok := d.m.store.TaskSnapshot(d.task.ID)
	if !ok || isTaskTerminal(snap.Status) {
		return false
	}
	if !open {
		return d.m.decisionParkPending(snap)
	}
	if q.ID != d.handled {
		d.handled = q.ID
		if snap.Status != TaskWaitingCapacity || AwaitDecisionQuestionID(snap.WaitReason) != q.ID {
			log.Printf("[autonomous] adopted worker of task %s raised blocking question %s: parking it for the decision", d.task.ID, q.ID)
			d.m.ParkForDecision(d.prd, d.task, q.ID)
		}
	}
	return true
}

// asked reports whether the worker has a blocking question open or the task is
// parked on one. It changes nothing.
func (d *adoptedDecision) asked() bool {
	if d == nil {
		return false
	}
	if _, open := d.m.OpenBlockingQuestionForTask(d.task.ID); open {
		return true
	}
	snap, ok := d.m.store.TaskSnapshot(d.task.ID)
	return ok && d.m.decisionParkPending(snap)
}

// closeNudgeForQuestion ends an open nudge as answered: the worker replied
// with a question.
func closeNudgeForQuestion(tr *WorkerNudgeTracker, hooks WorkerNudgeHooks, sessionID, label string) {
	if tr.done {
		return
	}
	log.Printf("[autonomous] idle worker %s task %s: nudge answered by a blocking question", sessionID, label)
	if hooks.Outcome != nil {
		hooks.Outcome(NudgeOutcomeAnswered)
	}
	tr.done = true
}
