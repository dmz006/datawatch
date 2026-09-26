package session

import (
	"sync"
	"time"
)

// AlertGate turns raw needs-input detections into real alerts. A detection
// only fires after the session has stayed waiting for the settle window
// (any new detection restarts it, Cancel drops it), and an identical
// detection for the same session is suppressed for the repeat window.
type AlertGate struct {
	mu       sync.Mutex
	timers   map[string]*time.Timer
	lastKey  map[string]string
	lastFire map[string]time.Time
	// counters exposed for observability
	Fired, Cancelled, Repeats int
}

func NewAlertGate() *AlertGate {
	return &AlertGate{
		timers:   map[string]*time.Timer{},
		lastKey:  map[string]string{},
		lastFire: map[string]time.Time{},
	}
}

// Schedule arms fire() to run after settle unless Cancel is called first or
// the same key already fired within repeat. settle<=0 fires immediately.
func (g *AlertGate) Schedule(sessionID, key string, settle, repeat time.Duration, fire func()) {
	g.mu.Lock()
	if t, ok := g.timers[sessionID]; ok {
		t.Stop()
		delete(g.timers, sessionID)
	}
	if g.lastKey[sessionID] == key && time.Since(g.lastFire[sessionID]) < repeat {
		g.Repeats++
		g.mu.Unlock()
		return
	}
	run := func() {
		g.mu.Lock()
		delete(g.timers, sessionID)
		g.lastKey[sessionID] = key
		g.lastFire[sessionID] = time.Now()
		g.Fired++
		g.mu.Unlock()
		fire()
	}
	if settle <= 0 {
		g.mu.Unlock()
		run()
		return
	}
	g.timers[sessionID] = time.AfterFunc(settle, run)
	g.mu.Unlock()
}

// Cancel drops a pending alert for the session (it left waiting_input).
func (g *AlertGate) Cancel(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if t, ok := g.timers[sessionID]; ok {
		t.Stop()
		delete(g.timers, sessionID)
		g.Cancelled++
	}
}

// Forget clears all state for an ended session.
func (g *AlertGate) Forget(sessionID string) {
	g.Cancel(sessionID)
	g.mu.Lock()
	delete(g.lastKey, sessionID)
	delete(g.lastFire, sessionID)
	g.mu.Unlock()
}
