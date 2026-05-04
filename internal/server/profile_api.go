// Package server — profile REST handlers (F10 sprint 2 story S2.2).
//
// Exposes Project + Cluster profile CRUD + smoke under /api/profiles/.
// All handlers return JSON; auth is enforced by the outer mux wrapper
// the same way the rest of the API is.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/profile"
)

// SetProjectStore wires the Project Profile store. Pass nil to leave
// handlers in 503 mode (tests do this to skip profile state).
func (s *Server) SetProjectStore(p *profile.ProjectStore) { s.projectStore = p }

// SetClusterStore mirrors SetProjectStore for Cluster Profiles.
func (s *Server) SetClusterStore(c *profile.ClusterStore) { s.clusterStore = c }

// ── URL layout ──────────────────────────────────────────────────────
//
//   /api/profiles/projects                 GET   list, POST create
//   /api/profiles/projects/{name}          GET   read, PUT update, DELETE
//   /api/profiles/projects/{name}/smoke    POST  dry-run validation
//
//   /api/profiles/clusters                 (same shape)
//
// Sub-path dispatch lives in the Handle* funcs rather than full gorilla
// routing since our existing mux registers flat prefixes; we parse the
// tail after /api/profiles/projects/ ourselves.

// handleProjectProfiles dispatches /api/profiles/projects and
// /api/profiles/projects/{name}[/smoke] to the appropriate method.
func (s *Server) handleProjectProfiles(w http.ResponseWriter, r *http.Request) {
	if s.projectStore == nil {
		http.Error(w, "project profile store not available", http.StatusServiceUnavailable)
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/api/profiles/projects")
	tail = strings.Trim(tail, "/")

	// Collection
	if tail == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"profiles": s.projectStore.List(),
			})
		case http.MethodPost:
			s.createProjectProfile(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Named resource: parse {name}[/smoke | /agent-settings]
	parts := strings.SplitN(tail, "/", 2)
	name := parts[0]
	if len(parts) == 2 {
		switch parts[1] {
		case "smoke":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.smokeProjectProfile(w, r, name)
		case "agent-settings":
			// BL251 — PATCH /api/profiles/projects/{name}/agent-settings
			// Updates only the AgentSettings block without touching other fields.
			if r.Method != http.MethodPatch {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.patchProjectAgentSettings(w, r, name)
		default:
			http.Error(w, "unknown subpath", http.StatusNotFound)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.readProjectProfile(w, r, name)
	case http.MethodPut:
		s.updateProjectProfile(w, r, name)
	case http.MethodDelete:
		s.deleteProjectProfile(w, r, name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) createProjectProfile(w http.ResponseWriter, r *http.Request) {
	var p profile.ProjectProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if err := s.projectStore.Create(&p); err != nil {
		writeProfileErr(w, err)
		return
	}
	saved, _ := s.projectStore.Get(p.Name) // includes stamped timestamps
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) readProjectProfile(w http.ResponseWriter, _ *http.Request, name string) {
	p, err := s.projectStore.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateProjectProfile(w http.ResponseWriter, r *http.Request, name string) {
	var p profile.ProjectProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	// Force the name from the URL so body-url mismatch can't silently
	// overwrite a different profile.
	p.Name = name
	if err := s.projectStore.Update(&p); err != nil {
		writeProfileErr(w, err)
		return
	}
	saved, _ := s.projectStore.Get(name)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteProjectProfile(w http.ResponseWriter, _ *http.Request, name string) {
	if err := s.projectStore.Delete(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) smokeProjectProfile(w http.ResponseWriter, _ *http.Request, name string) {
	r, err := s.projectStore.Smoke(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	status := http.StatusOK
	if !r.Passed() {
		// 422 so CI scripts can distinguish "not found" (404) from
		// "profile exists but has validation errors" (422).
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, r)
}

// ── Cluster profile handlers ────────────────────────────────────────────

func (s *Server) handleClusterProfiles(w http.ResponseWriter, r *http.Request) {
	if s.clusterStore == nil {
		http.Error(w, "cluster profile store not available", http.StatusServiceUnavailable)
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/api/profiles/clusters")
	tail = strings.Trim(tail, "/")

	if tail == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"profiles": s.clusterStore.List(),
			})
		case http.MethodPost:
			s.createClusterProfile(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	parts := strings.SplitN(tail, "/", 2)
	name := parts[0]
	if len(parts) == 2 && parts[1] == "smoke" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.smokeClusterProfile(w, r, name)
		return
	}
	if len(parts) > 1 {
		http.Error(w, "unknown subpath", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.readClusterProfile(w, r, name)
	case http.MethodPut:
		s.updateClusterProfile(w, r, name)
	case http.MethodDelete:
		s.deleteClusterProfile(w, r, name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) createClusterProfile(w http.ResponseWriter, r *http.Request) {
	var c profile.ClusterProfile
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if err := s.clusterStore.Create(&c); err != nil {
		writeProfileErr(w, err)
		return
	}
	saved, _ := s.clusterStore.Get(c.Name)
	writeJSON(w, http.StatusCreated, saved)
}

func (s *Server) readClusterProfile(w http.ResponseWriter, _ *http.Request, name string) {
	p, err := s.clusterStore.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateClusterProfile(w http.ResponseWriter, r *http.Request, name string) {
	var c profile.ClusterProfile
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	c.Name = name
	if err := s.clusterStore.Update(&c); err != nil {
		writeProfileErr(w, err)
		return
	}
	saved, _ := s.clusterStore.Get(name)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteClusterProfile(w http.ResponseWriter, _ *http.Request, name string) {
	if err := s.clusterStore.Delete(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) smokeClusterProfile(w http.ResponseWriter, _ *http.Request, name string) {
	r, err := s.clusterStore.Smoke(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	status := http.StatusOK
	if !r.Passed() {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, r)
}

// ── helpers ─────────────────────────────────────────────────────────────

// writeJSON writes v as JSON with the given status code. Logs encode
// errors but doesn't try to surface them to the client (body already
// sent by the time encoder fails).
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeProfileErr maps Create/Update errors to HTTP status codes —
// "already exists" → 409, "not found" → 404, "invalid ..." → 400,
// everything else → 500.
func writeProfileErr(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "already exists"):
		http.Error(w, msg, http.StatusConflict)
	case strings.Contains(msg, "not found"):
		http.Error(w, msg, http.StatusNotFound)
	case strings.HasPrefix(msg, "invalid project profile") ||
		strings.HasPrefix(msg, "invalid cluster profile"):
		http.Error(w, msg, http.StatusBadRequest)
	default:
		http.Error(w, msg, http.StatusInternalServerError)
	}
}

// patchProjectAgentSettings — PATCH /api/profiles/projects/{name}/agent-settings
// Updates only the AgentSettings block on a ProjectProfile (BL251).
// Body: {"claude_auth_key_secret":"...","opencode_ollama_url":"...","opencode_model":"..."}
// Omitted fields are cleared (set to ""). To leave a field unchanged, include
// it with its current value.
func (s *Server) patchProjectAgentSettings(w http.ResponseWriter, r *http.Request, name string) {
	existing, err := s.projectStore.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	var as profile.AgentSettings
	if err := json.NewDecoder(r.Body).Decode(&as); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	existing.AgentSettings = as
	if err := s.projectStore.Update(existing); err != nil {
		writeProfileErr(w, err)
		return
	}
	saved, _ := s.projectStore.Get(name)
	writeJSON(w, http.StatusOK, saved)
}
