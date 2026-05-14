package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// handleSmokeProgress — GET /api/smoke/progress, DELETE /api/smoke/progress
//
// GET: returns the last smoke run state written by scripts/release-smoke.sh.
//   Returns 204 when no file exists yet.
// DELETE: removes the progress file so the dashboard shows a clean slate.
func (s *Server) handleSmokeProgress(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, "could not find home dir", http.StatusInternalServerError)
		return
	}
	p := filepath.Join(home, ".datawatch", "smoke-progress.json")

	switch r.Method {
	case http.MethodDelete:
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case http.MethodGet:
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var raw json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			http.Error(w, "smoke-progress.json is not valid JSON", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data) //nolint:errcheck

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
