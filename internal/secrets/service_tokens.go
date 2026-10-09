// External-service secrets tokens.
//
// GH#203 (imap-mcp coordination, 2026-10-09) — an independent external
// service (not a datawatch-spawned F10 agent, not a federation peer)
// needs a minimal-capability way to resolve a small, named set of
// secrets via ${secret:name} without being able to read every secret in
// the store. The two existing paths don't fit:
//   - GET /api/agents/secrets/{name} is scoped correctly (per-secret,
//     via CallerCtx{Type:"agent",...}) but its tokens are minted only at
//     F10 agent-spawn time, kept in memory, and lost on daemon restart —
//     an external systemd service can never hold one.
//   - GET /api/secrets/{name} with a federation-peer/"secrets:read"
//     capability token is NOT per-secret scoped at all (CheckScope is
//     only applied to agent/plugin callers) — any such token reads
//     every secret in the store.
//
// ServiceTokenStore is the third option: a persistent (survives daemon
// restart, unlike agent tokens), operator-minted token bound to a named
// external service, used to build CallerCtx{Type:"service", Name:...}
// for the existing secrets.CheckScope — a secret's Scopes field
// ("service:imap-mcp" or "service:*") controls exactly which service
// tokens may read it, same mechanism "agent:"/"plugin:" scopes already
// use.
//
// Tokens are generated server-side (crypto/rand) and are meant to be
// minted ONLY via the `datawatch secrets mint-service-token` CLI command,
// run directly by the operator (not through an agent's tool calls) —
// the value is printed once to the operator's own terminal and never
// appears in this package's API responses, so it never has to pass
// through an LLM's context to get provisioned.

package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ServiceToken is one persisted entry. Token is stored in plaintext —
// same risk class and 0600-permission precedent as this codebase's
// other locally-held credentials (e.g. .mcp.json's DATAWATCH_TOKEN env).
type ServiceToken struct {
	Name        string    `json:"name"`
	Token       string    `json:"token"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ServiceTokenStore persists external-service tokens as a single JSON
// file, 0600, atomic tmp-then-rename writes.
type ServiceTokenStore struct {
	mu   sync.RWMutex
	path string
	toks map[string]ServiceToken // name -> token
}

// NewServiceTokenStore opens (or creates) the store at path.
func NewServiceTokenStore(path string) (*ServiceTokenStore, error) {
	if path == "" {
		return nil, fmt.Errorf("service token store: path required")
	}
	s := &ServiceTokenStore{path: path, toks: map[string]ServiceToken{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s.toks); err != nil {
			return nil, fmt.Errorf("service token store: parse %s: %w", path, err)
		}
	}
	return s, nil
}

func (s *ServiceTokenStore) persist() error {
	data, err := json.MarshalIndent(s.toks, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Mint generates a new random token for name (32 random bytes, hex-
// encoded) and persists it, replacing any existing token for the same
// name. Returns the plaintext token — callers must not log it; the CLI
// command is the only sanctioned place this return value is displayed.
func (s *ServiceTokenStore) Mint(name, description string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("service token: name required")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("service token: generate: %w", err)
	}
	tok := hex.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.toks[name] = ServiceToken{Name: name, Token: tok, Description: description, CreatedAt: time.Now()}
	if err := s.persist(); err != nil {
		delete(s.toks, name)
		return "", err
	}
	return tok, nil
}

// Lookup resolves a bearer token to the service name it belongs to.
// Returns ("", false) for an unknown or revoked token.
func (s *ServiceTokenStore) Lookup(token string) (name string, ok bool) {
	if token == "" {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.toks {
		if t.Token == token {
			return t.Name, true
		}
	}
	return "", false
}

// List returns every provisioned service's metadata, never the token
// values themselves.
func (s *ServiceTokenStore) List() []ServiceToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ServiceToken, 0, len(s.toks))
	for _, t := range s.toks {
		out = append(out, ServiceToken{Name: t.Name, Description: t.Description, CreatedAt: t.CreatedAt})
	}
	return out
}

// Revoke removes name's token. No-op (nil error) if it doesn't exist.
func (s *ServiceTokenStore) Revoke(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.toks[name]; !ok {
		return nil
	}
	delete(s.toks, name)
	return s.persist()
}
