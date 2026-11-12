// Package capacity is the admission ledger for autonomous work: named pools
// with limits, atomic multi-pool leases, and a fair (round-robin across PRDs,
// FIFO within a PRD, priority first) wait queue.
package capacity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// defaultAdoptionTimeout is how long BeginAdoption holds new requests back when
// Options.AdoptionTimeout is not set.
const defaultAdoptionTimeout = 3 * time.Minute

// ErrWaitTimeout is returned by Acquire when the wait exceeds its timeout.
var ErrWaitTimeout = errors.New("capacity wait timed out")

// Role classifies the work behind a request for dispatch ordering.
type Role string

const (
	// RoleVerifier is a verifier or guardrail session for a task that already
	// has finished work. It is granted before worker and planner requests.
	RoleVerifier Role = "verifier"
	// RoleWorker is an ordinary task session. It is the zero-value role.
	RoleWorker Role = "worker"
	// RolePlanner is a planning or decomposition session. It waits behind
	// verifiers and workers.
	RolePlanner Role = "planner"
)

// rank orders roles for dispatch; lower is granted first. The zero value and
// any unknown role rank as RoleWorker so existing callers keep their order.
func (r Role) rank() int {
	switch r {
	case RoleVerifier:
		return 0
	case RolePlanner:
		return 2
	default:
		return 1
	}
}

// Request describes one admission request.
type Request struct {
	Holder   string   // lease owner, normally the task id
	PRDID    string   // fairness group
	Priority int      // higher first
	Pools    []string // all must have room (all-or-nothing)
	Node     string   // primary compute node, for model affinity
	// Consumer is the consumer kind the work uses (session_spawn,
	// agent_spawn, council or ask), passed to Options.NodeGate when the
	// ledger checks whether a node may take the request. An empty Consumer
	// means no consumer check.
	Consumer string
	// NodeChoices lists compute nodes that can serve the request. With two or
	// more entries the ledger picks the node at dispatch time: least loaded
	// first (the node pool's used count over its limit, an unlimited pool
	// counting as 0); equally loaded nodes go to PreferNode, then the higher
	// Priority, then list order. It skips
	// ExcludeNodes and nodes Options.NodeHealthy reports unhealthy, and takes
	// the first whose node:<name> pool passes the same checks as every pool in
	// Pools. The lease
	// then carries node:<name> in Pools and sets Lease.Node. With none or one
	// entry the field is ignored and the caller keeps node:<name> in Pools and
	// sets Node as before.
	NodeChoices []NodeChoice
	// ExcludeNodes names nodes the ledger must not pick from NodeChoices.
	ExcludeNodes []string
	// PreferNode is the node a relaunch ran on before. It is only a tie breaker
	// between equally loaded candidates: it never outranks load, never adds a
	// node that is not in NodeChoices, never bypasses ExcludeNodes or
	// Options.NodeHealthy, and is ignored for a request with fewer than two
	// NodeChoices. Refresh does not replace it.
	PreferNode string
	Model      string // model wanted, for model affinity
	Role       Role   // dispatch rank; zero value means RoleWorker
	// NodeUnknown is set by Adopt callers that expected a compute node for the
	// running work and could not determine it. The adopted lease then holds no
	// node and carries Lease.NodeUnknown so the gap stays visible. Ignored by
	// Acquire and when Node is set.
	NodeUnknown bool
	// Interactive marks an operator-started session. Such a request is never
	// held back by Options.AutonomousBackpressure.
	Interactive bool
	// Refresh, when set, returns the current placement for this same holder. Before
	// each dispatch pass the ledger replaces Pools, Node, NodeChoices and
	// ExcludeNodes of a queued request with the returned values and keeps the
	// rest (Holder, PRDID, Priority, Role, Model, Interactive). A waiter holds no
	// slot, so moving it changes no pool count; the grant still goes through
	// resolveLocked, which enforces the pool limits. It runs on Acquire's first
	// pass, on every poll tick and on every Release and remove. Like the Options
	// functions it is called under the ledger lock: a fast in-memory read that
	// must never call back into the ledger. Nil keeps the request as queued.
	Refresh func() Request
}

// NodeChoice is one candidate compute node for a request. Priority breaks a
// tie between nodes that are equally loaded: the higher one is tried first.
type NodeChoice struct {
	Name     string
	Priority int
}

// multiNode reports whether the ledger chooses the node for this request.
func (r Request) multiNode() bool { return len(r.NodeChoices) >= 2 }

// candidates returns the nodes the ledger may pick, excluded and duplicate
// names dropped, in Priority-descending order (ties in list order). That is the
// order of equally loaded nodes; Ledger.rankedLocked puts load ahead of it.
func (r Request) candidates() []NodeChoice {
	skip := make(map[string]bool, len(r.ExcludeNodes))
	for _, n := range r.ExcludeNodes {
		skip[n] = true
	}
	var out []NodeChoice
	for _, c := range r.NodeChoices {
		if c.Name == "" || skip[c.Name] {
			continue
		}
		skip[c.Name] = true
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Priority > out[j].Priority })
	return out
}

// withNodePool returns the request bound to one chosen node: the node pool
// joins Pools, Node is set and the choice fields are cleared. A verifier
// request on a node that has a verifier pool binds node-verify:<name> in
// place of node:<name>; when it did, any node:<name> entry the request still
// carried is dropped so the lease holds exactly one pool for the node.
func (r Request) withNodePool(name, pool string) Request {
	r.Pools = append([]string(nil), r.Pools...)
	if pool != "node:"+name {
		pools := r.Pools[:0]
		for _, p := range r.Pools {
			if p == "node:"+name {
				continue
			}
			pools = append(pools, p)
		}
		r.Pools = pools
	}
	if !sharesPool(r.Pools, []string{pool}) {
		r.Pools = append(r.Pools, pool)
	}
	r.Node = name
	r.NodeChoices, r.ExcludeNodes = nil, nil
	return r
}

// withoutStaleNode treats a recorded Node as advisory. When the request lists
// candidate nodes and Node is not one of them, Node and its node:<name> pool
// are dropped so placement chooses again from the current list. A request
// with fewer than two candidates keeps its Node: callers bind a single-node
// entry through Node and Pools themselves.
func (r Request) withoutStaleNode() Request {
	if !r.multiNode() {
		return r
	}
	if r.Node != "" {
		for _, c := range r.NodeChoices {
			if c.Name == r.Node {
				return r
			}
		}
		stale := "node:" + r.Node
		pools := make([]string, 0, len(r.Pools))
		for _, p := range r.Pools {
			if p != stale {
				pools = append(pools, p)
			}
		}
		r.Pools, r.Node = pools, ""
	}
	return r
}

// allPools is Pools plus the node pool of every candidate: every pool whose
// state can affect whether the request is granted.
func (r Request) allPools() []string {
	if !r.multiNode() {
		return r.Pools
	}
	out := append([]string(nil), r.Pools...)
	for _, c := range r.candidates() {
		out = append(out, "node:"+c.Name)
	}
	return out
}

// candidateNames lists the eligible candidate node names in priority order,
// without load; it is for places that hold no ledger state.
func (r Request) candidateNames() []string {
	var out []string
	for _, c := range r.candidates() {
		out = append(out, c.Name)
	}
	return out
}

// Lease is a held slot set.
type Lease struct {
	Holder    string    `json:"holder"`
	PRDID     string    `json:"prd_id"`
	Pools     []string  `json:"pools"`
	Node      string    `json:"node,omitempty"`
	Model     string    `json:"model,omitempty"`
	Acquired  time.Time `json:"acquired"`
	SessionID string    `json:"session_id,omitempty"`
	// NodeUnknown marks an adopted lease whose compute node was expected but
	// could not be determined. Such a lease holds no node:<name> pool, yet it
	// still counts against every host and llm pool. It clears when a later
	// Adopt supplies the node.
	NodeUnknown bool `json:"node_unknown,omitempty"`
	// NodeOverflow marks an adopted lease whose session runs on Node although that
	// node was already at its limit when the lease was recorded. The session is
	// real, so the lease keeps Node and still counts against every host and llm
	// pool, but it is held outside the node:<name> pool so the pool never reads
	// over its limit. Admission still counts it as an occupant of the node (see
	// usedLocked), so new work sees the node as full. It clears, and the lease
	// joins the node pool, once the pool has room.
	NodeOverflow bool `json:"node_overflow,omitempty"`
}

