package server

import (
	"net/http"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/federation"
)

// dormantFeaturesBody is the body of GET /api/autonomous/dormant-features.
type dormantFeaturesBody struct {
	Features []autonomous.DormantFeature `json:"features"`
}

// handleDormantFeatures serves GET /api/autonomous/dormant-features: every
// Automata setting that is off or in shadow mode, with its key, its default,
// its purpose and whether it has ever been exercised. Read-only: it lists
// settings from the config the daemon runs with, runs no Automaton and
// writes no config. A federated peer needs the autonomous read capability.
func (s *Server) handleDormantFeatures(w http.ResponseWriter, r *http.Request) {
	if s.autonomousMgr == nil {
		http.Error(w, "autonomous is disabled", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAutonomousRead) {
		return
	}
	features := autonomous.ListDormantFeatures(s.cfg, autonomous.MergeShadowRecords{DataDir: s.statePath()})
	writeJSONOK(w, dormantFeaturesBody{Features: features})
}
