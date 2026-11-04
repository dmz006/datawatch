package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/git"
)

// handleGitSecretScanStats serves GET /api/git/secret-scan/stats — the
// daemon's in-memory pre-push secret-scan counters (runs, findings,
// shadow reports, refusals, last run/refusal times) plus the effective
// git.secret_scan mode. The counters live in package git and reset on
// daemon restart; this endpoint is a plain read of that snapshot.
func (s *Server) handleGitSecretScanStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}
	if !s.fedCap(w, r, federation.CapHealthRead) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"mode":  s.cfg.Git.EffectiveSecretScanMode(),
		"stats": git.SecretScanStatsSnapshot(),
	})
}
