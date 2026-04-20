// BL40 — stale-session REST endpoint.
//
//   GET /api/sessions/stale[?seconds=N]
//
// Returns sessions in StateRunning whose UpdatedAt is older than the
// supplied threshold (or session.stale_timeout_seconds when not given).

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/dmz006/datawatch/internal/session"
)

func (s *Server) handleSessionsStale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}

	threshold := time.Duration(0)
	if n := strconv.Itoa(0); r.URL.Query().Get("seconds") != "" {
		_ = n
		if v, err := strconv.Atoi(r.URL.Query().Get("seconds")); err == nil && v > 0 {
			threshold = time.Duration(v) * time.Second
		}
	}
	if threshold == 0 && s.cfg != nil && s.cfg.Session.StaleTimeoutSeconds > 0 {
		threshold = time.Duration(s.cfg.Session.StaleTimeoutSeconds) * time.Second
	}

	stale := session.ListStale(s.manager.ListSessions(), s.hostname, threshold, time.Now())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"threshold_seconds": int(threshold.Seconds()),
		"hostname":          s.hostname,
		"count":             len(stale),
		"sessions":          stale,
	})
}
