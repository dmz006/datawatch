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
	// NodeChoices lists compute nodes that can serve the request. With two or
	// more entries the ledger picks the node at dispatch time: least loaded
	// first (the node pool's used count over its limit, an unlimited pool
	// counting as 0), ties to the higher Priority, then list order. It skips
	// ExcludeNodes and nodes Options.NodeHealthy reports unhealthy, and takes
	// the first whose node:<name> pool passes the same checks as every pool in
	// Pools. The lease
	// then carries node:<name> in Pools and sets Lease.Node. With none or one
	// entry the field is ignored and the caller keeps node:<name> in Pools and
	// sets Node as before.
	NodeChoices []NodeChoice
	// ExcludeNodes names nodes the ledger must not pick from NodeChoices.
	ExcludeNodes []string
	Model        string // model wanted, for model affinity
	Role         Role   // dispatch rank; zero value means RoleWorker
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

// withNode returns the request bound to one chosen node: node:<name> joins
// Pools, Node is set and the choice fields are cleared.
func (r Request) withNode(name string) Request {
	r.Pools = append([]string(nil), r.Pools...)
	if pool := "node:" + name; !sharesPool(r.Pools, []string{pool}) {
		r.Pools = append(r.Pools, pool)
	}
	r.Node = name
	r.NodeChoices, r.ExcludeNodes = nil, nil
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
}

// Waiter is a queued request as reported by Snapshot.
type Waiter struct {
	Holder string    `json:"holder"`
	PRDID  string    `json:"prd_id"`
	Pools  []string  `json:"pools"`
	Since  time.Time `json:"since"`
	Reason string    `json:"reason"`
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
	Name     string   `json:"name"`
	Limit    int      `json:"limit"` // 0 = unlimited
	External int      `json:"external"`
	Held     int      `json:"held"`
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
// AutonomousBackpressure, HeldBackpressure, NodeHealthy) is invoked while the ledger lock is
// held. Each must therefore be a fast in-memory read: it must never call back
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
	NodeHealthy func(name string) bool
	// AgeOverride: waiters older than this jump ahead of model affinity.
	AgeOverride time.Duration
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

	wedgeReported atomic.Bool // set while a lock wedge has been reported; see SnapshotWatched
}

// New creates a ledger.
func New(opt Options) *Ledger {
	if opt.AgeOverride <= 0 {
		opt.AgeOverride = 10 * time.Minute
	}
	if opt.PollInterval <= 0 {
		opt.PollInterval = 5 * time.Second
	}
	if opt.Clock == nil {
		opt.Clock = realClock{}
	}
	return &Ledger{
		limits:    map[string]int{},
		leases:    map[string]*Lease{},
		lastGrant: map[string]time.Time{},
		lastModel: map[string]string{},
		rates:     map[string]RateCeiling{},
		rateUse:   map[string][]rateEntry{},
		opt:       opt,
	}
}

// SetLimit sets a pool limit (0 or less = unlimited) and re-dispatches.
func (l *Ledger) SetLimit(pool string, limit int) {
	l.mu.Lock()
	if limit <= 0 {
		delete(l.limits, pool)
	} else {
		l.limits[pool] = limit
	}
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

func (l *Ledger) usedLocked(pool string) int {
	used := l.heldLocked(pool)
	if l.opt.External != nil {
		used += l.opt.External(pool)
	}
	return used
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
// loaded first, ties to the higher Priority, then list order. A node's load is
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
		return out[i].Priority > out[j].Priority
	})
	return out
}

