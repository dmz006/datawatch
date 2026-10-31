package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// CapacityKeysFn maps a task's backend and model to the capacity pools it
// needs plus its primary compute node (for model affinity).
type CapacityKeysFn func(backend, model string) (pools []string, node string)

// CapacityChoicesFn is CapacityKeysFn's sibling: the candidate compute nodes
// (name and scheduling priority) of the LLM entry a backend and model resolve
// to. When it returns two or more, CapacityKeysFn leaves node:<name> out of the
// pools and the caller puts these on its capacity.Request so the ledger picks
// the node at dispatch time. Fewer than two means the single-node path: pools
// and node from CapacityKeysFn, no choices.
type CapacityChoicesFn func(backend, model string) []capacity.NodeChoice

var errCancelledWhileWaiting = errors.New("task cancelled while waiting for capacity")

// errDecomposeCancelled is returned by decomposeAdmitted when the operator
// cancels (or rejects) the PRD while its planner is still queued on
// capacity. The two decompose retry loops (Decompose, decomposeStreamingCore)
// must treat it as "not a decompose failure": the PRD already holds a
// terminal status, so neither the infra retry budget must be consumed nor
// the draft-rollback path of failDecomposeAttempt run — both of those would
// silently clobber PRDCancelled/PRDRejected back to PRDDraft.
var errDecomposeCancelled = errors.New("decompose cancelled while waiting for capacity")

// ErrWorkerStalled marks a verify() error as a worker-stall detection (SSE
// stall, or the log-mtime staleWorkerCheck) rather than a hard fault. The
// executor's retry budget applies to it exactly like a normal verification
// failure — the previous behavior (a bare error return) exited executeOne
// immediately on the FIRST stall, silently ignoring auto_fix_retries and
// contradicting the "retrying task" wording in the error message itself.
var ErrWorkerStalled = errors.New("worker stalled")

const defaultCapacityWait = 4 * time.Hour

// defaultCIWatchWait is the ci-watch completion guardrail's wait bound
// when Config.CIWatchTimeoutSeconds is unset — see that field's comment
// for the rationale behind 25 minutes.
const defaultCIWatchWait = 1500 * time.Second

// capRetryPause is the pause between session-cap retries (var for tests).
var capRetryPause = 15 * time.Second

// SetCapacity wires the admission ledger and pool resolver.
func (m *Manager) SetCapacity(l *capacity.Ledger, keys CapacityKeysFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capacity = l
	m.capacityKeys = keys
}

// SetCapacityChoices wires the candidate-node resolver that sits beside the
// CapacityKeysFn given to SetCapacity. Nil (the default) keeps every admission
// on the single-node path.
func (m *Manager) SetCapacityChoices(fn CapacityChoicesFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capacityChoices = fn
}

// EntryNodesFn reports how many compute nodes the LLM entry hosting a backend
// and model lists, whatever the serving filter leaves of them. It is 0 for a
// backend that runs on no local engine.
type EntryNodesFn func(backend, model string) int

// SessionNodeFn names the compute node a session runs on ("" when it runs on
// none or the node is unknown).
type SessionNodeFn func(sessionID string) string

// SetSessionNode wires the resolver that tells adoptCapacity which compute
// node a surviving session occupies. Nil leaves adopted leases without a node.
func (m *Manager) SetSessionNode(fn SessionNodeFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionNode = fn
}

// SessionEndpointFn names the endpoint a session was placed on (its node
// address, else its engine URL), "" when it records none. adoptCapacity only
// logs it, to show which endpoint matched no compute node.
type SessionEndpointFn func(sessionID string) string

// SetSessionEndpoint wires the endpoint reader used in the log line for a
// worker adopted on no known node. Nil leaves the endpoint out of that line.
func (m *Manager) SetSessionEndpoint(fn SessionEndpointFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionEndpoint = fn
}

// SetEntryNodes wires the count of compute nodes an LLM entry lists, which
// tells adoptCapacity a single-node entry from a multi-node entry whose
// candidates the serving filter collapsed to one. Nil keeps the resolver's node
// as the default for every entry.
func (m *Manager) SetEntryNodes(fn EntryNodesFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entryNodes = fn
}

