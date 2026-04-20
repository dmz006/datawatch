// BL20 — backend auto-selection routing rules.
//
// Pattern-driven backend dispatch on session start. The REST start
// handler applies the first matching rule before falling through to
// the request's explicit Backend or the manager default.
//
// Endpoints:
//   GET    /api/routing-rules              list rules
//   POST   /api/routing-rules              replace the entire ordered list
//                                          body: {"rules": [{pattern, backend, description?}]}
//   POST   /api/routing-rules/test         test which backend a task would route to
//                                          body: {"task": "..."}
//                                          response: {"matched": <bool>, "backend": "..."}

package server

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/dmz006/datawatch/internal/config"
)

// MatchRoutingRule returns the first rule whose pattern matches text,
// or nil if no rule matches. Invalid regexes are skipped silently.
func MatchRoutingRule(rules []config.RoutingRule, text string) *config.RoutingRule {
	for i := range rules {
		re, err := regexp.Compile(rules[i].Pattern)
		if err != nil {
			continue
		}
		if re.MatchString(text) {
			return &rules[i]
		}
	}
	return nil
}

func (s *Server) handleRoutingRules(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || s.cfgPath == "" {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"rules": s.cfg.Session.RoutingRules})
	case http.MethodPost:
		var req struct {
			Rules []config.RoutingRule `json:"rules"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Validate every regex up-front so a typo doesn't strand the operator.
		for _, rule := range req.Rules {
			if _, err := regexp.Compile(rule.Pattern); err != nil {
				http.Error(w,
					"invalid regex in rule "+rule.Pattern+": "+err.Error(),
					http.StatusBadRequest)
				return
			}
		}
		s.cfg.Session.RoutingRules = req.Rules
		if err := config.Save(s.cfg, s.cfgPath); err != nil {
			http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok", "count": len(req.Rules),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRoutingRulesTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Task string `json:"task"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	matched := MatchRoutingRule(s.cfg.Session.RoutingRules, req.Task)
	out := map[string]any{"matched": matched != nil}
	if matched != nil {
		out["backend"] = matched.Backend
		out["pattern"] = matched.Pattern
		if matched.Description != "" {
			out["description"] = matched.Description
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
