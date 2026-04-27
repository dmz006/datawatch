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

// SessionIDsForPRD walks the PRD's stories + tasks and returns every
// non-empty Task.SessionID. Used by the orchestrator REST handler
// (S13 follow-up) to join graph nodes against observer envelopes.
// Returns nil for unknown PRDs; empty slice for PRDs whose tasks
// haven't been scheduled yet.
func (a *API) SessionIDsForPRD(prdID string) []string {
	prd, ok := a.M.Store().GetPRD(prdID)
	if !ok {
		return nil
	}
	var out []string
	for _, s := range prd.Story {
		for _, t := range s.Tasks {
			if t.SessionID != "" {
				out = append(out, t.SessionID)
			}
		}
	}
	return out
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
//
// BL191 Q1 (v5.2.0) — Manager.Run now refuses to start unless the PRD
// is in PRDApproved (or legacy PRDActive) status. The status-only
// fallback path here also flips to PRDRunning so the operator's loop
// status reflects what's happening.
func (a *API) Run(id string) error {
	prd, ok := a.M.Store().GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	if prd.Status != PRDApproved && prd.Status != PRDActive && prd.Status != PRDRunning {
		return fmt.Errorf("prd %q status %q is not runnable; call /approve first", id, prd.Status)
	}
	prd.Status = PRDRunning
	if err := a.M.Store().SavePRD(prd); err != nil {
		return err
	}
	if a.spawnFn == nil || a.verify == nil {
		return nil // executors not configured — status-only mode
	}
	go func() {
		if err := a.M.Run(context.Background(), id, a.spawnFn, a.verify); err != nil {
			if p, ok := a.M.Store().GetPRD(id); ok {
				p.Status = PRDApproved // operator can re-trigger; don't drop back to draft
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
	prd.Status = PRDCancelled
	return a.M.Store().SavePRD(prd)
}

// BL191 Q1 (v5.2.0) — review/approve/reject/edit-task surfaces.

func (a *API) Approve(id, actor, note string) (any, error) {
	return a.M.Approve(id, actor, note)
}
func (a *API) Reject(id, actor, reason string) (any, error) {
	return a.M.Reject(id, actor, reason)
}
func (a *API) RequestRevision(id, actor, note string) (any, error) {
	return a.M.RequestRevision(id, actor, note)
}
func (a *API) EditTaskSpec(prdID, taskID, newSpec, actor string) (any, error) {
	return a.M.EditTaskSpec(prdID, taskID, newSpec, actor)
}
func (a *API) InstantiateTemplate(templateID string, vars map[string]string, actor string) (any, error) {
	return a.M.InstantiateTemplate(templateID, vars, actor)
}

// BL203 (v5.4.0) — flexible LLM overrides at PRD + task level.
func (a *API) SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (any, error) {
	return a.M.SetTaskLLM(prdID, taskID, backend, effort, model, actor)
}
func (a *API) SetPRDLLM(prdID, backend, effort, model, actor string) (any, error) {
	return a.M.SetPRDLLM(prdID, backend, effort, model, actor)
}

func (a *API) ListLearnings() []any {
	src := a.M.Store().ListLearnings()
	out := make([]any, len(src))
	for i, l := range src {
		out[i] = l
	}
	return out
}

// ListChildPRDs (BL191 Q4, v5.9.0) returns every PRD spawned from this
// PRD's SpawnPRD tasks. Empty list when none — same shape as the root
// list endpoint so PWA / chat clients can render uniformly.
func (a *API) ListChildPRDs(prdID string) []any {
	src := a.M.Store().ListChildPRDs(prdID)
	out := make([]any, len(src))
	for i, p := range src {
		out[i] = p
	}
	return out
}

// DeletePRD (v5.19.0) hard-removes a PRD + its SpawnPRD descendants.
// The operator-facing "remove from list" affordance.
func (a *API) DeletePRD(id string) error {
	return a.M.DeletePRD(id)
}

// EditPRDFields (v5.19.0) edits the PRD-level Title + Spec on a non-
// running PRD. Records a Decision audit row. Empty title/spec leaves
// that field unchanged.
func (a *API) EditPRDFields(id, title, spec, actor string) (any, error) {
	return a.M.EditPRDFields(id, title, spec, actor)
}
