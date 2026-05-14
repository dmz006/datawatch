package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) dashLayoutPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".datawatch", "dashboard-layout.json")
}

func (s *Server) handleDashboardLayout(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		data, err := os.ReadFile(s.dashLayoutPath())
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{}"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	case http.MethodPut:
		var body json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		path := s.dashLayoutPath()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			http.Error(w, "mkdir failed", http.StatusInternalServerError)
			return
		}
		out, _ := json.MarshalIndent(body, "", "  ")
		if err := os.WriteFile(path, out, 0o600); err != nil {
			http.Error(w, "write failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
