// internal/server/algorithm.go — REST surface for BL258 v6.9.0
// (Algorithm Mode per-session 7-phase harness).
//
// Routes:
//
//	GET    /api/algorithm                              — list every active session
//	POST   /api/algorithm/{sessionID}/start            — register a session in Algorithm Mode
//	GET    /api/algorithm/{sessionID}                  — read state
//	POST   /api/algorithm/{sessionID}/advance          — close current phase, advance to next
//	POST   /api/algorithm/{sessionID}/edit             — replace last recorded phase output
//	POST   /api/algorithm/{sessionID}/abort            — terminate mid-algorithm
//	DELETE /api/algorithm/{sessionID}                  — reset state
//
// All write paths emit audit entries (action=algorithm_*).
//
// Returns 503 when no algorithm tracker is wired.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/algorithm"
	"github.com/dmz006/datawatch/internal/audit"
)

type algorithmTracker interface {
	Start(string) *algorithm.State
	Get(string) *algorithm.State
	All() []*algorithm.State
	Advance(string, string) (*algorithm.State, error)
	Edit(string, string) (*algorithm.State, error)
	Abort(string) (*algorithm.State, error)
	Reset(string)
}

// SetAlgorithmTracker wires the runtime *algorithm.Tracker into the server.
func (s *Server) SetAlgorithmTracker(t algorithmTracker) { s.algorithmTracker = t }

func (s *Server) handleAlgorithm(w http.ResponseWriter, r *http.Request) {
	if s.algorithmTracker == nil {
		http.Error(w, "algorithm disabled", http.StatusServiceUnavailable)
		return
	}
	// Path: /api/algorithm[/{id}[/action]]
	rest := strings.TrimPrefix(r.URL.Path, "/api/algorithm")
	rest = strings.TrimPrefix(rest, "/")
	parts := strings.Split(rest, "/")

	if rest == "" {
		// list
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSONOK(w, map[string]any{
			"sessions": s.algorithmTracker.All(),
			"phases":   algorithm.Phases(),
		})
		return
	}
	id := parts[0]
	if id == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		st := s.algorithmTracker.Get(id)
		if st == nil {
			http.Error(w, "session not in Algorithm Mode", http.StatusNotFound)
			return
		}
		writeJSONOK(w, st)

	case action == "" && r.Method == http.MethodDelete:
		s.algorithmTracker.Reset(id)
		s.auditAlgorithm(id, "algorithm_reset")
		writeJSONOK(w, map[string]any{"status": "reset", "session_id": id})

	case action == "start" && r.Method == http.MethodPost:
		st := s.algorithmTracker.Start(id)
		s.auditAlgorithm(id, "algorithm_start")
		writeJSONOK(w, st)

	case action == "advance" && r.Method == http.MethodPost:
		var body struct {
			Output string `json:"output"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		st, err := s.algorithmTracker.Advance(id, body.Output)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.auditAlgorithm(id, "algorithm_advance")
		writeJSONOK(w, st)

	case action == "edit" && r.Method == http.MethodPost:
		var body struct {
			Output string `json:"output"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		st, err := s.algorithmTracker.Edit(id, body.Output)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.auditAlgorithm(id, "algorithm_edit")
		writeJSONOK(w, st)

	case action == "abort" && r.Method == http.MethodPost:
		st, err := s.algorithmTracker.Abort(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.auditAlgorithm(id, "algorithm_abort")
		writeJSONOK(w, st)

	default:
		http.Error(w, "unknown action or method", http.StatusMethodNotAllowed)
	}
}

func (s *Server) auditAlgorithm(id, action string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type": "algorithm_session",
			"resource_id":   id,
		},
	})
}
