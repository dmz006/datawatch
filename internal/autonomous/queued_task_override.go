// Queued-task LLM override (autonomous.override_queued_tasks).
//
// While the Automaton is running — a PRD status NOT in
// task_override_statuses (typically "running") — task-level and story-level
// LLM overrides (set_task_llm / set_story_llm) were refused outright. With
// override_queued_tasks enabled (the default: unset reads true — see
// EffectiveOverrideQueuedTasks), such a call is accepted instead, under this
// rule:
//
//   - The existing status gate (taskOverrideLocked) is unchanged: a PRD
//     status that is in the accepted list takes the classic path exactly as
//     before.
//   - Outside that list, the call is accepted when the setting is enabled
//     (refused, as before, when it is explicitly false).
//   - It touches ONLY tasks that have not started yet: pending, queued,
//     waiting for a capacity lease, waiting for a decision, awaiting
//     approval. Running and finished tasks are never modified.
//   - The new backend/effort/model are written to the record — story level
//     or task level — exactly where the operator pointed them, so the
//     existing admission/resolution code (resolveTaskExecutionLLM →
//     admit → acquireLease) already honours them on the task's next start.
//   - Any changed task that is sitting in the capacity queue has its single
//     live request retargeted in place to the new backend's pool
//     (capacity.Ledger.Retarget) — the queued request is cancelled and
//     re-queued on the new pool. Re-targeting never duplicates: retargeting
//     a queue entry that already carries the desired target is a no-op, and
//     a task that holds a live lease is left entirely alone. Changing the
//     same task twice therefore leaves exactly one live request for it, on
//     the latest pool.
//
// The structured per-task outcome (changed / left_alone, with a reason per
// task and whether its capacity request moved) travels back as
// LLMOverrideResult through the REST, MCP and CLI surfaces.

package autonomous

import (
	"fmt"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// LLMOverrideOutcome is one task's fate in a queued-mode override call.
type LLMOverrideOutcome struct {
	// TaskID is the task the outcome refers to.
	TaskID string `json:"task_id"`
	// Reason explains the outcome: why the change applies (changed) or why
	// the task was left alone (left_alone).
	Reason string `json:"reason"`
	// CapacityMoved is true when the task was waiting for a capacity lease
	// and its queued request was retargeted to the new backend's pool.
	CapacityMoved bool `json:"capacity_moved,omitempty"`
}

// LLMOverrideResult is the structured per-task outcome of a queued-mode
// set_task_llm / set_story_llm call. It is present only in that mode: a
// classic-accepted call (PRD status in task_override_statuses) and a
// refused call (setting disabled) return a nil result, exactly like the
// pre-behaviour.
type LLMOverrideResult struct {
	// Changed lists the unstarted tasks the override was applied to.
	Changed []LLMOverrideOutcome `json:"changed"`
	// LeftAlone lists the tasks that carry the same scope (for a
	// story-level call, the rest of the story's tasks) but were not
	// modified because they are running/finished/parked, with the reason.
	LeftAlone []LLMOverrideOutcome `json:"left_alone"`
}

// unstartedTask is the "not started yet" predicate a queued-mode override
// may act on: ready or in line (pending/queued), waiting for a capacity
// lease (waiting_capacity), waiting for a decision or approval, or never
// scheduled at all (empty status — a freshly created task). Running
// (in_progress, running_tests, verifying) and finished (completed, failed,
// cancelled) tasks are never modified, along with parked/blocked and
// review-needed tasks.
func unstartedTask(s TaskStatus) bool {
	if s == "" {
		return true
	}
	switch s {
	case TaskPending, TaskQueued, TaskWaitingCapacity, TaskWaitingDecision, TaskAwaitingApproval:
		return true
	}
	return false
}

// taskLeftAloneReason explains why a task a queued-mode override could not
// act on.
func taskLeftAloneReason(s TaskStatus) string {
	switch s {
	case TaskInProgress, TaskRunningTests, TaskVerifying:
		return "task is running; the override takes no effect while it runs"
	case TaskCompleted:
		return "task completed"
	case TaskFailed:
		return "task failed"
	case TaskCancelled:
		return "task cancelled"
	case TaskBlocked:
		return "task is blocked"
	case TaskReviewNeeded:
		return "task is waiting for review"
	default:
		return "task status " + string(s) + " is not unstarted"
	}
}

// applyQueuedStoryLLM is the story-level queued-mode path: the story record
// is updated (tasks with no task-level override of their own inherit it),
// and the per-task outcome is built against the story's tasks. See the
// package comment for the rule.
func (m *Manager) applyQueuedStoryLLM(prdID, storyID, backend, effort, model, actor string) (*PRD, *LLMOverrideResult, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, nil, fmt.Errorf("prd %q not found", prdID)
	}
	var story *Story
	for i := range prd.Story {
		if prd.Story[i].ID == storyID {
			story = &prd.Story[i]
			break
		}
	}
	if story == nil {
		return nil, nil, fmt.Errorf("story %q not found in prd %q", storyID, prdID)
	}
	// Refuse before any field changes: check the pair the story's tasks
	// will resolve to, using the story's new values over the record's.
	if err := checkResolvedLLMPair(prdID, &Task{}, &Story{Backend: backend, Model: model}, prd, m.AutomationForPRD(prd)); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	story.Backend = backend
	story.Effort = Effort(effort)
	story.Model = model
	story.UpdatedAt = now
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "set_story_llm", Actor: actor,
		Note: fmt.Sprintf("story=%s backend=%s effort=%s model=%s (queued override)", storyID, backend, effort, model),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, nil, err
	}

	res := LLMOverrideResult{}
	auto := m.AutomationForPRD(prd)
	for i := range story.Tasks {
		t := &story.Tasks[i]
		if !unstartedTask(t.Status) {
			res.LeftAlone = append(res.LeftAlone, LLMOverrideOutcome{TaskID: t.ID, Reason: taskLeftAloneReason(t.Status)})
			continue
		}
		out := LLMOverrideOutcome{TaskID: t.ID, Reason: "not started: the override takes effect when the task runs"}
		if t.Status == TaskWaitingCapacity && backend != "" {
			b, mod := resolveTaskBackendModel(t, story, prd, auto, m.cfg.ExecutionBackend)
			if m.requeueQueuedCapacity(prd, t, b, mod) {
				out.CapacityMoved = true
				out.Reason += "; its queued capacity request was retargeted to the new pool"
			} else {
				out.Reason += "; it holds no live capacity request right now, so nothing was retargeted"
			}
		}
		res.Changed = append(res.Changed, out)
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, &res, nil
}

