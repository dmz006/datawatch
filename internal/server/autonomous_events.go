// REST read side of the mcp_queue lifecycle subscription sink.
//
//	GET /api/autonomous/events?queue=<name>&since=<cursor>&limit=<n>
//
// Returns the lifecycle events a named pending-events queue retains with
// a cursor greater than since, oldest first:
//
//	{"events":[...],"next_cursor":<n>,"dropped":<bool>}
//
// The caller passes next_cursor back as since on its next poll. dropped
// is true when events it had not yet seen were evicted from the bounded
// queue. limit defaults to 50 and is capped at 200. A missing queue or a
// non-numeric since/limit is a 400; when no queue set is wired (a daemon
// running without the subscription engine) the endpoint is a 503.

package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

const (
	// pendingEventsDefaultLimit is the page size when ?limit= is absent.
	pendingEventsDefaultLimit = 50
	// pendingEventsMaxLimit caps ?limit=.
	pendingEventsMaxLimit = 200
)

// PendingEventsAPI is the surface the events endpoint needs.
// autonomous.PendingEventQueue satisfies it; tests may substitute a fake.
type PendingEventsAPI interface {
	Pending(queue string, sinceCursor uint64, limit int) (events []autonomous.LifecycleEvent, nextCursor uint64, dropped bool)
}

// SetAutonomousEvents wires the pending-events queue for
// /api/autonomous/events. Nil (the default) disables the endpoint — the
// handler returns 503, matching the other Set*Fn wiring in this package.
func (s *Server) SetAutonomousEvents(api PendingEventsAPI) {
	s.autonomousEvents = api
}

// handleAutonomousEvents implements GET /api/autonomous/events. Auth:
// autonomous:read, like the subscription list.
func (s *Server) handleAutonomousEvents(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.autonomousEvents == nil {
		http.Error(w, "pending events queue disabled", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	queue := strings.TrimSpace(q.Get("queue"))
	if queue == "" {
		http.Error(w, "queue is required", http.StatusBadRequest)
		return
	}
	var since uint64
	if v := strings.TrimSpace(q.Get("since")); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			http.Error(w, "since must be a non-negative integer cursor", http.StatusBadRequest)
			return
		}
		since = n
	}
	limit := pendingEventsDefaultLimit
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		limit = n
	}
	if limit > pendingEventsMaxLimit {
		limit = pendingEventsMaxLimit
	}

	events, next, dropped := s.autonomousEvents.Pending(queue, since, limit)
	if events == nil {
		events = []autonomous.LifecycleEvent{} // never "null" to the caller
	}
	writeJSONOK(w, map[string]any{
		"events":      events,
		"next_cursor": next,
		"dropped":     dropped,
	})
}
