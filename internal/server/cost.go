// BL6 — cost summary REST.
//
//   GET  /api/cost                         aggregate across all sessions
//   GET  /api/cost?session=<full_id>       per-session breakdown
//   POST /api/cost/usage                   record usage delta
//                                          body: {session, tokens_in, tokens_out,
//                                                 in_per_k?, out_per_k?}

package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/session"
)

func (s *Server) handleCostSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}

	if id := r.URL.Query().Get("session"); id != "" {
		sess, ok := s.manager.GetSession(id)
		if !ok {
			http.Error(w, "session not found: "+id, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id":    sess.ID,
			"backend":       sess.LLMBackend,
			"tokens_in":     sess.TokensIn,
			"tokens_out":    sess.TokensOut,
			"est_cost_usd":  sess.EstCostUSD,
		})
		return
	}

	summary := session.SummaryFor(s.manager.ListSessions())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(summary)
}

func (s *Server) handleCostUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Session   string  `json:"session"`
		TokensIn  int     `json:"tokens_in"`
		TokensOut int     `json:"tokens_out"`
		InPerK    float64 `json:"in_per_k"`
		OutPerK   float64 `json:"out_per_k"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Session == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}
	rate := session.CostRate{InPerK: req.InPerK, OutPerK: req.OutPerK}
	if err := s.manager.AddUsage(req.Session, req.TokensIn, req.TokensOut, rate); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sess, _ := s.manager.GetSession(req.Session)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":       "ok",
		"session_id":   sess.ID,
		"tokens_in":    sess.TokensIn,
		"tokens_out":   sess.TokensOut,
		"est_cost_usd": sess.EstCostUSD,
	})
}
