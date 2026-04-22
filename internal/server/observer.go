// BL171 (S9) — REST surface for the observer subsystem.
//
// Endpoints (all bearer-authenticated):
//   GET  /api/observer/stats                 alias for /api/stats when ?v=2 requested
//   GET  /api/observer/envelopes             envelope rollup only
//   GET  /api/observer/envelope?id=<id>      one envelope's process tree
//   GET  /api/observer/config                read observer config
//   PUT  /api/observer/config                replace observer config
//
// /api/stats itself is extended in handleStats so a v=2 query param
// returns the v2 shape while the bare path keeps its v1 response.

package server

import (
	"context"
	"encoding/json"
	"net/http"
)

// ObserverAPI is the narrow surface the REST handlers call. Defined
// here so tests don't need to import internal/observer.
type ObserverAPI interface {
	Config() any
	SetConfig(any) error
	Stats() any
	Envelopes() any
	EnvelopeTree(id string) any
	Start(ctx context.Context)
	Stop()
}

// SetObserverAPI wires the daemon's observer. Nil disables the
// /api/observer/* surface (503).
func (s *Server) SetObserverAPI(a ObserverAPI) { s.observerAPI = a }

func (s *Server) handleObserverStats(w http.ResponseWriter, r *http.Request) {
	if s.observerAPI == nil {
		http.Error(w, "observer disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONOK(w, s.observerAPI.Stats())
}

func (s *Server) handleObserverEnvelopes(w http.ResponseWriter, r *http.Request) {
	if s.observerAPI == nil {
		http.Error(w, "observer disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSONOK(w, map[string]any{
		"envelopes": s.observerAPI.Envelopes(),
	})
}

func (s *Server) handleObserverEnvelope(w http.ResponseWriter, r *http.Request) {
	if s.observerAPI == nil {
		http.Error(w, "observer disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}
	tree := s.observerAPI.EnvelopeTree(id)
	if tree == nil {
		http.Error(w, "envelope not found", http.StatusNotFound)
		return
	}
	writeJSONOK(w, tree)
}

func (s *Server) handleObserverConfig(w http.ResponseWriter, r *http.Request) {
	if s.observerAPI == nil {
		http.Error(w, "observer disabled", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSONOK(w, s.observerAPI.Config())
	case http.MethodPut, http.MethodPost:
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.observerAPI.SetConfig(body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
