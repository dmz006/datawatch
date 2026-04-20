// BL27 — project management endpoints.
//
//   GET    /api/projects           list all
//   POST   /api/projects           add or update one
//                                  body: {name, dir, default_backend?, description?}
//   DELETE /api/projects/{name}    remove
//   GET    /api/projects/{name}    fetch one
//
// Projects are an operator-registered alias from name → directory
// (and optional default backend). The session-start path can resolve
// `project=<name>` instead of repeating `project_dir=<abs>` every
// invocation.

package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// projectListItem flattens map → response array entry.
type projectListItem struct {
	Name           string `json:"name"`
	Dir            string `json:"dir"`
	DefaultBackend string `json:"default_backend,omitempty"`
	Description    string `json:"description,omitempty"`
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || s.cfgPath == "" {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/projects")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.listProjects(w)
	case rest == "" && r.Method == http.MethodPost:
		s.upsertProject(w, r)
	case rest != "" && r.Method == http.MethodGet:
		s.getProject(w, rest)
	case rest != "" && r.Method == http.MethodDelete:
		s.deleteProject(w, rest)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listProjects(w http.ResponseWriter) {
	out := make([]projectListItem, 0, len(s.cfg.Projects))
	for name, entry := range s.cfg.Projects {
		out = append(out, projectListItem{
			Name: name, Dir: entry.Dir,
			DefaultBackend: entry.DefaultBackend,
			Description:    entry.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) getProject(w http.ResponseWriter, name string) {
	entry, ok := s.cfg.Projects[name]
	if !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(projectListItem{
		Name: name, Dir: entry.Dir,
		DefaultBackend: entry.DefaultBackend,
		Description:    entry.Description,
	})
}

func (s *Server) upsertProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		Dir            string `json:"dir"`
		DefaultBackend string `json:"default_backend,omitempty"`
		Description    string `json:"description,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Dir = strings.TrimSpace(req.Dir)
	if req.Name == "" || req.Dir == "" {
		http.Error(w, "name and dir required", http.StatusBadRequest)
		return
	}
	if !filepath.IsAbs(req.Dir) {
		http.Error(w, "dir must be absolute", http.StatusBadRequest)
		return
	}
	if s.cfg.Projects == nil {
		s.cfg.Projects = map[string]config.ProjectConfigEntry{}
	}
	s.cfg.Projects[req.Name] = config.ProjectConfigEntry{
		Dir: req.Dir, DefaultBackend: req.DefaultBackend, Description: req.Description,
	}
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": req.Name})
}

func (s *Server) deleteProject(w http.ResponseWriter, name string) {
	if _, ok := s.cfg.Projects[name]; !ok {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	delete(s.cfg.Projects, name)
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "name": name})
}
