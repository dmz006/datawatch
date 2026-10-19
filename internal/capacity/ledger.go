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
	Model    string   // model wanted, for model affinity
	Role     Role     // dispatch rank; zero value means RoleWorker
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
}

// PoolStatus is one pool in a snapshot.
type PoolStatus struct {
	Name     string   `json:"name"`
	Limit    int      `json:"limit"` // 0 = unlimited
	External int      `json:"external"`
	Held     int      `json:"held"`
	Holders  []string `json:"holders"`
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
type Options struct {
	// External reports slots in a pool used by work outside the ledger (for
	// example operator sessions counting against the host pool).
	External func(pool string) int
	// Backpressure reports a soft block for a pool (for example GPU load).
	Backpressure func(pool string) (blocked bool, reason string)
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
	opt       Options
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
func (l *Ledger) blockReasonLocked(pools []string) string {
	for _, p := range pools {
		if lim := l.limits[p]; lim > 0 && l.usedLocked(p) >= lim {
			return fmt.Sprintf("pool %s full (%d/%d, held by %s)", p, l.usedLocked(p), lim, l.holdersLocked(p))
		}
		if l.opt.Backpressure != nil {
			if blocked, why := l.opt.Backpressure(p); blocked {
				return fmt.Sprintf("pool %s backpressure: %s", p, why)
			}
		}
	}
	return ""
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

// orderLocked sorts waiters into grant order: role (verifier, worker,
// planner), priority, aged waiters, model affinity, least-recently-served
// PRD, then FIFO.
func (l *Ledger) orderLocked(now time.Time) []*waiter {
	ws := append([]*waiter(nil), l.waiters...)
	aged := func(w *waiter) bool { return now.Sub(w.since) >= l.opt.AgeOverride }
	affine := func(w *waiter) bool {
		return w.req.Node != "" && w.req.Model != "" && l.lastModel[w.req.Node] == w.req.Model
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

// dispatchLocked grants every waiter that fits, in fair order.
func (l *Ledger) dispatchLocked() {
	if len(l.waiters) == 0 {
		return
	}
	now := l.opt.Clock.Now()
	for _, w := range l.orderLocked(now) {
		why := l.blockReasonLocked(w.req.Pools)
		w.reason = why
		if why != "" {
			continue
		}
		l.grantLocked(w, now)
	}
}

func (l *Ledger) grantLocked(w *waiter, now time.Time) {
	l.leases[w.req.Holder] = &Lease{
		Holder: w.req.Holder, PRDID: w.req.PRDID, Pools: append([]string(nil), w.req.Pools...),
		Node: w.req.Node, Model: w.req.Model, Acquired: now,
	}
	l.lastGrant[w.req.PRDID] = now
	if w.req.Node != "" && w.req.Model != "" {
		l.lastModel[w.req.Node] = w.req.Model
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
	tick := l.opt.Clock.After(l.opt.PollInterval)
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
			tick = l.opt.Clock.After(l.opt.PollInterval)
			if cancelled != nil && cancelled() {
				l.remove(w)
				return context.Canceled
			}
			l.mu.Lock()
			prev := w.reason
			l.dispatchLocked()
			cur := w.reason
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
// its session binding is refreshed.
func (l *Ledger) Adopt(req Request, sessionID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if ls, ok := l.leases[req.Holder]; ok {
		if sessionID != "" {
			ls.SessionID = sessionID
		}
		return
	}
	now := time.Now()
	l.leases[req.Holder] = &Lease{
		Holder: req.Holder, PRDID: req.PRDID, Pools: append([]string(nil), req.Pools...),
		Node: req.Node, Model: req.Model, Acquired: now, SessionID: sessionID,
	}
	l.lastGrant[req.PRDID] = now
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
	var st Status
	for _, ls := range l.leases {
		st.Leases = append(st.Leases, *ls)
		for _, p := range ls.Pools {
			names[p] = true
		}
	}
	for _, w := range l.waiters {
		st.Waiting = append(st.Waiting, Waiter{Holder: w.req.Holder, PRDID: w.req.PRDID, Pools: w.req.Pools, Since: w.since, Reason: w.reason})
		for _, p := range w.req.Pools {
			names[p] = true
		}
	}
	for p := range names {
		ps := PoolStatus{Name: p, Limit: l.limits[p], Held: l.heldLocked(p)}
		if l.opt.External != nil {
			ps.External = l.opt.External(p)
		}
		ps.Holders = strings.Split(strings.TrimPrefix(l.holdersLocked(p), "other sessions"), ",")
		if ps.Holders[0] == "" {
			ps.Holders = nil
		}
		st.Pools = append(st.Pools, ps)
	}
	sort.Slice(st.Pools, func(i, j int) bool { return st.Pools[i].Name < st.Pools[j].Name })
	sort.Slice(st.Leases, func(i, j int) bool { return st.Leases[i].Holder < st.Leases[j].Holder })
	sort.Slice(st.Waiting, func(i, j int) bool { return st.Waiting[i].Since.Before(st.Waiting[j].Since) })
	return st
}

// FormatText renders a compact human-readable summary of a Status: one line
// per pool (used/limit with holders) followed by the wait queue.
func FormatText(st Status) string {
	var b strings.Builder
	if len(st.Pools) == 0 {
		b.WriteString("Pools: none configured\n")
	}
	for _, p := range st.Pools {
		lim := "unlimited"
		if p.Limit > 0 {
			lim = fmt.Sprintf("%d", p.Limit)
		}
		fmt.Fprintf(&b, "%s: %d/%s", p.Name, p.Held+p.External, lim)
		if p.External > 0 {
			fmt.Fprintf(&b, " (%d external)", p.External)
		}
		if len(p.Holders) > 0 {
			fmt.Fprintf(&b, " held by %s", strings.Join(p.Holders, ","))
		}
		b.WriteString("\n")
	}
	if len(st.Waiting) == 0 {
		b.WriteString("Waiting: none\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Waiting (%d):\n", len(st.Waiting))
	for _, w := range st.Waiting {
		fmt.Fprintf(&b, "  %s (PRD %s) %s: %s\n", w.Holder, w.PRDID, time.Since(w.Since).Round(time.Second), w.Reason)
	}
	return b.String()
}