// SetNodeLoad wires the step that makes a worker's model resident on the
// node its lease was granted on, and the alert sink for a failed load. It
// runs only for an LLM entry that pools two or more nodes; a nil loader
// keeps every worker on the spawn path it always took.
func (m *Manager) SetNodeLoad(loader NodeLoader, alerts NodeLoadAlerts) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodeLoad = loader
	m.nodeAlerts = alerts
}

// SetCooldownUntilFn wires the BL30 global-cooldown reset time (zero = none
// active), used only to word the wait reason when a spawn hits
// session.ErrGlobalCooldown.
func (m *Manager) SetCooldownUntilFn(fn func() time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cooldownUntilFn = fn
}

// SetWaitClock injects the time source for the spawn wait loop (tests).
func (m *Manager) SetWaitClock(c capacity.Clock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waitClock = c
}

type realWaitClock struct{}

func (realWaitClock) Now() time.Time                         { return time.Now() }
func (realWaitClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

func (m *Manager) spawnWaitClock() capacity.Clock {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.waitClock != nil {
		return m.waitClock
	}
	return realWaitClock{}
}

// globalCooldownReason words the wait reason for an ErrGlobalCooldown spawn.
func (m *Manager) globalCooldownReason() string {
	m.mu.Lock()
	fn := m.cooldownUntilFn
	m.mu.Unlock()
	if fn != nil {
		if until := fn(); !until.IsZero() {
			return GlobalCooldownWaitText(m.spawnWaitClock().Now, until)
		}
	}
	return GlobalCooldownWaitText(nil, time.Time{})
}

// Capacity returns the ledger (nil when not wired).
func (m *Manager) Capacity() *capacity.Ledger {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capacity
}

// CapacityKeys returns the pool-resolution function wired via SetCapacity
// (nil when not wired). Exposed so callers outside the executor loop (the
// verifier's own /api/ask call, wired in cmd/datawatch/main.go) can acquire
// the same node:/llm: pools a task on that backend+model would use, even
// when the verifier's resolved backend differs from the task's own worker
// backend (BL25's "cross-backend independence" design).
func (m *Manager) CapacityKeys() CapacityKeysFn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capacityKeys
}

// CapacityChoices returns the candidate-node resolver wired via
// SetCapacityChoices (nil when not wired), for callers outside the executor
// loop that build their own capacity.Request next to CapacityKeys.
func (m *Manager) CapacityChoices() CapacityChoicesFn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capacityChoices
}

// nodeChoicesFor returns the candidate nodes for backend and model, or nil
// unless there are two or more: the signal to put them on the request.
func nodeChoicesFor(fn CapacityChoicesFn, backend, model string) []capacity.NodeChoice {
	if fn == nil {
		return nil
	}
	if choices := fn(backend, model); len(choices) >= 2 {
		return choices
	}
	return nil
}

// placementRefresh builds a Request.Refresh for base: each call re-reads the
// pool keys and node candidates through the manager's capacity functions and
// returns base with Pools, Node and NodeChoices rebuilt the way the first
// admission built them (a candidate list needs two or more entries). base's
// ExcludeNodes carry over. trim, when set, filters the pools (the planner
// drops "host"). m.mu is held only to read the functions, never across the
// ledger's call, since the ledger invokes Refresh under its own lock.
func (m *Manager) placementRefresh(base capacity.Request, backend, model string, trim func([]string) []string) func() capacity.Request {
	return func() capacity.Request {
		m.mu.Lock()
		keysFn, choicesFn := m.capacityKeys, m.capacityChoices
		m.mu.Unlock()
		cur := base
		if keysFn == nil {
			return cur
		}
		pools, node := keysFn(backend, model)
		if trim != nil {
			pools = trim(pools)
		}
		cur.Pools, cur.Node = pools, node
		cur.NodeChoices = nodeChoicesFor(choicesFn, backend, model)
		return cur
	}
}

// CapacityWaitDuration returns the configured capacity-wait bound (mirrors
// the private capacityWait() used inside the executor loop).
func (m *Manager) CapacityWaitDuration() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.CapacityWaitTimeoutSeconds > 0 {
		return time.Duration(m.cfg.CapacityWaitTimeoutSeconds) * time.Second
	}
	return defaultCapacityWait
}