// Waiter is a queued request as reported by Snapshot.
type Waiter struct {
	Holder string   `json:"holder"`
	PRDID  string   `json:"prd_id"`
	Pools  []string `json:"pools"`
	// Priority is the request's queue priority (higher first); 0 renders
	// exactly as before (omitempty). The owner's priority source (explicit,
	// kind, default) rides on the PRD's own record and queue views; the
	// ledger only knows the number.
	Priority int       `json:"priority,omitempty"`
	Since    time.Time `json:"since"`
	Reason   string    `json:"reason"`
	// Nodes lists the candidate nodes a waiter with NodeChoices may be placed on.
	Nodes []string `json:"nodes,omitempty"`
	// HoldName is the operator-readable form of Holder ("" = unresolved; the
	// renderer then shows the raw id exactly as before).
	HoldName string `json:"hold_name,omitempty"`
	// ReadReason is the same Reason with the holders it names resolved to
	// readable forms ("" = no resolution available).
	ReadReason string `json:"read_reason,omitempty"`
}

// PoolStatus is one pool in a snapshot.
type PoolStatus struct {
	Name     string `json:"name"`
	Limit    int    `json:"limit"` // 0 = unlimited
	External int    `json:"external"`
	Held     int    `json:"held"`
	// Drained and DrainUntil are the node pool's drain state, reported by
	// Options.NodeState; a non-node pool or an undrained node leaves them
	// unset (zero). A drained pool still shows its Held count: the tasks
	// still holding it.
	Drained    bool      `json:"drained,omitempty"`
	DrainUntil time.Time `json:"drain_until,omitzero"`
	// Overflow counts adopted leases running on this node beyond its limit (see
	// Lease.NodeOverflow). They are not in Held, so Held+External never exceeds
	// Limit, but they occupy the node: admission counts Held+External+Overflow.
	Overflow int      `json:"overflow,omitempty"`
	Holders  []string `json:"holders"`
	// HoldNames is the operator-readable form of Holders, in the same order
	// (nil = unresolved; the renderer then shows the raw ids as before).
	HoldNames []string `json:"hold_names,omitempty"`
	// HoldNodes is the node each holder's lease names, parallel to Holders
	// ("" = that lease names no node). Nil when no holder has a node, so a pool
	// without node-bound leases keeps its existing shape.
	HoldNodes []string `json:"hold_nodes,omitempty"`
	// Rate ceiling fields appear only for pools that carry one.
	RateRequests      int       `json:"rate_requests,omitempty"`
	RateTokens        int64     `json:"rate_tokens,omitempty"`
	RateWindowSeconds int64     `json:"rate_window_seconds,omitempty"`
	RequestsInWindow  int       `json:"requests_in_window,omitempty"`
	TokensInWindow    int64     `json:"tokens_in_window,omitempty"`
	RateResetsAt      time.Time `json:"rate_resets_at,omitzero"`
}

// Status is a point-in-time view of the ledger.
type Status struct {
	Pools   []PoolStatus `json:"pools"`
	Leases  []Lease      `json:"leases"`
	Waiting []Waiter     `json:"waiting"`
}

// NodeState is the state of one compute node as the daemon knows it: its
// drain, maintenance and consumer-denial state. The daemon fills it from a
// copied node record and the ledger reads it through Options.NodeState while
// building a snapshot; the value, not a reference, is what travels.
type NodeState struct {
	Drained         bool
	DrainUntil      time.Time
	InMaintenance   bool
	DeniedConsumers []string
}

// Clock supplies the time source the ledger uses for wait deadlines, poll
// ticks and grant ordering. Tests inject a fake; production uses the real clock.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// realClock is the wall clock backed by the time package.
type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Options tune dispatch behaviour.
//
// LOCK RULE: every function in Options (External, Backpressure,
// AutonomousBackpressure, HeldBackpressure, NodeHealthy, NodeGate, NodeState,
// VerifierHeadroom)
// is invoked while the ledger lock is held. Each must therefore be a fast
// in-memory read: it must never call back
// into the ledger (Snapshot, Held, LeaseOf, Limit, Acquire and the like, which
// would deadlock) or into the autonomous manager, never do network, file or
// process I/O, and never block on a lock that a ledger caller could hold (or
// that is held across such I/O). Slow data is cached and refreshed elsewhere.
// The Clock's Now is also read under the lock, so it must be a plain read too.
type Options struct {
	// External reports slots in a pool used by work outside the ledger (for
	// example operator sessions counting against the host pool). Called under
	// the lock from usedLocked and Snapshot: an in-memory count only.
	External func(pool string) int
	// Backpressure reports a soft block for a pool (for example GPU load).
	// Called under the lock: read cached figures, never probe the network or
	// a process.
	Backpressure func(pool string) (blocked bool, reason string)
	// AutonomousBackpressure is a soft block (for example a per-backend
	// back-off or a budget window) that applies to autonomous requests only;
	// requests marked Interactive skip it. Called under the lock: in-memory
	// state only.
	AutonomousBackpressure func(pool string) (blocked bool, reason string)
	// HeldBackpressure is a soft block for autonomous requests that needs the
	// pool's current usage. Both numbers come from the ledger itself: held is
	// the count of leases naming the pool (l.leases) and limit is the value set
	// by SetLimit (l.limits), passed as arguments so the function never reads
	// the ledger back. Called under the lock, so the lock rule applies.
	HeldBackpressure func(pool string, held, limit int) (blocked bool, reason string)
	// NodeHealthy reports whether a compute node may take new work. A candidate
	// of a multi-node request for which it returns false is skipped, as an
	// excluded node is, and the waiting reason names it as unhealthy. Nil means
	// every node is healthy. Called under the lock: an in-memory read only.
	// When NodeGate is set it decides its place in the candidate loop; a
	// single-node request is blocked by NodeGate, not by this hook.
	NodeHealthy func(name string) bool
	// NodeGate reports whether a compute node may take the request's work for
	// the given consumer kind (see Request.Consumer; an empty consumer means
	// no consumer check). For a multi-node request a candidate for which it
	// returns false is skipped, as an unhealthy node is, and its waiting
	// reason is the returned reason verbatim, for example "gpu-box-1: drained"
	// or "gpu-box-1: consumer session_spawn denied". When that refusal is
	// every candidate rather than one full node among others, the reason is
	// prefixed with AllCandidatesGatePrefix. For a single-node request
	// (Node set, fewer than two NodeChoices) it is checked before the pool
	// check, and a refusal blocks it with "waiting for node <name>: <reason>",
	// for example "waiting for node gpu-box-1: drained". A drained node keeps
	// its running leases: the gate only refuses new work, it never releases a
	// holder, so holders still count in Held. Nil falls back to NodeHealthy
	// (reason "unhealthy"); both nil means every node is open. Called under
	// the lock: an in-memory read only.
	NodeGate func(node, consumer string) (ok bool, reason string)
	// NodeState reports a compute node's drain, maintenance and
	// consumer-denial state for the node:<name> pool in a snapshot. Called
	// under the lock from Snapshot, once per node pool, so the LOCK RULE
	// applies: an in-memory read only, against state copied elsewhere. Nil
	// means the snapshot reports no node state; non-node pools are never
	// queried either way.
	NodeState func(name string) NodeState
	// VerifierHeadroom reports whether a node has room for new verifier work
	// (for example whether its sessions can hold the verifier's context).
	// Consulted under the lock for Request.Role == RoleVerifier only: a
	// candidate node it blocks is skipped the way NodeHealthy skips an
	// unhealthy node, and the waiting reason carries the returned reason in
	// plain English. Nil means no node is ever headroom-blocked. Called under
	// the lock: an in-memory read only.
	VerifierHeadroom func(node string) (blocked bool, reason string)
	// StrictNodeLimit, CountPoolByNode and AdoptBeforeAdmit gate the three
	// node over-admission rules. Each is read at call time, under the ledger
	// lock, so it must be a fast in-memory read (see the LOCK RULE). Nil means
	// the rule is on, which is the operator-answer default.
	//
	// StrictNodeLimit: Adopt never lets a node pool read over its limit. A
	// session adopted onto a full node is recorded outside the pool (see
	// Lease.NodeOverflow) yet still counts as an occupant for new admissions.
	// Off, Adopt adds the node pool whatever its use.
	StrictNodeLimit func() bool
	// CountPoolByNode: an llm:<name> pool counts the sessions on each node at
	// most up to that node's own limit, so sessions piled on a busy node (a
	// restart can leave them there) do not use up slots a free node could take.
	// The pool's operator-set limit is unchanged. Off, every lease counts once.
	CountPoolByNode func() bool
	// AdoptBeforeAdmit: after BeginAdoption, requests wait until
	// MarkAdoptionComplete is called or AdoptionTimeout passes, so work resumed
	// after a daemon restart is adopted before any new request is placed.
	AdoptBeforeAdmit func() bool
	// AdoptionTimeout bounds the wait of AdoptBeforeAdmit. Default 3 minutes.
	AdoptionTimeout time.Duration
	// AgeOverride: waiters older than this jump ahead of model affinity.
	AgeOverride time.Duration
	// PlannerAgeAfter: a planner that has waited this long ranks as a worker,
	// so priority applies between it and workers instead of workers always
	// winning. Zero means 10 minutes; negative turns this off.
	PlannerAgeAfter time.Duration
	// PollInterval is how often waiters re-check cancellation and
	// backpressure. Default 5s.
	PollInterval time.Duration
	// Clock is the time source. Nil means the real clock.
	Clock Clock
}

