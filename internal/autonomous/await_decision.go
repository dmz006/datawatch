package autonomous

// Parking a task on an open blocking worker question. A worker that raised a
// blocking question (the DATAWATCH_QUESTION: marker) cannot make progress until
// the operator decides, so its task stops holding the pool: the task is shown
// as waiting with the await_decision sub-reason, its capacity lease is
// returned and its project-tree lock is released unless the tree has unpushed
// work.
//
// Unlike an await-external cycle the park does not re-admit the task and does
// not prompt the session: the task resumes only through the coordinator tick
// task. Like it, the park is not a failure: no retry is counted.

import "log"

// awaitDecisionConditionPrefix starts the condition text of the await-release
// audit event for a task parked on a worker question: "decision <id>".
const awaitDecisionConditionPrefix = "decision "

// AwaitDecisionWaitReasonFor renders the wait reason for a task parked on the
// given question: "await_decision: <question id>".
func AwaitDecisionWaitReasonFor(questionID string) string {
	return AwaitDecisionWaitText(questionID)
}

// OpenBlockingQuestionForTask returns the open blocking worker question
// raised for the task, and whether there is one. A non-blocking question, a
// question that is no longer open, a question raised for another task and an
// empty task id never match. When several match, the newest is returned.
func (m *Manager) OpenBlockingQuestionForTask(taskID string) (Question, bool) {
	if taskID == "" {
		return Question{}, false
	}
	m.mu.Lock()
	store := m.decisionStore
	m.mu.Unlock()
	if store == nil {
		return Question{}, false
	}
	for _, q := range store.List(QuestionFilter{State: QuestionOpen, Limit: maxQuestionListLimit}) {
		if q.TaskID == taskID && q.Blocking {
			return q, true
		}
	}
	return Question{}, false
}

// ParkForDecision parks the task on the open blocking question questionID. It
// marks the task waiting with the await_decision reason, releases its capacity
// lease, releases the project-tree lock unless the tree has unpushed work, and
// emits the await-release audit event with the condition "decision <id>". It
// does not re-admit the task, does not prompt its session, and never counts a
// retry or records a failure.
//
// It is idempotent: a task already parked on the same question is left as it
// is, with no second audit event. A task parked with a decision that is not
// typed yet is left as it is too (decision_owed.go), as is one whose decision
// line is unfinished and which questionID may not take over
// (takeOverUnfinishedLine). It reports whether this call parked the task.
func (m *Manager) ParkForDecision(prd *PRD, t *Task, questionID string) bool {
	if prd == nil || t == nil || questionID == "" {
		return false
	}
	m.questionTick.parkMu.Lock()
	defer m.questionTick.parkMu.Unlock()
	// Whatever this call decides, every open question of the task is owed.
	m.noteOwedDecisions(t.ID, questionID, m.openBlockingQuestionsForTask(t.ID))
	if cur, ok := m.store.TaskSnapshot(t.ID); ok {
		if cur.Status == TaskWaitingCapacity && cur.WaitReason == AwaitDecisionWaitReasonFor(questionID) {
			return false
		}
		if m.parkedDecisionUntyped(cur) || !m.takeOverUnfinishedLine(prd, cur, questionID) {
			return false
		}
	}
	m.parkOnDecision(prd, t, questionID, false)
	return true
}

// ParkedOnDecision reports whether the task is parked on the question now. A
// wait loop uses it, with ParkForDecision, to tell a park that already stands
// from one that was refused and must be tried again.
func (m *Manager) ParkedOnDecision(taskID, questionID string) bool {
	cur, ok := m.store.TaskSnapshot(taskID)
	return ok && questionID != "" && parkedQuestionID(cur) == questionID
}

// parkOnDecision makes the park ParkForDecision describes. With running set
// the worker is not waiting for anybody (its decision is already made): only
// the wait state changes, and it keeps its slot and the tree lock.
func (m *Manager) parkOnDecision(prd *PRD, t *Task, questionID string, running bool) {
	reason := AwaitDecisionWaitReasonFor(questionID)

	// Visible wait state (the reason carries the question id).
	m.setWaiting(t, reason)

	if running {
		recordWorkerQuestion(WorkerQuestionOutcomeParked)
		log.Printf("[autonomous] %s: decision %s is typed into the running worker: slot and tree lock kept", t.ID, questionID)
		return
	}

	// Release the lease(s). Capture which pools for the audit event.
	var releasedPools []string
	if led := m.Capacity(); led != nil {
		if lease := led.LeaseOf(t.ID); lease != nil && len(lease.Pools) > 0 {
			releasedPools = append([]string(nil), lease.Pools...)
		}
	}
	m.releaseCapacity(t.ID)

	// Tree lock: keep only while the tree has uncommitted changes or commits
	// not reachable from a remote-tracking ref.
	keepTree := m.treeHasUnpushedWork(prd, t)
	if box := m.treeLockBoxLocked(t.ID); !keepTree && box != nil && *box != nil {
		(*box)()
		*box = nil
	}

	m.emitAwaitReleaseAuditFor(prd, t, releasedPools, keepTree, awaitDecisionConditionPrefix+questionID)
	recordWorkerQuestion(WorkerQuestionOutcomeParked)
	log.Printf("[autonomous] %s: parked on decision %s: released pools=%v tree lock kept=%v",
		t.ID, questionID, releasedPools, keepTree)
}
