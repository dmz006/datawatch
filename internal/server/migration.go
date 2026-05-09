// v7.0.0-alpha.15 (#229) — surface the v7-migration result so the PWA
// can render a one-time info toast on next load.
//
// GET  /api/migration/status              → reads ~/.datawatch/v7-migration-status.json
// DELETE /api/migration/status            → operator dismisses ("don't show again")
//
// Operator-spec'd 2026-05-09 (Q3 of plan): "BOTH PWA toast AND a
// docs/howto/v7-compute-migration.md walkthrough linked from the
// toast". Toast suppression is server-side via DELETE — the PWA
// reads localStorage too as a belt-and-suspenders client-side
// dismiss, but the daemon source-of-truth ensures the toast only
// shows after a real migration ran.

package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleMigrationStatus(w http.ResponseWriter, r *http.Request) {
	dataDir := ""
	if s.cfg != nil {
		dataDir = s.cfg.DataDir
	}
	if dataDir == "" {
		dataDir = "."
	}
	// Expand ~ if present (cfg may carry user-relative path).
	if strings.HasPrefix(dataDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dataDir = filepath.Join(home, dataDir[2:])
		}
	}
	path := filepath.Join(dataDir, "v7-migration-status.json")
	switch r.Method {
	case http.MethodGet:
		b, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				writeJSONOK(w, map[string]any{"migrated": []string{}, "show": false})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		doc["show"] = true
		writeJSONOK(w, doc)
	case http.MethodDelete:
		_ = os.Remove(path) // suppress further notice
		writeJSONOK(w, map[string]any{"dismissed": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
