package autonomous

// Design §6.ii — "await external": a task blocked on an EXTERNAL condition
// (a long CI run, a deploy finishing, an upstream review) releases its
// ACTIVE-inference slot while it waits, instead of pinning the pool the whole
// time. The park is a first-class, resumable state: the worker keeps its
// session and its context, its lease is returned to the pool, and it is
// re-admitted through the normal admission path once capacity frees — without
// counting a retry or a failure.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// AwaitExternalMarker is the parsable line the worker emits when it parks
// the task on an external condition. Same style as the existing
// DATAWATCH_COMPLETE / DATAWATCH_NEEDS_INPUT / DATAWATCH_RATE_LIMITED marker
// lines: one line, the prefix on its own, free text after it (the condition
// being awaited, e.g. "CI run #42 on prs/7da90001"). After emitting it the
// worker must stop and do no further work; the automaton re-prompts it when
// the slot is re-admitted.
const AwaitExternalMarker = "DATAWATCH_AWAIT_EXTERNAL:"

// STATUS-REASON CHOICE (design §6.ii asks that the choice be documented
// here):
//
// REJECTED: a new TaskStatus (e.g. TaskAwaitingExternal). The set of task
// statuses is switched over in at least a dozen places (runPRD,
// resetInProgressTasksForResume, reconcileStuckTasks,
// resolveStuckVerifyingTask, flattenTasks, isTaskTerminal…). A new status
// value silently lands on `default` in every unconsumed switch — for
// example reconcileStuckTasks' default path kills the session and retries
// the task, which would defeat the feature — and every new consumer added
// later has to remember the new value.
//
// CHOSEN: TaskWaitingCapacity with a distinct WaitReason sub-reason, the
// "await_external" prefix (AwaitExternalWaitReasonFor). Waiting capacity
// already means "idle, not a failure, not a retry attempt" — precisely what
// a parked task is — and every existing consumer treats it safely. The sub-
// reason is carried verbatim into the autonomous_status /
// autonomous_prd_get JSON in Task.WaitReason, which is the required
// visibility. IsAwaitExternalWaitReason is the single predicate for the
// sub-reason so current and future call sites stay consistent.
const (
	// AwaitExternalWaitReason is the bare sub-reason (no condition text).
	AwaitExternalWaitReason = "await_external"
	// AwaitExternalWaitReasonPrefix is the reason prefix. The full reason is
	// either the bare reason or "await_external: <condition>".
	AwaitExternalWaitReasonPrefix = "await_external"
	// maxAwaitExternalConditionChars bounds the condition text kept in the
	// wait reason so status JSON stays compact.
	maxAwaitExternalConditionChars = 400
)

// AwaitExternalWaitReasonFor renders the wait reason for a task parked on
// the given external condition.
func AwaitExternalWaitReasonFor(condition string) string {
	return AwaitExternalWaitText(condition)
}

// IsAwaitExternalWaitReason reports whether a Task.WaitReason value is the
// await-external sub-reason of waiting_capacity.
func IsAwaitExternalWaitReason(reason string) bool {
	r := strings.TrimSpace(reason)
	if r == "" {
		return false
	}
	if r == AwaitExternalWaitReasonPrefix {
		return true
	}
	// "await_external: <cond>" — allow any separator a worker might emit
	// (colon, em dash).
	return strings.HasPrefix(r, AwaitExternalWaitReasonPrefix+":") ||
		strings.HasPrefix(r, AwaitExternalWaitReasonPrefix+": ") ||
		strings.HasPrefix(r, AwaitExternalWaitReasonPrefix+" — ")
}

// ExtractAwaitExternalCondition scans worker output for the await-external
// marker. It returns the 0-based line number of the LAST marker line present,
// the trimmed condition text, and whether the marker is FRESH (actionable).
// A marker is stale once the worker went on to emit a DATAWATCH_COMPLETE
// marker AFTER it (e.g. it is a pre-restart marker of a session that has
// since finished) — processing it would re-admit and re-prompt a session
// that is already done. Line number is the caller's per-cycle key so the
// same marker line is not double-processed across wait-loop ticks.
func ExtractAwaitExternalCondition(output string) (markerLine int, condition string, fresh bool) {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || !strings.HasPrefix(trimmed, AwaitExternalMarker) {
			continue
		}
		cond := strings.TrimSpace(trimmed[len(AwaitExternalMarker):])
		// FRESH iff no DATAWATCH_COMPLETE marker appears after this line.
		stale := false
		for j := i + 1; j < len(lines); j++ {
			if strings.Contains(lines[j], "DATAWATCH_COMPLETE:") {
				stale = true
				break
			}
		}
		return i, cond, !stale
	}
	return 0, "", false
}