type waiter struct {
	req    Request
	seq    uint64
	since  time.Time
	grant  chan struct{}
	reason string
}

// Ledger is safe for concurrent use.
type Ledger struct {
	mu        sync.Mutex
	limits    map[string]int
	leases    map[string]*Lease // holder → lease
	waiters   []*waiter
	seq       uint64
	lastGrant map[string]time.Time // PRD → last grant, for round-robin
	lastModel map[string]string    // node → last granted model
	rates     map[string]RateCeiling
	rateUse   map[string][]rateEntry
	opt       Options
	changed   chan struct{} // closed and replaced on every capacity change; see Changed

	adopting   bool      // set by BeginAdoption; requests wait while it is set
	adoptSince time.Time // when BeginAdoption was called

	wedgeReported atomic.Bool // set while a lock wedge has been reported; see SnapshotWatched

	intervals      []LaneInterval // closed lease and wait intervals; see LaneIntervals
	recordingSince time.Time      // when the ledger began keeping intervals
}

// New creates a ledger.
func New(opt Options) *Ledger {
	if opt.AgeOverride <= 0 {
		opt.AgeOverride = 10 * time.Minute
	}
	if opt.PlannerAgeAfter == 0 {
		opt.PlannerAgeAfter = 10 * time.Minute
	}
	if opt.PollInterval <= 0 {
		opt.PollInterval = 5 * time.Second
	}
	if opt.AdoptionTimeout <= 0 {
		opt.AdoptionTimeout = defaultAdoptionTimeout
	}
	if opt.Clock == nil {
		opt.Clock = realClock{}
	}
	return &Ledger{
		recordingSince: opt.Clock.Now(),
		limits:         map[string]int{},
		leases:         map[string]*Lease{},
		lastGrant:      map[string]time.Time{},
		lastModel:      map[string]string{},
		rates:          map[string]RateCeiling{},
		rateUse:        map[string][]rateEntry{},
		opt:            opt,
		changed:        make(chan struct{}),
	}
}

// Changed returns a channel that is closed the next time capacity changes: a
// Release of a holder that held a lease (also a lease reaped or given back by
// a waiter granted as it was removed), SetLimit, SetRate and Replan. A waiter
// removed without freeing a lease is not a change. It is a broadcast: every
// caller holding the channel wakes together, and the ledger then installs a
// fresh open channel. A caller that takes the channel after a change simply
// gets that fresh one, so it must re-evaluate once right after taking it and
// then select on it; a change between the evaluation and the select is not
// lost, because the channel was taken before the evaluation.
func (l *Ledger) Changed() <-chan struct{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.changed
}

// signalChangedLocked wakes everyone waiting on Changed. Closing a channel is
// non-blocking, so it is safe under the lock; it is deliberately not an
// Options callback (see the LOCK RULE).
func (l *Ledger) signalChangedLocked() {
	close(l.changed)
	l.changed = make(chan struct{})
}

// WouldGrant reports whether req would be granted right now: a dry run of the
// check a real dispatch makes, through resolveLocked, so NodeChoices, per-node
// pools, rate ceilings and every Options backpressure hook count exactly as
// they would for Acquire. It takes no lease, queues no waiter, and changes no
// grant order, model affinity or rate usage. A Refresh on req runs first and
// a stale Node is dropped, as dispatchLocked does.
//
// It ignores queue order on purpose: other waiters ahead of req may still be
// granted first, so true means there is room now, not a reservation.
func (l *Ledger) WouldGrant(req Request) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if req.Refresh != nil {
		cur := req.Refresh()
		req.Pools, req.Node = cur.Pools, cur.Node
		req.NodeChoices, req.ExcludeNodes = cur.NodeChoices, cur.ExcludeNodes
	}
	req = req.withoutStaleNode()
	// resolveLocked reaches rateWindowLocked, which only drops entries already
	// outside the window (Snapshot does the same); it records no use.
	_, why := l.resolveLocked(req)
	return why == ""
}

// SetLimit sets a pool limit (0 or less = unlimited) and re-dispatches.
func (l *Ledger) SetLimit(pool string, limit int) {
	l.mu.Lock()
	if limit <= 0 {
		delete(l.limits, pool)
	} else {
		l.limits[pool] = limit
	}
	l.signalChangedLocked()
	l.dispatchLocked()
	l.mu.Unlock()
}

// Limit returns a pool's limit (0 = unlimited).
func (l *Ledger) Limit(pool string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.limits[pool]
}

func (l *Ledger) heldLocked(pool string) int {
	n := 0
	for _, ls := range l.leases {
		for _, p := range ls.Pools {
			if p == pool {
				n++
				break
			}
		}
	}
	return n
}

// optOn reads an Options switch: nil means on.
func optOn(f func() bool) bool { return f == nil || f() }

// overflowLocked returns the adopted leases that run on node beyond its limit:
// they name node but are held outside its pool (Lease.NodeOverflow).
func (l *Ledger) overflowLocked(node string) []string {
	var hs []string
	for h, ls := range l.leases {
		if ls.NodeOverflow && ls.Node == node {
			hs = append(hs, h)
		}
	}
	sort.Strings(hs)
	return hs
}

// heldByNodeLocked counts the leases naming pool the way an llm pool's limit
// does: the sessions on one node count at most up to that node's limit, so a
// busy node cannot use up slots a free node could take. A lease on a node with
// no limit, or on no node, counts once.
func (l *Ledger) heldByNodeLocked(pool string) int {
	perNode := map[string]int{}
	n := 0
	for _, ls := range l.leases {
		if !sharesPool(ls.Pools, []string{pool}) {
			continue
		}
		if ls.Node == "" || l.limits["node:"+ls.Node] <= 0 {
			n++
			continue
		}
		perNode[ls.Node]++
	}
	for node, c := range perNode {
		n += min(c, l.limits["node:"+node])
	}
	return n
}

