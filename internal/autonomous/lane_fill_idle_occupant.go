// Classifying whether a running Automaton truly occupies its lane.
//
// A lane is occupied only by an Automaton with a live worker, verifier or
// judge session, or by a task that is in the act of starting. The snapshot
// classifies every running PRD against the store and the capacity ledger:
//
//   - a task in a live-session state — its worker runs (in_progress), its
//     tests run (running_tests), or its verifier runs or is queued for a
//     slot (verifying) — occupies the lane, exactly as before this
//     classification existed;
//   - a lease the capacity ledger still holds for one of the tasks (the
//     worker, the verifier, or a judge) occupies the lane: the session it
//     names is live even when the task status has already moved on;
//   - a task in the act of starting occupies the lane: pending or admitted
//     and waiting for its capacity slot (waiting_capacity), or queued with
//     every dependency completed and nothing unmerged in front of it;
//   - verified work waiting on its merge into the PRD branch keeps the lane
//     while the stalled-merge window allows (the window runs from the time
//     the task records for its merge, else from when it was first seen
//     pending): stalled_merge_minutes = 0
//     switches the rule off (the lane is kept), otherwise every pending
//     merge past the threshold with no progress frees the lane and records
//     why (merge_stalled).
//
// When none of these holds — every task finished with nothing left to
// merge, or settled in a state nothing can start on its own — the Automaton
// is waiting: it does not enter the running list the planner reads, its
// lane is left idle for the next eligible work, and the status' Waiting list
// says why it waits. It also never enters the candidate list, so the planner
// cannot start a second run of it while it waits.
//
// An Automaton that is running but has not materialised any task yet is
// starting: it keeps its lane, and the planner never starts it a second
// time.
package autonomous

import (
	"fmt"
	"time"
)

// Waiting reason codes (LaneFillWaitingOcc.ReasonCode).
const (
	// LaneOccWaitingReviewMerge: every task is completed and there is
	// nothing left to merge; the Automaton waits for the completion review
	// and the closing of the PRD.
	LaneOccWaitingReviewMerge = "waiting_review_merge"
	// LaneOccMergeStalled: verified work has been pending its merge into
	// the PRD branch past stalled_merge_minutes with no progress.
	LaneOccMergeStalled = "merge_stalled"
	// LaneOccNoTaskStartable: no task can start now — none is live, none is
	// in the act of starting, none is ready — and no verified work is
	// pending a merge.
	LaneOccNoTaskStartable = "no_task_startable"
)

// laneOccLiveTask is the set of task states that hold a live worker, test or
// verifier session.
var laneOccLiveTask = map[TaskStatus]bool{
	TaskInProgress:   true,
	TaskRunningTests: true,
	TaskVerifying:    true,
}

// laneOccFacts is everything the classifier reads: the PRD snapshot, the
// capacity-ledger lease holders, the record of when each pending task-branch
// merge was first observed, the stalled-merge threshold in minutes (0 = the
// rule is off, the lane is kept), and the clock.
type laneOccFacts struct {
	prd                 *PRD
	held                map[string]bool
	pendingSince        map[string]time.Time
	stalledMergeMinutes int
	now                 time.Time
}

// laneOccDecision is the classifier's answer: the lane keeps the Automaton,
// or it is waiting and the entry that says why.
type laneOccDecision struct {
	Occupied bool
	Code     string
	Reason   string
}

func laneOccOccupied() laneOccDecision { return laneOccDecision{Occupied: true} }

func laneOccWaiting(code, reason string) laneOccDecision {
	return laneOccDecision{Occupied: false, Code: code, Reason: reason}
}

