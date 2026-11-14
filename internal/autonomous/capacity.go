package autonomous

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
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

// consumerSessionSpawn names this consumer on every capacity request the
// autonomous manager makes for task placement: workers, planners, adopt and
// re-admission. It is the "session_spawn" name the compute nodes understand
// (see internal/compute/node.go): every placement here lands as a session
// Automaton itself spawns, and that is the consumer the node's drain and
// permission rules are written against. An empty consumer on the ledger means
// "no consumer check", so naming the consumer here turns the node's RBAC denials
// into a refusal reason in the wait reason instead of silently granting a
// slot.
const consumerSessionSpawn = "session_spawn"

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

// SetCapacity wires the admission ledger and pool resolver. The optional
// third argument is the placement advisor (planner verdict supplier); with
// it the admission gate applies the live missing_model policy before the
// lease, so the lease is granted on the node placement approved. With no
// advisor (the zero-arg form) the gate is skipped and admission proceeds
// exactly as before.
func (m *Manager) SetCapacity(l *capacity.Ledger, keys CapacityKeysFn, advisor ...PlacementAdvisor) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capacity = l
	m.capacityKeys = keys
	if len(advisor) > 0 && advisor[0] != nil {
		m.placementAdvisor = advisor[0]
	}
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

// SessionBackendFn reports the backend and model a session really runs
// (both "" when the session is unknown or records no backend).
type SessionBackendFn func(sessionID string) (backend, model string)

// SetSessionBackend wires the lookup adoptCapacity uses to read the backend a
// resumed session really runs. Nil leaves adopted leases on the task's own
// resolved backend.
func (m *Manager) SetSessionBackend(fn SessionBackendFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionBackend = fn
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
	return m.placementRefreshPinned(base, backend, model, trim, "", nil)
}

// placementRefreshPinned is placementRefresh for a request that may be pinned
// to one compute node. With a non-empty pin every call recomputes which nodes
// are excluded from the candidates read at that moment, so a node that joins
// the candidates while the request waits is excluded too; failover is the
// caller's own exclude list and is always kept. When the pin stops being
// honourable while the request waits (the node left the candidates), the
// request is made unplaceable rather than granted on another node. An empty
// pin behaves exactly as placementRefresh.
func (m *Manager) placementRefreshPinned(base capacity.Request, backend, model string, trim func([]string) []string, pin string, failover []string) func() capacity.Request {
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
		if pin != "" {
			pinned, err := applyNodePin(cur, pin, failover)
			if err != nil {
				return unplaceableRequest(cur, pin)
			}
			cur = pinned
		}
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

// capacityWaitRemaining is bound less the queue time this task's current
// admission has already used.
func (m *Manager) capacityWaitRemaining(taskID string, bound time.Duration) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return bound - m.capacityWaitSpent[taskID]
}

func (m *Manager) addCapacityWaitSpent(taskID string, d time.Duration) {
	if d <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.capacityWaitSpent == nil {
		m.capacityWaitSpent = map[string]time.Duration{}
	}
	m.capacityWaitSpent[taskID] += d
}

// resetCapacityWait ends a task's admission: the next one gets a full bound.
func (m *Manager) resetCapacityWait(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.capacityWaitSpent, taskID)
}

// setWaitReqPools records the pool(s) taskID is currently waiting on so the
// wait-reason annotator (nameRequestedPool) names the up-to-date pool. It is
// seeded at admit time and rewritten by a queued-mode retarget before the next
// wait reason renders. Guarded by mu; the value is copied so a later rewrite
// of the caller's slice cannot corrupt the stored one.
func (m *Manager) setWaitReqPools(taskID string, pools []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.waitReqPools == nil {
		m.waitReqPools = map[string][]string{}
	}
	if taskID == "" || len(pools) == 0 {
		return
	}
	m.waitReqPools[taskID] = append([]string(nil), pools...)
}

// waitReqPoolsNow returns the pools taskID is currently waiting on, or nil
// when it is not waiting. Guarded by mu.
func (m *Manager) waitReqPoolsNow(taskID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.waitReqPools[taskID]
}

// clearWaitReqPools drops a task's current-wait record when its admission
// ends. Guarded by mu.
func (m *Manager) clearWaitReqPools(taskID string) {
	m.mu.Lock()
	delete(m.waitReqPools, taskID)
	m.mu.Unlock()
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
	m.beginCapacityWait(t)
	m.EmitPRDUpdate(t.PRDID)
}

