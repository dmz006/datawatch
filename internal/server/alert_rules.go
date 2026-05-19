// S14b — REST surface for per-pod alert rules + observer-driven autoscaling.
//
// Endpoints (all bearer-authenticated):
//   GET    /api/alert-rules              list all rules
//   POST   /api/alert-rules              create a rule
//   GET    /api/alert-rules/{name}       get one rule
//   PUT    /api/alert-rules/{name}       update a rule
//   DELETE /api/alert-rules/{name}       delete a rule
//   POST   /api/alert-rules/{name}/enable
//   POST   /api/alert-rules/{name}/disable
//   GET    /api/alert-rules/firings      recent firings (last 100)

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/alertrules"
	"github.com/dmz006/datawatch/internal/federation"
)

// AlertRulesAPI is the narrow interface the REST layer needs from
// internal/alertrules.Store. Defined here so server tests don't need
// to import the alertrules package.
type AlertRulesAPI interface {
	List() []alertrules.AlertRule
	Get(name string) (alertrules.AlertRule, bool)
	Add(r alertrules.AlertRule) error
	Update(r alertrules.AlertRule) error
	Delete(name string) bool
	SetEnabled(name string, on bool) bool
	Firings() []alertrules.Firing
}

// SetAlertRulesAPI wires the alert-rules store. Called from main.go.
func (s *Server) SetAlertRulesAPI(a AlertRulesAPI) { s.alertRulesAPI = a }

func (s *Server) handleAlertRules(w http.ResponseWriter, r *http.Request) {
	if s.alertRulesAPI == nil {
		http.Error(w, "alert-rules not available", http.StatusServiceUnavailable)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/alert-rules")
	rest = strings.TrimPrefix(rest, "/")

	// Special top-level paths first.
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			if !s.fedCap(w, r, federation.CapConfigRead) {
				return
			}
			writeJSONOK(w, map[string]any{"rules": s.alertRulesAPI.List()})
		case http.MethodPost:
			if !s.fedCap(w, r, federation.CapConfigWrite) {
				return
			}
			var rule alertrules.AlertRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			if rule.Name == "" {
				http.Error(w, "name required", http.StatusBadRequest)
				return
			}
			if err := s.alertRulesAPI.Add(rule); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			writeJSONOK(w, map[string]any{"status": "ok", "name": rule.Name})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// GET /api/alert-rules/firings
	if rest == "firings" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		writeJSONOK(w, map[string]any{"firings": s.alertRulesAPI.Firings()})
		return
	}

	// /{name}[/action]
	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			if !s.fedCap(w, r, federation.CapConfigRead) {
				return
			}
			rule, ok := s.alertRulesAPI.Get(name)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSONOK(w, rule)
		case http.MethodPut:
			if !s.fedCap(w, r, federation.CapConfigWrite) {
				return
			}
			var rule alertrules.AlertRule
			if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			rule.Name = name // enforce URL name
			if err := s.alertRulesAPI.Update(rule); err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSONOK(w, map[string]any{"status": "ok", "name": name})
		case http.MethodDelete:
			if !s.fedCap(w, r, federation.CapConfigWrite) {
				return
			}
			if !s.alertRulesAPI.Delete(name) {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSONOK(w, map[string]any{"status": "ok", "name": name})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

	case "enable", "disable":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.fedCap(w, r, federation.CapConfigWrite) {
			return
		}
		if !s.alertRulesAPI.SetEnabled(name, action == "enable") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok", "name": name, "action": action})

	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
	}
}