// resolveLocked decides where req would run now. For an ordinary request it is
// blockReasonLocked on req itself. For a request with NodeChoices it returns
// the request bound to the first healthy candidate node, least loaded first,
// that passes every check together with Pools, and an empty reason; when none
// fits it returns req unchanged and a reason naming every candidate and what
// blocks it. The order and the block check read the same usage figures under
// one lock hold, so the node chosen is one the check passed.
func (l *Ledger) resolveLocked(req Request) (Request, string) {
	if !req.multiNode() {
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
	for _, c := range ranked {
		name := c.Name
		names = append(names, name)
		if l.opt.NodeHealthy != nil && !l.opt.NodeHealthy(name) {
			parts = append(parts, name+": unhealthy")
			continue
		}
		bound := req.withNode(name)
		why := l.blockReasonLocked(bound)
		if why == "" {
			return bound, ""
		}
		why = strings.TrimPrefix(why, "waiting for node "+name+": ")
		parts = append(parts, name+": "+why)
	}
	return req, fmt.Sprintf("waiting for nodes %s: %s", strings.Join(names, ", "), strings.Join(parts, "; "))
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
// planner), priority, aged waiters, model affinity, least-recently-served
// PRD, then FIFO.
func (l *Ledger) orderLocked(now time.Time) []*waiter {
	return l.orderListLocked(l.waiters, now)
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
		if ra, rb := a.req.Role.rank(), b.req.Role.rank(); ra != rb {
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
	if len(l.waiters) == 0 {
		return
	}
	now := l.opt.Clock.Now()
	for _, w := range l.waiters {
		if w.req.Refresh != nil {
			cur := w.req.Refresh()
			w.req.Pools, w.req.Node = cur.Pools, cur.Node
			w.req.NodeChoices, w.req.ExcludeNodes = cur.NodeChoices, cur.ExcludeNodes
		}
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

// grantLocked takes w's lease. req is w.req as resolved by resolveLocked, so
// for a request with NodeChoices it already names the chosen node.
func (l *Ledger) grantLocked(w *waiter, req Request, now time.Time) {
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

	if onWait != nil {
		onWait(reason)
	}
	var deadline <-chan time.Time
	if timeout > 0 {
		deadline = l.opt.Clock.After(timeout)
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
			prev := w.reason
			l.dispatchLocked()
			cur := w.reason
			tick = l.opt.Clock.After(l.pollWaitLocked(w))
			l.mu.Unlock()
			select {
			case <-w.grant:
				return nil
			default:
			}
			if cur != prev && onWait != nil {
				onWait(cur)
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
			l.waiters = append(l.waiters[:i], l.waiters[i+1:]...)
			break
		}
	}
	select {
	case <-w.grant: // granted in the same instant: give it back
		delete(l.leases, w.req.Holder)
	default:
	}
	l.dispatchLocked()
}

// Adopt records a lease for work that is already running and must not wait:
// a task whose session survived a daemon restart (BL462). Unlike Acquire it
// never queues and ignores pool limits, because the session already occupies
// its slots. Idempotent: an existing lease for the holder is kept and only
// its session binding is refreshed, and a lease holding no node gains req.Node. Adopt never picks a node: a request with
// NodeChoices is recorded on req.Node (its node:<name> pool added) or, when
// Node is empty, on no node.
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
		if sessionID != "" {
			ls.SessionID = sessionID
		}
		if ls.Node == "" && req.Node != "" {
			ls.Node = req.Node
			if pool := "node:" + req.Node; !sharesPool(ls.Pools, []string{pool}) {
				ls.Pools = append(ls.Pools, pool)
			}
		}
		return
	}
	for _, s := range successors {
		if _, ok := l.leases[s]; ok {
			return
		}
	}
	if req.multiNode() || req.Node != "" {
		if req.Node != "" {
			// The session occupies this node whether or not it is still among
			// the current choices, so the lease is recorded on it regardless.
			req = req.withNode(req.Node)
		} else {
			req.NodeChoices, req.ExcludeNodes = nil, nil
		}
	}
	now := time.Now()
	l.leases[req.Holder] = &Lease{
		Holder: req.Holder, PRDID: req.PRDID, Pools: append([]string(nil), req.Pools...),
		Node: req.Node, Model: req.Model, Acquired: now, SessionID: sessionID,
	}
	l.lastGrant[req.PRDID] = now
	l.recordRequestsLocked(req.Pools, l.opt.Clock.Now())
	if req.Node != "" && req.Model != "" {
		l.lastModel[req.Node] = req.Model
	}
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
	if _, ok := l.leases[holder]; ok {
		delete(l.leases, holder)
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
			delete(l.leases, d.Holder)
			out = append(out, d.Holder)
		}
	}
	if len(out) > 0 {
		l.dispatchLocked()
	}
	sort.Strings(out)
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
		st.Waiting = append(st.Waiting, Waiter{Holder: w.req.Holder, PRDID: w.req.PRDID, Pools: w.req.Pools, Since: w.since, Reason: w.reason, Nodes: w.req.candidateNames()})
		for _, p := range w.req.allPools() {
			names[p] = true
		}
	}
	for p := range names {
		ps := PoolStatus{Name: p, Limit: l.limits[p], Held: l.heldLocked(p)}
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
// "none configured" line when there are no pools.
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
		if p.External > 0 {
			fmt.Fprintf(&b, " (%d external)", p.External)
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