// Manager wiring for the await-external cycle.

// SetSessionContinueFn wires the "resume a parked worker session" hook
// (design §6.ii): after the cycle re-admits the task, the parked session is
// prompted to re-check its condition and finish. Optional; nil skips it.
func (m *Manager) SetSessionContinueFn(fn func(sessionID string) error) {
	m.mu.Lock()
	m.sessionContinueFn = fn
	m.mu.Unlock()
}

func (m *Manager) sessionContinueFnLocked() func(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionContinueFn
}

// SetSessionContinueLineFn wires the line-carrying sibling of the continue
// hook: it prompts a parked worker session with the given one-line text
// instead of the fixed await-external prompt. The coordinator tick resumes a
// task parked on a worker question through it, so the prompt can carry the
// operator's decision. Optional; nil skips the prompt.
func (m *Manager) SetSessionContinueLineFn(fn func(sessionID, line string) error) {
	m.mu.Lock()
	m.sessionContinueLineFn = fn
	m.mu.Unlock()
}

func (m *Manager) sessionContinueLineFnLocked() func(sessionID, line string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessionContinueLineFn
}

// attachTreeRelease / detachTreeRelease / treeLockBoxLocked: the executor
// registers a pointer to its own task-tree-write-lock release closure
// (executeOne's treeRelease variable) here, so an await-external cycle can
// release the lock while the task waits (tree fully pushed + clean) and
// re-claim it on re-admission. Without this the manager could not touch the
// lock at all — it is owned by the executor goroutine.
func (m *Manager) attachTreeRelease(taskID string, box *func()) {
	if box == nil {
		return
	}
	m.treeBoxMu.Lock()
	if m.treeLockBox == nil {
		m.treeLockBox = make(map[string]*func())
	}
	m.treeLockBox[taskID] = box
	m.treeBoxMu.Unlock()
}

func (m *Manager) detachTreeRelease(taskID string) {
	m.treeBoxMu.Lock()
	delete(m.treeLockBox, taskID)
	m.treeBoxMu.Unlock()
}

func (m *Manager) treeLockBoxLocked(taskID string) *func() {
	m.treeBoxMu.Lock()
	defer m.treeBoxMu.Unlock()
	return m.treeLockBox[taskID]
}