func (l *Ledger) usedLocked(pool string) int {
	var used int
	switch {
	case strings.HasPrefix(pool, "node:"):
		used = l.heldLocked(pool) + len(l.overflowLocked(strings.TrimPrefix(pool, "node:")))
	case strings.HasPrefix(pool, "llm:") && optOn(l.opt.CountPoolByNode):
		used = l.heldByNodeLocked(pool)
	default:
		used = l.heldLocked(pool)
	}
	if l.opt.External != nil {
		used += l.opt.External(pool)
	}
	return used
}

// promoteOverflowLocked moves an adopted lease that was held outside a full node
// pool into the pool once the pool has room, so the lease reads as held. It runs
// before every dispatch.
func (l *Ledger) promoteOverflowLocked() {
	var over []*Lease
	for _, ls := range l.leases {
		if ls.NodeOverflow {
			over = append(over, ls)
		}
	}
	if len(over) == 0 {
		return
	}
	sort.Slice(over, func(i, j int) bool { return over[i].Acquired.Before(over[j].Acquired) })
	for _, ls := range over {
		pool := "node:" + ls.Node
		used := l.heldLocked(pool)
		if l.opt.External != nil {
			used += l.opt.External(pool)
		}
		if lim := l.limits[pool]; lim > 0 && used >= lim {
			continue // the pool is still full without the overflow leases
		}
		ls.Pools = append(ls.Pools, pool)
		ls.NodeOverflow = false
	}
}

// BeginAdoption holds every request back until MarkAdoptionComplete is called
// or Options.AdoptionTimeout passes. The daemon calls it as the ledger starts,
// before the work that survived a restart has been adopted, so a new request
// cannot take the slot a resumed session already occupies. Adopt itself is never
// held back. It does nothing when Options.AdoptBeforeAdmit is off.
func (l *Ledger) BeginAdoption() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !optOn(l.opt.AdoptBeforeAdmit) {
		return
	}
	l.adopting, l.adoptSince = true, l.opt.Clock.Now()
}

// MarkAdoptionComplete ends the hold of BeginAdoption and re-dispatches the
// requests it held back. Safe to call when no hold is set.
func (l *Ledger) MarkAdoptionComplete() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.adopting {
		return
	}
	l.adopting = false
	l.signalChangedLocked()
	l.dispatchLocked()
}

// adoptionPendingLocked returns the reason requests are held back for adoption,
// or "" when none is. A hold that outlived its timeout, or whose option was
// turned off, is cleared here.
func (l *Ledger) adoptionPendingLocked() string {
	if !l.adopting {
		return ""
	}
	if !optOn(l.opt.AdoptBeforeAdmit) {
		l.adopting = false
		return ""
	}
	if waited := l.opt.Clock.Now().Sub(l.adoptSince); waited >= l.opt.AdoptionTimeout {
		l.adopting = false
		return ""
	}
	return "waiting for work resumed after the restart to be adopted"
}

// blockReasonLocked returns "" when every pool has room and is not
// backpressured, otherwise a human-readable reason naming the pool.
func (l *Ledger) blockReasonLocked(req Request) string {
	now := l.opt.Clock.Now()
	for _, p := range req.Pools {
		if lim := l.limits[p]; lim > 0 && l.usedLocked(p) >= lim {
			if name, ok := strings.CutPrefix(p, "node:"); ok {
				return fmt.Sprintf("waiting for node %s: %d/%d engines in use, held by %s", name, l.usedLocked(p), lim, l.holdersLocked(p))
			}
			if name, ok := strings.CutPrefix(p, "node-verify:"); ok {
				return fmt.Sprintf("waiting for node %s: %d/%d verifier slots in use, held by %s", name, l.usedLocked(p), lim, l.holdersLocked(p))
			}
			return fmt.Sprintf("pool %s full (%d/%d, held by %s)", p, l.usedLocked(p), lim, l.holdersLocked(p))
		}
		if why := l.rateBlockLocked(p, now); why != "" {
			return why
		}
		if l.opt.Backpressure != nil {
			if blocked, why := l.opt.Backpressure(p); blocked {
				return fmt.Sprintf("pool %s backpressure: %s", p, why)
			}
		}
		if l.opt.AutonomousBackpressure != nil && !req.Interactive {
			if blocked, why := l.opt.AutonomousBackpressure(p); blocked {
				return fmt.Sprintf("pool %s backpressure: %s", p, why)
			}
		}
		if l.opt.HeldBackpressure != nil && !req.Interactive {
			if blocked, why := l.opt.HeldBackpressure(p, l.heldLocked(p), l.limits[p]); blocked {
				return fmt.Sprintf("pool %s backpressure: %s", p, why)
			}
		}
	}
	return ""
}

// rankedLocked returns the nodes req may be placed on, in try order: least
// loaded first, ties to req.PreferNode, then the higher Priority, then list
// order. A node's load is
// its pool's used count (leases plus Options.External, the figure the block
// check uses) over its limit; a pool with no limit counts as 0.
func (l *Ledger) rankedLocked(req Request) []NodeChoice {
	out := req.candidates()
	used := make(map[string]int, len(out))
	limit := make(map[string]int, len(out))
	for _, c := range out {
		pool := "node:" + c.Name
		if lim := l.limits[pool]; lim > 0 {
			used[c.Name], limit[c.Name] = l.usedLocked(pool), lim
		} else {
			used[c.Name], limit[c.Name] = 0, 1
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Name, out[j].Name
		// used[a]/limit[a] < used[b]/limit[b], without division.
		if x, y := used[a]*limit[b], used[b]*limit[a]; x != y {
			return x < y
		}
		if req.PreferNode != "" && a != b {
			if pa, pb := a == req.PreferNode, b == req.PreferNode; pa != pb {
				return pa
			}
		}
		return out[i].Priority > out[j].Priority
	})
	return out
}

// AllCandidatesGatePrefix introduces the wait reason of a multi-node
// request refused by the node gate on every candidate (drained, in
// maintenance, or its consumer denied) rather than merely full: the same
// per-candidate "node: reason" parts, named up front as gate refusals.
const AllCandidatesGatePrefix = "all candidate nodes drained or denied: "

// ledgerNodePoolLocked is the pool a request takes on node: for verifier
// requests the node's verifier pool when it has a limit above 0, otherwise
// the executor pool node:<node> exactly as before. A node with no verifier
// pool (limit 0 or unset) leaves verifiers on the node pool.
func (l *Ledger) ledgerNodePoolLocked(role Role, node string) string {
	if role == RoleVerifier && l.limits[VerifierPool(node)] > 0 {
		return VerifierPool(node)
	}
	return "node:" + node
}

// swapVerifierNodePoolLocked rewrites req's node:<node> pool entry to
// node-verify:<node> when the request is a verifier and the node has a
// verifier pool. It is the single-node analogue of ledgerNodePoolLocked:
// it never adds a pool the caller did not ask for, and it leaves non-
// verifiers and nodes without a verifier pool alone.
func (l *Ledger) swapVerifierNodePoolLocked(req *Request, node string) {
	if req.Role != RoleVerifier || node == "" {
		return
	}
	np := "node:" + node
	vp := VerifierPool(node)
	if l.limits[vp] <= 0 || !sharesPool(req.Pools, []string{np}) {
		return
	}
	pools := make([]string, 0, len(req.Pools))
	swapped := false
	for _, p := range req.Pools {
		if p == np {
			if !swapped {
				pools = append(pools, vp)
				swapped = true
			}
			continue
		}
		pools = append(pools, p)
	}
	req.Pools = pools
}

