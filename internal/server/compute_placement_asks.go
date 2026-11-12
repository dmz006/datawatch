// Placement ask REST surface (missing-model policy = "ask").
//
// When the placement engine plans a model that is present on no eligible
// node and the policy is "ask", the ask is recorded in the in-process
// placement ask store and surfaced here for the operator:
//
//	GET  /api/compute/placements            → pending asks (oldest first)
//	POST /api/compute/placements/<id>/approve  → approve (one-shot)
//	POST /api/compute/placements/<id>/deny    → deny (one-shot)
//
// Decisions are one-shot: approving a denied/expired/already-approved ask
// is refused (409). Unknown IDs are 404.

package server

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/federation"
)

// The registry is daemon-wide state, not per-Server: the placement
// engine, REST, CLI, comm channel and MCP must all share one queue.
var (
	placementAskMu   sync.RWMutex
	placementAskReg  = compute.NewPlacementAskStore()
)

// PlacementAskRegistry returns the daemon-wide placement ask store.
// The placement engine (ask policy) records asks through it; the
// handlers below read and decide on it.
func PlacementAskRegistry() *compute.PlacementAskStore {
	placementAskMu.RLock()
	defer placementAskMu.RUnlock()
	return placementAskReg
}

// resetPlacementAsksForTest swaps in a fresh store (tests only).
func resetPlacementAsksForTest() {
	placementAskMu.Lock()
	placementAskReg = compute.NewPlacementAskStore()
	placementAskMu.Unlock()
}

func (s *Server) handleComputePlacements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "use POST /api/compute/placements/<id>/approve|deny to decide an ask", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapComputeList) {
		return
	}
	st := PlacementAskRegistry()
	st.ExpireStale(time.Now())
	pending := st.Pending()
	writeJSONOK(w, map[string]any{"asks": pending, "pending": len(pending)})
}

func (s *Server) handleComputePlacementAskDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (use POST)", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/compute/placements/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" {
		http.Error(w, `expected /api/compute/placements/<id>/approve or /deny`, http.StatusBadRequest)
		return
	}
	id, action := parts[0], strings.ToLower(parts[1])
	var decision compute.PlacementAskDecision
	switch action {
	case "approve":
		decision = compute.AskDecisionApproved
	case "deny":
		decision = compute.AskDecisionDenied
	default:
		http.Error(w, "unknown action "+action+` (use approve|deny)`, http.StatusBadRequest)
		return
	}
	if !s.fedCap(w, r, federation.CapComputeWrite) {
		return
	}
	ask, err := PlacementAskRegistry().Answer(id, decision, time.Now(), "operator")
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "unknown ask") {
			http.Error(w, msg, http.StatusNotFound)
			return
		}
		http.Error(w, msg, http.StatusConflict)
		return
	}
	writeJSONOK(w, map[string]any{"ask": ask})
}