// AwaitExternal runs one await-external cycle for a worker session that
// parked on an external condition. Called by the verify loop (cmd/datawatch)
// while the task still holds its capacity lease, and — when the task writes
// in the project tree — while the executor's tree-write lock is held and
// registered via attachTreeRelease. The cycle:
//
//  1. marks the task waiting with the await_external sub-reason (visible in
//     autonomous_status / autonomous_prd_get),
//  2. releases the task's capacity lease(s) — the Active inference slot,
//  3. RELEASES the project-tree write lock unless the tree has uncommitted
//     changes or commits not reachable from a remote-tracking ref — that is
//     the "keep the lock only while there is unpushed work" half of design
//     §6.ii,
//  4. emits the autonomous_idle_wait_release audit event,
//  5. re-admits the task (bounded by the normal capacity wait). Success does
//     NOT count a retry and does NOT record a failure; the task is the same
//     attempt resuming,
//  6. re-claims the tree lock (if released) and prompts the parked session
//     to continue, so it resumes WITHOUT a fresh spawn.
//
// On a re-admission error (timeout/cancelled) the task is left lease-free in
// waiting state; the surrounding retry machinery takes over from the returned
// error, exactly like a normal capacity-wait failure.
func (m *Manager) AwaitExternal(ctx context.Context, prd *PRD, t *Task, condition string) error {
	if prd == nil || t == nil {
		return fmt.Errorf("AwaitExternal: prd and task are required")
	}

	// 1. Visible wait state (the reason carries the awaited condition).
	m.setWaiting(t, AwaitExternalWaitReasonFor(condition))

	// 2. Release the lease(s). Capture which pools for the audit event.
	var releasedPools []string
	releasedNode := t.ComputeNode
	if led := m.Capacity(); led != nil {
		if lease := led.LeaseOf(t.ID); lease != nil {
			if len(lease.Pools) > 0 {
				releasedPools = append([]string(nil), lease.Pools...)
			}
			if lease.Node != "" {
				releasedNode = lease.Node
			}
		}
	}
	m.releaseCapacity(t.ID)

	// 3. Tree lock: keep ONLY while the tree has uncommitted changes or
	// commits not reachable from a remote-tracking ref.
	keepTree := m.treeHasUnpushedWork(prd, t)
	box := m.treeLockBoxLocked(t.ID)
	releasedTree := !keepTree && box != nil
	if releasedTree {
		(*box)()
	}

	// 4. Audit.
	m.emitAwaitReleaseAudit(prd, t, releasedPools, keepTree)

	drained := m.Capacity() != nil
	if !drained {
		// Capacity not wired at all: nothing to re-admit; prompt on.
		m.resumeAwaitExternal(prd, t, releasedTree, box)
		return nil
	}

	// 5. Bounded re-admission.
	if err := m.awaitReAdmit(ctx, prd, t, condition, releasedNode); err != nil {
		return fmt.Errorf("task %s: await-external re-admission: %w", t.ID, err)
	}

	// 6. Re-claim the lock (if released) and resume the worker.
	m.resumeAwaitExternal(prd, t, releasedTree, box)
	log.Printf("[autonomous] %s: await-external cycle complete: released pools=%v tree lock kept=%v; worker resumed",
		t.ID, releasedPools, keepTree)
	return nil
}

// resumeAwaitExternal re-claims the tree lock (when the cycle had released
// it) and prompts the parked session to continue the task.
func (m *Manager) resumeAwaitExternal(prd *PRD, t *Task, releasedTree bool, box *func()) {
	m.reclaimTreeLock(prd, t, releasedTree, box)
	if contFn := m.sessionContinueFnLocked(); contFn != nil && t.SessionID != "" {
		if cerr := contFn(t.SessionID); cerr != nil {
			log.Printf("[autonomous] %s: await-external continue prompt to session %s failed: %v", t.ID, t.SessionID, cerr)
		}
	}
}

// reclaimTreeLock re-claims the project-tree write lock for a resumed task
// when the park had released it, and re-registers the release closure in the
// executor's box (attachTreeRelease set a plain pointer; the map entry already
// points at the same box).
func (m *Manager) reclaimTreeLock(prd *PRD, t *Task, releasedTree bool, box *func()) {
	if !releasedTree {
		return
	}
	rel, rerr := m.acquireTreeWritersLock(context.Background(), prd, t)
	if rerr != nil {
		// Another same-tree writer now holds the lock — the session may
		// proceed for its follow-up (reading status), and the executor
		// re-acquires before any further tree writes.
		log.Printf("[autonomous] %s: resume: tree re-claim blocked (another writer holds it): %v", t.ID, rerr)
		return
	}
	if box != nil {
		*box = rel
	}
}

// awaitReAdmit is the bounded re-admission for an await-external task.
// Structured like admit() but the wait reason KEEPS the await_external
// prefix — the status JSON must show the task queued FOR the external
// condition (plus the pool it is parked behind), not a bare pool-reason
// string. prefer is the node the task released for the wait: placement still
// picks the least loaded node, and that node only wins a tie.
func (m *Manager) awaitReAdmit(ctx context.Context, prd *PRD, t *Task, condition, prefer string) error {
	return m.awaitReAdmitAs(ctx, prd, t, AwaitExternalWaitReasonFor(condition), prefer)
}

