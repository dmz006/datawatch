// Stalled task-branch merge pass. A verified task that works on its own
// branch waits to be merged into the Automaton branch (task_merge.go). When
// that merge stays pending longer than autonomous.lane_fill.stalled_merge_minutes
// this pass raises one alert for the episode and retries the merge.
//
// A stalled merge is a task with a WorktreeBranch, status completed and a
// MergeState that is empty or pending. How long it has been pending comes from
// taskMergePendingAge, the one source of time the occupancy classifier
// (lane_fill_idle_occupant.go) reads too: the time the task records for its
// merge block when it has one, else the time the poll first saw it pending
// (laneFillState.mergePendingSince).
//
// The pass runs from laneFillPoll and follows the lane fill mode:
//
//   - off, or stalled_merge_minutes = 0: nothing happens;
//   - shadow: the stalled merge is recorded as a lane fill decision (which
//     the existing lane_fill audit entry reports) and nothing else - no
//     alert, no retry;
//   - on: one alert per episode, through the same path the idle lane alert
//     uses, and a retry of the Automaton's merges. A merge held for the
//     operator (it carries a merge block, is held or needs review, or its
//     Automaton is blocked) is alerted about but never retried.
//
// An episode ends when the merge succeeds or the Automaton stops running; the
// next stall of the same task alerts again. Nothing here is persisted.
package autonomous

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// taskMergeStallRetryEvery is the least time between two retries of one
// Automaton's merges while it stays stalled, so a poll every minute does not
// run git every minute.
const taskMergeStallRetryEvery = 5 * time.Minute

// taskMergeStallRetryTimeout bounds one retry of an Automaton's merges.
const taskMergeStallRetryTimeout = 2 * time.Minute

// taskMergeStallBlocker is the Blocker text of a shadow decision made by the
// stalled-merge pass.
const taskMergeStallBlocker = "stalled task-branch merge"

// taskMerger retries the merges of one Automaton. *Manager is the production
// implementation; tests supply a fake so no git runs.
type taskMerger interface {
	MergeCompletedTasks(ctx context.Context, prd *PRD) (int, error)
}

// taskMergeStallState is the pass's in-memory bookkeeping. The zero value is
// ready to use.
type taskMergeStallState struct {
	mu sync.Mutex
	// merger overrides the Manager as the thing that retries (tests).
	merger taskMerger
	// alerted holds the task ids whose current episode already alerted.
	alerted map[string]bool
	// retryAt is when each Automaton's merges were last retried.
	retryAt map[string]time.Time
}

// taskMergePendingAge is how long the task's merge has been pending: the one
// source of time the occupancy classifier and the stalled-merge pass share.
// seen is the record of when each pending merge was first observed
// (laneFillState.mergePendingSince); a merge seen for the first time is as
// young as it can be, so its age is 0.
func taskMergePendingAge(t *Task, seen map[string]time.Time, now time.Time) time.Duration {
	if age := now.Sub(laneOccMergeSince(t, seen, now)); age > 0 {
		return age
	}
	return 0
}

// taskMergeOperatorHeld says why the task's merge is held for the operator,
// or "" when it is not: a merge block records an action only the operator can
// take (merge_block.go), a held or review-needed task was put there by
// holdMergeTask, and a blocked Automaton waits on the operator as a whole.
func taskMergeOperatorHeld(prd *PRD, t *Task) string {
	switch {
	case t.MergeBlock != nil:
		reason := strings.TrimSpace(t.MergeBlock.Reason)
		if reason == "" {
			reason = "a merge block is recorded on the task"
		}
		return reason
	case t.MergeState == MergeStateHeld || t.Status == TaskReviewNeeded:
		return "the task is held for the operator"
	case NormalizePRDStatus(prd.Status) == PRDBlocked:
		return "the Automaton is blocked for the operator"
	}
	return ""
}

// taskMergeStallMode is the mode the pass runs in: on when either lane kind
// is on, shadow when either is shadow, else off.
func taskMergeStallMode(kinds LaneFillKindModes) string {
	for _, want := range []string{string(LaneFillModeOn), string(LaneFillModeShadow)} {
		if kinds.GPU == want || kinds.Claude == want {
			return want
		}
	}
	return string(LaneFillModeOff)
}

// taskMergeStallReset forgets every episode, so a merge that was pending while
// the pass was off is not alerted about as if it had stalled all along.
func (c *Coordinator) taskMergeStallReset() {
	s := &c.lf.stall
	s.mu.Lock()
	s.alerted = nil
	s.retryAt = nil
	s.mu.Unlock()
}

