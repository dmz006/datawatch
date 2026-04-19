// BL107 — REST handler for the agent audit trail query.

package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/dmz006/datawatch/internal/agents"
)

// handleAgentAudit serves GET /api/agents/audit with optional
// ?event=, ?agent_id=, ?project=, and ?limit= filters. Returns JSON
// {events: [...], path: "..."}. Refuses when no audit file is
// configured (file IO disabled) or when the active file is CEF
// (queryable via the operator's SIEM, not via this handler).
func (s *Server) handleAgentAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.agentAuditPath == "" {
		http.Error(w, "agent audit not enabled", http.StatusServiceUnavailable)
		return
	}
	if s.agentAuditCEF {
		http.Error(w,
			"agent audit file is CEF-formatted; query your SIEM instead",
			http.StatusNotImplemented)
		return
	}

	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	filter := agents.ReadEventsFilter{
		Event:   r.URL.Query().Get("event"),
		AgentID: r.URL.Query().Get("agent_id"),
		Project: r.URL.Query().Get("project"),
	}

	events, err := agents.ReadEvents(s.agentAuditPath, filter, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"path":   s.agentAuditPath,
		"events": events,
	})
}