// awaitReAdmitAs is awaitReAdmit with the wait reason prefix chosen by the
// caller: an await-external cycle keeps "await_external: <condition>", a task
// resumed after a worker question keeps its "await_decision: <question id>"
// reason, so the status JSON names what the task is queued for.
func (m *Manager) awaitReAdmitAs(ctx context.Context, prd *PRD, t *Task, prefix, prefer string) error {
	if !m.capacityOn() {
		return nil
	}
	m.mu.Lock()
	led, keysFn, choicesFn := m.capacity, m.capacityKeys, m.capacityChoices
	fallback := m.cfg.ExecutionBackend
	m.mu.Unlock()
	story := findStory(prd, t.StoryID)
	backend, model := resolveTaskBackendModel(t, story, prd, m.AutomationForPRD(prd), fallback)
	pools, node := keysFn(backend, model)
	if len(pools) == 0 {
		return nil
	}
	req := capacity.Request{
		Holder: t.ID, PRDID: prd.ID, Priority: prd.Priority, Pools: pools, Node: node, NodeChoices: nodeChoicesFor(choicesFn, backend, model), PreferNode: prefer, Model: model, Role: capacity.RoleWorker,
	}
	req.Refresh = m.placementRefresh(req, backend, model, nil)
	err := led.Acquire(ctx, req, m.capacityWait(), func() bool { return m.taskCancelled(t.ID) }, func(reason string) {
		m.setWaiting(t, prefix+" — "+reason)
	})
	switch {
	case err == nil:
		m.clearWaiting(t)
		return nil
	case errors.Is(err, context.Canceled) && m.taskCancelled(t.ID):
		return errCancelledWhileWaiting
	case errors.Is(err, capacity.ErrWaitTimeout):
		return fmt.Errorf("capacity: %w (await-external re-admission)", err)
	default:
		return err
	}
}

// treeHasUnpushedWork reports whether the task's project tree has work a
// same-tree writer must not run underneath: uncommitted changes, or HEAD not
// reachable from any remote-tracking ref — the "keep the tree lock"
// predicate of design §6.ii. Reuses the exact reachability semantics of
// defaultCIGit (the same "Pushed" check the CI watcher uses: HEAD is
// reachable from at least one remote-tracking ref). Uncertain (not a repo,
// git failed, no HEAD) defaults to true — holding the lock is the safe side.
func (m *Manager) treeHasUnpushedWork(prd *PRD, t *Task) bool {
	dir := m.resolveTaskProjectDir(prd, t)
	if dir == "" {
		return false // no resolvable project dir: nothing shared to protect
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, _, pushed, err := defaultCIGit(ctx, dir)
	if err != nil {
		return true // can't prove it's pushed → keep the lock
	}
	if !pushed {
		return true // commits not reachable from a remote-tracking ref
	}
	// Pushed, but uncommitted changes still block same-tree writers: another
	// session could commit or overwrite them mid-run.
	return m.treeNotClean(ctx, dir)
}

// treeNotClean is the dirty-tree half of treeHasUnpushedWork: any uncommitted
// change to tracked files, or untracked (non-ignored) files. Runs git
// directly instead of gitProbe because a clean tree is a LEGITIMATE empty
// output — gitProbe converts empty output into an error. The .datawatch
// advisory-lock directory is excluded: it is datawatch's own machinery, not
// worker output, and it must not count as unpushed work for its own holder.
func (m *Manager) treeNotClean(ctx context.Context, dir string) bool {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "status", "--porcelain", "--", ".", ":(exclude).datawatch").Output() // #nosec G204 -- fixed subcommand
	if err != nil {
		return true // uncertain → conservative
	}
	return strings.TrimSpace(string(out)) != ""
}

// emitAwaitReleaseAudit fires the autonomous_idle_wait_release audit event
// for a completed await-external cycle. Best-effort; must never fail the
// cycle.
func (m *Manager) emitAwaitReleaseAudit(prd *PRD, t *Task, releasedPools []string, treeLockKept bool) {
	m.emitAwaitReleaseAuditFor(prd, t, releasedPools, treeLockKept, "")
}

// emitAwaitReleaseAuditFor is emitAwaitReleaseAudit with the condition the
// task is parked on ("decision <id>" for a worker question); an empty
// condition leaves the key out.
func (m *Manager) emitAwaitReleaseAuditFor(prd *PRD, t *Task, releasedPools []string, treeLockKept bool, condition string) {
	if m.auditFn == nil {
		return
	}
	details := map[string]any{
		"prd":            prd.ID,
		"task":           t.ID,
		"released_pools": releasedPools,
		"tree_lock_kept": treeLockKept,
	}
	if condition != "" {
		details["condition"] = condition
	}
	m.auditFn("autonomous_idle_wait_release", details)
}