// resolveLocked decides where req would run now. For an ordinary request it is
// blockReasonLocked on req itself. For a request with NodeChoices it returns
// the request bound to the first healthy candidate node, least loaded first,
// that passes every check together with Pools, and an empty reason; when none
// fits it returns req unchanged and a reason naming every candidate and what
// blocks it, prefixed with AllCandidatesGatePrefix when the gate — not a full
// pool — blocked every candidate. The order and the block check read the same
// usage figures under one lock hold, so the node chosen is one the check
// passed.
// nodeGateLocked reports whether node may take new work for the request's
// consumer now: NodeGate when set (its reason verbatim), else the legacy
// NodeHealthy hook ("unhealthy" when it refuses), else every node is open.
func (l *Ledger) nodeGateLocked(node, consumer string) (bool, string) {
	if l.opt.NodeGate != nil {
		ok, reason := l.opt.NodeGate(node, consumer)
		return ok, reason
	}
	if l.opt.NodeHealthy != nil && !l.opt.NodeHealthy(node) {
		return false, "unhealthy"
	}
	return true, ""
}

func (l *Ledger) resolveLocked(req Request) (Request, string) {
	if why := l.adoptionPendingLocked(); why != "" {
		return req, why
	}
	if !req.multiNode() {
		if req.Node != "" && l.opt.NodeGate != nil {
			if ok, why := l.opt.NodeGate(req.Node, req.Consumer); !ok {
				return req, fmt.Sprintf("waiting for node %s: %s", req.Node, why)
			}
		}
		l.swapVerifierNodePoolLocked(&req, req.Node)
		return req, l.blockReasonLocked(req)
	}
	ranked := l.rankedLocked(req)
	if len(ranked) == 0 {
		var all []string
		for _, c := range req.NodeChoices {
			all = append(all, c.Name)
		}
		return req, fmt.Sprintf("no eligible node among %s: all excluded", strings.Join(all, ", "))
	}
	names := make([]string, 0, len(ranked))
	parts := make([]string, 0, len(ranked))
	gateBlocked := 0
	for _, c := range ranked {
		name := c.Name
		names = append(names, name)
		if ok, why := l.nodeGateLocked(name, req.Consumer); !ok {
			gateBlocked++
			parts = append(parts, name+": "+why)
			continue
		}
		if req.Role == RoleVerifier && l.opt.VerifierHeadroom != nil {
			if blocked, why := l.opt.VerifierHeadroom(name); blocked {
				parts = append(parts, fmt.Sprintf("%s: verifier headroom: %s", name, why))
				continue
			}
		}
		bound := req.withNodePool(name, l.ledgerNodePoolLocked(req.Role, name))
		why := l.blockReasonLocked(bound)
		if why == "" {
			return bound, ""
		}
		why = strings.TrimPrefix(why, "waiting for node "+name+": ")
		parts = append(parts, name+": "+why)
	}
	inner := strings.Join(parts, "; ")
	if gateBlocked == len(ranked) && gateBlocked >= 2 {
		inner = AllCandidatesGatePrefix + inner
	}
	return req, fmt.Sprintf("waiting for nodes %s: %s", strings.Join(names, ", "), inner)
}

func (l *Ledger) holdersLocked(pool string) string {
	var hs []string
	for h, ls := range l.leases {
		for _, p := range ls.Pools {
			if p == pool {
				hs = append(hs, h)
				break
			}
		}
	}
	if node, ok := strings.CutPrefix(pool, "node:"); ok {
		hs = append(hs, l.overflowLocked(node)...)
	}
	sort.Strings(hs)
	if len(hs) == 0 {
		return "other sessions"
	}
	return strings.Join(hs, ",")
}

// holderNodesLocked returns the node of each holder's lease, parallel to
// holders ("" when that lease names no node). Nil when no holder has a node.
func (l *Ledger) holderNodesLocked(holders []string) []string {
	var nodes []string
	for i, h := range holders {
		ls := l.leases[h]
		if ls == nil || ls.Node == "" {
			continue
		}
		if nodes == nil {
			nodes = make([]string, len(holders))
		}
		nodes[i] = ls.Node
	}
	return nodes
}

// shownHoldNodes returns HoldNodes when the pool's holders span two or more
// nodes, else nil. A one-node entry (every holder on the same node, or none)
// renders exactly as it did before the node was carried.
func (p PoolStatus) shownHoldNodes() []string {
	if len(p.HoldNodes) != len(p.Holders) {
		return nil
	}
	seen := map[string]bool{}
	for _, n := range p.HoldNodes {
		if n != "" {
			seen[n] = true
		}
	}
	if len(seen) < 2 {
		return nil
	}
	return p.HoldNodes
}

// orderLocked sorts waiters into grant order: role (verifier, worker,
// planner; an aged planner ranks as a worker), priority, aged waiters, model affinity, least-recently-served
// PRD, then FIFO.
func (l *Ledger) orderLocked(now time.Time) []*waiter {
	return l.orderListLocked(l.waiters, now)
}

// effectiveRank is the role rank used for ordering. A planner that has waited
// at least Options.PlannerAgeAfter ranks as a worker so it cannot starve behind
// a steady stream of workers; verifiers keep rank 0.
func (l *Ledger) effectiveRank(w *waiter, now time.Time) int {
	r := w.req.Role.rank()
	if w.req.Role == RolePlanner && l.opt.PlannerAgeAfter > 0 && now.Sub(w.since) >= l.opt.PlannerAgeAfter {
		return RoleWorker.rank()
	}
	return r
}

// orderListLocked is orderLocked over an arbitrary waiter list; it sorts a
// copy and leaves the argument alone.
func (l *Ledger) orderListLocked(list []*waiter, now time.Time) []*waiter {
	ws := append([]*waiter(nil), list...)
	aged := func(w *waiter) bool { return now.Sub(w.since) >= l.opt.AgeOverride }
	affine := func(w *waiter) bool {
		if w.req.Model == "" {
			return false
		}
		if w.req.multiNode() {
			for _, n := range w.req.candidateNames() {
				if l.lastModel[n] == w.req.Model {
					return true
				}
			}
			return false
		}
		return w.req.Node != "" && l.lastModel[w.req.Node] == w.req.Model
	}
	sort.SliceStable(ws, func(i, j int) bool {
		a, b := ws[i], ws[j]
		if ra, rb := l.effectiveRank(a, now), l.effectiveRank(b, now); ra != rb {
			return ra < rb
		}
		if a.req.Priority != b.req.Priority {
			return a.req.Priority > b.req.Priority
		}
		if aged(a) != aged(b) {
			return aged(a)
		}
		if affine(a) != affine(b) {
			return affine(a)
		}
		ga, gb := l.lastGrant[a.req.PRDID], l.lastGrant[b.req.PRDID]
		if !ga.Equal(gb) {
			return ga.Before(gb)
		}
		return a.seq < b.seq
	})
	return ws
}

// dispatchLocked grants every waiter that fits, in fair order. A waiter with
// Request.Refresh first takes the placement it returns, so the node it queues
// on and the reason it shows follow the current compute_nodes list.
func (l *Ledger) dispatchLocked() {
	l.promoteOverflowLocked()
	if len(l.waiters) == 0 {
		return
	}
	now := l.opt.Clock.Now()
	for _, w := range l.waiters {
		l.refreshWaiterLocked(w)
	}
	for _, w := range l.orderLocked(now) {
		bound, why := l.resolveLocked(w.req)
		w.reason = why
		if why != "" {
			continue
		}
		l.grantLocked(w, bound, now)
	}
}

// refreshWaiterLocked applies the waiter's Request.Refresh and drops a stale
// Node, the first half of a dispatch pass.
func (l *Ledger) refreshWaiterLocked(w *waiter) {
	if w.req.Refresh != nil {
		cur := w.req.Refresh()
		w.req.Pools, w.req.Node = cur.Pools, cur.Node
		w.req.NodeChoices, w.req.ExcludeNodes = cur.NodeChoices, cur.ExcludeNodes
	}
	w.req = w.req.withoutStaleNode()
}

// Replan re-runs every waiter's Request.Refresh and dispatches at once. The
// poll tick and every Release do this already; Replan is for a change the
// ledger cannot see, such as an LLM entry's compute_nodes list being edited,
// so waiters move to the new list now instead of on their next tick. A waiter
// with no Refresh keeps the placement it queued with.
func (l *Ledger) Replan() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.signalChangedLocked()
	l.dispatchLocked()
}

