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
	"sync"
	"sync/atomic"

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

// resumeBook tracks the resumes under way, so the wait loops can tell a worker
// is still parked and a new attempt can remove an old resume from the queue
// first. It has its own lock.
type resumeBook struct {
	mu sync.Mutex
	// pending counts the resumes of a parked task that have not finished.
	pending map[string]int
	// queued is the task's resume that is asking for a slot.
	queued map[string]*queuedResume
}

type queuedResume struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (b *resumeBook) begin(taskID string) (end func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending == nil {
		b.pending = map[string]int{}
	}
	b.pending[taskID]++
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.pending[taskID]--; b.pending[taskID] <= 0 {
			delete(b.pending, taskID)
		}
	}
}

func (b *resumeBook) isPending(taskID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending[taskID] > 0
}

// enter registers a resume that is about to ask for a slot. leave must be
// called once it holds the slot or has given up.
func (b *resumeBook) enter(ctx context.Context, taskID string) (rctx context.Context, leave func()) {
	rctx, cancel := context.WithCancel(ctx)
	q := &queuedResume{cancel: cancel, done: make(chan struct{})}
	b.mu.Lock()
	if b.queued == nil {
		b.queued = map[string]*queuedResume{}
	}
	b.queued[taskID] = q
	b.mu.Unlock()
	return rctx, func() {
		b.mu.Lock()
		if b.queued[taskID] == q {
			delete(b.queued, taskID)
		}
		b.mu.Unlock()
		cancel()
		close(q.done)
	}
}

// supersede ends the task's queued resume and returns once it has left the
// capacity queue. The ledger knows a request by its holder, so a resume left
// queued beside a new attempt could take, or give back, that attempt's slot.
func (b *resumeBook) supersede(taskID string) {
	b.mu.Lock()
	q := b.queued[taskID]
	b.mu.Unlock()
	if q != nil {
		q.cancel()
		<-q.done
	}
}

// DecisionResumeWaiting reports that the task's worker is still parked although
// nothing may be open: its decision is made but the line is not typed yet, or a
// resume is waiting for its slot or the project tree. A wait loop must not
// nudge, stall or type into such a worker.
func (m *Manager) DecisionResumeWaiting(taskID string) bool {
	if m.resumes.isPending(taskID) {
		return true
	}
	snap, ok := m.store.TaskSnapshot(taskID)
	return ok && m.decisionParkPending(snap)
}

// verificationTookOver reports that the turn of a parked task's worker is over:
// the task is being verified, or its verifier or judge asks for or holds a
// slot. A worker resume must give way, or the task would take a second slot and
// could leave its own verifier waiting behind it.
func (m *Manager) verificationTookOver(led *capacity.Ledger, taskID string) bool {
	if cur, ok := m.store.TaskSnapshot(taskID); ok && (cur.Status == TaskVerifying || cur.Status == TaskRunningTests) {
		return true
	}
	verify, judge := VerifyHolder(taskID), JudgeHolder(taskID)
	if led.Held(verify) || led.Held(judge) {
		return true
	}
	for _, w := range led.Snapshot().Waiting {
		if w.Holder == verify || w.Holder == judge {
			return true
		}
	}
	return false
}

// readmitSession waits for the slot of a resumed worker session and binds the
// slot to that session, so it is held exactly as long as the session runs. The
// wait ends as soon as the session ends or, for a parked task, its
// verification takes over (errResumeSessionGone), and a slot granted too late
// is given back at once. prefix starts the task's wait reason; empty shows the
// pool reason alone.
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
	// A retry in the same session starts from a finished verification, so only
	// a task that is parked now can be overtaken by one.
	snap, found := m.store.TaskSnapshot(t.ID)
	parked := found && snap.Status == TaskWaitingCapacity
	var overtaken atomic.Bool
	state := func() resumeTarget {
		if overtaken.Load() {
			return resumeTargetEnded
		}
		s := m.resumeTargetState(t.ID, sessionID)
		if s == resumeTargetEnded && unwatched {
			s = resumeTargetLive
		}
		if s == resumeTargetLive && parked && m.verificationTookOver(led, t.ID) {
			overtaken.Store(true)
			return resumeTargetEnded
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
	rctx, leave := m.resumes.enter(ctx, t.ID)
	defer leave()
	err = led.Acquire(rctx, req, m.capacityWait(), cancelled, func(reason string) {
		if state() != resumeTargetLive {
			return // the task has moved on; leave its status alone
		}
		if prefix != "" {
			reason = prefix + " — " + reason
		}
		m.setWaiting(t, reason)
	})
	superseded := rctx.Err() != nil && ctx.Err() == nil
	switch {
	case superseded:
		if err == nil {
			releaseResumeGrant(led, t.ID, sessionID)
		}
		return errResumeSessionGone
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
	defer m.resumes.begin(t.ID)()
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

// parkedLast splits the Automaton's tasks for the restart pass: every other
// task, then the workers parked on an outside wait. A parked worker is prompted
// only when its slot fits, which is known only once the running workers, that
// already use theirs, have been counted.
func parkedLast(prd *PRD) [2][]*Task {
	var out [2][]*Task
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			t := &prd.Story[si].Tasks[ti]
			g := 0
			if t.Status == TaskWaitingCapacity && IsAwaitExternalWaitReason(t.WaitReason) {
				g = 1
			}
			out[g] = append(out[g], t)
		}
	}
	return out
}

// restartOrder gives lanes back to the work that held them before a restart or
// a resume: first the tasks whose worker or verification is under way, then the
// parked workers, then everything else, each group in its old order. A running
// task may already use a slot, so it needs a lane before a task that still has
// to wait for one: otherwise the waiting tasks can fill every lane and wait for
// a slot that only the task with no lane would free. A parked task may have
// kept the project tree, which a task that never started would wait for. A task
// whose dependencies are not all completed keeps its place among the rest.
func (m *Manager) restartOrder(prd *PRD, order []string) []string {
	var running, parked, rest []string
	m.store.WithLock(func() {
		byID := map[string]*Task{}
		for _, t := range flattenTasks(prd) {
			byID[t.ID] = t
		}
		depsDone := func(t *Task) bool {
			for _, dep := range t.DependsOn {
				if d, ok := byID[dep]; ok && d.Status != TaskCompleted {
					return false
				}
			}
			return true
		}
		for _, id := range order {
			t := byID[id]
			switch {
			case t == nil || t.SessionID == "" || !depsDone(t):
				rest = append(rest, id)
			case t.Status == TaskInProgress || t.Status == TaskVerifying || t.Status == TaskRunningTests:
				running = append(running, id)
			case t.Status == TaskWaitingCapacity && (IsAwaitExternalWaitReason(t.WaitReason) ||
				(t.AdoptedSession && IsAwaitDecisionWaitReason(t.WaitReason))):
				parked = append(parked, id)
			default:
				rest = append(rest, id)
			}
		}
	})
	return append(append(running, parked...), rest...)
}
