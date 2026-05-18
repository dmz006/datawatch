// BL242 Phase 1 — REST handlers for the centralized secrets manager.
//
//   GET    /api/secrets          — list (no values)
//   POST   /api/secrets          — create / update
//   GET    /api/secrets/{name}   — get with value (audited)
//   PUT    /api/secrets/{name}   — update existing
//   DELETE /api/secrets/{name}   — delete

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/secrets"
)

// handleAgentSecretsGet serves GET /api/agents/secrets/{name}.
// This endpoint is registered pre-auth (like bootstrap) so agents can
// call it using their per-agent SecretsToken without knowing the
// operator token. Scope is enforced: the secret's Scopes must allow
// CallerCtx{Type:"agent", Name:<profileName>}.
//
// Authorization: Bearer <secrets-token>
// Response: {"name":"…","value":"…"}
func (s *Server) handleAgentSecretsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.agentMgr == nil || s.secretsStore == nil {
		http.Error(w, "secrets not available", http.StatusServiceUnavailable)
		return
	}

	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	profileName, ok := s.agentMgr.LookupSecretsToken(tok)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/agents/secrets/"), "/")
	if name == "" {
		http.Error(w, "secret name required", http.StatusBadRequest)
		return
	}

	sec, err := s.secretsStore.Get(name)
	if err != nil {
		if errors.Is(err, secrets.ErrSecretNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := secrets.CheckScope(sec, secrets.CallerCtx{Type: "agent", Name: profileName}); err != nil {
		http.Error(w, "forbidden: "+err.Error(), http.StatusForbidden)
		return
	}

	if s.auditLog != nil {
		_ = s.auditLog.Write(audit.Entry{
			Actor:  "agent:" + profileName,
			Action: "secret_access",
			Details: map[string]any{
				"resource_type": "secret",
				"resource_id":   name,
				"via":           "agent-secrets-token",
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"name": sec.Name, "value": sec.Value})
}

// secretsStore is the narrow interface the REST handlers need.
type secretsStore interface {
	List() ([]secrets.Secret, error)
	Get(name string) (secrets.Secret, error)
	Set(name, value string, tags []string, description string, scopes []string) error
	Delete(name string) error
	Exists(name string) (bool, error)
}

// vaultBacked is satisfied by VaultStore. The status handler does a
// type-assertion to extract Vault-specific telemetry without coupling
// the secrets store interface to a Vault-only surface.
type vaultBacked interface {
	Status() secrets.VaultStatus
	CheckHealth() error
}

// handleVaultStatus serves /api/secrets/vault/status — connectivity +
// last-success / last-error / kv mount + path layout for the PWA card
// + nav badge.
func (s *Server) handleVaultStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.fedCap(w, r, federation.CapSecretsRead) {
		return
	}
	v, ok := s.secretsStore.(vaultBacked)
	if !ok {
		// Active backend isn't Vault — return a sentinel so the PWA
		// hides the card / badge cleanly.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"backend_active":false}`))
		return
	}
	stat := v.Status()
	out := struct {
		BackendActive bool                `json:"backend_active"`
		Status        secrets.VaultStatus `json:"status"`
	}{BackendActive: true, Status: stat}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// SetSecretsStore wires the secrets store for /api/secrets.
func (s *Server) SetSecretsStore(st secretsStore) { s.secretsStore = st }

// handleSecrets dispatches GET/POST /api/secrets and all /api/secrets/{name} sub-paths.
func (s *Server) handleSecrets(w http.ResponseWriter, r *http.Request) {
	if s.secretsStore == nil {
		http.Error(w, "secrets store not enabled", http.StatusServiceUnavailable)
		return
	}

	// BL267 (v6.15.0) — /api/secrets/vault/status surfaces Vault
	// connectivity for the PWA Settings card + nav badge. Reserved
	// path; doesn't conflict with secret-named routes because the
	// "vault" path segment is not a valid secret name in any backend
	// (operators can't create a secret named "vault" via the wrapper).
	if r.URL.Path == "/api/secrets/vault/status" {
		s.handleVaultStatus(w, r)
		return
	}

	// Exact /api/secrets or /api/secrets/ → collection operations.
	// /api/secrets/{name}[/exists] → named-secret operations.
	path := r.URL.Path
	var name string
	if path == "/api/secrets" || path == "/api/secrets/" {
		name = ""
	} else {
		name = strings.TrimSpace(strings.TrimPrefix(path, "/api/secrets/"))
	}

	if name == "" {
		switch r.Method {
		case http.MethodGet:
			if !s.fedCap(w, r, federation.CapSecretsList) {
				return
			}
			s.handleSecretsList(w, r)
		case http.MethodPost:
			if !s.fedCap(w, r, federation.CapSecretsWrite) {
				return
			}
			s.handleSecretsCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	if strings.HasSuffix(name, "/exists") {
		name = strings.TrimSuffix(name, "/exists")
		if !s.fedCap(w, r, federation.CapSecretsRead) {
			return
		}
		s.handleSecretsExists(w, r, name)
		return
	}

	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapSecretsRead) {
			return
		}
		s.handleSecretsGet(w, r, name)
	case http.MethodPut:
		if !s.fedCap(w, r, federation.CapSecretsWrite) {
			return
		}
		s.handleSecretsUpdate(w, r, name)
	case http.MethodDelete:
		if !s.fedCap(w, r, federation.CapSecretsWrite) {
			return
		}
		s.handleSecretsDelete(w, r, name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSecretsList(w http.ResponseWriter, _ *http.Request) {
	list, err := s.secretsStore.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []secrets.Secret{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"count": len(list), "secrets": list})
}

func (s *Server) handleSecretsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string   `json:"name"`
		Value       string   `json:"value"`
		Tags        []string `json:"tags"`
		Scopes      []string `json:"scopes"`
		Description string   `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if err := s.secretsStore.Set(body.Name, body.Value, body.Tags, body.Description, body.Scopes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"name": body.Name, "status": "created"})
}

func (s *Server) handleSecretsGet(w http.ResponseWriter, r *http.Request, name string) {
	sec, err := s.secretsStore.Get(name)
	if err != nil {
		if errors.Is(err, secrets.ErrSecretNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Audit every value fetch.
	if s.auditLog != nil {
		_ = s.auditLog.Write(audit.Entry{
			Actor:  "operator",
			Action: "secret_access",
			Details: map[string]any{
				"resource_type": "secret",
				"resource_id":   name,
				"via":           "rest",
			},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sec)
}

func (s *Server) handleSecretsUpdate(w http.ResponseWriter, r *http.Request, name string) {
	var body struct {
		Value       string   `json:"value"`
		Tags        []string `json:"tags"`
		Scopes      []string `json:"scopes"`
		Description string   `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.secretsStore.Set(name, body.Value, body.Tags, body.Description, body.Scopes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"name": name, "status": "updated"})
}

func (s *Server) handleSecretsExists(w http.ResponseWriter, _ *http.Request, name string) {
	exists, err := s.secretsStore.Exists(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"name": name, "exists": exists})
}

func (s *Server) handleSecretsDelete(w http.ResponseWriter, _ *http.Request, name string) {
	if err := s.secretsStore.Delete(name); err != nil {
		if errors.Is(err, secrets.ErrSecretNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"name": name, "status": "deleted"})
}