func (m *Manager) capacityOn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capacity != nil && m.capacityKeys != nil && (m.cfg.CapacityEnabled == nil || *m.cfg.CapacityEnabled)
}

func (m *Manager) capacityWait() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.CapacityWaitTimeoutSeconds > 0 {
		return time.Duration(m.cfg.CapacityWaitTimeoutSeconds) * time.Second
	}
	return defaultCapacityWait
}

// ciWatchTimeout bounds how long the ci-watch completion guardrail waits
// for this automaton's own pushed commit's CI runs (mirrors capacityWait).
func (m *Manager) ciWatchTimeout() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cfg.CIWatchTimeoutSeconds > 0 {
		return time.Duration(m.cfg.CIWatchTimeoutSeconds) * time.Second
	}
	return defaultCIWatchWait
}

func (m *Manager) taskCancelled(id string) bool {
	if cur, ok := m.store.TaskSnapshot(id); ok {
		return cur.Status == TaskCancelled
	}
	return false
}

func (m *Manager) setWaiting(t *Task, reason string) {
	var old TaskStatus
	applied := m.store.MutateTask(t.ID, func(c *Task) {
		old = c.Status
		c.Status = TaskWaitingCapacity
		c.WaitReason = reason
	})
	if applied {
		m.emitTaskStatus(nil, t, old, nil, nil)
	}
	m.EmitPRDUpdate(t.PRDID)
}

func (m *Manager) clearWaiting(t *Task) {
	var old TaskStatus
	applied := m.store.MutateTask(t.ID, func(c *Task) {
		old = c.Status
		if c.Status == TaskWaitingCapacity {
			c.Status = TaskInProgress
		}
		c.WaitReason = ""
	})
	if applied {
		m.emitTaskStatus(nil, t, old, nil, nil)
	}
	m.EmitPRDUpdate(t.PRDID)
}

// admit blocks until the task holds a capacity lease. Waiting is visible as
// TaskWaitingCapacity and never counts as a retry or failure. role ranks the
// request in the ledger's dispatch order (see capacity.Role).
func (m *Manager) admit(ctx context.Context, prd *PRD, t *Task, backend, model string, role capacity.Role) error {
	return m.admitPreferring(ctx, prd, t, backend, model, role, "")
}

// admitPreferring is admit with a tie-breaking node: among equally loaded
// candidates the ledger picks prefer first. A relaunch passes the node of its
// previous attempt, so placement still chooses the least loaded node and the
// old one only wins a tie.
func (m *Manager) admitPreferring(ctx context.Context, prd *PRD, t *Task, backend, model string, role capacity.Role, prefer string) error {
	if role == capacity.RoleWorker {
		if err := m.waitAutomationBudget(ctx, prd, t); err != nil {
			return err
		}
	}
	return m.acquireLease(ctx, prd, t, backend, model, role, nil, prefer)
}

// acquireLease blocks until the task holds a capacity lease, never on a node
// named in exclude, and breaking a tie between equally loaded nodes toward
// prefer. admit calls it with nil and ""; the node-load failover calls it
// again with the nodes that failed for this attempt.
func (m *Manager) acquireLease(ctx context.Context, prd *PRD, t *Task, backend, model string, role capacity.Role, exclude []string, prefer string) error {
	if !m.capacityOn() {
		return nil
	}
	m.mu.Lock()
	led, keysFn, choicesFn := m.capacity, m.capacityKeys, m.capacityChoices
	m.mu.Unlock()
	pools, node := keysFn(backend, model)
	if len(pools) == 0 {
		return nil
	}
	choices := nodeChoicesFor(choicesFn, backend, model)
	waited := false
	rec := m.capacityAudit()
	var waitStart time.Time
	var lastCause WaitCause
	req := capacity.Request{
		Holder: t.ID, PRDID: prd.ID, Priority: prd.Priority, Pools: pools, Node: node, NodeChoices: choices, ExcludeNodes: exclude, PreferNode: prefer, Model: model, Role: role,
	}
	req.Refresh = m.placementRefresh(req, backend, model, nil)
	err := led.Acquire(ctx, req, m.capacityWait(), func() bool { return m.taskCancelled(t.ID) }, func(reason string) {
		if !waited {
			waitStart = rec.Now()
		}
		// The ledger reports every change of reason text (a holder joining a
		// pool, a count moving); the audit log records only a change of cause.
		if cause := ClassifyWaitReason(reason); !waited || cause != lastCause {
			rec.AdmissionWait(t.ID, prd.ID, cause, reason, pools)
			lastCause = cause
		}
		waited = true
		m.setWaiting(t, reason)
	})
	switch {
	case err == nil:
		if waited {
			rec.AdmissionAdmitted(t.ID, waitStart, pools)
			m.clearWaiting(t)
		}
		return nil
	case errors.Is(err, context.Canceled) && m.taskCancelled(t.ID):
		return errCancelledWhileWaiting
	case errors.Is(err, capacity.ErrWaitTimeout):
		return fmt.Errorf("capacity: %w", err)
	default:
		return err
	}
}

