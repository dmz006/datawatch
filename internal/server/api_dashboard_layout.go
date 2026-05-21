package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dmz006/datawatch/internal/federation"
)


func (s *Server) dashLayoutPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".datawatch", "dashboard-layout.json")
}

func (s *Server) handleDashboardLayout(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapDashboardRead) {
			return
		}
		layout, err := s.readDashLayout()
		if err != nil {
			http.Error(w, "read layout: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, layout)
	case http.MethodPut:
		if !s.fedCap(w, r, federation.CapDashboardWrite) {
			return
		}
		var incoming dashLayout
		if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		// Merge system cards back in so a bulk PUT can never wipe them.
		incoming.Cards = mergeSystemCards(incoming.Cards)
		if err := s.writeDashLayout(incoming); err != nil {
			http.Error(w, "write failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