// applyQueuedTaskLLM is the task-level queued-mode path: the override is
// written to the unstarted task, and if it was waiting for a capacity
// lease its queued request is retargeted to the new backend's pool.
func (m *Manager) applyQueuedTaskLLM(prdID, taskID, backend, effort, model, actor string) (*PRD, *LLMOverrideResult, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, nil, fmt.Errorf("prd %q not found", prdID)
	}
	task, story := findTaskInPRD(prd, taskID)
	if task == nil {
		return nil, nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	// Same started-task rule as the story-level path, and the documented
	// SetTaskLLM contract: queued mode never rewrites a task that is running
	// or already finished — it is returned in left_alone with the reason and
	// its record is left exactly as it was.
	if !unstartedTask(task.Status) {
		updated, _ := m.store.GetPRD(prdID)
		return updated, &LLMOverrideResult{
			LeftAlone: []LLMOverrideOutcome{{TaskID: taskID, Reason: taskLeftAloneReason(task.Status)}},
		}, nil
	}
	// Refuse before any field changes: check the pair the task will run
	// with, after the task's new values are applied over the story and the
	// PRD the executor falls back to.
	if err := checkResolvedLLMPair(prdID, &Task{Backend: backend, Model: model}, story, prd, m.AutomationForPRD(prd)); err != nil {
		return nil, nil, err
	}
	oldBackend, oldStatus := task.Backend, task.Status
	now := time.Now()
	task.Backend = backend
	task.Effort = Effort(effort)
	task.Model = model
	task.AdmittedBackend, task.AdmittedModel = "", ""
	task.AdmittedTaskBackend, task.AdmittedTaskModel = "", ""
	task.UpdatedAt = now
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{
		At: now, Kind: "set_task_llm", Actor: actor,
		Note: fmt.Sprintf("task=%s backend=%s effort=%s model=%s (queued override)", taskID, backend, effort, model),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, nil, err
	}

	out := LLMOverrideOutcome{TaskID: taskID, Reason: "not started: the override takes effect when the task runs"}
	// The move is not gated on the record's old backend differing: a task
	// whose stored backend already matches can still sit queued on another
	// pool (inherited from its story or PRD, or an earlier fall-through).
	// Retarget is a no-op when the request already carries the placement.
	_ = oldBackend
	if oldStatus == TaskWaitingCapacity && backend != "" {
		_, mod := resolveTaskBackendModel(task, story, prd, m.AutomationForPRD(prd), m.cfg.ExecutionBackend)
		if m.requeueQueuedCapacity(prd, task, backend, mod) {
			out.CapacityMoved = true
			out.Reason += "; its queued capacity request was retargeted to the new pool"
		} else {
			out.Reason += "; it holds no live capacity request right now, so nothing was retargeted"
		}
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, &LLMOverrideResult{Changed: []LLMOverrideOutcome{out}}, nil
}

// requeueQueuedCapacity cancels the task's queued capacity wait (if it has
// one) and immediately re-queues it against the new backend's pool,
// reusing the admission-time request shape (placementRefresh) so the
// grant, on a free pool, completes with no further polling. It returns
// false when there is no live request to move — a not-yet-queued task or
// one that already holds a lease — leaving that task's capacity state
// (or lease) exactly as it was.
func (m *Manager) requeueQueuedCapacity(prd *PRD, t *Task, backend, model string) bool {
	led := m.Capacity()
	if led == nil || !m.capacityOn() {
		return false
	}
	m.mu.Lock()
	keysFn, choicesFn := m.capacityKeys, m.capacityChoices
	m.mu.Unlock()
	var (
		pools   []string
		node    string
		choices []capacity.NodeChoice
	)
	if keysFn != nil {
		pools, node = keysFn(backend, model)
	}
	choices = nodeChoicesFor(choicesFn, backend, model)
	if len(pools) == 0 {
		return false
	}
	req := capacity.Request{
		Holder: t.ID, PRDID: prd.ID, Priority: prd.Priority,
		Pools: pools, Node: node, NodeChoices: choices,
		Model: model, Role: capacity.RoleWorker, Consumer: consumerSessionSpawn,
	}
	req.Refresh = m.placementRefresh(req, backend, model, nil)
	if led.Retarget(t.ID, req) {
		// Rewrite the wait-reason's current-pool record so the holder's next
		// wait reason names the NEW pool it moved to, not the one it entered
		// the queue with.
		m.setWaitReqPools(t.ID, req.Pools)
		// A fall-through recorded for the old request no longer describes
		// where the task waits.
		m.setFallthrough(t.ID, nil)
		return true
	}
	return false
}