// taskMergeStallPass finds the stalled task-branch merges of running
// Automata and, by mode, records, alerts and retries them. It is called by
// laneFillPoll after the snapshot, with the thresholds already read.
func (c *Coordinator) taskMergeStallPass(ctx context.Context, kinds LaneFillKindModes, thresholdMinutes int, now time.Time) {
	if c == nil || c.m == nil || c.m.store == nil {
		return
	}
	mode := taskMergeStallMode(kinds)
	if mode == string(LaneFillModeOff) || thresholdMinutes <= 0 {
		c.taskMergeStallReset()
		return
	}
	threshold := time.Duration(thresholdMinutes) * time.Minute
	s := &c.lf.stall

	type stalled struct {
		prd  *PRD
		task *Task
		held string
		age  time.Duration
	}
	pendingNow := map[string]bool{}
	byPRD := map[string][]stalled{}
	var prdOrder []string
	for _, listed := range c.m.store.ListPRDs() {
		snap, ok := c.m.store.SnapshotPRD(listed.ID)
		if !ok {
			continue
		}
		if !laneFillRunning(snap.Status) {
			// The Automaton is not running (finished, blocked, cancelled): its
			// merge episodes end here.
			continue
		}
		seen := c.laneFillNotePendingMerges(snap, now)
		for _, t := range laneOccTasks(snap) {
			if !laneOccMergePending(t) {
				continue
			}
			pendingNow[t.ID] = true
			age := taskMergePendingAge(t, seen, now)
			if age <= threshold {
				continue
			}
			if _, ok := byPRD[snap.ID]; !ok {
				prdOrder = append(prdOrder, snap.ID)
			}
			byPRD[snap.ID] = append(byPRD[snap.ID], stalled{prd: snap, task: t, held: taskMergeOperatorHeld(snap, t), age: age})
		}
	}

	// Episodes of merges that are no longer pending in a running Automaton
	// have ended; so has their first-seen time.
	c.lf.mu.Lock()
	for id := range c.lf.mergePendingSince {
		if !pendingNow[id] {
			delete(c.lf.mergePendingSince, id)
		}
	}
	c.lf.mu.Unlock()
	s.mu.Lock()
	for id := range s.alerted {
		if !pendingNow[id] {
			delete(s.alerted, id)
		}
	}
	for id := range s.retryAt {
		if _, ok := byPRD[id]; !ok {
			delete(s.retryAt, id)
		}
	}
	s.mu.Unlock()

	sort.Strings(prdOrder)
	for _, prdID := range prdOrder {
		group := byPRD[prdID]
		if mode == string(LaneFillModeShadow) {
			c.taskMergeStallRecord(group[0].prd, group[0].task, group[0].held, thresholdMinutes, now)
			continue
		}

		// on: retry the Automaton's merges unless every stalled one is held
		// for the operator, then alert once per task episode.
		retryable := false
		for _, g := range group {
			if g.held == "" {
				retryable = true
			}
		}
		result := "retry not attempted: the merge is held for the operator"
		if retryable {
			s.mu.Lock()
			last, retried := s.retryAt[prdID]
			s.mu.Unlock()
			if retried && now.Sub(last) < taskMergeStallRetryEvery {
				result = "retry not attempted yet: the last retry was less than " + taskMergeStallRetryEvery.String() + " ago"
			} else {
				result = c.taskMergeStallRetry(ctx, prdID)
				s.mu.Lock()
				if s.retryAt == nil {
					s.retryAt = map[string]time.Time{}
				}
				s.retryAt[prdID] = now
				s.mu.Unlock()
			}
		}

		for _, g := range group {
			s.mu.Lock()
			done := s.alerted[g.task.ID]
			s.mu.Unlock()
			if done {
				continue
			}
			taskResult := result
			if g.held != "" {
				taskResult = "retry not attempted: the merge is held for the operator (" + g.held + ")"
			}
			if cur, ok := c.m.store.SnapshotPRD(prdID); ok && taskMergeSettled(cur, g.task.ID) {
				taskResult += "; the task is merged now"
				c.taskMergeStallEnd(g.task.ID, prdID)
			} else {
				s.mu.Lock()
				if s.alerted == nil {
					s.alerted = map[string]bool{}
				}
				s.alerted[g.task.ID] = true
				s.mu.Unlock()
			}
			c.taskMergeStallAlert(g.prd, g.task, g.age, thresholdMinutes, taskResult, now)
		}
	}
}

