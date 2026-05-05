// internal/server/identity.go — REST surface for BL257 Phase 1 v6.8.0
// (operator identity / Telos layer).
//
// Routes:
//
//	GET   /api/identity            — read full identity
//	PUT   /api/identity            — replace full identity
//	PATCH /api/identity            — merge non-empty fields
//
// All write paths emit audit entries (action=identity_set / identity_update).
//
// Returns 503 when no identity manager is wired (daemon started without
// identity support). Public adapter type IdentityManagerAdapter wraps
// *identity.Manager so cmd/datawatch can wire it via SetIdentityManager.

package server

import (
	"encoding/json"
	"net/http"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/identity"
)

// identityManager is the server-side interface around identity.Manager.
type identityManager interface {
	Get() identity.Identity
	Set(identity.Identity) (identity.Identity, error)
	Update(identity.Identity) (identity.Identity, error)
}

// SetIdentityManager wires the runtime *identity.Manager into the server.
func (s *Server) SetIdentityManager(m identityManager) { s.identityMgr = m }

// IdentityManagerAdapter wraps *identity.Manager so cmd/datawatch can
// pass it to SetIdentityManager without exposing internal/identity to
// the server interface.
type IdentityManagerAdapter struct{ M *identity.Manager }

// Get forwards.
func (a IdentityManagerAdapter) Get() identity.Identity { return a.M.Get() }

// Set forwards.
func (a IdentityManagerAdapter) Set(id identity.Identity) (identity.Identity, error) {
	return a.M.Set(id)
}

// Update forwards.
func (a IdentityManagerAdapter) Update(id identity.Identity) (identity.Identity, error) {
	return a.M.Update(id)
}

func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	if s.identityMgr == nil {
		http.Error(w, "identity disabled", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSONOK(w, s.identityMgr.Get())
	case http.MethodPut:
		var body identity.Identity
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		got, err := s.identityMgr.Set(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.auditIdentity("identity_set")
		writeJSONOK(w, got)
	case http.MethodPatch:
		var body identity.Identity
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		got, err := s.identityMgr.Update(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.auditIdentity("identity_update")
		writeJSONOK(w, got)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) auditIdentity(action string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "operator",
		Action: action,
		Details: map[string]any{
			"resource_type": "identity",
			"resource_id":   "self",
		},
	})
}
