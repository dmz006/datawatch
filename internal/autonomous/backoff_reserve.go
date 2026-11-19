// The review reserve gate: while a backend is held in usage back-off, a
// bounded number of live slots stay free for the work that reviews that
// backend's output — guardrail judges and verifiers (planning role rank 0)
// and decompose/planning sessions (rank 2) — bounded further by a rolling
// one-hour token budget. Worker sessions never get a reserve slot and are
// refused exactly as before.
//
// The gate keeps only in-memory, thread-safe state (per-backend rolling
// token stamps and a bounded audit-event queue), so it may decide on the
// capacity ledger's lock path where hooks must be plain reads.
package autonomous

import (
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// reserveEventCap is the maximum number of queued reserve verdicts not yet
// drained by the daemon's audit sink: the queue is back-bounded.
const reserveEventCap = 4096

// reserveTokenWindow is the rolling window of the reserve's token budget.
const reserveTokenWindow = time.Hour

// ReserveRole reports whether a capacity role may take a reserve slot on a
// backed-off backend: the review-carrying roles — the verifier (guards'
// judges run as the verifier) and the planner (decompose / planning). The
// worker, and the empty role (the zero value), never take a reserve slot.
func ReserveRole(r capacity.Role) bool {
	return r == capacity.RoleVerifier || r == capacity.RolePlanner
}

// ReserveEvent is one verdict of the review reserve, queued for the daemon
// to emit as an audit entry: the decision, the backend, the requesting
// role, and the slot and token figures at decision time.
type ReserveEvent struct {
	Admitted   bool
	Backend    string
	Role       string
	Slots      int
	UsedSlots  int
	TokensUsed int64
	TokenCap   int64
	At         time.Time
}

// BackoffReserve is the in-memory side of the review reserve: a per-backend
// rolling one-hour token budget and a bounded queue of verdict events. Live
// slot usage is counted by the capacity ledger itself (leases held by review
// roles); the gate only bounds the token flow through the reserve.
type BackoffReserve struct {
	mu      sync.Mutex
	now     func() time.Time
	slotsFn func() int
	capFn   func() int64
	tokens  map[string][]tokenStamp
	events  []ReserveEvent
}

type tokenStamp struct {
	at time.Time
	n  int64
}

// NewBackoffReserve builds the gate. now, slotsFn and capFn must be plain
// reads of the effective config (they are called under the gate's mutex), and
// now is injectable so the rolling window is testable. Any nil function reads
// as zero (no slots, a nil clock falls back to the wall clock).
func NewBackoffReserve(now func() time.Time, slotsFn func() int, capFn func() int64) *BackoffReserve {
	if now == nil {
		now = time.Now
	}
	return &BackoffReserve{now: now, slotsFn: slotsFn, capFn: capFn, tokens: map[string][]tokenStamp{}}
}

// Admit decides one verdict for the named backend: true when the requesting
// role is allowed a reserve slot (ReserveRole), a slot is free (heldReview
// is the ledger's count of the backend's live leases held by review roles,
// and the configured slot count is above it), and the rolling one-hour token
// budget is not exhausted (a zero cap means no cap). A non-reserve role is
// refused either way. Every verdict queues an audit event.
func (g *BackoffReserve) Admit(backend string, role capacity.Role, heldReview int) (admitted bool, reason string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	slots := 0
	if g.slotsFn != nil {
		slots = g.slotsFn()
	}
	tokenCap := int64(0)
	if g.capFn != nil {
		tokenCap = g.capFn()
	}
	now := g.now()
	used := g.rolledUsedLocked(backend, now)
	admitted = ReserveRole(role) &&
		slots > 0 &&
		heldReview < slots &&
		(tokenCap == 0 || used < tokenCap)
	g.pushEvent(ReserveEvent{
		Admitted:   admitted,
		Backend:    backend,
		Role:       string(role),
		Slots:      slots,
		UsedSlots:  heldReview,
		TokensUsed: used,
		TokenCap:   tokenCap,
		At:         now,
	})
	if !admitted {
		reason = BackoffReserveWaitReason
	}
	return admitted, reason
}

// RecordTokens folds a session's token usage into the owning backend's
// rolling reserve budget. Non-positive amounts are dropped.
func (g *BackoffReserve) RecordTokens(backend string, tokens int64) {
	if backend == "" || tokens <= 0 {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tokens[backend] = append(g.tokens[backend], tokenStamp{at: g.now(), n: tokens})
}

// UsedTokens totals the tokens the named backend has taken through the
// reserve in the rolling one-hour window.
func (g *BackoffReserve) UsedTokens(backend string) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.rolledUsedLocked(backend, g.now())
}

// DrainEvents returns and clears the queue of reserve verdicts (oldest
// first) for the daemon's audit sink.
func (g *BackoffReserve) DrainEvents() []ReserveEvent {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := g.events
	g.events = nil
	return out
}

// pushEvent appends a verdict, dropping the oldest when the queue exceeds
// its cap. Caller holds g.mu.
func (g *BackoffReserve) pushEvent(ev ReserveEvent) {
	g.events = append(g.events, ev)
	if len(g.events) > reserveEventCap {
		g.events = g.events[len(g.events)-reserveEventCap:]
	}
}

// rolledUsedLocked drops stamps that have aged out of the rolling window and
// totals the rest. Caller holds g.mu.
func (g *BackoffReserve) rolledUsedLocked(backend string, now time.Time) int64 {
	list := g.tokens[backend]
	cutoff := now.Add(-reserveTokenWindow)
	i := 0
	for i < len(list) && list[i].at.Before(cutoff) {
		i++
	}
	if i > 0 {
		list = append([]tokenStamp(nil), list[i:]...)
		g.tokens[backend] = list
	}
	var sum int64
	for _, s := range list {
		sum += s.n
	}
	return sum
}