// heldNode returns the compute node the task's worker lease was granted on,
// or "" when capacity is off or the lease names no node. It is read after
// admit succeeds, so the spawn runs where the ledger placed the task.
func (m *Manager) heldNode(taskID string) string {
	led := m.Capacity()
	if led == nil {
		return ""
	}
	if ls := led.LeaseOf(taskID); ls != nil {
		return ls.Node
	}
	return ""
}

// leaseNodeStale reports whether the node the task's lease was granted on is
// no longer a candidate for backend and model: absent from the current
// candidate list, or different from the single node the resolver now names.
func (m *Manager) leaseNodeStale(taskID, backend, model string) bool {
	node := m.heldNode(taskID)
	if node == "" {
		return false
	}
	m.mu.Lock()
	keysFn, choicesFn := m.capacityKeys, m.capacityChoices
	m.mu.Unlock()
	if choicesFn != nil {
		if choices := choicesFn(backend, model); len(choices) > 0 {
			for _, c := range choices {
				if c.Name == node {
					return false
				}
			}
			return true
		}
	}
	if keysFn == nil {
		return false
	}
	_, cur := keysFn(backend, model)
	return cur != "" && cur != node
}

// replaceStaleLease treats the leased node as advisory. When the node was
// removed from the LLM entry's compute_nodes list after the lease was granted,
// the lease is released and acquired again so placement chooses from the
// current list, and the spawn never carries the removed node.
func (m *Manager) replaceStaleLease(ctx context.Context, prd *PRD, t *Task, backend, model string) error {
	if !m.leaseNodeStale(t.ID, backend, model) {
		return nil
	}
	m.releaseCapacity(t.ID)
	return m.acquireLease(ctx, prd, t, backend, model, capacity.RoleWorker, nil, "")
}

