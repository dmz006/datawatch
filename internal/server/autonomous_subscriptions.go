// BL427 (PRD lifecycle events, phase 3) — REST CRUD for the persisted
// lifecycle event subscriptions from internal/autonomous.SubscriptionStore
// (the same store the daemon's LifecycleSubscriptionEngine runs from
// <data_dir>/lifecycle_subscriptions.json).
//
//	GET    /api/autonomous/subscriptions        — list (optional ?owner= filter)
//	POST   /api/autonomous/subscriptions        — create (201 + the record)
//	DELETE /api/autonomous/subscriptions/{id}   — delete (204)
//	DELETE /api/autonomous/subscriptions?id=    — delete (204, same as above)
//
// The store's Add/Delete run ValidateSubscription, so malformed input is
// a 400 carrying the store's message and an unknown id on delete is a
// 404. When nothing is wired (unit tests, or a daemon running without
// the subscription engine) the handlers return 503 instead. Because the
// engine re-attaches its dispatcher sinks on every store change
// (SubscriptionStore.OnChanged), a subscription created or deleted here
// takes effect without a restart.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// LifecycleSubscriptionsAPI is the surface the subscription REST handlers
// need. autonomous.SubscriptionStore satisfies it; tests may substitute a
// fake.
type LifecycleSubscriptionsAPI interface {
	Add(autonomous.Subscription) (autonomous.Subscription, error)
	List(owner string) []autonomous.Subscription
	Get(id string) (autonomous.Subscription, bool)
	Delete(id string) error
}

// SetAutonomousSubscriptions wires the lifecycle subscription store for
// /api/autonomous/subscriptions. Nil (the default) disables the endpoints
// — handlers return "lifecycle subscriptions disabled" (503), matching
// the other Set*Fn wiring in this package.
func (s *Server) SetAutonomousSubscriptions(api LifecycleSubscriptionsAPI) {
	s.autonomousSubs = api
}

// handleAutonomousSubscriptions implements the three endpoints above.
// Auth: GET requires autonomous:read, POST/DELETE autonomous:write — the
// same split every other write/read endpoint under /api/autonomous uses
// (federation parity, AGENT.md rule).
func (s *Server) handleAutonomousSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapAutonomousRead) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapAutonomousWrite) {
			return
		}
	}
	if s.autonomousSubs == nil {
		http.Error(w, "lifecycle subscriptions disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/autonomous/subscriptions")
	rest = strings.Trim(rest, "/")
	id := rest
	if id == "" {
		id = r.URL.Query().Get("id") // delete also accepts ?id=
	}

	if id == "" {
		switch r.Method {
		case http.MethodGet:
			owner := r.URL.Query().Get("owner") // optional filter; empty = all
			list := s.autonomousSubs.List(owner)
			if list == nil {
				list = []autonomous.Subscription{} // never "null" to the operator
			}
			writeJSONOK(w, list)
		case http.MethodPost:
			var sub autonomous.Subscription
			if err := json.NewDecoder(r.Body).Decode(&sub); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			sub.ID = "" // the server assigns it; a client-supplied id is not honoured
			created, err := s.autonomousSubs.Add(sub)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit(r.Context(), "create", "lifecycle_subscription", created.ID, nil) // BL427 phase 3
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(created)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.autonomousSubs.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.audit(r.Context(), "delete", "lifecycle_subscription", id, nil)
	w.WriteHeader(http.StatusNoContent)
}
