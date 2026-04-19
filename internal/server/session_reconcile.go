package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
)

// handleSessionReconcile (BL93) — POST /api/sessions/reconcile
//
// Body: {"auto_import": bool}
// Response: ReconcileResult JSON (imported, orphaned, errors).
//
// Dry-run by default (auto_import=false) so an operator can inspect
// what the daemon would import before mutating the registry.
func (s *Server) handleSessionReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AutoImport bool `json:"auto_import"`
	}
	// Body is optional — empty body = dry run.
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	res, err := s.manager.ReconcileSessions(req.AutoImport)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.AutoImport && len(res.Imported) > 0 {
		go s.hub.BroadcastSessions(s.manager.ListSessions())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

// handleSessionImport (BL94) — POST /api/sessions/import
//
// Body: {"dir": "<absolute or session-id path>"}
// If dir is a bare session ID it is resolved under
// <dataDir>/sessions/<id>. Returns the imported session record.
func (s *Server) handleSessionImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Dir string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Dir == "" {
		http.Error(w, "dir is required", http.StatusBadRequest)
		return
	}
	dir := req.Dir
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(s.manager.DataDir(), "sessions", dir)
	}
	sess, imported, err := s.manager.ImportSessionDir(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if imported {
		go s.hub.BroadcastSessions(s.manager.ListSessions())
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"imported": imported,
		"session":  sess,
	})
}
