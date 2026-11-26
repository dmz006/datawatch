package supervisorloop

import (
	"sync"
	"time"
)

// DefaultEventsSilentAfter is how old the last delivered event wake may be
// before Status reports the event path as silent.
const DefaultEventsSilentAfter = time.Hour

// WakeRecord remembers when an event wake last typed its line and when the
// last scheduled check ran, plus how many of each, so a silent event path is
// visible. It counts the supervisor passes event wakes ran apart from the
// typed lines. It holds only times and counts.
type WakeRecord struct {
	mu          sync.Mutex
	clock       Clock
	silentAfter time.Duration
	lastEvent   time.Time
	lastCheck   time.Time
	events      int
	checks      int
	passes      int
}

// NewWakeRecord returns a record using clock for every timestamp. A silentAfter
// of zero or less selects DefaultEventsSilentAfter.
func NewWakeRecord(clock Clock, silentAfter time.Duration) *WakeRecord {
	if silentAfter <= 0 {
		silentAfter = DefaultEventsSilentAfter
	}
	return &WakeRecord{clock: clock, silentAfter: silentAfter}
}

// MarkEvent records one event wake whose line was typed.
func (r *WakeRecord) MarkEvent() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastEvent = r.clock.Now()
	r.events++
}

// MarkPass records one supervisor pass that an event wake ran, whether or not
// its line was typed.
func (r *WakeRecord) MarkPass() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.passes++
}

// MarkScheduledCheck records one scheduled check.
func (r *WakeRecord) MarkScheduledCheck() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastCheck = r.clock.Now()
	r.checks++
}

// WakeStatus is the read-only view of a WakeRecord.
type WakeStatus struct {
	LastEvent          time.Time `json:"last_event"`
	LastScheduledCheck time.Time `json:"last_scheduled_check"`
	EventCount         int       `json:"event_count"`
	ScheduledCount     int       `json:"scheduled_count"`
	PassCount          int       `json:"pass_count"`
	// EventsSilentFor is set when the last event is older than the threshold,
	// or when none has arrived yet but a scheduled check has run for longer
	// than the threshold.
	EventsSilentFor string `json:"events_silent_for,omitempty"`
}

// Status returns the current view.
func (r *WakeRecord) Status() WakeStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := WakeStatus{
		LastEvent: r.lastEvent, LastScheduledCheck: r.lastCheck,
		EventCount: r.events, ScheduledCount: r.checks, PassCount: r.passes,
	}
	now := r.clock.Now()
	switch {
	case !r.lastEvent.IsZero():
		if age := now.Sub(r.lastEvent); age > r.silentAfter {
			s.EventsSilentFor = age.Round(time.Second).String()
		}
	case !r.lastCheck.IsZero():
		if age := now.Sub(r.lastCheck); age > r.silentAfter {
			s.EventsSilentFor = age.Round(time.Second).String()
		}
	}
	return s
}
