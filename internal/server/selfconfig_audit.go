// BL412 — read view over the self-config audit trail.
//
//   GET /api/audit/self-config?limit=N
//
// Lists every attempt that went through the self-config path — the MCP
// config-write tools, the session/peer REST config PUTs and the comm
// `configure` command — as recorded by the shared audit point
// (federation.ConfigAuditor, see internal/federation/config_audit.go).
// Newest-first; an empty list when nothing has been recorded yet.
// Read-only and capability-gated like /api/audit (CapAuditRead); the
// audit trail itself is written by the audit point, not this handler.

package server

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
)

const (
	selfConfigAuditDefaultLimit = 100
	selfConfigAuditMaxLimit     = 1000
)

// selfConfigAuditPath resolves the JSONL file the self-config attempts
// are appended to: the running audit sink's path first (the daemon
// resolves its default there at startup), the configured value second,
// "" when neither exists (nothing recorded, nothing to read).
func (s *Server) selfConfigAuditPath() string {
	if p := s.configAudit.Path(); p != "" {
		return p
	}
	return strings.TrimSpace(s.cfg.MCP.SelfConfigAuditPath)
}

// handleSelfConfigAudit is the self-config audit log view. It only
// reads: the shared audit point (federation.ConfigAuditor) is the sole
// writer, and a missing file or a corrupt line simply means "less to
// show", never a 5xx.
func (s *Server) handleSelfConfigAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapAuditRead) {
		return
	}
	limit := selfConfigAuditDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		if n > selfConfigAuditMaxLimit {
			n = selfConfigAuditMaxLimit
		}
		limit = n
	}
	path := s.selfConfigAuditPath()
	all := make([]federation.ConfigAuditRecord, 0, limit)
	if path != "" {
		if f, err := os.Open(path); err == nil {
			sc := bufio.NewScanner(f)
			sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" {
					continue
				}
				var rec federation.ConfigAuditRecord
				if json.Unmarshal([]byte(line), &rec) != nil {
					continue // corrupt line: skip, never fail the view
				}
				all = append(all, rec)
			}
			_ = f.Close() //nolint:errcheck
		}
	}
	// The sink appends chronologically — reverse for newest-first, then cap.
	recs := make([]federation.ConfigAuditRecord, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		recs = append(recs, all[i])
	}
	if len(recs) > limit {
		recs = recs[:limit]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"count":   len(recs),
		"entries": recs,
		"path":    path,
	})
}
