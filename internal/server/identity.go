// internal/server/identity.go — REST surface for BL257 Phase 1 v6.8.0
// (operator identity / Telos layer).
//
// Routes:
//
//	GET   /api/identity            — read full identity
//	PUT   /api/identity            — replace full identity
//	PATCH /api/identity            — merge non-empty fields
//	POST  /api/identity            — alias for PATCH (mobile Android compat, BL329)
//	GET   /api/identity/backups    — list saved copies, newest first, plus the retention limit
//	POST  /api/identity/restore    — bring a backup back; body {"backup": "<name or latest>"}
//	POST  /api/identity/reset      — the explicit clear; keeps a backup of what it replaced
//
// A write that would replace a saved identity with an empty one is
// refused, so no caller can wipe the identity by accident. PUT, PATCH and
// POST answer HTTP 409 with
//
//	{"error": "<plain sentence>", "code": "identity_wipe_refused",
//	 "fields_kept": ["role", ...]}
//
// and nothing is written. Clearing on purpose is POST /api/identity/reset.
//
// Restore answers 200 with the restored identity, 400 for bad JSON or a
// missing backup name, 404 for an unknown or invalid name and 422 for a
// backup with no operator content. Any other method on a route is 405.
//
// Audit actions: identity_set / identity_update (writes),
// identity_set_refused (a refused empty write; details name the method and
// the fields kept), identity_restore (details name the backup) and
// identity_reset.
//
// Returns 503 when no identity manager is wired (daemon started without
// identity support). Public adapter type IdentityManagerAdapter wraps
// *identity.Manager so cmd/datawatch can wire it via SetIdentityManager.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/identity"
)

// identityWipeRefusedCode is the machine-readable code of the 409 body.
const identityWipeRefusedCode = "identity_wipe_refused"

