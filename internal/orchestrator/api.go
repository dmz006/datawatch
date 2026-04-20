// BL117 — adapter for the server-package OrchestratorAPI interface.
// Keeps server.go free of a hard dependency on this package.

package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
)

// API wraps a *Runner for the REST/MCP/CLI layers.
type API struct{ R *Runner }

func NewAPI(r *Runner) *API { return &API{R: r} }

func (a *API) Config() any { return a.R.Config() }

func (a *API) SetConfig(v any) error {
	raw, ok := v.(json.RawMessage)
	if !ok {
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		raw = b
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	a.R.SetConfig(cfg)
	return nil
}

func (a *API) CreateGraph(title, projectDir string, prdIDs []string) (any, error) {
	return a.R.Store().CreateGraph(title, projectDir, prdIDs)
}

func (a *API) GetGraph(id string) (any, bool) {
	g, ok := a.R.Store().GetGraph(id)
	if !ok {
		return nil, false
	}
	return g, true
}

func (a *API) ListGraphs() []any {
	src := a.R.Store().ListGraphs()
	out := make([]any, len(src))
	for i, g := range src {
		out[i] = g
	}
	return out
}

func (a *API) CancelGraph(id string) error {
	g, ok := a.R.Store().GetGraph(id)
	if !ok {
		return fmt.Errorf("graph %q not found", id)
	}
	g.Status = GraphCancelled
	return a.R.Store().SaveGraph(g)
}

func (a *API) RunGraph(ctx context.Context, id string) error {
	return a.R.Run(ctx, id)
}

func (a *API) PlanGraph(id string, deps map[string][]string) (any, error) {
	return a.R.Plan(id, deps)
}

func (a *API) ListVerdicts() []any {
	src := a.R.Store().ListVerdicts()
	out := make([]any, len(src))
	for i, v := range src {
		out[i] = v
	}
	return out
}