// taskMergeSettled says the task of the stored Automaton is no longer waiting
// to merge: its merge succeeded.
func taskMergeSettled(prd *PRD, taskID string) bool {
	for _, t := range laneOccTasks(prd) {
		if t.ID == taskID {
			return t.MergeState == MergeStateMerged
		}
	}
	return false
}

// taskMergeStallEnd drops everything the pass and the classifier remember
// about one task's episode.
func (c *Coordinator) taskMergeStallEnd(taskID, prdID string) {
	c.lf.mu.Lock()
	delete(c.lf.mergePendingSince, taskID)
	c.lf.mu.Unlock()
	s := &c.lf.stall
	s.mu.Lock()
	delete(s.alerted, taskID)
	delete(s.retryAt, prdID)
	s.mu.Unlock()
}

// taskMergeStallRetry retries the Automaton's merges under a timeout and
// reports the result in words.
func (c *Coordinator) taskMergeStallRetry(ctx context.Context, prdID string) string {
	prd, ok := c.m.store.GetPRD(prdID)
	if !ok {
		return "retry failed: the Automaton is gone"
	}
	s := &c.lf.stall
	s.mu.Lock()
	merger := s.merger
	s.mu.Unlock()
	if merger == nil {
		merger = c.m
	}
	rctx, cancel := context.WithTimeout(ctx, taskMergeStallRetryTimeout)
	defer cancel()
	n, err := merger.MergeCompletedTasks(rctx, prd)
	switch {
	case err != nil:
		log.Printf("[autonomous] %s stalled merge retry: %v", prdID, err)
		return fmt.Sprintf("retry failed: %v (merged %d task(s) first)", err, n)
	case n > 0:
		return fmt.Sprintf("retry merged %d task(s)", n)
	}
	return "retry ran but merged nothing; the merge is still pending"
}

// taskMergeStallAlert raises the one alert for a stalled merge through the
// path the idle lane alert uses: it is kept in the lane fill status' recent
// alerts and sent to the alert sink.
func (c *Coordinator) taskMergeStallAlert(prd *PRD, t *Task, age time.Duration, thresholdMinutes int, result string, now time.Time) {
	prdName := prd.Title
	if prdName == "" {
		prdName = prd.ID
	}
	taskName := t.Title
	if taskName == "" {
		taskName = t.ID
	}
	desc := fmt.Sprintf("%s (%s)", taskName, t.ID)
	msg := fmt.Sprintf("Automaton %q: the merge of task %q into its Automaton branch has been pending %s, past the %d-minute stalled-merge threshold. %s.",
		prdName, taskName, age.Round(time.Minute), thresholdMinutes, capitalizeFirst(result))
	c.lf.mu.Lock()
	c.lf.alerts = append([]LaneFillAlert{{At: now, Work: desc, Message: msg}}, c.lf.alerts...)
	if len(c.lf.alerts) > laneFillAlertCap {
		c.lf.alerts = c.lf.alerts[:laneFillAlertCap]
	}
	c.lf.mu.Unlock()
	c.alert("warn", fmt.Sprintf("Task merge stalled: %s", taskName), msg)
}

// taskMergeStallRecord is shadow mode: the stalled merge is recorded as a
// lane fill decision, the existing lane_fill audit entry reports a new one,
// and nothing is alerted or retried. Repeats across polls fold into one
// record because the reason carries no age.
func (c *Coordinator) taskMergeStallRecord(prd *PRD, t *Task, held string, thresholdMinutes int, now time.Time) {
	c.lf.mu.Lock()
	rec := c.lf.records
	c.lf.mu.Unlock()
	if rec == nil {
		return
	}
	reason := fmt.Sprintf("task %s's merge into the Automaton branch is pending past %d minutes; on mode would alert and retry it", t.ID, thresholdMinutes)
	if held != "" {
		reason = fmt.Sprintf("task %s's merge into the Automaton branch is pending past %d minutes and is held for the operator; on mode would alert and not retry it", t.ID, thresholdMinutes)
	}
	e, appended := rec.observe(LaneFillDecision{
		At: now, Mode: LaneFillModeShadow, Action: LaneFillActionReportMerge,
		PRDID: prd.ID, Title: prd.Title, Reason: reason, Blocker: taskMergeStallBlocker,
	}, now)
	if appended {
		c.laneFillAudit(rec, e)
	}
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
