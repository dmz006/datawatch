// internal/server/skills.go — REST surface for BL255 v6.7.0 skill registries.
//
// Routes:
//   GET    /api/skills/registries                    — list registries
//   POST   /api/skills/registries                    — create
//   GET    /api/skills/registries/{name}             — get one
//   PUT    /api/skills/registries/{name}             — update
//   DELETE /api/skills/registries/{name}             — delete (cascades synced)
//   POST   /api/skills/registries/{name}/connect     — shallow clone + browse
//   GET    /api/skills/registries/{name}/available   — list cached available
//   POST   /api/skills/registries/{name}/sync        — sync selected/all
//   POST   /api/skills/registries/{name}/unsync      — unsync selected/all
//   POST   /api/skills/registries/add-default        — idempotent PAI default
//   GET    /api/skills                               — list synced (across registries)
//   GET    /api/skills/{name}                        — get synced manifest + content
//   GET    /api/skills/{name}/content                — load skill markdown (option D)
//
// All write paths emit audit entries via s.auditLog if configured.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/skills"
)

// skillsManager is the interface server-side code uses to talk to the
// skills package. The concrete *skills.Manager satisfies it.
type skillsManager interface {
	Store() *skills.Store
	AddDefault() error
	Connect(name string) ([]*skills.AvailableSkill, error)
	Browse(name string) ([]*skills.AvailableSkill, error)
	Sync(registry string, names []string) ([]*skills.Synced, error)
	Unsync(registry string, names []string) ([]string, error)
	LoadSkillContent(name string) (string, error)
}

// SetSkillsManager wires the runtime skills.Manager into the server.
// Cmd/datawatch/main.go calls this after constructing both.
func (s *Server) SetSkillsManager(m skillsManagerImpl) { s.skillsMgr = m }

// skillsManagerImpl is the local adapter type; matches *skills.Manager.
// Wrapping lets us add a Store() accessor without touching the package.
type skillsManagerImpl interface {
	skillsManager
}

// Provide an adapter so *skills.Manager satisfies skillsManager. The
// real *skills.Manager exposes Store as a public field, not method;
// add the method on a thin wrapper.
//
// Used in cmd/datawatch/main.go: httpServer.SetSkillsManager(skillsManagerAdapter{m: skillsMgr})

// SkillsManagerAdapter wraps *skills.Manager so it satisfies the
// server-side skillsManager interface. Public so cmd/datawatch can use it.
type SkillsManagerAdapter struct{ M *skills.Manager }

// Store returns the underlying store.
func (a SkillsManagerAdapter) Store() *skills.Store { return a.M.Store }

// AddDefault forwards.
func (a SkillsManagerAdapter) AddDefault() error { return a.M.AddDefault() }

// Connect forwards.
func (a SkillsManagerAdapter) Connect(name string) ([]*skills.AvailableSkill, error) {
	return a.M.Connect(name)
}

// Browse forwards.
func (a SkillsManagerAdapter) Browse(name string) ([]*skills.AvailableSkill, error) {
	return a.M.Browse(name)
}

// Sync forwards.
func (a SkillsManagerAdapter) Sync(registry string, names []string) ([]*skills.Synced, error) {
	return a.M.Sync(registry, names)
}

// Unsync forwards.
func (a SkillsManagerAdapter) Unsync(registry string, names []string) ([]string, error) {
	return a.M.Unsync(registry, names)
}

// LoadSkillContent forwards.
func (a SkillsManagerAdapter) LoadSkillContent(name string) (string, error) {
	return a.M.LoadSkillContent(name)
}

// ── handlers ────────────────────────────────────────────────────────────

