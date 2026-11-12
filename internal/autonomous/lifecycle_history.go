// A short in-memory history of the lifecycle events this daemon has seen, so
// the subscription picker can answer "how many events of these kinds would a
// rule have matched in the last day" for lifecycle kinds (the kinds the
// picker lists), which the Events log does not hold.
//
// The history is bounded (lifecycleHistoryMax events) and lives in memory: it
// covers events since this daemon started, and RecentLifecycleEvents says when
// recording began so a caller can tell a quiet day from a fresh start.

package autonomous

import (
	"sync"
	"time"
)

// lifecycleHistoryMax caps the events kept; the oldest are dropped first.
const lifecycleHistoryMax = 5000

type lifecycleHistory struct {
	mu     sync.Mutex
	since  time.Time
	events []LifecycleEvent
}

func (h *lifecycleHistory) start(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.since.IsZero() {
		h.since = now
	}
}

func (h *lifecycleHistory) add(ev LifecycleEvent) {
	if ev.Test {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	h.events = append(h.events, ev)
	if n := len(h.events); n > lifecycleHistoryMax {
		h.events = append([]LifecycleEvent(nil), h.events[n-lifecycleHistoryMax:]...)
	}
}

// RecentLifecycleEvents returns the recorded lifecycle events at or after
// since, newest first, and the instant recording began. Synthetic test events
// are not recorded.
func (m *Manager) RecentLifecycleEvents(since time.Time) (events []LifecycleEvent, recordingSince time.Time) {
	if m == nil {
		return nil, time.Time{}
	}
	m.lifecycleBus() // makes sure the recorder is attached
	h := &m.lifeHistory
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.events) - 1; i >= 0; i-- {
		if !h.events[i].At.Before(since) {
			events = append(events, h.events[i])
		}
	}
	return events, h.since
}
