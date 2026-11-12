package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/session"
)

// spawnGrace is how long a lease with no bound session is presumed to be
// mid-spawn before the reaper treats it as leaked.
const spawnGrace = 10 * time.Minute

// verifierRegistry tracks verifier leases whose admit hook is still held
// (the verifier is running) and the verifier session each one spawned. A
// verifier lease is keyed "verify:<taskID>" but its ledger SessionID is either
// the finished worker session (after a hand-off) or empty, so the session
// alone cannot say whether the verifier is alive.
type verifierRegistry struct {
	mu       sync.Mutex
	active   map[string]bool   // holder -> admitted and not yet released
	sessions map[string]string // holder -> verifier session id
}

func newVerifierRegistry() *verifierRegistry {
	return &verifierRegistry{active: map[string]bool{}, sessions: map[string]string{}}
}

// begin marks holder's verifier as running and returns the function that
// clears it; call it from the lease release.
func (r *verifierRegistry) begin(holder string) func() {
	r.mu.Lock()
	r.active[holder] = true
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.active, holder)
		delete(r.sessions, holder)
		r.mu.Unlock()
	}
}

// bindSession records the verifier session started for holder.
func (r *verifierRegistry) bindSession(holder, sessionID string) {
	r.mu.Lock()
	r.sessions[holder] = sessionID
	r.mu.Unlock()
}

func (r *verifierRegistry) isActive(holder string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[holder]
}

func (r *verifierRegistry) sessionOf(holder string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.sessions[holder]
}

// leaseHolderLive reports whether a lease still has a live holder. A verify:
// lease is judged by its verifier session when one was started, and otherwise
// stays live while its verifier is registered as active, whatever the bound
// (worker) session or lease age says. A judge:<unit> lease is judged by the
// same rule — its judge session decides first, then active registration,
// including the /api/ask path where the judge runs inline and has no
// session; it reaps through the normal bound-session or spawn-grace check
// once the judge is off and its session is done. Any other lease is live
// while its bound session is running, or for spawnGrace when it has none.
func leaseHolderLive(ls capacity.Lease, reg *verifierRegistry, holderAlive func(holder string) bool, sessionAlive func(id string) bool, now time.Time) bool {
	live, _ := leaseLiveVerdict(ls, reg, holderAlive, sessionAlive, now)
	return live
}

// leaseLiveVerdict is leaseHolderLive with the reason. When the lease is not
// live, why names the evidence the reaper judged it by: the verifier session or
// bound session that is not running, or the spawn grace that has run out.
func leaseLiveVerdict(ls capacity.Lease, reg *verifierRegistry, holderAlive func(holder string) bool, sessionAlive func(id string) bool, now time.Time) (live bool, why string) {
	if strings.HasPrefix(ls.Holder, autonomous.VerifyHolder("")) && reg != nil {
		if sid := reg.sessionOf(ls.Holder); sid != "" {
			if sessionAlive(sid) {
				return true, ""
			}
			return false, fmt.Sprintf("verifier session %s is not running", sid)
		}
		if reg.isActive(ls.Holder) {
			return true, ""
		}
	}
	if strings.HasPrefix(ls.Holder, autonomous.JudgeHolder("")) {
		// Same live-session / spawn-grace logic as verifier leases: the
		// judge's bound session (set through led.Bind when its spawn is
		// reported, mirroring the judges registry) decides first — a dead
		// judge session is a dead lease even if registration lingers — and
		// only with no session bound is active registration (the /api/ask
		// path, spawn in progress) kept alive, after which the
		// spawn-grace rule below applies.
		if ls.SessionID != "" {
			if sessionAlive(ls.SessionID) {
				return true, ""
			}
			return false, fmt.Sprintf("judge session %s is not running", ls.SessionID)
		}
		if holderAlive != nil && holderAlive(ls.Holder) {
			return true, ""
		}
	}
	if ls.SessionID == "" {
		age := now.Sub(ls.Acquired)
		if age < spawnGrace {
			return true, ""
		}
		return false, fmt.Sprintf("no bound session and acquired %s ago, past the %s spawn grace", endSecs(age), endSecs(spawnGrace))
	}
	if sessionAlive(ls.SessionID) {
		return true, ""
	}
	return false, fmt.Sprintf("bound session %s is not running", ls.SessionID)
}

// sessionRunning reports whether id names a session that has not finished.
func sessionRunning(mgr *session.Manager, id string) bool {
	s, ok := mgr.GetSession(id)
	return ok && s.State != session.StateComplete && s.State != session.StateFailed && s.State != session.StateKilled
}

// verifierSessionStarted is called by the one-shot runner as soon as a
// session is started, with its name and id. wireCapacity points it at the
// verifier registry so the verifier's lease follows the verifier session.
var verifierSessionStarted func(sessionName, sessionID string)

// verifierCapacitySlotState is the read-only view of the daemon's capacity
// ledger used by the one-shot verifier wait loop for its slot-state signal:
// SlotHeld when the verifier's holder holds a lease, SlotWaiting when its
// request is queued, otherwise SlotNone. wireCapacity points it at the live
// ledger; nil until wired (which the loop treats as SlotNone — no slot
// signal). Read-only by contract: it never acquires, releases, or otherwise
// touches admission — that happens daemon-side, before the verifier session
// is spawned.
var verifierCapacitySlotState func(holder string) autonomous.SlotState

// verifierTaskFromSessionName extracts the task id from
// "autonomous:verify:<task id>"; ok is false for any other name.
func verifierTaskFromSessionName(name string) (string, bool) {
	p := verifierSessionNamePrefix + ":"
	if !strings.HasPrefix(name, p) || len(name) == len(p) {
		return "", false
	}
	return name[len(p):], true
}
