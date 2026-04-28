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
	"sync"
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

	// v5.26.16 — track per-PRD run goroutines so Cancel / DeletePRD
	// can stop the executor mid-flight. Pre-v5.26.16 the goroutine
	// kept spawning new tmux sessions even after the PRD was
	// cancelled or hard-deleted, leaving orphan `autonomous:*`
	// sessions that ate the session.max_sessions cap. The cancel
	// callbacks are removed from the map when the goroutine exits.
	runMu      sync.Mutex
	runCancels map[string]context.CancelFunc
}

// NewAPI is a convenience constructor.
func NewAPI(m *Manager) *API {
	return &API{M: m, runCancels: map[string]context.CancelFunc{}}
}

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
	prd, err := a.M.CreatePRD(spec, projectDir, backend, Effort(effort))
	if err == nil && prd != nil {
		a.M.EmitPRDUpdate(prd.ID)
	}
	return prd, err
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
	out, err := a.M.Decompose(id)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
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
	a.M.EmitPRDUpdate(id)
	if a.spawnFn == nil || a.verify == nil {
		return nil // executors not configured — status-only mode
	}
	// v5.26.16 — cancellable per-PRD context so Cancel / DeletePRD
	// can stop the executor goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	a.runMu.Lock()
	if a.runCancels == nil {
		a.runCancels = map[string]context.CancelFunc{}
	}
	// Cancel any previous run for the same PRD ID before replacing.
	if prev, ok := a.runCancels[id]; ok {
		prev()
	}
	a.runCancels[id] = cancel
	a.runMu.Unlock()
	go func() {
		defer func() {
			a.runMu.Lock()
			if a.runCancels[id] != nil {
				a.runCancels[id]()
				delete(a.runCancels, id)
			}
			a.runMu.Unlock()
		}()
		if err := a.M.Run(ctx, id, a.spawnFn, a.verify); err != nil {
			if p, ok := a.M.Store().GetPRD(id); ok {
				// If cancelled by operator, leave status as cancelled.
				// Otherwise revert to PRDApproved so they can retry.
				if p.Status != PRDCancelled {
					p.Status = PRDApproved
					_ = a.M.Store().SavePRD(p)
				}
			}
		}
		// v5.24.0 — emit a final update so the PWA reflects the
		// terminal state (completed / blocked / approved-on-error).
		a.M.EmitPRDUpdate(id)
	}()
	return nil
}

// cancelRun is the internal trampoline Cancel + DeletePRD share to
// stop the executor goroutine. Best-effort — silent if no run is
// active.
func (a *API) cancelRun(id string) {
	a.runMu.Lock()
	cancel, ok := a.runCancels[id]
	if ok {
		delete(a.runCancels, id)
	}
	a.runMu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
}

func (a *API) Cancel(id string) error {
	prd, ok := a.M.Store().GetPRD(id)
	if !ok {
		return fmt.Errorf("prd %q not found", id)
	}
	a.cancelRun(id) // v5.26.16 — stop the executor goroutine first
	prd.Status = PRDCancelled
	if err := a.M.Store().SavePRD(prd); err != nil {
		return err
	}
	a.M.EmitPRDUpdate(id)
	return nil
}

// BL191 Q1 (v5.2.0) — review/approve/reject/edit-task surfaces.
// v5.24.0 — every mutating wrapper emits a PRD update so the PWA
// can refresh the Autonomous tab over WS without operator action.

func (a *API) Approve(id, actor, note string) (any, error) {
	out, err := a.M.Approve(id, actor, note)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}
func (a *API) Reject(id, actor, reason string) (any, error) {
	out, err := a.M.Reject(id, actor, reason)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}
func (a *API) RequestRevision(id, actor, note string) (any, error) {
	out, err := a.M.RequestRevision(id, actor, note)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}
func (a *API) EditTaskSpec(prdID, taskID, newSpec, actor string) (any, error) {
	out, err := a.M.EditTaskSpec(prdID, taskID, newSpec, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) EditStory(prdID, storyID, newTitle, newDescription, actor string) (any, error) {
	out, err := a.M.EditStory(prdID, storyID, newTitle, newDescription, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}

// Phase 3 (v5.26.60) — per-story endpoints.
func (a *API) SetStoryProfile(prdID, storyID, profile, actor string) (any, error) {
	out, err := a.M.SetStoryProfile(prdID, storyID, profile, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) ApproveStory(prdID, storyID, actor string) (any, error) {
	out, err := a.M.ApproveStory(prdID, storyID, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) RejectStory(prdID, storyID, actor, reason string) (any, error) {
	out, err := a.M.RejectStory(prdID, storyID, actor, reason)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) InstantiateTemplate(templateID string, vars map[string]string, actor string) (any, error) {
	newPRD, err := a.M.InstantiateTemplate(templateID, vars, actor)
	if err == nil && newPRD != nil {
		a.M.EmitPRDUpdate(newPRD.ID)
	}
	return newPRD, err
}

// BL203 (v5.4.0) — flexible LLM overrides at PRD + task level.
func (a *API) SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (any, error) {
	out, err := a.M.SetTaskLLM(prdID, taskID, backend, effort, model, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
}
func (a *API) SetPRDLLM(prdID, backend, effort, model, actor string) (any, error) {
	out, err := a.M.SetPRDLLM(prdID, backend, effort, model, actor)
	if err == nil {
		a.M.EmitPRDUpdate(prdID)
	}
	return out, err
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
	// v5.26.16 — stop the executor goroutine before mutating store.
	// Manager.DeletePRD will refuse if status==PRDRunning, but the
	// executor goroutine for an already-cancelled PRD might still be
	// finishing up a spawn round-trip. Cancelling forces the loop to
	// bail on next iteration.
	a.cancelRun(id)
	if err := a.M.DeletePRD(id); err != nil {
		return err
	}
	// v5.24.0 — broadcast deletion as nil-PRD so PWA drops the row.
	// notifyPRDUpdate handles the nil case gracefully.
	a.M.EmitPRDUpdate(id)
	return nil
}

// EditPRDFields (v5.19.0) edits the PRD-level Title + Spec on a non-
// running PRD. Records a Decision audit row. Empty title/spec leaves
// that field unchanged.
func (a *API) EditPRDFields(id, title, spec, actor string) (any, error) {
	out, err := a.M.EditPRDFields(id, title, spec, actor)
	if err == nil {
		a.M.EmitPRDUpdate(id)
	}
	return out, err
}

// SetPRDProfiles (v5.26.19) attaches F10 project + cluster profiles
// to an existing PRD. Either or both can be empty to leave the
// existing value untouched (use empty string explicitly to clear via
// the future patch endpoint). Manager.SetPRDProfiles validates the
// referenced profiles exist before persisting.
func (a *API) SetPRDProfiles(prdID, projectProfile, clusterProfile string) error {
	if err := a.M.SetPRDProfiles(prdID, projectProfile, clusterProfile); err != nil {
		return err
	}
	a.M.EmitPRDUpdate(prdID)
	return nil
}