// decomposeAdmitted runs one decompose attempt inside a RolePlanner capacity
// lease on the same pools a task on that backend+model would use, minus the
// "host" pool.
//
// The host exclusion is not cosmetic. A session-based decompose spawns the
// autonomous:decompose one-shot session, and the ledger's External counter
// (cmd/datawatch) already charges that session against the host pool. Holding
// a host lease at the same time would stack a second slot on a session that
// is already counted, halving the effective host budget for planning. The
// ask-based decompose spawns no session, but its engine concurrency is still
// bounded by the llm: pool it takes here, so nothing is under-counted either
// way. (The task path keeps its host lease as-is: only the decompose session
// is in External's count.)
//
// The lease is held only for the attempt, so a PRD waiting on a busy node
// queues behind workers and verifiers (RolePlanner ranks last) instead of
// spawning past the limit, and it is released on every exit path — success,
// error, cancel, timeout — because Release is deferred immediately after the
// successful Acquire.
//
// The wait is visible: while queued, the reason is persisted on the PRD itself
// (PRD.WaitReason, same convention as Task.WaitReason for waiting tasks) and
// emitted as a PRD update, and it is cleared as soon as the wait ends —
// admitted, timed out, or cancelled. Cancelling the PRD while the planner is
// still queued drops the waiter immediately (plannerCancelled, mirroring the
// task path's taskCancelled) instead of holding its queue position until the
// global capacity wait timeout. With capacity off the attempt runs unchanged.
func (m *Manager) decomposeAdmitted(prd *PRD, backend, model string, req DecomposeRequest) (string, error) {
	if !m.capacityOn() {
		return m.decompose(req)
	}
	m.mu.Lock()
	led, keysFn, choicesFn := m.capacity, m.capacityKeys, m.capacityChoices
	m.mu.Unlock()
	allPools, node := keysFn(backend, model)
	pools := decomposePools(allPools)
	choices := nodeChoicesFor(choicesFn, backend, model)
	if len(pools) == 0 && len(choices) == 0 {
		return m.decompose(req)
	}
	holder := "plan:" + prd.ID
	lreq := capacity.Request{
		Holder: holder, PRDID: prd.ID, Priority: prd.Priority, Pools: pools, Node: node, NodeChoices: choices, Model: model, Role: capacity.RolePlanner,
	}
	lreq.Refresh = m.placementRefresh(lreq, backend, model, decomposePools)
	err := led.Acquire(context.Background(), lreq, m.capacityWait(), func() bool { return m.plannerCancelled(prd.ID) }, func(reason string) {
		m.setPRDWaitReason(prd.ID, reason)
	})
	if err != nil {
		// The wait ended without admission (timeout or cancel): nothing is held,
		// but the queued reason must not outlive the wait.
		m.clearPRDWaitReason(prd.ID)
		if m.plannerCancelled(prd.ID) {
			// Not an attempt failure — the PRD went terminal under us. The
			// retry loops must neither retry (budget) nor roll back (draft);
			// see errDecomposeCancelled.
			return "", errDecomposeCancelled
		}
		return "", fmt.Errorf("capacity: %w", err)
	}
	defer led.Release(holder)
	m.clearPRDWaitReason(prd.ID)
	return m.decompose(req)
}

