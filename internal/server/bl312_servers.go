// BL312 S1 — REST surface for the multi-server registry.
//
//	GET    /api/servers                → list all entries (builtins + runtime)
//	POST   /api/servers                → create a runtime entry
//	GET    /api/servers/{name}         → fetch one entry
//	PUT    /api/servers/{name}         → replace a runtime entry
//	DELETE /api/servers/{name}         → remove a runtime entry
//	POST   /api/servers/{name}/test    → ping the named server, return latency + version
//
// NOTE: The legacy /api/servers endpoint (handleListServers) is superseded
// by handleBL312Servers when the multiserver store is wired, but the old
// handler is kept for backward compat when the store is nil.

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// SetServerStore wires the runtime multi-server registry into the HTTP server.
// Called from cmd/datawatch/main.go after the store is initialised.
func (s *Server) SetServerStore(st *multiserver.Store) {
	s.serverStore = st
}

// SetFedGroupStore (BL316) wires the custom capability group store.
func (s *Server) SetFedGroupStore(gs *federation.GroupStore) {
	s.fedGroupStore = gs
}

// serverStore is a field on Server — added at the bottom of api.go via
// a struct embedding is not ideal; we keep it here and add it as a field
// in api.go directly.

// handleBL312Servers handles /api/servers (collection) and
// /api/servers/{name} and /api/servers/{name}/test.
func (s *Server) handleBL312Servers(w http.ResponseWriter, r *http.Request) {
	if s.serverStore == nil {
		// Fall through to the legacy static-list handler.
		s.handleListServers(w, r)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/servers")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "" && r.Method == http.MethodGet:
		writeJSONOK(w, map[string]any{"servers": s.serverStore.List()})

	case rest == "" && r.Method == http.MethodPost:
		var e multiserver.Entry
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.serverStore.Add(&e); err != nil {
			code := http.StatusBadRequest
			if err == multiserver.ErrConflict {
				code = http.StatusConflict
			}
			http.Error(w, err.Error(), code)
			return
		}
		writeJSONOK(w, map[string]any{"name": e.Name, "ok": true})

	case strings.HasSuffix(rest, "/test") && r.Method == http.MethodPost:
		name := strings.TrimSuffix(rest, "/test")
		latency, version, err := s.serverStore.Test(r.Context(), name)
		if err != nil {
			if err == multiserver.ErrNotFound {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSONOK(w, map[string]any{
				"ok":         false,
				"latency_ms": latency,
				"error":      err.Error(),
			})
			return
		}
		writeJSONOK(w, map[string]any{
			"ok":         true,
			"latency_ms": latency,
			"version":    version,
		})

	case rest != "" && r.Method == http.MethodGet:
		e, ok := s.serverStore.Get(rest)
		if !ok {
			http.Error(w, "server not found", http.StatusNotFound)
			return
		}
		writeJSONOK(w, e)

	case rest != "" && r.Method == http.MethodPut:
		var updated multiserver.Entry
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.serverStore.Update(rest, &updated); err != nil {
			code := http.StatusInternalServerError
			switch err {
			case multiserver.ErrNotFound:
				code = http.StatusNotFound
			case multiserver.ErrBuiltin:
				code = http.StatusForbidden
			}
			http.Error(w, err.Error(), code)
			return
		}
		writeJSONOK(w, map[string]any{"name": rest, "ok": true})

	case rest != "" && r.Method == http.MethodDelete:
		if err := s.serverStore.Delete(rest); err != nil {
			code := http.StatusInternalServerError
			switch err {
			case multiserver.ErrNotFound:
				code = http.StatusNotFound
			case multiserver.ErrBuiltin:
				code = http.StatusForbidden
			}
			http.Error(w, err.Error(), code)
			return
		}
		writeJSONOK(w, map[string]any{"name": rest, "ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
