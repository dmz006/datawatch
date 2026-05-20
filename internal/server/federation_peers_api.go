// BL316 — /api/federation/peers and /api/federation/groups REST handlers.
//
// Federation peers are runtime-mutable multiserver.Entry records that have
// Federated=true. They authenticate with their own Token and are granted only
// the capabilities listed in their Capabilities field. Admin token requests
// always bypass capability checks.
//
// Route map:
//   GET    /api/federation/peers           — list peers
//   POST   /api/federation/peers           — add peer
//   GET    /api/federation/peers/{name}    — get one peer
//   PUT    /api/federation/peers/{name}    — update peer
//   DELETE /api/federation/peers/{name}    — delete peer
//   POST   /api/federation/peers/{name}/test — ping health endpoint
//
//   GET    /api/federation/groups          — list builtin + custom groups
//   GET    /api/federation/groups/builtins — list builtin groups only
//   POST   /api/federation/groups          — add custom group
//   GET    /api/federation/groups/{name}   — get group (builtin or custom)
//   PUT    /api/federation/groups/{name}   — update custom group
//   DELETE /api/federation/groups/{name}   — delete custom group

package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

// -------------------------------------------------------------------------
// Peers

func (s *Server) handleFederationPeers(w http.ResponseWriter, r *http.Request) {
	// Capability check before nil guards so peers get 403 not 503.
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapFederationList) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapFederationWrite) {
			return
		}
	}
	if s.serverStore == nil {
		http.Error(w, "server registry not configured", http.StatusServiceUnavailable)
		return
	}

	// /api/federation/peers/{name}[/test]
	tail := strings.TrimPrefix(r.URL.Path, "/api/federation/peers")
	tail = strings.TrimPrefix(tail, "/")
	if tail != "" {
		parts := strings.SplitN(tail, "/", 2)
		name := parts[0]
		action := ""
		if len(parts) > 1 {
			action = parts[1]
		}
		if action == "test" && r.Method == http.MethodPost {
			s.fedPeerTest(w, r, name)
			return
		}
		switch r.Method {
		case http.MethodGet:
			s.fedPeerGet(w, name)
		case http.MethodPut:
			s.fedPeerUpdate(w, r, name)
		case http.MethodDelete:
			s.fedPeerDelete(w, name)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/federation/peers
	switch r.Method {
	case http.MethodGet:
		s.fedPeerList(w)
	case http.MethodPost:
		s.fedPeerAdd(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) fedPeerList(w http.ResponseWriter) {
	peers := s.serverStore.ListFederated()
	writeJSONOK(w, peers)
}

func (s *Server) fedPeerGet(w http.ResponseWriter, name string) {
	e, ok := s.serverStore.Get(name)
	if !ok {
		http.Error(w, "peer not found", http.StatusNotFound)
		return
	}
	if !e.Federated {
		http.Error(w, "not a federation peer", http.StatusNotFound)
		return
	}
	writeJSONOK(w, e)
}

func (s *Server) fedPeerAdd(w http.ResponseWriter, r *http.Request) {
	var body multiserver.Entry
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	// BL331 — channel_identity is decoded directly from the Entry struct.
	body.Federated = true
	if body.AuthType == "" {
		body.AuthType = "token"
	}
	if len(body.Capabilities) == 0 {
		body.Capabilities = []string{"federation-peer"}
	}
	if err := s.serverStore.Add(&body); err != nil {
		switch err {
		case multiserver.ErrConflict:
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	e, _ := s.serverStore.Get(body.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(e)
}

func (s *Server) fedPeerUpdate(w http.ResponseWriter, r *http.Request, name string) {
	// Fix #80 — partial PUT was wiping unset fields to zero values.
	// Read existing entry first, then unmarshal body on top of it so only
	// explicitly-supplied JSON keys are changed.
	existing, ok := s.serverStore.Get(name)
	if !ok {
		http.Error(w, multiserver.ErrNotFound.Error(), http.StatusNotFound)
		return
	}
	if !existing.Federated {
		http.Error(w, "not a federation peer", http.StatusNotFound)
		return
	}
	merged := *existing // copy
	if err := json.NewDecoder(r.Body).Decode(&merged); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	merged.Federated = true // BL316 — always a federation peer
	if err := s.serverStore.Update(name, &merged); err != nil {
		switch err {
		case multiserver.ErrNotFound:
			http.Error(w, err.Error(), http.StatusNotFound)
		case multiserver.ErrBuiltin:
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	e, _ := s.serverStore.Get(name)
	writeJSONOK(w, e)
}

func (s *Server) fedPeerDelete(w http.ResponseWriter, name string) {
	if err := s.serverStore.Delete(name); err != nil {
		switch err {
		case multiserver.ErrNotFound:
			http.Error(w, err.Error(), http.StatusNotFound)
		case multiserver.ErrBuiltin:
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) fedPeerTest(w http.ResponseWriter, r *http.Request, name string) {
	latencyMs, version, err := s.serverStore.Test(r.Context(), name)
	if err != nil {
		writeJSONOK(w, map[string]any{
			"ok":         false,
			"latency_ms": latencyMs,
			"error":      err.Error(),
		})
		return
	}
	writeJSONOK(w, map[string]any{
		"ok":         true,
		"latency_ms": latencyMs,
		"version":    version,
	})
}

// -------------------------------------------------------------------------
// Groups

func (s *Server) handleFederationGroups(w http.ResponseWriter, r *http.Request) {
	// Capability check before nil guards so peers get 403 not 503.
	if r.Method == http.MethodGet {
		if !s.fedCap(w, r, federation.CapFederationList) {
			return
		}
	} else {
		if !s.fedCap(w, r, federation.CapFederationWrite) {
			return
		}
	}
	// /api/federation/groups/builtins
	if strings.HasSuffix(r.URL.Path, "/builtins") {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSONOK(w, federation.ListBuiltinGroups())
		return
	}

	tail := strings.TrimPrefix(r.URL.Path, "/api/federation/groups")
	tail = strings.TrimPrefix(tail, "/")

	if tail != "" {
		switch r.Method {
		case http.MethodGet:
			s.fedGroupGet(w, tail)
		case http.MethodPut:
			s.fedGroupUpdate(w, r, tail)
		case http.MethodDelete:
			s.fedGroupDelete(w, tail)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.fedGroupList(w)
	case http.MethodPost:
		s.fedGroupAdd(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) fedGroupList(w http.ResponseWriter) {
	builtins := federation.ListBuiltinGroups()
	var custom []*federation.CapabilityGroup
	if s.fedGroupStore != nil {
		custom = s.fedGroupStore.List()
	}
	writeJSONOK(w, map[string]any{
		"builtins": builtins,
		"custom":   custom,
	})
}

func (s *Server) fedGroupGet(w http.ResponseWriter, name string) {
	// Check builtins first.
	if g, ok := federation.BuiltinGroups[name]; ok {
		writeJSONOK(w, g)
		return
	}
	if s.fedGroupStore == nil {
		http.Error(w, "group store not configured", http.StatusServiceUnavailable)
		return
	}
	g, err := s.fedGroupStore.Get(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSONOK(w, g)
}

func (s *Server) fedGroupAdd(w http.ResponseWriter, r *http.Request) {
	if s.fedGroupStore == nil {
		http.Error(w, "group store not configured", http.StatusServiceUnavailable)
		return
	}
	var g federation.CapabilityGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.fedGroupStore.Add(&g); err != nil {
		switch err {
		case federation.ErrGroupConflict:
			http.Error(w, err.Error(), http.StatusConflict)
		case federation.ErrGroupBuiltin:
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	out, _ := s.fedGroupStore.Get(g.Name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) fedGroupUpdate(w http.ResponseWriter, r *http.Request, name string) {
	if s.fedGroupStore == nil {
		http.Error(w, "group store not configured", http.StatusServiceUnavailable)
		return
	}
	var g federation.CapabilityGroup
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.fedGroupStore.Update(name, &g); err != nil {
		switch err {
		case federation.ErrGroupNotFound:
			http.Error(w, err.Error(), http.StatusNotFound)
		case federation.ErrGroupBuiltin:
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		return
	}
	out, _ := s.fedGroupStore.Get(name)
	writeJSONOK(w, out)
}

func (s *Server) fedGroupDelete(w http.ResponseWriter, name string) {
	if s.fedGroupStore == nil {
		http.Error(w, "group store not configured", http.StatusServiceUnavailable)
		return
	}
	if err := s.fedGroupStore.Delete(name); err != nil {
		switch err {
		case federation.ErrGroupNotFound:
			http.Error(w, err.Error(), http.StatusNotFound)
		case federation.ErrGroupBuiltin:
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