// decomposePools drops the "host" pool from a decompose lease. The
// autonomous:decompose one-shot already counts against the host pool through
// the ledger's External counter (cmd/datawatch), so the planner lease must
// never stack on it; everything else (llm:, node:) stays.
func decomposePools(pools []string) []string {
	out := make([]string, 0, len(pools))
	for _, p := range pools {
		if p == "host" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// plannerCancelled reports whether the operator cancelled (or rejected) the
// PRD while the planner is still queued on capacity, so the waiter drops out
// immediately instead of running to the global capacity wait timeout. The
// PRD can also be gone; that is not a cancel (the waiter simply finishes and
// the LLM call fails on the missing PRD, same as before capacity existed).
func (m *Manager) plannerCancelled(prdID string) bool {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return false
	}
	return prd.Status == PRDCancelled || prd.Status == PRDRejected
}

// setPRDWaitReason persists why the planner is queued on the PRD and emits a
// PRD update, so the status block / PWA show it without a poll. The reason
// must name what the planner is waiting for (e.g. "llm:ollama at 1/1 (1
// waiting)"). No-op when the reason is unchanged. The write goes through
// MutatePRD (under the store lock) so it is ordered against concurrent
// SnapshotPRD reads — the direct GetPRD-mutate-SavePRD form raced a poll
// under -race the moment the planner queued while a status reader ran.
func (m *Manager) setPRDWaitReason(prdID, reason string) {
	applied := m.store.MutatePRD(prdID, func(p *PRD) bool {
		if p.WaitReason == reason {
			return false
		}
		p.WaitReason = reason
		return true
	})
	if applied {
		m.EmitPRDUpdate(prdID)
	}
}

// clearPRDWaitReason empties PRD.WaitReason once the wait is over — the
// planner admitted, or the wait ended in timeout/cancel. A stale reason is
// worse than none: an operator reading "waiting on node:alpha" after the
// planner moved on would chase a queue that no longer exists. Like
// setPRDWaitReason this mutates under the store lock (MutatePRD) so it is
// synchronized with concurrent SnapshotPRD readers.
func (m *Manager) clearPRDWaitReason(prdID string) {
	applied := m.store.MutatePRD(prdID, func(p *PRD) bool {
		if p.WaitReason == "" {
			return false
		}
		p.WaitReason = ""
		return true
	})
	if applied {
		m.EmitPRDUpdate(prdID)
	}
}

// adoptCapacity (BL462 F1) gives a task whose session survived a daemon
// restart the lease it held before the restart. The ledger is in-memory
// only, so without this a live task holds no slot and the host pool
// over-admits. It never waits: the session already occupies its slots.
// Idempotent, so the boot-time pass and executeOne's skip-spawn path can
// both call it. A task whose worker lease was handed to its verifier already
// holds its slot under VerifyHolder, so a later pass adopts nothing for it.
//
// The node the lease names is, in order: the node recorded on the task when
// its worker was started, the node the session says it runs on (its explicit
// reference, then its own endpoint), the resolver's node when the worker's LLM
// entry lists exactly one compute node, and otherwise none. An entry that
// lists several nodes never falls back to its first. When the worker is a
// local-engine session of an entry with two or more nodes and none resolves,
// the lease holds no node and is flagged node-unknown, and one line is logged.
func (m *Manager) adoptCapacity(prd *PRD, t *Task) {
	if !m.capacityOn() {
		return
	}
	m.mu.Lock()
	led, keysFn, choicesFn := m.capacity, m.capacityKeys, m.capacityChoices
	fallback := m.cfg.ExecutionBackend
	sessionNode, entryNodes, sessionEndpoint := m.sessionNode, m.entryNodes, m.sessionEndpoint
	m.mu.Unlock()
	story := findStory(prd, t.StoryID)
	backend, model := resolveTaskBackendModel(t, story, prd, m.AutomationForPRD(prd), fallback)
	pools, node := keysFn(backend, model)
	if len(pools) == 0 {
		return
	}
	node = adoptedNode(t, sessionNode, node, entryNodes, backend, model)
	choices := nodeChoicesFor(choicesFn, backend, model)
	unknown := node == "" && nodeExpected(entryNodes, len(choices), backend, model)
	if unknown {
		if ls := led.LeaseOf(t.ID); ls == nil || !ls.NodeUnknown {
			endpoint := ""
			if sessionEndpoint != nil && t.SessionID != "" {
				endpoint = sessionEndpoint(t.SessionID)
			}
			log.Printf("[autonomous] adopt: task=%s session=%s endpoint=%q matches no compute node; lease holds no node (node_unknown)", t.ID, t.SessionID, endpoint)
		}
	}
	led.Adopt(capacity.Request{
		Holder: t.ID, PRDID: prd.ID, Priority: prd.Priority, Pools: poolsWithoutNodes(pools), Node: node, NodeChoices: choices, Model: model, NodeUnknown: unknown,
	}, t.SessionID, VerifyHolder(t.ID))
}

// nodeExpected reports whether an adopted worker should sit on a compute node:
// it is a local-engine session of an LLM entry that lists two or more nodes.
// Without an entry count it falls back to the live candidate list. A backend
// that never takes a node reports 0 nodes and no candidates.
func nodeExpected(entryNodes EntryNodesFn, candidates int, backend, model string) bool {
	if entryNodes != nil {
		return entryNodes(backend, model) >= 2
	}
	return candidates >= 2
}

// adoptedNode picks the node an adopted worker occupies: the node recorded on
// the task, then the node its session reports, then resolved when the entry
// lists a single node (or no count is wired), else "".
func adoptedNode(t *Task, sessionNode SessionNodeFn, resolved string, entryNodes EntryNodesFn, backend, model string) string {
	if t.ComputeNode != "" {
		return t.ComputeNode
	}
	if sessionNode != nil && t.SessionID != "" {
		if n := sessionNode(t.SessionID); n != "" {
			return n
		}
	}
	if entryNodes == nil || entryNodes(backend, model) == 1 {
		return resolved
	}
	return ""
}

// poolsWithoutNodes drops every node:<name> pool. The ledger adds the pool of
// the node an adopted lease names, so a node pool the resolver chose as the
// default cannot be held next to the node the worker really occupies.
func poolsWithoutNodes(pools []string) []string {
	out := make([]string, 0, len(pools))
	for _, p := range pools {
		if !strings.HasPrefix(p, "node:") {
			out = append(out, p)
		}
	}
	return out
}

// releaseForRelaunch frees every lease a relaunched task still holds from its
// previous attempt (the worker lease and the one handed to the verifier) and
// returns the node that attempt ran on, or "" when unknown. The node comes from
// the leases, then from the node recorded on the task. Freeing the leases
// sends the next admission through placement instead of letting Acquire
// return at once for a holder that is already admitted.
func (m *Manager) releaseForRelaunch(t *Task) string {
	prev := ""
	if led := m.Capacity(); led != nil {
		for _, h := range []string{t.ID, VerifyHolder(t.ID)} {
			if ls := led.LeaseOf(h); ls != nil && ls.Node != "" {
				prev = ls.Node
				break
			}
		}
	}
	if prev == "" {
		prev = t.ComputeNode
	}
	m.releaseCapacity(t.ID)
	m.releaseCapacity(VerifyHolder(t.ID))
	return prev
}

// releaseCapacity frees the task's lease (safe when none is held).
func (m *Manager) releaseCapacity(taskID string) {
	m.mu.Lock()
	led := m.capacity
	m.mu.Unlock()
	if led != nil {
		led.Release(taskID)
	}
}

// VerifyHolder is the capacity-holder name the verifier admits under while a
// worker lease is handed to it, so one task holds exactly one slot from spawn
// through verification (the worker lease "<taskID>" becomes
// "verify:<taskID>").
func VerifyHolder(taskID string) string { return "verify:" + taskID }

// JudgeHolder is the capacity-holder name a guardrail judge admits under
// while judging a unit (task, story, or PRD). One unit never has two
// concurrent judges — runPerTaskGuardrails / runPerStoryGuardrails run their
// resolved guardrail list strictly sequentially (executor.go), the PRD
// completion judges (rules review, CI watch) fire one at a time at rollup,
// and rejudge runs only after the PRD left the running state — so a bare
// "judge:<unitID>" is always unique per unit and needs no guardrail-name
// suffix.
func JudgeHolder(unitID string) string { return "judge:" + unitID }

// HandoffVerifyLease re-keys the task's worker lease (holder taskID) to the
// verifier's "verify:<taskID>" lease so the task in verification holds exactly
// one slot instead of two, with no release-and-re-wait window in which a
// sibling task could steal the slot. It reports false — changing nothing —
// when capacity is off, the task holds no worker lease, or the verifier's
// backend/model resolves to a different pool set than the worker lease
// covers; in that case the caller must fall back to releasing the worker
// lease and admitting the verifier separately.
func (m *Manager) HandoffVerifyLease(taskID, verifierBackend, verifierModel string) bool {
	if taskID == "" || !m.capacityOn() {
		return false
	}
	m.mu.Lock()
	led, keysFn, choicesFn := m.capacity, m.capacityKeys, m.capacityChoices
	m.mu.Unlock()
	_, ok := judgeHandoff(led, keysFn, choicesFn, taskID, VerifyHolder(taskID), verifierBackend, verifierModel)
	return ok
}

// judgeHandoff re-keys the worker lease of taskID to holder when a judge that
// resolves to backend and model can run on it, and returns the node the judge
// holds. The node is "" when the judge's entry has fewer than two candidate
// nodes: a single-node judge keeps the rule it always had, the lease's pool
// set must equal the judge's pool set exactly.
//
// A judge whose entry pools two or more nodes is different. The worker lease
// names one of those nodes in its pools (node:<name>), and the judge's own
// pools carry no node pool, so the two are compared without their node pools.
// The hand-off then needs the worker's node among the judge's candidates, and
// keeps that node: the lease moves as it is, with no release and no re-wait.
func judgeHandoff(led *capacity.Ledger, keysFn CapacityKeysFn, choicesFn CapacityChoicesFn, taskID, holder, backend, model string) (string, bool) {
	if led == nil || keysFn == nil {
		return "", false
	}
	workerLease := led.LeaseOf(taskID)
	if workerLease == nil {
		return "", false
	}
	verifyPools, _ := keysFn(backend, model)
	if len(verifyPools) == 0 {
		return "", false
	}
	choices := nodeChoicesFor(choicesFn, backend, model)
	if len(choices) == 0 {
		if !samePoolSet(workerLease.Pools, verifyPools) {
			return "", false
		}
		return "", led.Handoff(taskID, holder)
	}
	if workerLease.Node == "" || !slices.ContainsFunc(choices, func(c capacity.NodeChoice) bool { return c.Name == workerLease.Node }) {
		return "", false
	}
	if !samePoolSet(withoutNodePools(workerLease.Pools), withoutNodePools(verifyPools)) {
		return "", false
	}
	if !led.Handoff(taskID, holder) {
		return "", false
	}
	return workerLease.Node, true
}

// withoutNodePools returns pools minus every node:<name> pool.
func withoutNodePools(pools []string) []string {
	out := make([]string, 0, len(pools))
	for _, p := range pools {
		if !strings.HasPrefix(p, "node:") {
			out = append(out, p)
		}
	}
	return out
}

// samePoolSet reports whether the two pool lists name the same set of pools
// (order-insensitive; duplicates count).
func samePoolSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, p := range a {
		counts[p]++
	}
	for _, p := range b {
		counts[p]--
	}
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
}

