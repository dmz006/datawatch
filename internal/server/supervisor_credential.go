package server

// The supervisor credential: a thin, removable way for an external supervisor
// (the detector timer) to start sessions in the supervisor admission class over
// REST until the daemon runs that job itself. It is a scoped token held in the
// session token store under the reserved owner prefix below, carrying exactly
// sessions:write and capacity:supervisor-start. An operator mints, lists and
// revokes it; a session credential or a federation peer cannot. The class is
// honoured only for a token minted here, so a session token that merely lists
// the capability gets nothing. Supervisor class never goes past a pool's
// supervisor buffer, and every credentialed start is audited with the name.
// Remove this file, the capability, the audit actions and the setting when the
// native detector ships.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/auth"
	"github.com/dmz006/datawatch/internal/federation"
)

// supervisorCredentialPrefix is the token-store owner prefix of every
// supervisor credential. It starts with auth.InstanceTokenOwnerPrefix so the
// session-orphan sweep keeps it.
const supervisorCredentialPrefix = auth.InstanceTokenOwnerPrefix + "supervisor-credential:"

var supervisorCredentialName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,39}$`)

func supervisorCredentialOwner(name string) string { return supervisorCredentialPrefix + name }

// supervisorCredentialName returns the name of the supervisor credential r
// authenticated with, or "" when it did not (or the setting is off).
func (s *Server) supervisorCredentialName(r *http.Request) string {
	if s.sessionTokens == nil || sessionCapsFromContext(r.Context()) == nil {
		return ""
	}
	if s.cfg != nil && !s.cfg.Autonomous.CapacitySupervisorCredentialOn() {
		return ""
	}
	tok := callerTokenFromContext(r.Context())
	if tok == "" {
		return ""
	}
	owner := s.sessionTokens.SessionIDForToken(tok)
	if !strings.HasPrefix(owner, supervisorCredentialPrefix) {
		return ""
	}
	if caps, ok := s.sessionTokens.CapsForToken(tok); !ok || !federation.Check(caps, federation.CapCapacitySupervisorStart) {
		return ""
	}
	return strings.TrimPrefix(owner, supervisorCredentialPrefix)
}

// auditSupervisorCredentialStart records a credentialed start.
func (s *Server) auditSupervisorCredentialStart(name, backend, model, holder string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:  "supervisor-credential:" + name,
		Action: audit.ActionSupervisorCredentialStart,
		Details: map[string]any{
			"credential": name, "backend": backend, "model": model, "holder": holder,
		},
	})
}

// handleSupervisorCredential serves /api/capacity/supervisor-credential:
// POST {"name"} mints (the token is shown once), GET lists names, DELETE
// ?name= revokes. Operator (admin token) only.
func (s *Server) handleSupervisorCredential(w http.ResponseWriter, r *http.Request) {
	if sessionCapsFromContext(r.Context()) != nil || peerFromContext(r.Context()) != nil {
		http.Error(w, "supervisor credentials are managed by the operator only", http.StatusForbidden)
		return
	}
	if s.sessionTokens == nil {
		http.Error(w, "session token store is not available", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		names := []string{}
		for _, o := range s.sessionTokens.ListOwners(supervisorCredentialPrefix) {
			names = append(names, strings.TrimPrefix(o, supervisorCredentialPrefix))
		}
		writeJSONOK(w, map[string]any{"credentials": names})
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || !supervisorCredentialName.MatchString(body.Name) {
			http.Error(w, "name is required: 1 to 40 characters of a-z, 0-9, dot, dash, underscore, starting with a letter or digit", http.StatusBadRequest)
			return
		}
		tok, err := s.sessionTokens.Mint(supervisorCredentialOwner(body.Name), []string{federation.CapSessionsWrite, federation.CapCapacitySupervisorStart})
		if err != nil {
			http.Error(w, "mint: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.auditSupervisorCredential(audit.ActionSupervisorCredentialMint, body.Name)
		writeJSONOK(w, map[string]any{"name": body.Name, "token": tok, "note": "shown once; minting the same name again replaces it"})
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if !supervisorCredentialName.MatchString(name) {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if err := s.sessionTokens.Revoke(supervisorCredentialOwner(name)); err != nil {
			http.Error(w, "revoke: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.auditSupervisorCredential(audit.ActionSupervisorCredentialRevoke, name)
		writeJSONOK(w, map[string]any{"name": name, "revoked": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) auditSupervisorCredential(action, name string) {
	if s.auditLog == nil {
		return
	}
	_ = s.auditLog.Write(audit.Entry{Actor: "operator", Action: action, Details: map[string]any{"credential": name}})
}
