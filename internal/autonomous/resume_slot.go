package autonomous

// The slot of a worker session that is resumed instead of spawned: after a
// worker question or an outside wait, for a retry in the same session, and for
// a parked worker found again after a restart. One rule covers them all: the
// resume waits for a slot like any new work, the slot it gets names the
// session, and a session that ends takes no slot and leaves the queue.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/dmz006/datawatch/internal/capacity"
)

// errResumeSessionGone reports that the session a resume was for ended, or its
// task moved to another session, before the resume had its slot. The resume
// holds nothing.
var errResumeSessionGone = errors.New("the session ended before its resume had a capacity slot")

// resumeTarget is the state of the session a resume is waiting for.
type resumeTarget int

const (
	resumeTargetLive resumeTarget = iota
	// resumeTargetEnded: the session is not running, or the task has finished.
	resumeTargetEnded
	// resumeTargetReplaced: the task now names another session. Whatever the
	// task's holder owns belongs to the newer attempt.
	resumeTargetReplaced
)

// resumeTargetState judges the session a resume of taskID was started for. An
// empty sessionID cannot be judged and counts as live, as does any session
// when no liveness hook is wired.
func (m *Manager) resumeTargetState(taskID, sessionID string) resumeTarget {
	if sessionID == "" {
		return resumeTargetLive
	}
	if cur, ok := m.store.TaskSnapshot(taskID); ok {
		if !sameSessionID(cur.SessionID, sessionID) && cur.SessionID != sessionID {
			return resumeTargetReplaced
		}
		if cur.Status != TaskCancelled && isTerminalTaskStatus(cur.Status) {
			return resumeTargetEnded
		}
	}
	m.mu.Lock()
	aliveFn := m.sessionAliveFn
	m.mu.Unlock()
	if aliveFn != nil && !aliveFn(sessionID) {
		return resumeTargetEnded
	}
	return resumeTargetLive
}

// resumeSessionID is the session the task names now.
func (m *Manager) resumeSessionID(t *Task) string {
	if cur, ok := m.store.TaskSnapshot(t.ID); ok {
		return cur.SessionID
	}
	return t.SessionID
}

// releaseResumeGrant frees the slot held under holder when it is the resume's
// own: a lease that names no session yet, or names sessionID.
func releaseResumeGrant(led *capacity.Ledger, holder, sessionID string) {
	if ls := led.LeaseOf(holder); ls != nil && (ls.SessionID == "" || ls.SessionID == sessionID) {
		led.ReleaseIf(holder, ls.Acquired)
	}
}

// resumeRequest builds the capacity request of a resumed worker, and reports
// false when its backend takes no pool. The backend and model are the ones the
// session really runs when that is known, else the task's admitted pair.
// prefer is the node the task held before: it wins a tie between equally
// loaded nodes, and with stay set it is the only node taken while the entry
// still lists it.
func (m *Manager) resumeRequest(prd *PRD, t *Task, sessionID, prefer string, stay bool) (capacity.Request, bool, error) {
	m.mu.Lock()
	keysFn, choicesFn, sessionBackend := m.capacityKeys, m.capacityChoices, m.sessionBackend
	fallback := executionFallbackBackend(m.cfg, prd)
	m.mu.Unlock()
	story := findStory(prd, t.StoryID)
	backend, model := admittedWorker(t, story, prd, m.AutomationForPRD(prd), fallback)
	if sessionBackend != nil && sessionID != "" {
		if sb, sm := sessionBackend(sessionID); sb != "" {
			backend = sb
			if sm != "" {
				model = sm
			}
		}
	}
	pools, node := keysFn(backend, model)
	if len(pools) == 0 {
		return capacity.Request{}, false, nil
	}
	choices := nodeChoicesFor(choicesFn, backend, model)
	var exclude []string
	if stay && prefer != "" {
		onList := false
		for _, c := range choices {
			onList = onList || c.Name == prefer
		}
		if onList {
			for _, c := range choices {
				if c.Name != prefer {
					exclude = append(exclude, c.Name)
				}
			}
		}
	}
	req := capacity.Request{
		Holder: t.ID, PRDID: prd.ID, Priority: m.capacityPriorityOf(prd), Pools: pools, Node: node, NodeChoices: choices, ExcludeNodes: exclude, PreferNode: prefer, Model: model, Role: capacity.RoleWorker, Consumer: consumerSessionSpawn,
	}
	// A pinned worker resumes on its pinned node only.
	pin := m.workerNodePin(prd, t, capacity.RoleWorker)
	if pin != "" {
		pinned, perr := applyNodePin(req, pin, exclude)
		if perr != nil {
			return capacity.Request{}, false, perr
		}
		req = pinned
	}
	req.Refresh = m.placementRefreshPinned(req, backend, model, nil, pin, exclude)
	return req, true, nil
}

