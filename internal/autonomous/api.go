// BL24+BL25 — adapter that lets the server package consume the
// autonomous Manager through the small AutonomousAPI interface
// (server/api.go) without an import cycle.
//
// Returns concrete autonomous types as `any` — the JSON encoder on
// the server side does the right thing.

package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
)

// API wraps a *Manager to satisfy server.AutonomousAPI.
//
// v4.0.1: Spawn/Verify fns can be wired via SetExecutors so the REST
// POST /api/autonomous/prds/{id}/run actually walks the task DAG
// (Manager.Run) rather than just flipping PRD.Status. When either fn
// is nil, Run() falls back to the v3.10 status-only behaviour so the
// daemon still starts cleanly on operators that haven't configured
// worker spawning yet.
type API struct {
	M       *Manager
	spawnFn SpawnFn
	verify  VerifyFn
}

// NewAPI is a convenience constructor.
func NewAPI(m *Manager) *API { return &API{M: m} }

// SetExecutors wires the real spawn + verify indirections used by the
// executor walk. Called from main.go once session.Manager + BL103
// validator wiring are available.
func (a *API) SetExecutors(spawn SpawnFn, verify VerifyFn) {
	a.spawnFn = spawn
	a.verify = verify
}

func (a *API) Config() any { return a.M.Config() }

// SetConfig accepts json.RawMessage (what the REST handler passes) so
// callers don't need to know our concrete Config shape.
func (a *API) SetConfig(v any) error {
	raw, ok := v.(json.RawMessage)
	if !ok {
		// Try to marshal whatever was passed back through JSON.
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
	a.M.SetConfig(cfg)
	return nil
}

func (a *API) Status() any { return a.M.Status() }

func (a *API) CreatePRD(spec, projectDir, backend, effort string) (any, error) {
	return a.M.CreatePRD(spec, projectDir, backend, Effort(effort))
}

func (a *API) GetPRD(id string) (any, bool) {
	p, ok := a.M.Store().GetPRD(id)
	if !ok {
		return nil, false
	}
	return p, true
}

func (a *API) ListPRDs() []any {
	src := a.M.Store().ListPRDs()
	out := make([]any, len(src))
	for i, p := range src {
		out[i] = p
	}
	return out
}

func (a *API) Decompose(id string) (any, error) {
	return a.M.Decompose(id)
}

// Run walks the PRD task DAG through Manager.Run when executors are
// wired. Falls back to status-only update when they're not, so the
// REST surface continues to behave sanely in bare-daemon mode.
func (a *API) Run(id string) error {
	prd, ok := a.M.Store().GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	prd.Status = PRDActive
	if err := a.M.Store().SavePRD(prd); err != nil {
		return err
	}
	if a.spawnFn == nil || a.verify == nil {
		return nil // executors not configured — status-only mode
	}
	// Execute the DAG in the background so the REST response stays
	// fast; the caller polls GET /api/autonomous/prds/{id} for state.
	go func() {
		if err := a.M.Run(context.Background(), id, a.spawnFn, a.verify); err != nil {
			if p, ok := a.M.Store().GetPRD(id); ok {
				p.Status = PRDDraft // operator can re-trigger after fix
				_ = a.M.Store().SavePRD(p)
			}
		}
	}()
	return nil
}

func (a *API) Cancel(id string) error {
	prd, ok := a.M.Store().GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	prd.Status = PRDArchived
	return a.M.Store().SavePRD(prd)
}

func (a *API) ListLearnings() []any {
	src := a.M.Store().ListLearnings()
	out := make([]any, len(src))
	for i, l := range src {
		out[i] = l
	}
	return out
}