// grantLocked takes w's lease. req is w.req as resolved by resolveLocked, so
// for a request with NodeChoices it already names the chosen node.
func (l *Ledger) grantLocked(w *waiter, req Request, now time.Time) {
	l.recordWaitEndLocked(w, now)
	l.leases[req.Holder] = &Lease{
		Holder: req.Holder, PRDID: req.PRDID, Pools: append([]string(nil), req.Pools...),
		Node: req.Node, Model: req.Model, Acquired: now,
	}
	l.lastGrant[req.PRDID] = now
	l.recordRequestsLocked(req.Pools, now)
	if req.Node != "" && req.Model != "" {
		l.lastModel[req.Node] = req.Model
	}
	for i, x := range l.waiters {
		if x == w {
			l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
			break
		}
	}
	close(w.grant)
}

// Acquire blocks until the request is granted, ctx is done, cancelled()
// returns true, or timeout (>0) elapses. onWait is called whenever the wait
// reason changes (including the first time the request has to wait).
// cancelled and onWait are always invoked WITHOUT the ledger lock held, so
// they may call back into the ledger; the Options functions are not so
// lucky (see Options).
func (l *Ledger) Acquire(ctx context.Context, req Request, timeout time.Duration, cancelled func() bool, onWait func(reason string)) error {
	l.mu.Lock()
	if _, held := l.leases[req.Holder]; held {
		l.mu.Unlock()
		return nil
	}
	l.seq++
	w := &waiter{req: req, seq: l.seq, since: l.opt.Clock.Now(), grant: make(chan struct{})}
	l.waiters = append(l.waiters, w)
	l.dispatchLocked()
	select {
	case <-w.grant:
		l.mu.Unlock()
		return nil
	default:
	}
	reason := w.reason
	l.mu.Unlock()

	// The wait bound counts from the moment the request was queued (w.since,
	// the first time it could be admitted to its pool), so neither the onWait
	// callback's own time nor a later change of reason text moves it.
	var deadline <-chan time.Time
	if timeout > 0 {
		remaining := timeout - l.opt.Clock.Now().Sub(w.since)
		if remaining < 0 {
			remaining = 0
		}
		deadline = l.opt.Clock.After(remaining)
	}
	reported := reason
	if onWait != nil {
		onWait(reason)
	}
	l.mu.Lock()
	tick := l.opt.Clock.After(l.pollWaitLocked(w))
	l.mu.Unlock()
	for {
		select {
		case <-w.grant:
			return nil
		case <-ctx.Done():
			l.remove(w)
			return ctx.Err()
		case <-deadline:
			l.mu.Lock()
			why := w.reason
			l.mu.Unlock()
			l.remove(w)
			return fmt.Errorf("%w: %s", ErrWaitTimeout, why)
		case <-tick:
			if cancelled != nil && cancelled() {
				l.remove(w)
				return context.Canceled
			}
			l.mu.Lock()
			l.dispatchLocked()
			cur := w.reason
			tick = l.opt.Clock.After(l.pollWaitLocked(w))
			l.mu.Unlock()
			select {
			case <-w.grant:
				return nil
			default:
			}
			// Compare with the last reason REPORTED, not the one the ledger
			// held before this pass: Snapshot may have refreshed it between
			// ticks.
			if cur != reported {
				reported = cur
				if onWait != nil {
					onWait(cur)
				}
			}
		}
	}
}

// pollWaitLocked is the delay to the next re-dispatch: the poll interval, or
// less when a rate ceiling blocking the waiter resets sooner.
func (l *Ledger) pollWaitLocked(w *waiter) time.Duration {
	d := l.opt.PollInterval
	probe := w
	if w.req.multiNode() {
		probe = &waiter{req: Request{Pools: w.req.allPools()}}
	}
	if rd, ok := l.rateWakeLocked(probe, l.opt.Clock.Now()); ok && rd < d {
		d = rd
	}
	return d
}

func (l *Ledger) remove(w *waiter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, x := range l.waiters {
		if x == w {
			l.recordWaitEndLocked(w, l.opt.Clock.Now())
			l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
			break
		}
	}
	select {
	case <-w.grant: // granted in the same instant: give it back
		delete(l.leases, w.req.Holder)
		l.signalChangedLocked()
	default:
	}
	l.dispatchLocked()
}

// Retarget is the shared requeue helper for an in-flight wait: one holder's
// single queued request is cancelled and immediately re-queued in place
// against the placement req describes (its pools, node and node choices),
// so the holder ends up waiting with exactly one live request, on the new
// pool. It is the building block for re-routing a queued task or planning
// run to another worker: the caller builds the new Request (a Refresh that
// re-reads the current pools is honored exactly as on dispatch) and passes
// it in.
//
// Semantics, all under the ledger lock so no poll tick or Release can run
// in between:
//
//   - A holder with a live lease is left entirely alone (its grant stands),
//     and Retarget reports false: there is no request to move, and no
//     double-booking happens either.
//   - A queued holder is retargeted in place: the same waiter keeps its
//     queue position (seq, since, open grant channel), so no priority is
//     lost and no second request for the holder can exist. Re-targeting a
//     waiter that already sits on the same placement is a no-op and reports
//     false — retargeting twice therefore can never produce two live
//     requests.
//   - When the new placement has room, dispatchLocked grants the waiter in
//     the same call, so the grant happens immediately on a free pool.
//   - Returns true when the queued request was actually re-targeted,
//     false when there was no queued request for the holder (not queued, or
//     already granted) or when it already carried that placement.
//
// The caller is still responsible for the lease-release half when the
// holder was granted later: a Retargeted wait that is granted owns its
// lease under the same holder key, so one Release(holder) frees it.
func (l *Ledger) Retarget(holder string, req Request) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, held := l.leases[holder]; held {
		return false
	}
	var w *waiter
	for _, x := range l.waiters {
		if x.req.Holder == holder {
			w = x
			break
		}
	}
	if w == nil {
		return false
	}
	if req.Refresh != nil {
		cur := req.Refresh()
		req.Pools, req.Node = cur.Pools, cur.Node
		req.NodeChoices, req.ExcludeNodes = cur.NodeChoices, cur.ExcludeNodes
	}
	req = req.withoutStaleNode()
	if samePlacement(req, w.req) {
		return false
	}
	w.req = req
	l.signalChangedLocked()
	l.dispatchLocked()
	return true
}

// samePlacement reports whether a carries the placement b was queued with:
// the pool set (all-or-nothing, so order-insensitive), the node, the node
// choices and the excluded nodes. It is the comparison Retarget uses to tell
// an already-correct queue entry from a move.
func samePlacement(a, b Request) bool {
	aPools := append([]string(nil), a.Pools...)
	bPools := append([]string(nil), b.Pools...)
	sort.Strings(aPools)
	sort.Strings(bPools)
	if len(aPools) != len(bPools) {
		return false
	}
	for i := range aPools {
		if aPools[i] != bPools[i] {
			return false
		}
	}
	if a.Node != b.Node {
		return false
	}
	if !slicesEqualStrings(a.ExcludeNodes, b.ExcludeNodes) {
		return false
	}
	if len(a.NodeChoices) != len(b.NodeChoices) {
		return false
	}
	for i := range a.NodeChoices {
		if a.NodeChoices[i] != b.NodeChoices[i] {
			return false
		}
	}
	return true
}

func slicesEqualStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Adopt records a lease for work that is already running and must not wait:
// a task whose session survived a daemon restart (BL462). Unlike Acquire it
// never queues, because the session already occupies its slots; the host and
// llm pools take it whatever their use. A node already at its limit does not
// take it into the node pool (see Lease.NodeOverflow): the lease still names
// that node and counts as one of its occupants for new admissions, so Adopt is
// never the reason a node pool reads over its limit. A session already
// recorded under another holder key is not recorded twice. Idempotent: an existing lease for the holder is kept and only
// its session binding is refreshed, and a lease holding no node gains req.Node.
// Adopt never picks a node: a request with NodeChoices is recorded on req.Node
// (its node:<name> pool added) or, when Node is empty, on no node. With
// req.NodeUnknown set that nodeless lease is also flagged NodeUnknown; gaining
// a node clears the flag.
//
// successors names the holders the work's lease may since have been re-keyed
// to by Handoff. When one of them holds a lease the work already occupies its
// slot under that name, so Adopt adds nothing: a second lease under
// req.Holder would count one session twice. The check and the insert share
// one lock hold, so a hand-off cannot slip between them.
func (l *Ledger) Adopt(req Request, sessionID string, successors ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ls, ok := l.leases[req.Holder]; ok {
		l.refreshAdoptedLocked(ls, req, sessionID)
		return
	}
	for _, s := range successors {
		if _, ok := l.leases[s]; ok {
			return
		}
	}
	// One session is one slot. A session already recorded under another holder
	// key (a task key and its relaunch key, or a lease handed to a verifier)
	// keeps that lease; counting it again would put one session in every pool
	// twice.
	if sessionID != "" {
		for _, ls := range l.leases {
			if ls.SessionID == sessionID {
				l.refreshAdoptedLocked(ls, req, sessionID)
				return
			}
		}
	}
	if req.multiNode() || req.Node != "" {
		if req.Node == "" {
			req.NodeChoices, req.ExcludeNodes = nil, nil
		}
	}
	now := time.Now()
	ls := &Lease{
		Holder: req.Holder, PRDID: req.PRDID, Pools: append([]string(nil), req.Pools...),
		Model: req.Model, Acquired: now, SessionID: sessionID,
		NodeUnknown: req.NodeUnknown && req.Node == "",
	}
	if req.Node != "" {
		// The session occupies this node whether or not it is still among the
		// current choices, so the lease is recorded on it regardless.
		l.placeAdoptedLocked(ls, req.Node)
	}
	l.leases[req.Holder] = ls
	l.lastGrant[req.PRDID] = now
	l.recordRequestsLocked(ls.Pools, l.opt.Clock.Now())
	if req.Node != "" && req.Model != "" {
		l.lastModel[req.Node] = req.Model
	}
}

// refreshAdoptedLocked applies a repeated Adopt to the lease already recorded:
// the session binding is refreshed, and a lease holding no node gains req.Node.
func (l *Ledger) refreshAdoptedLocked(ls *Lease, req Request, sessionID string) {
	if sessionID != "" {
		ls.SessionID = sessionID
	}
	if req.Node == "" && len(req.NodeChoices) == 0 && !req.NodeUnknown &&
		!samePoolSet(nonNodePools(ls.Pools), nonNodePools(req.Pools)) {
		l.rehomeLocked(ls, req.Pools, "")
		return
	}
	if ls.Node == "" && req.Node != "" {
		ls.NodeUnknown = false
		l.placeAdoptedLocked(ls, req.Node)
	} else if ls.Node == "" && req.NodeUnknown {
		ls.NodeUnknown = true
	}
}

// nonNodePools returns pools without the node:<name> entries.
func nonNodePools(pools []string) []string {
	var out []string
	for _, p := range pools {
		if !strings.HasPrefix(p, "node:") {
			out = append(out, p)
		}
	}
	return out
}

// samePoolSet reports whether a and b hold the same names, ignoring order.
func samePoolSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
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

// Rehome moves the lease held by holder onto pools. node:<node> is added only
// when node is non-empty; with an empty node the lease gives up its node and
// clears NodeOverflow and NodeUnknown. Acquired, SessionID and PRDID are kept
// and the lease count does not change. Waiters queued on a freed pool are
// granted in the same call. It returns false, changing nothing, when holder
// holds no lease.
func (l *Ledger) Rehome(holder string, pools []string, node string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	ls, ok := l.leases[holder]
	if !ok {
		return false
	}
	l.rehomeLocked(ls, pools, node)
	return true
}

func (l *Ledger) rehomeLocked(ls *Lease, pools []string, node string) {
	ls.Pools = poolsWithNode(pools, node)
	ls.Node = node
	if node == "" {
		ls.NodeOverflow = false
		ls.NodeUnknown = false
	}
	l.signalChangedLocked()
	l.dispatchLocked()
}

// poolsWithNode copies pools and appends node:<node> when node is set and absent.
func poolsWithNode(pools []string, node string) []string {
	out := append([]string(nil), pools...)
	if node != "" && !sharesPool(out, []string{"node:" + node}) {
		out = append(out, "node:"+node)
	}
	return out
}

// placeAdoptedLocked records that the adopted lease ls runs on node. It joins
// the node's pool when the pool has room, or when StrictNodeLimit is off. A
// node already at its limit keeps the lease out of the pool and flags it
// NodeOverflow (see Lease.NodeOverflow): the session is on the node, but Adopt
// is not the reason the pool reads over its limit.
func (l *Ledger) placeAdoptedLocked(ls *Lease, node string) {
	ls.Node = node
	pool := "node:" + node
	if sharesPool(ls.Pools, []string{pool}) {
		return
	}
	if lim := l.limits[pool]; lim > 0 && optOn(l.opt.StrictNodeLimit) && l.usedLocked(pool) >= lim {
		ls.NodeOverflow = true
		return
	}
	ls.Pools = append(ls.Pools, pool)
}

// Bind records the session backing a lease.
func (l *Ledger) Bind(holder, sessionID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ls, ok := l.leases[holder]; ok {
		ls.SessionID = sessionID
	}
}

// Release frees a holder's lease and dispatches waiters. Safe if not held.
func (l *Ledger) Release(holder string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ls, ok := l.leases[holder]; ok {
		l.recordLeaseEndLocked(ls, l.opt.Clock.Now())
		delete(l.leases, holder)
		l.signalChangedLocked()
		l.dispatchLocked()
	}
}

// Held reports whether holder has a lease.
func (l *Ledger) Held(holder string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.leases[holder]
	return ok
}

// LeaseOf reports a copy of the lease holder currently holds, or nil if it
// holds none. The copy lets callers inspect the lease (pools, node, session)
// without racing the ledger's internals.
func (l *Ledger) LeaseOf(holder string) *Lease {
	l.mu.Lock()
	ls := l.leases[holder]
	l.mu.Unlock()
	if ls == nil {
		return nil
	}
	cp := *ls
	return &cp
}

