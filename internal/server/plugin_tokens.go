// Per-plugin token usage listing (GET /api/plugins/{name}/tokens).
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
	"fmt"
	"net/http"
	"time"

	"github.com/dmz006/datawatch/internal/plugins"
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
