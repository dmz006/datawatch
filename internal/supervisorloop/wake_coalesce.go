package supervisorloop

import (
	"strings"
	"sync"
	"time"
)

// WakeClock supplies time and one-shot timers to the Coalescer, so tests can
// drive the coalesce window without sleeping.
type WakeClock interface {
	Now() time.Time
	// AfterFunc runs f once after d and returns a function that cancels it.
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// WakeEvent is one lifecycle event that may wake the supervisor.
type WakeEvent struct {
	Kind        string
	AutomatonID string
	TaskID      string
	// Fingerprint identifies the state of the condition, so a changed
	// condition counts as a new one.
	Fingerprint string
	At          time.Time
}

// Key is the condition key: kind, Automaton id, task id and fingerprint.
func (e WakeEvent) Key() string {
	return strings.Join([]string{e.Kind, e.AutomatonID, e.TaskID, e.Fingerprint}, "\x1f")
}

// WakeBatch is the set of distinct conditions delivered together as one wake.
type WakeBatch struct {
	Events []WakeEvent
}

// Coalescer folds a burst of wake events into one batch and never redelivers
// a condition that has not changed until Clear is called for its key.
type Coalescer struct {
	clock   WakeClock
	window  time.Duration
	deliver func(WakeBatch)

	mu        sync.Mutex
	pending   []WakeEvent
	pendingK  map[string]bool
	delivered map[string]bool
	stop      func() bool
}

// NewCoalescer builds a Coalescer. A window of zero or less delivers each new
// condition at once.
func NewCoalescer(clock WakeClock, window time.Duration, deliver func(WakeBatch)) *Coalescer {
	return &Coalescer{
		clock:     clock,
		window:    window,
		deliver:   deliver,
		pendingK:  map[string]bool{},
		delivered: map[string]bool{},
	}
}

// Offer accepts an event. It reports false when the condition was already
// delivered or is already waiting in the open window.
func (c *Coalescer) Offer(ev WakeEvent) bool {
	key := ev.Key()
	if ev.At.IsZero() {
		ev.At = c.clock.Now()
	}
	c.mu.Lock()
	if c.delivered[key] || c.pendingK[key] {
		c.mu.Unlock()
		return false
	}
	c.pendingK[key] = true
	c.pending = append(c.pending, ev)
	if c.window <= 0 {
		batch := c.takeLocked()
		c.mu.Unlock()
		c.deliver(batch)
		return true
	}
	if c.stop == nil {
		c.stop = c.clock.AfterFunc(c.window, c.flush)
	}
	c.mu.Unlock()
	return true
}

// Clear marks the condition resolved, so the same condition may wake the
// supervisor again. A condition still waiting in the window is dropped.
func (c *Coalescer) Clear(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.delivered, key)
	if !c.pendingK[key] {
		return
	}
	delete(c.pendingK, key)
	kept := c.pending[:0]
	for _, ev := range c.pending {
		if ev.Key() != key {
			kept = append(kept, ev)
		}
	}
	c.pending = kept
}

func (c *Coalescer) flush() {
	c.mu.Lock()
	batch := c.takeLocked()
	c.mu.Unlock()
	if len(batch.Events) > 0 {
		c.deliver(batch)
	}
}

// takeLocked moves the pending events into a batch and marks them delivered.
func (c *Coalescer) takeLocked() WakeBatch {
	batch := WakeBatch{Events: c.pending}
	for _, ev := range c.pending {
		k := ev.Key()
		c.delivered[k] = true
		delete(c.pendingK, k)
	}
	c.pending = nil
	c.stop = nil
	return batch
}
