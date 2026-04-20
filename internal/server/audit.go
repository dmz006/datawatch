// BL9 — operator audit log REST.
//
//   GET /api/audit?since=<RFC3339>&actor=&action=&session_id=&limit=N
//
// Returns audit-log entries newest-first. Without a query, returns
// the most recent 100 entries. Filtering happens server-side; the
// log file is full-scanned (file is line-oriented JSON).

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/dmz006/datawatch/internal/audit"
)

// SetAuditLog wires the operator audit log used by /api/audit.
func (s *Server) SetAuditLog(l *audit.Log) { s.auditLog = l }

// AuditLog returns the wired log (nil if disabled).
func (s *Server) AuditLog() *audit.Log { return s.auditLog }

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.auditLog == nil {
		http.Error(w, "audit log not enabled", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	filter := audit.QueryFilter{
		Actor:     q.Get("actor"),
		Action:    q.Get("action"),
		SessionID: q.Get("session_id"),
		Limit:     100,
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if v := q.Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.Since = t
		}
	}
	if v := q.Get("until"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.Until = t
		}
	}
	entries, err := s.auditLog.Read(filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"count":   len(entries),
		"entries": entries,
	})
}
