// v4.0.3 — POST /api/backends/active. Small, dedicated wire shape
// for the mobile client's backend-picker radio list; closes
// https://github.com/dmz006/datawatch/issues/7. The same effect is
// reachable via PUT /api/config {"session.llm_backend": name} but
// that requires the caller to know the general config schema and
// round-trip through the broader reload path — this endpoint is a
// one-call "switch default backend" action.
//
// Does NOT restart the daemon or migrate existing sessions; running
// workers keep their backend until they exit. New sessions (from any
// channel) use the new default.

package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/config"
)

func (s *Server) handleBackendsActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg == nil || s.cfgPath == "" {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	// Validate the backend is in the registered set.
	valid := false
	for _, b := range s.availableBackends {
		if b == req.Name {
			valid = true
			break
		}
	}
	if !valid {
		http.Error(w, "unknown backend: "+req.Name, http.StatusBadRequest)
		return
	}
	s.cfg.Session.LLMBackend = req.Name
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Update the live Manager so new sessions pick the new backend
	// without requiring a full daemon restart. Name-only setter
	// preserves the launcher registry wired at startup.
	if s.manager != nil {
		s.manager.SetLLMBackendName(req.Name)
	}
	writeJSONOK(w, map[string]any{
		"status": "ok", "active": req.Name,
	})
}