// Handoff re-keys from's held lease to to atomically: the slot from holds
// becomes to's lease with no release-and-re-wait in between, so pool counts
// do not change and no waiter ever sees the slot briefly free. The lease's
// other fields (PRD, pools, node, session binding) travel with it. It
// reports false, changing nothing, when from holds no lease, to already
// holds one, or either name is empty (or the names are equal).
func (l *Ledger) Handoff(from, to string) bool {
	if from == "" || to == "" || from == to {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	ls := l.leases[from]
	if ls == nil || l.leases[to] != nil {
		return false
	}
	delete(l.leases, from)
	l.leases[to] = ls
	ls.Holder = to
	return true
}

// Reap releases leases the callback reports as not live and returns the
// released holders. Call periodically so a missed Release cannot leak
// capacity. The callback runs WITHOUT the ledger lock held, so it may call back
// into the ledger or the session manager; a lease that changed while the
// callback ran is left alone.
func (l *Ledger) Reap(live func(Lease) bool) []string {
	l.mu.Lock()
	cand := make([]Lease, 0, len(l.leases))
	for _, ls := range l.leases {
		cand = append(cand, *ls)
	}
	l.mu.Unlock()

	var dead []Lease
	for _, ls := range cand {
		if !live(ls) {
			dead = append(dead, ls)
		}
	}
	if len(dead) == 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, d := range dead {
		if cur, ok := l.leases[d.Holder]; ok && cur.Acquired.Equal(d.Acquired) {
			l.recordLeaseEndLocked(cur, l.opt.Clock.Now())
			delete(l.leases, d.Holder)
			out = append(out, d.Holder)
		}
	}
	if len(out) > 0 {
		l.signalChangedLocked()
		l.dispatchLocked()
	}
	sort.Strings(out)
	return out
}

// Leases returns a copy of every lease currently held, one per holder, in
// holder-name order (stable for tests and logs). It is the flat lease list
// for readers that do not need the pool, limit or waiting view (Snapshot
// provides those); each Lease keeps the Holder, PRD and SessionID the
// acquirer named.
func (l *Ledger) Leases() []Lease {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := make([]string, 0, len(l.leases))
	for h := range l.leases {
		names = append(names, h)
	}
	sort.Strings(names)
	out := make([]Lease, 0, len(names))
	for _, h := range names {
		out = append(out, *l.leases[h])
	}
	return out
}

// Snapshot returns the current state.
func (l *Ledger) Snapshot() Status {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := map[string]bool{}
	for p := range l.limits {
		names[p] = true
	}
	for p := range l.rates {
		names[p] = true
	}
	var st Status
	for _, ls := range l.leases {
		st.Leases = append(st.Leases, *ls)
		for _, p := range ls.Pools {
			names[p] = true
		}
	}
	for _, w := range l.waiters {
		// Recompute the reason as a dispatch pass would (refresh, then
		// resolve) without granting, reordering or recording rate use, so a
		// read between passes never reports a stale reason.
		l.refreshWaiterLocked(w)
		_, w.reason = l.resolveLocked(w.req)
		st.Waiting = append(st.Waiting, Waiter{Holder: w.req.Holder, PRDID: w.req.PRDID, Pools: w.req.Pools, Priority: w.req.Priority, Since: w.since, Reason: w.reason, Nodes: w.req.candidateNames()})
		for _, p := range w.req.allPools() {
			names[p] = true
		}
		// A verifier waiter may be admitted on each candidate's verifier pool,
		// so report it like any other pool when the node configured one.
		if w.req.Role == RoleVerifier {
			vs := append([]string{}, w.req.candidateNames()...)
			if w.req.Node != "" {
				vs = append(vs, w.req.Node)
			}
			for _, n := range vs {
				if n == "" {
					continue
				}
				if l.limits[VerifierPool(n)] > 0 {
					names[VerifierPool(n)] = true
				}
			}
		}
	}
	for p := range names {
		ps := PoolStatus{Name: p, Limit: l.limits[p], Held: l.heldLocked(p)}
		if node, ok := strings.CutPrefix(p, "node:"); ok {
			ps.Overflow = len(l.overflowLocked(node))
			if l.opt.NodeState != nil {
				if ns := l.opt.NodeState(node); ns.Drained {
					ps.Drained = true
					if !ns.DrainUntil.IsZero() {
						ps.DrainUntil = ns.DrainUntil
					}
				}
			}
		}
		if l.opt.External != nil {
			ps.External = l.opt.External(p)
		}
		if c, ok := l.rates[p]; ok {
			now := l.opt.Clock.Now()
			_, reqs, toks := l.rateWindowLocked(p, now)
			ps.RateRequests, ps.RateTokens = c.Requests, c.Tokens
			ps.RateWindowSeconds = int64(c.Window / time.Second)
			ps.RequestsInWindow, ps.TokensInWindow = reqs, toks
			if at, ok := l.rateResetLocked(p, now); ok {
				ps.RateResetsAt = at
			}
		}
		ps.Holders = strings.Split(strings.TrimPrefix(l.holdersLocked(p), "other sessions"), ",")
		if ps.Holders[0] == "" {
			ps.Holders = nil
		}
		ps.HoldNodes = l.holderNodesLocked(ps.Holders)
		st.Pools = append(st.Pools, ps)
	}
	sort.Slice(st.Pools, func(i, j int) bool { return st.Pools[i].Name < st.Pools[j].Name })
	sort.Slice(st.Leases, func(i, j int) bool { return st.Leases[i].Holder < st.Leases[j].Holder })
	sort.Slice(st.Waiting, func(i, j int) bool { return st.Waiting[i].Since.Before(st.Waiting[j].Since) })
	return st
}

// FormatPools renders one line per pool: used/limit with holders (readable
// forms when the pool carries them, otherwise the raw ids), or a
// "none configured" line when there are no pools. A drained node pool shows
// "drained until <time>" (or just "drained" when the drain is open-ended)
// beside the figures, and "N task(s) still finishing" when it is drained and
// still holds leases.
func FormatPools(pools []PoolStatus) string {
	var b strings.Builder
	if len(pools) == 0 {
		b.WriteString("Pools: none configured\n")
	}
	for _, p := range pools {
		lim := "unlimited"
		if p.Limit > 0 {
			lim = fmt.Sprintf("%d", p.Limit)
		}
		fmt.Fprintf(&b, "%s: %d/%s", p.Name, p.Held+p.External, lim)
		if p.Drained {
			if p.DrainUntil.IsZero() {
				b.WriteString(" drained")
			} else {
				fmt.Fprintf(&b, " drained until %s", p.DrainUntil.UTC().Format(time.RFC3339))
			}
		}
		if p.Drained && p.Held > 0 {
			plural := "s"
			if p.Held == 1 {
				plural = ""
			}
			fmt.Fprintf(&b, " (%d task%s still finishing)", p.Held, plural)
		}
		if p.External > 0 {
			fmt.Fprintf(&b, " (%d external)", p.External)
		}
		if p.Overflow > 0 {
			fmt.Fprintf(&b, " (+%d adopted over the limit)", p.Overflow)
		}
		if len(p.Holders) > 0 {
			holders := p.Holders
			if len(p.HoldNames) == len(p.Holders) {
				holders = p.HoldNames
			}
			nodes := p.shownHoldNodes()
			parts := make([]string, len(holders))
			for i, h := range holders {
				parts[i] = h
				if nodes != nil && nodes[i] != "" {
					parts[i] += " on " + nodes[i]
				}
			}
			fmt.Fprintf(&b, " held by %s", strings.Join(parts, ","))
		}
		if p.RateRequests > 0 || p.RateTokens > 0 {
			b.WriteString(" rate")
			if p.RateRequests > 0 {
				fmt.Fprintf(&b, " req %d/%d", p.RequestsInWindow, p.RateRequests)
			}
			if p.RateTokens > 0 {
				fmt.Fprintf(&b, " tok %d/%d", p.TokensInWindow, p.RateTokens)
			}
			fmt.Fprintf(&b, " per %s", (time.Duration(p.RateWindowSeconds) * time.Second).String())
		}
		b.WriteString("\n")
	}
	return b.String()
}

// FormatText renders a compact human-readable summary of a Status: one line
// per pool (used/limit with holders) followed by the wait queue. A waiter
// shows its readable holder form and readable reason when the snapshot
// carries them, otherwise the raw holder id and reason.
func FormatText(st Status) string {
	var b strings.Builder
	b.WriteString(FormatPools(st.Pools))
	unknown := 0
	for _, ls := range st.Leases {
		if ls.NodeUnknown {
			unknown++
		}
	}
	if unknown > 0 {
		fmt.Fprintf(&b, "Leases with unknown node: %d (counted in host and llm pools, held on no node pool)\n", unknown)
	}
	if len(st.Waiting) == 0 {
		b.WriteString("Waiting: none\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Waiting (%d):\n", len(st.Waiting))
	for _, w := range st.Waiting {
		holder, reason := w.Holder, w.Reason
		if w.HoldName != "" {
			holder = w.HoldName
		}
		if w.ReadReason != "" {
			reason = w.ReadReason
		}
		fmt.Fprintf(&b, "  %s (PRD %s) %s: %s\n", holder, w.PRDID, time.Since(w.Since).Round(time.Second), reason)
	}
	return b.String()
}
