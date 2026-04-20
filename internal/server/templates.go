// BL5 — session template CRUD.
//
//   GET    /api/templates           list all
//   POST   /api/templates           upsert (body: {name, ...template fields})
//   GET    /api/templates/{name}    fetch one
//   DELETE /api/templates/{name}    remove

package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

type templateListItem struct {
	Name           string            `json:"name"`
	ProjectDir     string            `json:"project_dir,omitempty"`
	Backend        string            `json:"backend,omitempty"`
	Profile        string            `json:"profile,omitempty"`
	Effort         string            `json:"effort,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	AutoGitCommit  *bool             `json:"auto_git_commit,omitempty"`
	AutoGitInit    *bool             `json:"auto_git_init,omitempty"`
	Description    string            `json:"description,omitempty"`
}

func (s *Server) handleTemplates(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || s.cfgPath == "" {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/templates")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.listTemplates(w)
	case rest == "" && r.Method == http.MethodPost:
		s.upsertTemplate(w, r)
	case rest != "" && r.Method == http.MethodGet:
		s.getTemplate(w, rest)
	case rest != "" && r.Method == http.MethodDelete:
		s.deleteTemplate(w, rest)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listTemplates(w http.ResponseWriter) {
	out := make([]templateListItem, 0, len(s.cfg.Templates))
	for name, t := range s.cfg.Templates {
		out = append(out, toTemplateItem(name, t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) getTemplate(w http.ResponseWriter, name string) {
	t, ok := s.cfg.Templates[name]
	if !ok {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(toTemplateItem(name, t))
}

func (s *Server) upsertTemplate(w http.ResponseWriter, r *http.Request) {
	var req templateListItem
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if s.cfg.Templates == nil {
		s.cfg.Templates = map[string]config.SessionTemplateEntry{}
	}
	s.cfg.Templates[req.Name] = config.SessionTemplateEntry{
		ProjectDir: req.ProjectDir, Backend: req.Backend, Profile: req.Profile,
		Effort: req.Effort, Env: req.Env,
		AutoGitCommit: req.AutoGitCommit, AutoGitInit: req.AutoGitInit,
		Description: req.Description,
	}
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": req.Name})
}

func (s *Server) deleteTemplate(w http.ResponseWriter, name string) {
	if _, ok := s.cfg.Templates[name]; !ok {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}
	delete(s.cfg.Templates, name)
	if err := config.Save(s.cfg, s.cfgPath); err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "name": name})
}

func toTemplateItem(name string, t config.SessionTemplateEntry) templateListItem {
	return templateListItem{
		Name: name, ProjectDir: t.ProjectDir, Backend: t.Backend,
		Profile: t.Profile, Effort: t.Effort, Env: t.Env,
		AutoGitCommit: t.AutoGitCommit, AutoGitInit: t.AutoGitInit,
		Description: t.Description,
	}
}