// identityManager is the server-side interface around identity.Manager.
type identityManager interface {
	Get() identity.Identity
	Set(identity.Identity) (identity.Identity, error)
	Update(identity.Identity) (identity.Identity, error)
	Reset() (identity.Identity, error)
	ListBackups() ([]identity.Backup, error)
	Restore(name string) (identity.Identity, error)
	BackupsToKeep() int
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

// Reset forwards.
func (a IdentityManagerAdapter) Reset() (identity.Identity, error) { return a.M.Reset() }

// ListBackups forwards.
func (a IdentityManagerAdapter) ListBackups() ([]identity.Backup, error) {
	return a.M.ListBackups()
}

// Restore forwards.
func (a IdentityManagerAdapter) Restore(name string) (identity.Identity, error) {
	return a.M.Restore(name)
}

// BackupsToKeep forwards.
func (a IdentityManagerAdapter) BackupsToKeep() int { return a.M.BackupsToKeep() }

// identityWipeRefusal is the JSON body of the 409 for a refused empty write.
type identityWipeRefusal struct {
	Error      string   `json:"error"`
	Code       string   `json:"code"`
	FieldsKept []string `json:"fields_kept"`
}

func (s *Server) handleIdentity(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapConfigRead) {
			return
		}
		if s.identityMgr == nil {
			http.Error(w, "identity disabled", http.StatusServiceUnavailable)
			return
		}
		writeJSONOK(w, s.identityMgr.Get())
	case http.MethodPut:
		s.identityWrite(w, r, "identity_set", func(m identityManager, id identity.Identity) (identity.Identity, error) {
			return m.Set(id)
		})
	case http.MethodPatch:
		s.identityWrite(w, r, "identity_update", func(m identityManager, id identity.Identity) (identity.Identity, error) {
			return m.Update(id)
		})
	case http.MethodPost:
		// POST is an alias for PATCH — partial update (mobile Android compat, BL329)
		s.identityWrite(w, r, "identity_update", func(m identityManager, id identity.Identity) (identity.Identity, error) {
			return m.Update(id)
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// identityWrite is the shared PUT / PATCH / POST path: capability guard,
// decode, write, and the 409 answer when the manager refuses to wipe.
func (s *Server) identityWrite(w http.ResponseWriter, r *http.Request, action string,
	write func(identityManager, identity.Identity) (identity.Identity, error)) {
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if s.identityMgr == nil {
		http.Error(w, "identity disabled", http.StatusServiceUnavailable)
		return
	}
	var body identity.Identity
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	got, err := write(s.identityMgr, body)
	if errors.Is(err, identity.ErrEmptyOverwrite) {
		kept := identityFieldNames(s.identityMgr.Get())
		s.auditIdentity(r, "identity_set_refused", map[string]any{
			"method":      r.Method,
			"fields_kept": strings.Join(kept, ","),
		})
		writeJSONError(w, http.StatusConflict, identityWipeRefusal{
			Error:      "refusing to replace the saved identity with an empty one; to clear it on purpose use POST /api/identity/reset",
			Code:       identityWipeRefusedCode,
			FieldsKept: kept,
		})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.auditIdentity(r, action, nil)
	writeJSONOK(w, got)
}

// handleIdentityBackups serves GET /api/identity/backups.
func (s *Server) handleIdentityBackups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapConfigRead) {
		return
	}
	if s.identityMgr == nil {
		http.Error(w, "identity disabled", http.StatusServiceUnavailable)
		return
	}
	backups, err := s.identityMgr.ListBackups()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if backups == nil {
		backups = []identity.Backup{}
	}
	writeJSONOK(w, map[string]any{
		"backups": backups,
		"keep":    s.identityMgr.BackupsToKeep(),
	})
}

// handleIdentityRestore serves POST /api/identity/restore.
func (s *Server) handleIdentityRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if s.identityMgr == nil {
		http.Error(w, "identity disabled", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Backup string `json:"backup"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Backup) == "" {
		http.Error(w, `missing "backup": give a backup name from GET /api/identity/backups, or "latest"`, http.StatusBadRequest)
		return
	}
	got, err := s.identityMgr.Restore(body.Backup)
	switch {
	case errors.Is(err, identity.ErrBackupNotFound):
		jsonError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, identity.ErrBackupEmpty):
		jsonError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.auditIdentity(r, "identity_restore", map[string]any{"backup": body.Backup})
	writeJSONOK(w, got)
}

// handleIdentityReset serves POST /api/identity/reset, the explicit clear.
func (s *Server) handleIdentityReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapConfigWrite) {
		return
	}
	if s.identityMgr == nil {
		http.Error(w, "identity disabled", http.StatusServiceUnavailable)
		return
	}
	got, err := s.identityMgr.Reset()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.auditIdentity(r, "identity_reset", nil)
	writeJSONOK(w, got)
}

// identityFieldNames names the identity fields that have content, in the
// order the identity file lists them. It is never nil, so it encodes as a
// JSON array.
func identityFieldNames(id identity.Identity) []string {
	out := []string{}
	if id.Role != "" {
		out = append(out, "role")
	}
	if len(id.NorthStarGoals) > 0 {
		out = append(out, "north_star_goals")
	}
	if len(id.CurrentProjects) > 0 {
		out = append(out, "current_projects")
	}
	if len(id.Values) > 0 {
		out = append(out, "values")
	}
	if id.CurrentFocus != "" {
		out = append(out, "current_focus")
	}
	if id.ContextNotes != "" {
		out = append(out, "context_notes")
	}
	return out
}

// auditIdentity records one identity audit entry. extra adds to the
// resource details common to every identity action.
func (s *Server) auditIdentity(r *http.Request, action string, extra map[string]any) {
	if s.auditLog == nil {
		return
	}
	details := map[string]any{
		"resource_type": "identity",
		"resource_id":   "self",
	}
	for k, v := range extra {
		details[k] = v
	}
	_ = s.auditLog.Write(audit.Entry{
		Actor:   s.auditActor(r.Context()),
		Action:  action,
		Details: details,
	})
}
