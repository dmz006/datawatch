// BL30 — global rate-limit cooldown REST surface.
//
//   GET    /api/cooldown                  fetch current state
//   POST   /api/cooldown                  set { until_unix_ms, reason }
//   DELETE /api/cooldown                  clear

package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
)

func (s *Server) handleCooldown(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		if s.manager == nil {
			http.Error(w, "manager not available", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.manager.CooldownStatus())
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.manager == nil {
			http.Error(w, "manager not available", http.StatusServiceUnavailable)
			return
		}
		var req struct {
			UntilUnixMs int64  `json:"until_unix_ms"`
			Reason      string `json:"reason,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.UntilUnixMs <= 0 {
			http.Error(w, "until_unix_ms required (>0)", http.StatusBadRequest)
			return
		}
		until := time.UnixMilli(req.UntilUnixMs)
		if !until.After(time.Now()) {
			http.Error(w, "until_unix_ms must be in the future", http.StatusBadRequest)
			return
		}
		s.manager.SetGlobalCooldown(until, req.Reason)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.manager.CooldownStatus())
	case http.MethodDelete:
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if s.manager == nil {
			http.Error(w, "manager not available", http.StatusServiceUnavailable)
			return
		}
		s.manager.ClearGlobalCooldown()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
