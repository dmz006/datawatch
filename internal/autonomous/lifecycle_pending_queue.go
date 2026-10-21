// Automaton lifecycle events: the pending-events queue behind the
// mcp_queue subscription sink. Generic MCP clients that cannot receive
// pushes poll a named queue with a cursor instead. Design:
// docs/plans/2026-10-13-prd-lifecycle-events.md (Q3 "Generic MCP client").

package autonomous

import "sync"

// DefaultPendingEventsMax is the per-queue retention used when the
// configured maximum is unset or below 1.
const DefaultPendingEventsMax = 500

// PendingEntry is one retained event with its queue cursor.
type PendingEntry struct {
	Cursor uint64         `json:"cursor"`
	Event  LifecycleEvent `json:"event"`
}

// pendingRing is one queue's retained entries, oldest first.
type pendingRing struct {
	entries []PendingEntry
	// evictedThrough is the cursor of the newest entry ever evicted from
	// this ring (0 = nothing evicted yet).
	evictedThrough uint64
}

// PendingEventQueue holds bounded in-memory rings of lifecycle events,
// keyed by queue name. Cursors are monotonic per daemon run (starting
// at 1) and shared across queues. Append never blocks and a queue never
// holds more than the maximum; the oldest entries are evicted first.
// Safe for concurrent use.
type PendingEventQueue struct {
	mu     sync.Mutex
	max    int
	next   uint64
	queues map[string]*pendingRing
}

// NewPendingEventQueue builds a queue set retaining at most max entries
// per queue; max < 1 falls back to DefaultPendingEventsMax.
func NewPendingEventQueue(max int) *PendingEventQueue {
	if max < 1 {
		max = DefaultPendingEventsMax
	}
	return &PendingEventQueue{max: max, queues: make(map[string]*pendingRing)}
}

// Max returns the per-queue retention bound.
func (q *PendingEventQueue) Max() int { return q.max }

// Append adds ev to the named queue, evicting the oldest entries when
// the queue exceeds its maximum, and returns the entry's cursor.
func (q *PendingEventQueue) Append(queue string, ev LifecycleEvent) uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.next++
	r := q.queues[queue]
	if r == nil {
		r = &pendingRing{}
		q.queues[queue] = r
	}
	r.entries = append(r.entries, PendingEntry{Cursor: q.next, Event: ev})
	if over := len(r.entries) - q.max; over > 0 {
		r.evictedThrough = r.entries[over-1].Cursor
		// Copy down so the evicted entries are released, not pinned by
		// the backing array.
		n := copy(r.entries, r.entries[over:])
		for i := n; i < len(r.entries); i++ {
			r.entries[i] = PendingEntry{}
		}
		r.entries = r.entries[:n]
	}
	return q.next
}

// Pending returns up to limit events from the named queue with a cursor
// strictly greater than sinceCursor, oldest first (limit < 1 means no
// limit beyond the queue maximum). nextCursor is the cursor to pass on
// the following call: the last returned event's cursor, or sinceCursor
// when nothing was returned. dropped is true when events the caller has
// not seen were evicted (sinceCursor is older than the oldest retained
// entry and entries were lost in between).
func (q *PendingEventQueue) Pending(queue string, sinceCursor uint64, limit int) (events []LifecycleEvent, nextCursor uint64, dropped bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	nextCursor = sinceCursor
	r := q.queues[queue]
	if r == nil || len(r.entries) == 0 {
		return nil, nextCursor, false
	}
	// Cursors are shared across queues, so a gap between sinceCursor and
	// the oldest retained cursor proves nothing; the ring records the
	// newest cursor it has evicted, and the caller lost events exactly
	// when that is past its position.
	if r.evictedThrough > sinceCursor {
		dropped = true
	}
	for _, e := range r.entries {
		if e.Cursor <= sinceCursor {
			continue
		}
		if limit > 0 && len(events) >= limit {
			break
		}
		events = append(events, e.Event)
		nextCursor = e.Cursor
	}
	return events, nextCursor, dropped
}

// Release drops the named queue and its retained events (called when
// its last subscription is deleted).
func (q *PendingEventQueue) Release(queue string) {
	q.mu.Lock()
	delete(q.queues, queue)
	q.mu.Unlock()
}

// Queues returns the number of live queues (for tests and diagnostics).
func (q *PendingEventQueue) Queues() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queues)
}

// Names returns the live queue names.
func (q *PendingEventQueue) Names() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]string, 0, len(q.queues))
	for n := range q.queues {
		out = append(out, n)
	}
	return out
}
