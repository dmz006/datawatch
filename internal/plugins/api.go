// BL33 — adapter that lets the server package consume the plugin
// Registry through the narrow server.PluginsAPI interface (no import
// cycle). Concrete types leak out as `any`; JSON encoding on the
// server side handles it.

package plugins

import (
	"context"
	"fmt"
)

// API wraps a *Registry for the REST/MCP/CLI layers.
type API struct{ R *Registry }

// NewAPI is a convenience constructor.
func NewAPI(r *Registry) *API { return &API{R: r} }

func (a *API) List() []any {
	src := a.R.List()
	out := make([]any, len(src))
	for i, p := range src {
		out[i] = p
	}
	return out
}

func (a *API) Get(name string) (any, bool) {
	p, ok := a.R.Get(name)
	if !ok {
		return nil, false
	}
	return p, true
}

func (a *API) Reload() error { return a.R.Discover() }

func (a *API) SetEnabled(name string, on bool) bool {
	return a.R.SetEnabled(name, on)
}

func (a *API) Test(ctx context.Context, name, hook string, payload map[string]any) (any, error) {
	req := Request{Hook: Hook(hook), Payload: payload}
	// Convenience — promote common top-level fields from payload.
	if v, ok := payload["line"].(string); ok {
		req.Line = v
	}
	if v, ok := payload["severity"].(string); ok {
		req.Severity = v
	}
	if v, ok := payload["text"].(string); ok {
		req.Text = v
	}
	if v, ok := payload["session_id"].(string); ok {
		req.Session = v
	}
	resp, err := a.R.Invoke(ctx, name, Hook(hook), req)
	if err != nil {
		return nil, fmt.Errorf("invoke: %w", err)
	}
	return resp, nil
}
