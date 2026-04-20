// BL31 — device aliases.
//
//   GET    /api/device-aliases               list all aliases
//   POST   /api/device-aliases               body: {"alias": "<name>", "server": "<remote-name>"}
//   DELETE /api/device-aliases/{alias}       remove
//
// Aliases are operator-friendly shortcuts that the router resolves
// in `new: @<alias>: <task>` to the matching `servers:` entry.

package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

type deviceAlias struct {
	Alias  string `json:"alias"`
	Server string `json:"server"`
}

func (s *Server) handleDeviceAliases(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || s.cfgPath == "" {
		http.Error(w, "config not available", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/device-aliases")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		out := make([]deviceAlias, 0, len(s.cfg.Session.DeviceAliases))
		for k, v := range s.cfg.Session.DeviceAliases {
			out = append(out, deviceAlias{Alias: k, Server: v})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Alias < out[j].Alias })
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	case rest == "" && r.Method == http.MethodPost:
		var req deviceAlias
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		req.Alias = strings.TrimSpace(req.Alias)
		req.Server = strings.TrimSpace(req.Server)
		if req.Alias == "" || req.Server == "" {
			http.Error(w, "alias and server required", http.StatusBadRequest)
			return
		}
		if s.cfg.Session.DeviceAliases == nil {
			s.cfg.Session.DeviceAliases = map[string]string{}
		}
		s.cfg.Session.DeviceAliases[req.Alias] = req.Server
		if err := config.Save(s.cfg, s.cfgPath); err != nil {
			http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "alias": req.Alias})
	case rest != "" && r.Method == http.MethodDelete:
		if _, ok := s.cfg.Session.DeviceAliases[rest]; !ok {
			http.Error(w, "alias not found", http.StatusNotFound)
			return
		}
		delete(s.cfg.Session.DeviceAliases, rest)
		if err := config.Save(s.cfg, s.cfgPath); err != nil {
			http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "deleted", "alias": rest})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
