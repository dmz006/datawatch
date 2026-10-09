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

// SetServiceTokenStore wires the external-service token store (GH#203)
// used by GET /api/external/secrets/{name}.
func (s *Server) SetServiceTokenStore(st *secrets.ServiceTokenStore) { s.serviceTokenStore = st }

// handleExternalSecretsGet serves GET /api/external/secrets/{name}
// (GH#203). Registered pre-auth, like handleAgentSecretsGet, so an
// independent external service (not a spawned F10 agent, not a
// federation peer — e.g. imap-mcp, running as its own systemd service)
// can resolve a secret using a persistent token the operator minted via
// `datawatch secrets mint-service-token <name>`. Scope is enforced: the
// secret's Scopes must allow CallerCtx{Type:"service", Name:<svcName>}.
//
// Authorization: Bearer <service-token>
// Response: {"name":"…","value":"…"}
func (s *Server) handleExternalSecretsGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.serviceTokenStore == nil || s.secretsStore == nil {
		http.Error(w, "external secrets not available", http.StatusServiceUnavailable)
		return
	}

	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if tok == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	svcName, ok := s.serviceTokenStore.Lookup(tok)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/external/secrets/"), "/")
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

	if err := secrets.CheckScope(sec, secrets.CallerCtx{Type: "service", Name: svcName}); err != nil {
		http.Error(w, "forbidden: "+err.Error(), http.StatusForbidden)
		return
	}

	if s.auditLog != nil {
		_ = s.auditLog.Write(audit.Entry{
			Actor:  "service:" + svcName,
			Action: "secret_access",
			Details: map[string]any{
				"resource_type": "secret",
				"resource_id":   name,
				"via":           "external-service-token",
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"name": sec.Name, "value": sec.Value})
}

// handleSecretServiceTokens dispatches the admin-only (regular
// fedAuthMiddleware-gated) CRUD surface for external-service tokens
// (GH#203):
//
//	GET    /api/secrets/service-tokens        — list (name/description/created_at only, never the token)
//	POST   /api/secrets/service-tokens        — mint a new token for {"name","description"}, returns {"name","token"} ONCE
//	DELETE /api/secrets/service-tokens/{name} — revoke
//
// This is the operator-facing side; handleExternalSecretsGet is the
// unrelated pre-auth side the external service itself calls with the
// minted token. Minting should only ever be run directly by the
// operator in their own terminal (`datawatch secrets mint-service-token
// <name>`) — never by an agent on the operator's behalf — since the
// response is the only place the plaintext token is ever shown, and an
// agent running the command would see it in its own tool output.
func (s *Server) handleSecretServiceTokens(w http.ResponseWriter, r *http.Request) {
	if s.serviceTokenStore == nil {
		http.Error(w, "external secrets not available", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/secrets/service-tokens")
	name := strings.Trim(path, "/")

	switch {
	case r.Method == http.MethodGet && name == "":
		if !s.fedCap(w, r, federation.CapSecretsRead) {
			return
		}
		list := s.serviceTokenStore.List()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"count": len(list), "service_tokens": list})

	case r.Method == http.MethodPost && name == "":
		if !s.fedCap(w, r, federation.CapSecretsWrite) {
			return
		}
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			http.Error(w, "name required", http.StatusBadRequest)
			return
		}
		tok, err := s.serviceTokenStore.Mint(body.Name, body.Description)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.auditLog != nil {
			_ = s.auditLog.Write(audit.Entry{
				Actor:  "operator",
				Action: "service_token_mint",
				Details: map[string]any{"resource_type": "service_token", "resource_id": body.Name},
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": body.Name, "token": tok})

	case r.Method == http.MethodDelete && name != "":
		if !s.fedCap(w, r, federation.CapSecretsWrite) {
			return
		}
		if err := s.serviceTokenStore.Revoke(name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.auditLog != nil {
			_ = s.auditLog.Write(audit.Entry{
				Actor:  "operator",
				Action: "service_token_revoke",
				Details: map[string]any{"resource_type": "service_token", "resource_id": name},
			})
		}
		writeJSONOK(w, map[string]bool{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
	// Audit every value fetch. HLLM-003 — Actor names the real caller
	// (operator / session:<id> / peer:<name>), not a hardcoded "operator".
	if s.auditLog != nil {
		_ = s.auditLog.Write(audit.Entry{
			Actor:  s.auditActor(r.Context()),
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