// IsCapacityError reports whether a spawn error means "no free session slot".
func IsCapacityError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "max sessions")
}

// IsWorkspaceLockError (BL407 Phase 4) reports whether a spawn error is
// agents.Manager.checkWorkspaceLock's "another active agent already
// owns this (project_profile, branch) tuple" refusal. Every
// cluster-dispatched task of one PRD now requests the SAME
// Phase-4-assigned branch (one PR per PRD, Decision 8) — the
// executor's own concurrent task pool (BL370) can easily try to
// dispatch a second one before the first's agent reaches a terminal
// state, which the workspace lock (designed for two *unrelated*
// agents colliding by coincidence) would otherwise reject outright.
// Treating it as retryable, exactly like a capacity-full wait, is the
// correct fix: two containers racing pushes to the SAME branch is a
// real hazard this lock exists to prevent, so the right behavior is
// to wait for the sibling task's agent to finish, not to bypass the
// lock.
func IsWorkspaceLockError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "workspace lock:")
}

// isSpawnWaitError reports whether a spawn error is a refusal to wait out
// (a full session cap, a workspace-lock collision, the global cooldown or the
// capacity wait timeout) rather than a worker that failed to start. These are
// never infrastructure failures.
func isSpawnWaitError(err error) bool {
	return IsCapacityError(err) || IsWorkspaceLockError(err) ||
		errors.Is(err, session.ErrGlobalCooldown) || errors.Is(err, capacity.ErrWaitTimeout)
}

