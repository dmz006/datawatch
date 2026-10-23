package main

import (
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
// (worker) session or lease age says. Any other lease is live while its bound
// session is running, or for spawnGrace when it has none.
func leaseHolderLive(ls capacity.Lease, reg *verifierRegistry, sessionAlive func(id string) bool, now time.Time) bool {
	if strings.HasPrefix(ls.Holder, autonomous.VerifyHolder("")) && reg != nil {
		if sid := reg.sessionOf(ls.Holder); sid != "" {
			return sessionAlive(sid)
		}
		if reg.isActive(ls.Holder) {
			return true
		}
	}
	if ls.SessionID == "" {
		return now.Sub(ls.Acquired) < spawnGrace
	}
	return sessionAlive(ls.SessionID)
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

// verifierTaskFromSessionName extracts the task id from
// "autonomous:verify:<task id>"; ok is false for any other name.
func verifierTaskFromSessionName(name string) (string, bool) {
	p := verifierSessionNamePrefix + ":"
	if !strings.HasPrefix(name, p) || len(name) == len(p) {
		return "", false
	}
	return name[len(p):], true
}
