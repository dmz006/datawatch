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

	"github.com/dmz006/datawatch/internal/config"
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

// handleCostRates exposes per-backend rates for read + override.
//
//   GET /api/cost/rates                  current effective rates
//   PUT /api/cost/rates                  body: {"rates": {backend: {in_per_k, out_per_k}}}
func (s *Server) handleCostRates(w http.ResponseWriter, r *http.Request) {
	if s.manager == nil {
		http.Error(w, "manager not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		// Resolve the effective table (defaults if operator hasn't overridden).
		out := map[string]session.CostRate{}
		// Show what the manager would use for known backends.
		for name := range session.DefaultCostRates() {
			if r, ok := s.exposedRate(name); ok {
				out[name] = r
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"rates": out})
	case http.MethodPut:
		var req struct {
			Rates map[string]session.CostRate `json:"rates"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.manager.SetCostRates(req.Rates)
		// Persist to config.
		if s.cfg != nil && s.cfgPath != "" {
			s.cfg.Session.CostRates = map[string]config.CostRateConfig{}
			for name, r := range req.Rates {
				s.cfg.Session.CostRates[name] = config.CostRateConfig{
					InPerK: r.InPerK, OutPerK: r.OutPerK,
				}
			}
			_ = config.Save(s.cfg, s.cfgPath)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// exposedRate returns the effective rate the manager will use for a
// given backend name. Pulled out for testability.
func (s *Server) exposedRate(backend string) (session.CostRate, bool) {
	// Reuse the manager's family fallback by AddUsage's helper —
	// but AddUsage modifies state. We re-implement the lookup here.
	if s.cfg != nil && len(s.cfg.Session.CostRates) > 0 {
		if r, ok := s.cfg.Session.CostRates[backend]; ok {
			return session.CostRate{InPerK: r.InPerK, OutPerK: r.OutPerK}, true
		}
	}
	r, ok := session.DefaultCostRates()[backend]
	return r, ok
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
