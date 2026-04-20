// BL33 — REST surface for the subprocess plugin framework.
//
// Endpoints (all bearer-authenticated):
//   GET    /api/plugins                    list discovered + status
//   POST   /api/plugins/reload             rescan dir
//   GET    /api/plugins/{name}             one plugin
//   POST   /api/plugins/{name}/enable
//   POST   /api/plugins/{name}/disable
//   POST   /api/plugins/{name}/test        body: {hook, payload}

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// PluginsAPI is the narrow interface the REST layer needs from
// internal/plugins.Registry. Defined here so server tests don't need
// to import the plugins package. The daemon wires a concrete
// implementation via SetPluginsAPI.
type PluginsAPI interface {
	List() []any
	Get(name string) (any, bool)
	Reload() error
	SetEnabled(name string, on bool) bool
	Test(ctx context.Context, name, hook string, payload map[string]any) (any, error)
}

// SetPluginsAPI — called from main.go.
func (s *Server) SetPluginsAPI(p PluginsAPI) { s.pluginsAPI = p }

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	if s.pluginsAPI == nil {
		http.Error(w, "plugins disabled (set plugins.enabled in config)", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/plugins")
	rest = strings.TrimPrefix(rest, "/")
	if rest == "" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSONOK(w, map[string]any{"plugins": s.pluginsAPI.List()})
		return
	}
	if rest == "reload" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.pluginsAPI.Reload(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok", "count": len(s.pluginsAPI.List())})
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
		p, ok := s.pluginsAPI.Get(name)
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, p)
	case "enable", "disable":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.pluginsAPI.SetEnabled(name, action == "enable") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, map[string]any{"status": "ok", "name": name, "action": action})
	case "test":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Hook    string         `json:"hook"`
			Payload map[string]any `json:"payload,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Hook == "" {
			http.Error(w, "hook required", http.StatusBadRequest)
			return
		}
		resp, err := s.pluginsAPI.Test(r.Context(), name, req.Hook, req.Payload)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, resp)
	default:
		http.Error(w, "unknown action: "+action, http.StatusBadRequest)
	}
}