// resumeRules are the two ways resumes differ.
type resumeRules struct {
	// stay takes only the previous node while the entry still lists it.
	stay bool
	// dropEnded drops the resume of a session that is already not running when
	// the wait begins. An outside-wait cycle leaves it false: the worker's wait
	// loop starts it for a session it has just seen running, and ends the
	// cycle's own wait only when that session stops during it. A slot taken by
	// a session that had already stopped still names it, so the reaper frees it
	// on its next pass.
	dropEnded bool
}

// readmitSession waits for the slot of a resumed worker session and binds the
// slot to that session, so it is held exactly as long as the session runs. The
// wait ends as soon as the session ends (errResumeSessionGone), and a slot
// granted to a session that has ended is given back at once. prefix starts the
// task's wait reason; empty shows the pool reason alone.
func (m *Manager) readmitSession(ctx context.Context, prd *PRD, t *Task, prefix, prefer string, rules resumeRules) error {
	if !m.capacityOn() {
		return nil
	}
	led := m.Capacity()
	sessionID := m.resumeSessionID(t)
	first := m.resumeTargetState(t.ID, sessionID)
	if first == resumeTargetReplaced || (first == resumeTargetEnded && rules.dropEnded) {
		return errResumeSessionGone
	}
	unwatched := first == resumeTargetEnded
	state := func() resumeTarget {
		s := m.resumeTargetState(t.ID, sessionID)
		if s == resumeTargetEnded && unwatched {
			return resumeTargetLive
		}
		return s
	}
	req, ok, err := m.resumeRequest(prd, t, sessionID, prefer, rules.stay)
	if err != nil || !ok {
		return err
	}
	cancelled := func() bool {
		return m.taskCancelled(t.ID) || state() != resumeTargetLive
	}
	err = led.Acquire(ctx, req, m.capacityWait(), cancelled, func(reason string) {
		if prefix != "" {
			reason = prefix + " — " + reason
		}
		m.setWaiting(t, reason)
	})
	switch {
	case err == nil:
	case errors.Is(err, context.Canceled) && m.taskCancelled(t.ID):
		return errCancelledWhileWaiting
	case errors.Is(err, context.Canceled) && ctx.Err() == nil:
		return errResumeSessionGone
	case errors.Is(err, capacity.ErrWaitTimeout):
		return fmt.Errorf("capacity: %w (re-admission of a resumed session)", err)
	default:
		return err
	}
	if m.taskCancelled(t.ID) {
		releaseResumeGrant(led, t.ID, sessionID)
		return errCancelledWhileWaiting
	}
	// Bind before the last look, so a session that ends from here on frees the
	// slot through the session-end release.
	got := state()
	if got == resumeTargetLive {
		if sessionID != "" {
			led.Bind(t.ID, sessionID)
		}
		got = state()
	}
	switch got {
	case resumeTargetEnded:
		releaseResumeGrant(led, t.ID, sessionID)
		return errResumeSessionGone
	case resumeTargetReplaced:
		return errResumeSessionGone
	}
	m.clearWaiting(t)
	return nil
}

// endDroppedResume ends the visible wait of a resume that was dropped. A wait
// the task has since taken for something else is left alone.
func (m *Manager) endDroppedResume(t *Task, prefix string) {
	cur, ok := m.store.TaskSnapshot(t.ID)
	if ok && cur.Status == TaskWaitingCapacity && strings.HasPrefix(cur.WaitReason, prefix) {
		m.clearWaiting(t)
	}
}