// classifyLaneOccupant applies the rules in the package comment, in order.
func classifyLaneOccupant(f laneOccFacts) laneOccDecision {
	if f.prd == nil {
		// No snapshot: the lane keeps the Automaton, as before this
		// classification existed.
		return laneOccOccupied()
	}
	tasks := laneOccTasks(f.prd)
	if len(tasks) == 0 {
		// Running but nothing materialised yet: the Automaton is starting,
		// so it keeps its lane and the planner never starts it again.
		return laneOccOccupied()
	}

	// (a) A live-session state, or a lease the ledger still holds for one of
	// the tasks (the worker, the verifier, or a judge).
	for _, t := range tasks {
		if laneOccLiveTask[t.Status] {
			return laneOccOccupied()
		}
	}
	if f.held != nil {
		for _, t := range tasks {
			if f.held[t.ID] ||
				f.held[VerifyHolder(t.ID)] ||
				f.held[JudgeHolder(t.ID)] ||
				(t.StoryID != "" && f.held[JudgeHolder(t.StoryID)]) ||
				f.held[JudgeHolder(f.prd.ID)] {
				return laneOccOccupied()
			}
		}
	}

	// (b) A task in the act of starting: pending, admitted and waiting for
	// its capacity slot, or queued with every dependency completed and
	// nothing unmerged in front of it.
	for _, t := range tasks {
		if t.Status == TaskPending || t.Status == TaskWaitingCapacity {
			return laneOccOccupied()
		}
	}
	for _, t := range tasks {
		if t.Status == TaskQueued &&
			laneOccDepsSatisfied(f.prd, t) && mergeBlocker(f.prd, t) == "" {
			return laneOccOccupied()
		}
	}

	// (c) Verified work pending its merge into the PRD branch. 0 minutes
	// switches the stalled rule off: the lane is kept. Otherwise a fresh
	// pending merge keeps it; every one past the threshold frees it.
	hasMerge, hasFresh := false, false
	threshold := time.Duration(f.stalledMergeMinutes) * time.Minute
	var oldest *Task
	var oldestAge time.Duration
	for _, t := range tasks {
		if !laneOccMergePending(t) {
			continue
		}
		hasMerge = true
		if f.stalledMergeMinutes <= 0 {
			continue
		}
		// The age is taskMergePendingAge's, the same source the
		// stalled-merge pass reads (task_merge_stall.go).
		age := taskMergePendingAge(t, f.pendingSince, f.now)
		if age <= threshold {
			hasFresh = true
		}
		if oldest == nil || age > oldestAge {
			oldest, oldestAge = t, age
		}
	}
	if hasMerge {
		if f.stalledMergeMinutes <= 0 || hasFresh {
			return laneOccOccupied()
		}
		mins := 0
		if oldest != nil {
			mins = int(oldestAge.Minutes())
		}
		name := "a task branch"
		if oldest != nil {
			name = oldest.ID
		}
		return laneOccWaiting(LaneOccMergeStalled,
			fmt.Sprintf("task-branch merge of %s has been pending %d min with no progress (stalled-merge threshold %d min)",
				name, mins, f.stalledMergeMinutes))
	}

	// (d) Settled work only: everything completed and merged waits on the
	// completion review; anything else settled is a task nothing can start
	// on its own.
	allTerminal := true
	for _, t := range tasks {
		if !isTaskTerminal(t.Status) {
			allTerminal = false
			break
		}
	}
	if allTerminal {
		return laneOccWaiting(LaneOccWaitingReviewMerge,
			"all tasks are completed and merged; the Automaton waits on the completion review to close it")
	}
	return laneOccWaiting(LaneOccNoTaskStartable,
		"no task can start now: none is live, none is in the act of starting, none is ready, and no verified work is pending a merge")
}

// laneOccTasks walks the PRD's stories and returns its tasks in story and
// task order.
func laneOccTasks(p *PRD) []*Task {
	var out []*Task
	for si := range p.Story {
		for ti := range p.Story[si].Tasks {
			out = append(out, &p.Story[si].Tasks[ti])
		}
	}
	return out
}

// laneOccDepsSatisfied says every task t depends on is completed. A
// dependency the PRD does not name is satisfied: there is nothing to wait
// for.
func laneOccDepsSatisfied(p *PRD, t *Task) bool {
	byID := map[string]*Task{}
	for si := range p.Story {
		for ti := range p.Story[si].Tasks {
			x := &p.Story[si].Tasks[ti]
			byID[x.ID] = x
		}
	}
	for _, id := range t.DependsOn {
		d := byID[id]
		if d == nil {
			continue
		}
		if d.Status != TaskCompleted {
			return false
		}
	}
	return true
}

// laneOccMergeSince is when the task's pending merge started waiting: the
// time the task records for its merge block when it has one, otherwise the
// time the poll first saw it pending, otherwise now (a merge seen for the
// first time is as young as it can be).
func laneOccMergeSince(t *Task, seen map[string]time.Time, now time.Time) time.Time {
	if t.MergeBlock != nil && !t.MergeBlock.Since.IsZero() {
		return t.MergeBlock.Since
	}
	if at, ok := seen[t.ID]; ok {
		return at
	}
	return now
}

// laneOccMergePending says the task's verified work still waits on its merge
// into the PRD branch: it has a branch of its own, it is completed, and its
// merge state is unset or pending (the states mergeBlocker and
// MergeCompletedTasks treat as "waiting to merge").
func laneOccMergePending(t *Task) bool {
	return t.WorktreeBranch != "" && t.Status == TaskCompleted &&
		(t.MergeState == "" || t.MergeState == MergeStatePending)
}
