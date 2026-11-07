// Per-plugin token usage listing (GET /api/plugins/{name}/tokens) and
// per-secret rotate + delete for the secrets a plugin's manifest
// references:
//
//	POST   /api/plugins/{name}/tokens/{secret}/rotate
//	DELETE /api/plugins/{name}/tokens/{secret}
//
// Operator-authenticated only: a federation peer or a session-scoped
// credential must not be able to enumerate the secrets a plugin uses or
// the service tokens minted for it.
//
// The response carries, per secret the plugin manifest references, the
// manifest fields that carry the reference (connection.auth.token,
// env.<KEY>) and whether the secret exists in the store — plus the
// names and creation times of the service tokens minted for the
// service. It never carries a secret value or a service token value.

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/plugins"
	"github.com/dmz006/datawatch/internal/secrets"
)

// PluginTokensSource exposes a plugin's manifest for the token-usage
// listing. Satisfied by plugins.API (see its Manifest method).
type PluginTokensSource interface {
	Manifest(name string) (plugins.Manifest, bool)
}

// SetPluginTokens wires the token-usage listing. Nil disables
// /api/plugins/{name}/tokens (handlers return 503).
func (s *Server) SetPluginTokens(src PluginTokensSource) {
	s.pluginTokensSrc = src
}

// secretUsageView is one secret reference the plugin manifest uses.
type secretUsageView struct {
	Name   string   `json:"name"`
	Fields []string `json:"fields"`
	Exists bool     `json:"exists"`
}

// serviceTokenView is one minted service token for the service: the
// name and when it was minted — never the token itself.
type serviceTokenView struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handlePluginTokens(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Operator auth only — never a federation peer or a session-scoped
	// credential (same gate as the admin-token rotation route).
	if peerFromContext(r.Context()) != nil || sessionCapsFromContext(r.Context()) != nil {
		http.Error(w, "operator token required", http.StatusForbidden)
		return
	}
	src := s.pluginTokensSrc
	if src == nil {
		http.Error(w, "plugins not available", http.StatusServiceUnavailable)
		return
	}
	m, ok := src.Manifest(name)
	if !ok {
		http.Error(w, fmt.Sprintf("plugin %q not found", name), http.StatusNotFound)
		return
	}

	refs := plugins.SecretRefs(m)
	if refs == nil {
		refs = []plugins.SecretRefUsage{}
	}
	usage := make([]secretUsageView, 0, len(refs))
	for _, ref := range refs {
		exists := false
		if s.secretsStore != nil {
			if ok, err := s.secretsStore.Exists(ref.Name); err == nil {
				exists = ok
			}
		}
		usage = append(usage, secretUsageView{Name: ref.Name, Fields: ref.Fields, Exists: exists})
	}

	svcs := []serviceTokenView{}
	if s.serviceTokenStore != nil {
		for _, t := range s.serviceTokenStore.List() {
			if t.Name != name {
				continue
			}
			svcs = append(svcs, serviceTokenView{
				Name:      t.Name,
				CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"plugin":         name,
		"secrets":        usage,
		"service_tokens": svcs,
	})
}

// pluginTokenSecretGuard runs the shared preamble for the per-secret
// rotate and delete routes: operator auth only (a federation peer or a
// session capability token must not mutate secrets), the named plugin
// exists, and the named secret is actually referenced by the plugin's
// manifest — a rotate or delete targeting a secret the plugin does not
// use is refused. It writes the error response itself and returns
// false when the request must not proceed.
func (s *Server) pluginTokenSecretGuard(w http.ResponseWriter, r *http.Request, name, secret string) bool {
	if peerFromContext(r.Context()) != nil || sessionCapsFromContext(r.Context()) != nil {
		http.Error(w, "operator token required", http.StatusForbidden)
		return false
	}
	src := s.pluginTokensSrc
	if src == nil {
		http.Error(w, "plugins not available", http.StatusServiceUnavailable)
		return false
	}
	m, ok := src.Manifest(name)
	if !ok {
		http.Error(w, fmt.Sprintf("plugin %q not found", name), http.StatusNotFound)
		return false
	}
	referenced := false
	for _, ref := range plugins.SecretRefs(m) {
		if ref.Name == secret {
			referenced = true
			break
		}
	}
	if !referenced {
		http.Error(w, fmt.Sprintf("plugin %q does not reference secret %q", name, secret), http.StatusBadRequest)
		return false
	}
	if s.secretsStore == nil {
		http.Error(w, "secrets store not enabled", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// serviceTokenNamed reports whether the service-token store holds a
// token registered under the given (secret) name.
func (s *Server) serviceTokenNamed(name string) bool {
	if s.serviceTokenStore == nil {
		return false
	}
	for _, t := range s.serviceTokenStore.List() {
		if t.Name == name {
			return true
		}
	}
	return false
}

// handlePluginTokenRotate is POST
// /api/plugins/{name}/tokens/{secret}/rotate — the operator rotates
// one of the secrets the named plugin's manifest references. The new
// value comes from the request body ("value") and is stored through the
// same secrets-store Set path every other secret write uses. The value
// is never echoed back: the response carries only the plugin and
// secret names. When the secret is also registered in the
// service-token store, the old token is revoked and a fresh one minted
// under the same name — no other token or secret is touched. The
// rotation lands in the audit log.
func (s *Server) handlePluginTokenRotate(w http.ResponseWriter, r *http.Request, name, secret string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.pluginTokenSecretGuard(w, r, name, secret) {
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Value == "" {
		http.Error(w, "value required", http.StatusBadRequest)
		return
	}
	if err := s.secretsStore.Set(secret, body.Value, nil, "", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.serviceTokenNamed(secret) {
		// Revoke the old token first, then mint under the same name;
		// order matters — a MINT-then-REVOKE sequence would remove the
		// freshly minted token too.
		if err := s.serviceTokenStore.Revoke(secret); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if _, err := s.serviceTokenStore.Mint(secret, "rotated via plugin token rotation"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	s.audit(r.Context(), "plugin_token_rotate", "secret", secret, map[string]any{"plugin": name})
	writeJSONOK(w, map[string]any{
		"status": "rotated",
		"plugin": name,
		"secret": secret,
	})
}

// handlePluginTokenRemove is DELETE
// /api/plugins/{name}/tokens/{secret} — the operator deletes one of the
// secrets the named plugin's manifest references, through the same
// secrets-store Delete path every other secret delete uses. The delete
// lands in the audit log.
func (s *Server) handlePluginTokenRemove(w http.ResponseWriter, r *http.Request, name, secret string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.pluginTokenSecretGuard(w, r, name, secret) {
		return
	}
	if err := s.secretsStore.Delete(secret); err != nil {
		if errors.Is(err, secrets.ErrSecretNotFound) {
			http.Error(w, fmt.Sprintf("secret %q not found", secret), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.audit(r.Context(), "plugin_token_remove", "secret", secret, map[string]any{"plugin": name})
	writeJSONOK(w, map[string]any{
		"status": "removed",
		"plugin": name,
		"secret": secret,
	})
}