// resumeSlotAndTree re-admits a parked task and, when its park released the
// project-tree lock, takes the lock again. It never waits for the tree while
// holding a slot: the writer that has the tree may need that slot to finish.
// When the tree is taken, the slot is given back, the tree is waited for, and
// the task is admitted again.
//
// errResumeSessionGone means the session ended: the task holds no slot, and
// the tree lock is still re-claimed so what follows runs under it. dropEnded
// is resumeRules.dropEnded.
func (m *Manager) resumeSlotAndTree(ctx context.Context, prd *PRD, t *Task, prefix, prefer string, dropEnded, releasedTree bool, box *func()) error {
	rules := resumeRules{dropEnded: dropEnded}
	// A session running now and gone after the tree wait is an end seen here.
	runningAtStart := m.resumeTargetState(t.ID, m.resumeSessionID(t)) == resumeTargetLive
	err := m.readmitSession(ctx, prd, t, prefix, prefer, rules)
	gone := errors.Is(err, errResumeSessionGone)
	if err != nil && !gone {
		return err
	}
	if gone {
		m.endDroppedResume(t, prefix)
	}
	if !releasedTree || m.taskCancelled(t.ID) {
		return err
	}
	if !m.tryReclaimTreeLock(prd, t, box) {
		return err
	}
	// Another writer has the tree.
	if !gone && m.capacityOn() {
		releaseResumeGrant(m.Capacity(), t.ID, m.resumeSessionID(t))
		log.Printf("[autonomous] %s: resume: the project tree is held by another writer; the capacity slot is given back while waiting for it", t.ID)
	}
	m.reclaimTreeLock(prd, t, releasedTree, box)
	if gone {
		return err
	}
	rules.dropEnded = rules.dropEnded || runningAtStart
	err = m.readmitSession(ctx, prd, t, prefix, prefer, rules)
	if errors.Is(err, errResumeSessionGone) {
		m.endDroppedResume(t, prefix)
	}
	return err
}

// resumeFitsNow reports whether the slot of a parked worker would be granted
// this instant. It is false while the ledger still holds new work back for
// adoption after a restart, because what the pools hold is not yet known.
func (m *Manager) resumeFitsNow(prd *PRD, t *Task) bool {
	if !m.capacityOn() {
		return true
	}
	req, ok, err := m.resumeRequest(prd, t, t.SessionID, t.ComputeNode, true)
	if err != nil {
		return false
	}
	return !ok || m.Capacity().WouldGrant(req)
}

// parkedAtRestart reports whether the task is a parked worker the restart
// found alive but had no slot to resume yet.
func (m *Manager) parkedAtRestart(t *Task) bool {
	cur, ok := m.store.TaskSnapshot(t.ID)
	return ok && cur.AdoptedSession && cur.SessionID != "" &&
		cur.Status == TaskWaitingCapacity && IsAwaitExternalWaitReason(cur.WaitReason)
}

// resumeParkedAtRestart waits for the slot of a worker that was parked on an
// outside wait across a restart, then prompts it. A session that ends while
// waiting is left for the ordinary adopted-session path to find.
func (m *Manager) resumeParkedAtRestart(ctx context.Context, prd *PRD, t *Task) error {
	cur, _ := m.store.TaskSnapshot(t.ID)
	prefix := cur.WaitReason
	if i := strings.Index(prefix, " — "); i >= 0 {
		prefix = prefix[:i]
	}
	err := m.readmitSession(ctx, prd, t, prefix, cur.ComputeNode, resumeRules{stay: true, dropEnded: true})
	if errors.Is(err, errResumeSessionGone) {
		m.endDroppedResume(t, prefix)
		return nil
	}
	if err != nil {
		return err
	}
	m.clearWaiting(t)
	if contFn := m.sessionContinueFnLocked(); contFn != nil {
		if cerr := contFn(cur.SessionID); cerr != nil {
			log.Printf("[autonomous] %s: await-external continue prompt to session %s: %v", t.ID, cur.SessionID, cerr)
		}
	}
	return nil
}

// admitRetryResume takes the slot a retry in the same session needs, before
// anything is sent into the session. The slot the finished attempt held went
// to its verifier or back to the pool, so the retry is new work.
func (m *Manager) admitRetryResume(ctx context.Context, prd *PRD, t *Task) error {
	if !m.capacityOn() {
		return nil
	}
	// A slot still held under the verifier's name is the worker's again.
	m.Capacity().Handoff(VerifyHolder(t.ID), t.ID)
	requested := m.attemptNow()
	if err := m.readmitSession(ctx, prd, t, "", t.ComputeNode, resumeRules{stay: true, dropEnded: true}); err != nil {
		if errors.Is(err, errResumeSessionGone) {
			m.endDroppedResume(t, "")
		}
		return err
	}
	m.addQueueWait(t.ID, m.attemptNow().Sub(requested))
	return nil
}
