// BL171 (S9) — REST surface for the observer subsystem.
//
// Endpoints (all bearer-authenticated):
//   GET  /api/observer/stats                       alias for /api/stats when ?v=2 requested
//   GET  /api/observer/envelopes                   envelope rollup only (local)
//   GET  /api/observer/envelopes/all-peers         BL180 Phase 2 cross-host: local + every peer with cross-peer Caller attribution
//   GET  /api/observer/envelope?id=<id>            one envelope's process tree
//   GET  /api/observer/config                      read observer config
//   PUT  /api/observer/config                      replace observer config

package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/observer"
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

	// EnvelopeSummary returns the (cpu_pct, rss_bytes) for one
	// envelope by id from the LOCAL collector's latest snapshot.
	// ok=false when the envelope isn't present. S13 follow — used by
	// the orchestrator handler to enrich graph nodes; the handler
	// also walks peer registry snapshots to fold in remote agents.
	EnvelopeSummary(id string) (cpuPct float64, rssBytes uint64, ok bool)
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

// handleObserverEnvelopesAllPeers (BL180 Phase 2 cross-host, v5.12.0)
// is the federation aggregator's join. Collects local envelopes plus
// every peer's last-pushed snapshot, runs CorrelateAcrossPeers to
// surface cross-host caller attribution, and returns the unified view.
//
// GET /api/observer/envelopes/all-peers
//
// Returns:
//   {
//     "by_peer": {
//       "local":      [Envelope, ...],
//       "<peer-1>":   [Envelope, ...],
//       ...
//     }
//   }
//
// Each Envelope's Callers includes any cross-peer attributions: a
// session on peer-A talking to ollama on peer-B will appear on
// peer-B's ollama envelope as Callers[i].Caller = "peer-A:session:opencode-x1y2".
func (s *Server) handleObserverEnvelopesAllPeers(w http.ResponseWriter, r *http.Request) {
	if s.observerAPI == nil {
		http.Error(w, "observer disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	byPeer := map[string][]observer.Envelope{}
	if locals, ok := s.observerAPI.Envelopes().([]observer.Envelope); ok && len(locals) > 0 {
		byPeer["local"] = append([]observer.Envelope{}, locals...)
	}
	if s.peerRegistry != nil {
		for _, p := range s.peerRegistry.List() {
			snap := s.peerRegistry.LastPayload(p.Name)
			if snap == nil {
				continue
			}
			byPeer[p.Name] = append([]observer.Envelope{}, snap.Envelopes...)
		}
	}
	observer.CorrelateAcrossPeers(byPeer, "local")
	writeJSONOK(w, map[string]any{"by_peer": byPeer})
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
