// BL9 — operator audit log REST.
//
//   GET /api/audit?actor=&action=&subject=&session_id=&since=<RFC3339>&until=<RFC3339>&limit=N
//
// Returns audit-log entries newest-first. Without a query, returns
// the most recent 100 entries. Filtering happens server-side; the
// log file is full-scanned (file is line-oriented JSON). action matches
// exactly, or by prefix when it ends in '*'. A since/until that is not
// RFC 3339 is a 400, never a silently dropped bound.

package server

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
)

// auditQueryParams are the query parameters /api/audit reads — the shared
// filter set every audit surface exposes.
var auditQueryParams = audit.QueryParamNames

// AuditQueryParams returns the query parameter names /api/audit accepts.
func AuditQueryParams() []string { return slices.Clone(auditQueryParams) }

// SetAuditLog wires the operator audit log used by /api/audit.
func (s *Server) SetAuditLog(l *audit.Log) { s.auditLog = l }

// AuditLog returns the wired log (nil if disabled).
func (s *Server) AuditLog() *audit.Log { return s.auditLog }

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAuditRead) {
		return
	}
	if s.auditLog == nil {
		http.Error(w, "audit log not enabled", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	filter, err := audit.ParseQuery(func(name string) string {
		if !slices.Contains(auditQueryParams, name) {
			return ""
		}
		return q.Get(name)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
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
