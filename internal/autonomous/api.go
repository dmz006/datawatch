// BL24+BL25 — adapter that lets the server package consume the
// autonomous Manager through the small AutonomousAPI interface
// (server/api.go) without an import cycle.
//
// Returns concrete autonomous types as `any` — the JSON encoder on
// the server side does the right thing.

package autonomous

import (
	"encoding/json"
	"fmt"
)

// API wraps a *Manager to satisfy server.AutonomousAPI.
type API struct{ M *Manager }

// NewAPI is a convenience constructor.
func NewAPI(m *Manager) *API { return &API{M: m} }

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

// Run / Cancel are placeholders in v1 — wired to executor in main.go
// once SpawnFn / VerifyFn are configured. For now they update status
// metadata so the REST surface returns sensible responses.
func (a *API) Run(id string) error {
	prd, ok := a.M.Store().GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	prd.Status = PRDActive
	return a.M.Store().SavePRD(prd)
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