func (s *Server) handleSkillsRegistries(w http.ResponseWriter, r *http.Request) {
	if s.skillsMgr == nil {
		http.Error(w, "skills disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/skills/registries")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			writeJSONOK(w, map[string]any{"registries": s.skillsMgr.Store().ListRegistries()})
		case http.MethodPost:
			var req skills.Registry
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			if err := s.skillsMgr.Store().CreateRegistry(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit("skills_registry_create", "registry", req.Name, nil)
			writeJSONOK(w, req)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if rest == "add-default" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.skillsMgr.AddDefault(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.audit("skills_registry_add_default", "registry", skills.PAIDefaultRegistry.Name, nil)
		writeJSONOK(w, map[string]any{"status": "ok", "name": skills.PAIDefaultRegistry.Name})
		return
	}

	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch action {
	case "":
		switch r.Method {
		case http.MethodGet:
			reg, ok := s.skillsMgr.Store().GetRegistry(name)
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			writeJSONOK(w, reg)
		case http.MethodPut:
			var req skills.Registry
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
				return
			}
			req.Name = name
			if err := s.skillsMgr.Store().UpdateRegistry(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit("skills_registry_update", "registry", name, nil)
			writeJSONOK(w, req)
		case http.MethodDelete:
			n, err := s.skillsMgr.Store().DeleteRegistry(name)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.audit("skills_registry_delete", "registry", name, map[string]any{"removed_synced": n})
			writeJSONOK(w, map[string]any{"status": "deleted", "name": name, "removed_synced": n})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return

	case "connect":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		avail, err := s.skillsMgr.Connect(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit("skills_registry_connect", "registry", name, map[string]any{"available_count": len(avail)})
		writeJSONOK(w, map[string]any{"available": avail})

	case "available":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		avail, err := s.skillsMgr.Browse(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSONOK(w, map[string]any{"available": avail})

	case "sync":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Skills []string `json:"skills"`
			All    bool     `json:"all"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		names := req.Skills
		if req.All {
			names = []string{"*"}
		}
		out, err := s.skillsMgr.Sync(name, names)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit("skills_registry_sync", "registry", name, map[string]any{"synced_count": len(out)})
		writeJSONOK(w, map[string]any{"synced": out})

	case "unsync":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Skills []string `json:"skills"`
			All    bool     `json:"all"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		names := req.Skills
		if req.All {
			names = []string{"*"}
		}
		removed, err := s.skillsMgr.Unsync(name, names)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.audit("skills_registry_unsync", "registry", name, map[string]any{"removed_count": len(removed)})
		writeJSONOK(w, map[string]any{"removed": removed})

	default:
		http.Error(w, "unknown action: "+action, http.StatusNotFound)
	}
}

// handleSkills serves the synced-skills surface (separate from the
// /registries CRUD).
//
//   GET /api/skills                  — list all synced
//   GET /api/skills/{name}           — get manifest + path
//   GET /api/skills/{name}/content   — load markdown
func (s *Server) handleSkills(w http.ResponseWriter, r *http.Request) {
	if s.skillsMgr == nil {
		http.Error(w, "skills disabled", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/skills")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSONOK(w, map[string]any{"skills": s.skillsMgr.Store().ListSynced("")})
		return
	}

	parts := strings.SplitN(rest, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch action {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sk, ok := s.skillsMgr.Store().GetSynced("", name)
		if !ok {
			http.Error(w, "not synced", http.StatusNotFound)
			return
		}
		writeJSONOK(w, sk)

	case "content":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := s.skillsMgr.LoadSkillContent(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(body))

	default:
		http.Error(w, "unknown action: "+action, http.StatusNotFound)
	}
}

// audit writes an audit entry if the audit log is configured.
// Mirrors the pattern used elsewhere in the server package.
func (s *Server) audit(action, resourceType, resourceID string, details map[string]any) {
	if s.auditLog == nil {
		return
	}
	d := details
	if d == nil {
		d = map[string]any{}
	}
	d["resource_type"] = resourceType
	d["resource_id"] = resourceID
	_ = s.auditLog.Write(audit.Entry{Action: action, Actor: "operator", Details: d})
}
