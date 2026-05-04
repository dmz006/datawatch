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
	"github.com/dmz006/datawatch/internal/secrets"
)

// secretsStore is the narrow interface the REST handlers need.
type secretsStore interface {
	List() ([]secrets.Secret, error)
	Get(name string) (secrets.Secret, error)
	Set(name, value string, tags []string, description string, scopes []string) error
	Delete(name string) error
	Exists(name string) (bool, error)
}

// SetSecretsStore wires the secrets store for /api/secrets.
func (s *Server) SetSecretsStore(st secretsStore) { s.secretsStore = st }

// handleSecrets dispatches GET/POST /api/secrets and all /api/secrets/{name} sub-paths.
func (s *Server) handleSecrets(w http.ResponseWriter, r *http.Request) {
	if s.secretsStore == nil {
		http.Error(w, "secrets store not enabled", http.StatusServiceUnavailable)
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
			s.handleSecretsList(w, r)
		case http.MethodPost:
			s.handleSecretsCreate(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// /api/secrets/{name}/exists — check without revealing value
	if strings.HasSuffix(name, "/exists") {
		name = strings.TrimSuffix(name, "/exists")
		s.handleSecretsExists(w, r, name)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleSecretsGet(w, r, name)
	case http.MethodPut:
		s.handleSecretsUpdate(w, r, name)
	case http.MethodDelete:
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