// capacityRetrySpawn wraps a SpawnFn so a full session cap, a workspace-lock
// collision between two of the same PRD's own cluster-dispatched tasks
// (BL407 Phase 4), or the BL30 global cooldown (session.ErrGlobalCooldown)
// is a wait rather than a hard failure: it retries with a pause, showing
// the task as waiting_capacity. None of these consume the auto-retry
// budget; on autonomous.capacity_wait_timeout_seconds the wrapper returns a
// capacity.ErrWaitTimeout error, the same path every other wait takes.
func (m *Manager) capacityRetrySpawn(t *Task, spawn SpawnFn) SpawnFn {
	return func(ctx context.Context, req SpawnRequest) (SpawnResult, error) {
		clk := m.spawnWaitClock()
		start := clk.Now()
		waited := false
		for {
			sr, err := spawn(ctx, req)
			cooldown := errors.Is(err, session.ErrGlobalCooldown)
			if !IsCapacityError(err) && !IsWorkspaceLockError(err) && !cooldown {
				if waited {
					m.clearWaiting(t)
				}
				return sr, err
			}
			reason := ""
			if cooldown {
				reason = m.globalCooldownReason()
			} else {
				reason = err.Error()
			}
			if clk.Now().Sub(start) >= m.capacityWait() {
				if IsWorkspaceLockError(err) {
					return sr, fmt.Errorf("workspace lock: %w: %v", capacity.ErrWaitTimeout, err)
				}
				if cooldown {
					return sr, fmt.Errorf("capacity: %w: %s", capacity.ErrWaitTimeout, reason)
				}
				return sr, fmt.Errorf("capacity: %w: %v", capacity.ErrWaitTimeout, err)
			}
			waited = true
			m.setWaiting(t, reason)
			select {
			case <-ctx.Done():
				return sr, ctx.Err()
			case <-clk.After(capRetryPause):
			}
			if m.taskCancelled(t.ID) {
				return sr, errCancelledWhileWaiting
			}
		}
	}
}