func (m *Manager) clearWaiting(t *Task) {
	m.endCapacityWait(t.ID)
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
//
// For RoleWorker, the placement gate runs first. When the model is missing
// (no node serves it) the gate applies the live missing_model policy: refuse
// fails the task with the planner's refusal text, ask parks the task as
// waiting ("model placement awaiting approval") until the operator answers or
// the ask times out, and auto_load goes straight to loading. Where the gate
// approves a node, the model is loaded there first and the lease is then
// restricted to that node, so the worker session starts on the node the model
// is on. When a node already serves the model, or no advisor is wired (tests,
// capacity off), the gate has no opinion and admission is unchanged: the
// ledger picks the node and loadWorkerNode makes the model resident on it.
func (m *Manager) admitPreferring(ctx context.Context, prd *PRD, t *Task, backend, model string, role capacity.Role, prefer string) error {
	if role != capacity.RoleWorker {
		return m.acquireLease(ctx, prd, t, backend, model, role, nil, prefer)
	}
	if err := m.waitAutomationBudget(ctx, prd, t); err != nil {
		return err
	}
	m.mu.Lock()
	keysFn, choicesFn, loader, sink := m.capacityKeys, m.capacityChoices, m.nodeLoad, m.nodeAlerts
	m.mu.Unlock()
	fallback := ""
	if keysFn != nil {
		_, fallback = keysFn(backend, model)
	}
	target, gerr := m.placementGate(ctx, t, backend, model, fallback)
	if gerr != nil {
		var refused *PlacementRefused
		if errors.As(gerr, &refused) {
			raisePlacementAlert(sink, refused)
		}
		return gerr
	}
	if target.Node == "" {
		return m.acquireLease(ctx, prd, t, backend, model, role, nil, prefer)
	}
	// Load the model on the approved node before the lease, so the worker's
	// first request finds it resident.
	if loader != nil {
		if lerr := m.loadOnNode(ctx, t, loader, target.Node, model); lerr != nil {
			if ctx.Err() != nil && m.taskCancelled(t.ID) {
				return errCancelledWhileWaiting
			}
			return fmt.Errorf("placement load on node %s: %w", target.Node, lerr)
		}
		m.emitPlacementAudit(audit.ActionPlacementLoaded, model, target.Node, target.Policy, "model resident before the lease")
	}
	// Lease on the approved node only: the other candidates do not have the
	// model, so a lease there would start the worker where it is not loaded.
	var exclude []string
	if choicesFn != nil {
		choices := choicesFn(backend, model)
		onList := false
		for _, c := range choices {
			if c.Name == target.Node {
				onList = true
			}
		}
		if onList {
			for _, c := range choices {
				if c.Name != target.Node {
					exclude = append(exclude, c.Name)
				}
			}
		}
	}
	return m.acquireLease(ctx, prd, t, backend, model, role, exclude, target.Node)
}

// capacityPriorityOf resolves the PRD's capacity-queue priority at read
// time: an explicitly set priority (PrioritySource "explicit") always
// wins; otherwise the priority_by_kind table (autonomous.priority_by_kind)
// decides by effective kind, with the "other" entry as fallback. Resolving
// here (instead of trusting the persisted value) keeps the ledger in sync
// with a live table change without re-writing the PRD record.
func (m *Manager) capacityPriorityOf(prd *PRD) int {
	p, _ := ResolvePriority(prd, m.PriorityByKind())
	return p
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
	defer m.endCapacityWait(t.ID)
	rec := m.capacityAudit()
	var waitStart time.Time
	var lastCause WaitCause
	req := capacity.Request{
		Holder: t.ID, PRDID: prd.ID, Priority: m.capacityPriorityOf(prd), Pools: pools, Node: node, NodeChoices: choices, ExcludeNodes: exclude, PreferNode: prefer, Model: model, Role: role, Consumer: consumerSessionSpawn,
	}
	// A pinned worker may only be placed on its node. A pin that cannot be
	// honoured fails the admission here, naming the node, so the task never
	// runs elsewhere.
	pin := m.workerNodePin(prd, t, role)
	if pin != "" {
		pinned, perr := applyNodePin(req, pin, exclude)
		if perr != nil {
			return perr
		}
		req = pinned
	}
	req.Refresh = m.placementRefreshPinned(req, backend, model, nil, pin, exclude)
	// The pools the task asked for are registered per-task (not captured in a
	// local) so a queued-mode LLM override that retargets the holder
	// (requeueQueuedCapacity) can rewrite them before the next wait reason
	// renders. A fall-through then swaps the wait to the alternate pool while
	// the reason still names what was requested.
	m.setWaitReqPools(t.ID, pools)
	defer m.clearWaitReqPools(t.ID)
	m.setFallthrough(t.ID, nil)
	var fell *CapacityAlternate
	var fromPools []string
	// A pinned worker never falls through: the alternate backend would be
	// placed without the pin.
	if role == capacity.RoleWorker && pin == "" {
		if alt, altReq := m.pickFallthrough(led, req, backend, model); alt != nil {
			fell, fromPools = alt, pools
			req, pools = altReq, altReq.Pools
		}
	}
	// A worker's capacity bound is one budget for the whole admission: time
	// already spent queued by an earlier acquireLease of this admission (the
	// node-load failover re-enters here) counts against it, so re-entering
	// does not start a fresh window. It starts counting here, once the request
	// is admissible (the pools resolved and the request about to be queued),
	// and uses the audit recorder's clock so tests can drive it.
	bound := m.capacityWait()
	if role == capacity.RoleWorker {
		bound = m.capacityWaitRemaining(t.ID, bound)
		if bound <= 0 {
			return fmt.Errorf("capacity: %w", capacity.ErrWaitTimeout)
		}
		entered := rec.Now()
		defer func() { m.addCapacityWaitSpent(t.ID, rec.Now().Sub(entered)) }()
	}
	err := led.Acquire(ctx, req, bound, func() bool { return m.taskCancelled(t.ID) }, func(reason string) {
		if !waited {
			waitStart = rec.Now()
		}
		reason = nameRequestedPool(reason, m.waitReqPoolsNow(t.ID))
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
		if fell != nil {
			m.setFallthrough(t.ID, fell)
			note := fmt.Sprintf("fell through from %s/%s to %s/%s: requested pool full, alternate idle", backend, model, fell.Backend, fell.Model)
			rec.AdmissionFallthrough(t.ID, prd.ID, fromPools, pools, note)
			log.Printf("[autonomous] prd=%s task=%s %s", prd.ID, t.ID, note)
			m.recordAdmissionLaneDecision(prd.ID, t.ID, backend, model, fromPools, fell, pools)
		}
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

// admissionLaneDecisionKind is the Decision.Kind written when admission
// places a worker on another pool than the one its effective backend asked for.
const admissionLaneDecisionKind = "capacity_fallthrough"

// recordAdmissionLaneDecision appends the Automaton decision-log entry for a
// worker admitted on an alternate pool: the backend and model it now runs on,
// the pool it sits on, and why (the pool its effective backend asked for was
// full and the alternate had a free slot). Same mechanism as the other
// decisions (store lock, then save); a failed save is logged and never fails
// the admission.
func (m *Manager) recordAdmissionLaneDecision(prdID, taskID, reqBackend, reqModel string, fromPools []string, alt *CapacityAlternate, toPools []string) {
	p, ok := m.store.GetPRD(prdID)
	if !ok {
		return
	}
	note := fmt.Sprintf("task %s: admitted on %s/%s (pool %s) instead of %s/%s (pool %s): requested pool full, alternate pool free",
		taskID, alt.Backend, alt.Model, strings.Join(sortedPools(toPools), ","),
		reqBackend, reqModel, strings.Join(sortedPools(fromPools), ","))
	m.store.WithLock(func() {
		p.Decisions = append(p.Decisions, Decision{
			At:      m.attemptNow(),
			Kind:    admissionLaneDecisionKind,
			Actor:   "autonomous",
			Backend: alt.Backend,
			Model:   alt.Model,
			Note:    note,
		})
	})
	if err := m.store.SavePRD(p); err != nil {
		log.Printf("[autonomous] prd=%s save %s decision: %v", prdID, admissionLaneDecisionKind, err)
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

// Backend-change paths for a task that already holds a lease:
//   - adoptCapacity: reconciles the held lease (Rehome / Adopt refresh) so a
//     backend with no node drops node:<name>.
//   - acquireLease + pickFallthrough: runs before the grant; a holder that is
//     already admitted returns at once, so no held lease is moved.
//   - replaceStaleLease: releases the old lease, then re-acquires.
//   - burn guard RouteBackend and policy_retarget free-slot move: act on
//     waiting requests and new work only; a held lease is neither moved nor
//     reused, and a retarget Take lease is a fresh one on the target.
//
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
	if req.OnPlannerSpawned == nil {
		// Name the planner session on the PRD as soon as it is spawned, so a
		// daemon restart can find it (see RecordPlannerSession).
		prdID := prd.ID
		req.OnPlannerSpawned = func(sessionID, outputFile string) {
			m.RecordPlannerSession(prdID, sessionID, outputFile)
		}
	}
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
		Holder: holder, PRDID: prd.ID, Priority: m.capacityPriorityOf(prd), Pools: pools, Node: node, NodeChoices: choices, Model: model, Role: capacity.RolePlanner, Consumer: consumerSessionSpawn,
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
	// Planning requeue (planning_requeue.go): the PRD's decomposition pair
	// can change while this admission was queued — the queue entry is
	// retargeted onto this pool — so the execution must carry the pair the
	// PRD holds NOW, not the one resolved before the capacity wait.
	return m.decompose(m.planningRequestAtGrant(prd, req))
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
	sessionBackend := m.sessionBackend
	m.mu.Unlock()
	story := findStory(prd, t.StoryID)
	backend, model := admittedWorker(t, story, prd, m.AutomationForPRD(prd), fallback)
	configured := backend
	mismatch := false
	if sessionBackend != nil && t.SessionID != "" {
		if sb, sm := sessionBackend(t.SessionID); sb != "" {
			mismatch = sb != backend
			backend = sb
			if sm != "" {
				model = sm
			}
		}
	}
	pools, node := keysFn(backend, model)
	if mismatch {
		m.noteAdoptMismatch(led, prd, t, configured, backend, pools)
	}
	if len(pools) == 0 {
		return
	}
	choices := nodeChoicesFor(choicesFn, backend, model)
	if node == "" && len(choices) == 0 && !localEngineBackends[backend] && (entryNodes == nil || entryNodes(backend, model) == 0) {
		// The backend takes no local compute node (claude-code, say; never a
		// local-engine backend such as pi-prompt, whose model name may simply
		// not resolve to a hosting entry here). A node
		// recorded while the task ran on another backend is stale: ignore it,
		// drop it from the task so it is not re-used. The node the live session
		// reports is ignored too: a backend that takes no node holds none.
		var stale bool
		m.store.WithLock(func() { stale = t.ComputeNode != "" })
		if stale {
			m.store.MutateTask(t.ID, func(st *Task) { st.ComputeNode = "" })
		}
		base := poolsWithoutNodes(pools)
		if ls := led.LeaseOf(t.ID); ls != nil && ls.Node != "" {
			led.Rehome(t.ID, base, "")
		}
		led.Adopt(capacity.Request{
			Holder: t.ID, PRDID: prd.ID, Priority: m.capacityPriorityOf(prd), Pools: base, Model: model, Consumer: consumerSessionSpawn,
		}, t.SessionID, VerifyHolder(t.ID))
		return
	}
	node = adoptedNode(t, sessionNode, node, entryNodes, backend, model)
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
		Holder: t.ID, PRDID: prd.ID, Priority: m.capacityPriorityOf(prd), Pools: poolsWithoutNodes(pools), Node: node, NodeChoices: choices, Model: model, NodeUnknown: unknown, Consumer: consumerSessionSpawn,
	}, t.SessionID, VerifyHolder(t.ID))
}

// noteAdoptMismatch handles a resumed session that runs a different backend
// than the one its task was configured for: it frees a stale lease whose pools
// no longer match, logs one line, and raises one operator alert per task and
// session pair.
func (m *Manager) noteAdoptMismatch(led *capacity.Ledger, prd *PRD, t *Task, configured, actual string, pools []string) {
	if ls := led.LeaseOf(t.ID); ls != nil && !samePools(nonNodePoolList(ls.Pools), poolsWithoutNodes(pools)) {
		led.Release(t.ID)
	}
	key := t.ID + "|" + t.SessionID
	m.mu.Lock()
	if m.adoptMismatchAlerted == nil {
		m.adoptMismatchAlerted = map[string]bool{}
	}
	seen := m.adoptMismatchAlerted[key]
	m.adoptMismatchAlerted[key] = true
	m.mu.Unlock()
	if seen {
		return
	}
	log.Printf("[autonomous] adopt: task=%s session=%s runs backend=%s but was configured for %s; leased from the session", t.ID, t.SessionID, actual, configured)
	if fn := m.AlertSink(); fn != nil {
		fn("warn", "Resumed session leased from a different backend",
			fmt.Sprintf("Automaton task %s: session %s was leased on backend %s but runs backend %s. The lease now follows the backend the session runs.", t.ID, t.SessionID, configured, actual))
	}
}

func nonNodePoolList(pools []string) []string { return poolsWithoutNodes(pools) }

func samePools(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, p := range a {
		seen[p]++
	}
	for _, p := range b {
		if seen[p] == 0 {
			return false
		}
		seen[p]--
	}
	return true
}

// markCapacityAdoptionComplete ends the ledger's hold on new requests (see
// capacity.Ledger.BeginAdoption). Safe when no ledger is wired or no hold is set.
func (m *Manager) markCapacityAdoptionComplete() {
	m.mu.Lock()
	led := m.capacity
	m.mu.Unlock()
	if led != nil {
		led.MarkAdoptionComplete()
	}
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

// ErrLaunchRefusedDraining marks a spawn that was refused because the daemon
// that received it is draining (shutting down). The worker never started and
// the task did not get an attempt, so it is a wait, never a failure.
var ErrLaunchRefusedDraining = errors.New("launch refused: daemon is draining")

// drainingRefusalText is the part of session.ErrDaemonDraining's message that
// survives when the refusal comes back as an HTTP 500 body from a peer node.
const drainingRefusalText = "daemon is shutting down"

// IsLaunchRefusedDraining reports whether err is a launch refused by a draining
// daemon: the typed session.ErrDaemonDraining, ErrLaunchRefusedDraining, or the
// same message arriving as plain text (an HTTP 500 body).
func IsLaunchRefusedDraining(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, session.ErrDaemonDraining) || errors.Is(err, ErrLaunchRefusedDraining) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), drainingRefusalText)
}

// isSpawnWaitError reports whether a spawn error is a refusal to wait out
// (a full session cap, a workspace-lock collision, the global cooldown or the
// capacity wait timeout) rather than a worker that failed to start. These are
// never infrastructure failures.
func isSpawnWaitError(err error) bool {
	return IsCapacityError(err) || IsWorkspaceLockError(err) || IsLaunchRefusedDraining(err) ||
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
			draining := IsLaunchRefusedDraining(err)
			if !IsCapacityError(err) && !IsWorkspaceLockError(err) && !cooldown && !draining {
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
				if draining {
					return sr, fmt.Errorf("draining: %w: %v", capacity.ErrWaitTimeout, err)
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

// CapacityAlternate is another backend and model a task may be admitted on
// when its requested pool is full and Config.CapacityFallthrough is on.
type CapacityAlternate struct {
	Backend string
	Model   string
}

// CapacityAlternatesFn lists the other allowed backend and model pairs for a
// task requested on backend and model, in preference order.
type CapacityAlternatesFn func(backend, model string) []CapacityAlternate

// SetCapacityAlternates wires the fall-through alternates resolver. Nil (the
// default) leaves fall-through inert even when the setting is on.
func (m *Manager) SetCapacityAlternates(fn CapacityAlternatesFn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capacityAlternates = fn
}

// fallthroughOf returns the alternate the task was admitted on, if any.
func (m *Manager) fallthroughOf(taskID string) (CapacityAlternate, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	alt, ok := m.fallthroughs[taskID]
	return alt, ok
}

func (m *Manager) setFallthrough(taskID string, alt *CapacityAlternate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if alt == nil {
		delete(m.fallthroughs, taskID)
		return
	}
	if m.fallthroughs == nil {
		m.fallthroughs = map[string]CapacityAlternate{}
	}
	m.fallthroughs[taskID] = *alt
}

// pickFallthrough returns the first alternate whose pools the ledger would
// grant now, with its request, when the requested pools cannot be granted.
// Off, no resolver, or room on the requested pools: nil.
func (m *Manager) pickFallthrough(led *capacity.Ledger, base capacity.Request, backend, model string) (*CapacityAlternate, capacity.Request) {
	m.mu.Lock()
	on, altFn, keysFn, choicesFn := m.cfg.CapacityFallthrough, m.capacityAlternates, m.capacityKeys, m.capacityChoices
	m.mu.Unlock()
	if !on || altFn == nil || keysFn == nil || led.WouldGrant(base) {
		return nil, base
	}
	for _, alt := range altFn(backend, model) {
		if alt.Backend == backend && alt.Model == model {
			continue
		}
		pools, node := keysFn(alt.Backend, alt.Model)
		if len(pools) == 0 {
			continue
		}
		req := base
		req.Pools, req.Node, req.Model = pools, node, alt.Model
		req.NodeChoices = nodeChoicesFor(choicesFn, alt.Backend, alt.Model)
		req.Refresh = m.placementRefresh(req, alt.Backend, alt.Model, nil)
		if led.WouldGrant(req) {
			a := alt
			return &a, req
		}
	}
	return nil, base
}
