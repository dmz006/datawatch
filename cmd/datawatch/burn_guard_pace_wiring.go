// burn_guard_pace_wiring.go — the daemon side of the burn guard's pace policy. In mode on the
// runner (internal/server/burn_guard_pace.go) holds new work for a backend through the registry
// here, and the capacity ledger's role gate refuses a new worker while a hold is live. The hold is
// its own registry, not a flag in the back-off registry: the ladder's hold rung and a rate limit
// both write that one slot per backend, and one must never lift the other.
//
// What a hold refuses, and what it never does:
//   - It refuses new worker admission on the held backend's llm pool and nothing else. A task
//     that is already running keeps its session; its verifier and judge (review roles) are not
//     held, so running work finishes.
//   - An operator-started session is never held: the ledger skips the role gate for requests
//     marked Interactive, and a supervisor session is started the same way. Supervisor sessions
//     are accounted by the usage report, not limited by the hold.
//   - A hold has a deadline the runner renews on each tick that keeps it, so a runner that stops
//     ticking lets the hold lapse by itself.
package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// paceHold is one live hold.
type paceHold struct {
	Backend string
	Reason  string
	Until   time.Time
}

// paceHolds is the registry of live pace holds by backend. It is safe for concurrent use and its
// reads only take the registry's own short mutex, so the ledger may call it under its lock.
type paceHolds struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]paceHold
}

func newPaceHolds(now func() time.Time) *paceHolds {
	if now == nil {
		now = time.Now
	}
	return &paceHolds{now: now, m: map[string]paceHold{}}
}

// burnGuardPaceHolds is the daemon's registry, the one the ledger gate and the guard share.
var burnGuardPaceHolds = newPaceHolds(time.Now)

// Set records (or renews) the hold on backend until the deadline.
func (p *paceHolds) Set(backend, reason string, until time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.m[backend] = paceHold{Backend: backend, Reason: reason, Until: until}
}

// Clear removes the hold on backend.
func (p *paceHolds) Clear(backend string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, backend)
}

// Blocked reports whether new work for backend is held now, with the wait reason. A hold past
// its deadline is not live.
func (p *paceHolds) Blocked(backend string) (bool, string) {
	if p == nil {
		return false, ""
	}
	now := p.now()
	p.mu.Lock()
	h, ok := p.m[backend]
	p.mu.Unlock()
	if !ok || !h.Until.After(now) {
		return false, ""
	}
	return true, fmt.Sprintf("backend %s backed off by the burn guard pace hold until %s: %s", backend, h.Until.Format("15:04"), h.Reason)
}

// List returns the live holds ordered by backend.
func (p *paceHolds) List() []paceHold {
	if p == nil {
		return nil
	}
	now := p.now()
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []paceHold
	for _, h := range p.m {
		if h.Until.After(now) {
			out = append(out, h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Backend < out[j].Backend })
	return out
}

// PaceHold is the pace policy's admission seam (server.BurnGuardPaceHold).
func (a burnGuardAdmission) PaceHold(backend string, on bool, reason string, until time.Time) error {
	ph := a.pace
	if ph == nil {
		ph = burnGuardPaceHolds
	}
	if !on {
		ph.Clear(backend)
		return nil
	}
	ph.Set(backend, reason, until)
	return nil
}

// paceHoldBackpressure is the ledger's role gate with the pace hold in front of inner (the
// back-off reserve gate). It touches only "llm:<backend>" pools and only the worker role; every
// other request goes to inner unchanged. The ledger never calls it for an Interactive request.
// It runs under the ledger lock (rule on capacity.Options): the registry read is an in-memory map
// under its own mutex.
func paceHoldBackpressure(ph *paceHolds, inner func(pool string, role capacity.Role, heldByRole map[capacity.Role]int) (bool, string)) func(pool string, role capacity.Role, heldByRole map[capacity.Role]int) (bool, string) {
	return func(pool string, role capacity.Role, heldByRole map[capacity.Role]int) (bool, string) {
		if backend, ok := strings.CutPrefix(pool, "llm:"); ok && ph != nil && role == capacity.RoleWorker {
			if blocked, why := ph.Blocked(backend); blocked {
				return true, "backoff: " + why
			}
		}
		if inner == nil {
			return false, ""
		}
		return inner(pool, role, heldByRole)
	}
}
