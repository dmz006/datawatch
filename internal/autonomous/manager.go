// BL24 — central coordinator. Wires the store, decomposer, and the
// background loop. Mirrors nightwire/autonomous/manager.py shape.

package autonomous

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// maxDecisionsPerPRD (BL291, v5.5.0) — cap on PRD.Decisions to prevent
// unbounded growth on long-lived PRDs. The autonomous loop appends a
// row on every decompose / approve / run / verify / edit / set_llm /
// set_task_llm action; without a cap a PRD that's been re-decomposed +
// re-run hundreds of times grows to multi-MB JSONL rows that bloat
// every Store.SavePRD write. Trim keeps the most recent N entries.
const maxDecisionsPerPRD = 200

// trimDecisions returns ds capped at maxDecisionsPerPRD entries (most
// recent kept). Caller passes the slice after appending; safe on nil
// or empty.
func trimDecisions(ds []Decision) []Decision {
	if len(ds) <= maxDecisionsPerPRD {
		return ds
	}
	return ds[len(ds)-maxDecisionsPerPRD:]
}

// Config is the operator-tunable knobs for the autonomous system.
// Mirrors session.* config fields per the no-hard-coded-config rule.
// JSON tags use snake_case so REST + YAML payloads can be applied
// directly via SetConfig.
type Config struct {
	Enabled              bool   `json:"enabled"`
	PollIntervalSeconds  int    `json:"poll_interval_seconds,omitempty"`
	MaxParallelTasks     int    `json:"max_parallel_tasks,omitempty"`
	DecompositionBackend string `json:"decomposition_backend,omitempty"`
	VerificationBackend  string `json:"verification_backend,omitempty"`
	DecompositionEffort  string `json:"decomposition_effort,omitempty"`
	VerificationEffort   string `json:"verification_effort,omitempty"`
	StaleTaskSeconds     int    `json:"stale_task_seconds,omitempty"`
	AutoFixRetries       int    `json:"auto_fix_retries,omitempty"`
	SecurityScan         bool   `json:"security_scan,omitempty"`

	// BL191 Q4 (v5.9.0) — recursive child-PRD knobs. MaxRecursionDepth
	// caps the parent→child PRD chain length (default 5; 0 disables
	// recursion entirely). AutoApproveChildren skips the
	// needs_review→approved gate for spawned children — needed for any
	// useful recursion, since otherwise every level hangs on operator
	// review. Defaults: depth=5, auto_approve=true.
	MaxRecursionDepth     int  `json:"max_recursion_depth,omitempty"`
	AutoApproveChildren   bool `json:"auto_approve_children,omitempty"`
}

// DefaultConfig returns sane defaults — autonomous OFF until operator opts in.
func DefaultConfig() Config {
	return Config{
		Enabled:             false,
		PollIntervalSeconds: 30,
		MaxParallelTasks:    3,
		AutoFixRetries:      1,
		SecurityScan:        true,
		MaxRecursionDepth:   5,
		AutoApproveChildren: true,
	}
}

// Manager is the public façade for the autonomous package.
type Manager struct {
	mu        sync.Mutex
	cfg       Config
	store     *Store
	decompose DecomposeFn

	// loop state
	ctx    context.Context
	cancel context.CancelFunc
	status LoopStatus
}

// NewManager constructs the Manager and opens the store under dataDir.
func NewManager(dataDir string, cfg Config, decompose DecomposeFn) (*Manager, error) {
	st, err := NewStore(dataDir)
	if err != nil {
		return nil, err
	}
	return &Manager{
		cfg:       cfg,
		store:     st,
		decompose: decompose,
	}, nil
}

// SetConfig replaces the runtime config (hot-reload entry).
func (m *Manager) SetConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

// Config returns a copy of the current config.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Store exposes the underlying store (REST handlers and tests use it).
func (m *Manager) Store() *Store { return m.store }

// CreatePRD records a draft PRD without decomposing — call Decompose
// next or pass to Run() which decomposes lazily.
func (m *Manager) CreatePRD(spec, projectDir, backend string, effort Effort) (*PRD, error) {
	return m.store.CreatePRD(spec, projectDir, backend, effort)
}

// Decompose calls the LLM via the configured DecomposeFn and persists
// the resulting story tree. Returns the updated PRD.
//
// BL191 (v5.2.0) — status transitions: PRDDraft (or PRDRevisionsAsked)
// → PRDDecomposing → PRDNeedsReview. The operator must explicitly
// Approve before Run is allowed; see Manager.Approve and Manager.Run.
// Decomposition records a Decision row on the PRD with the LLM call
// metadata (backend, prompt size, response size).
func (m *Manager) Decompose(prdID string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if m.decompose == nil {
		return nil, fmt.Errorf("decompose fn not configured")
	}
	if prd.IsTemplate {
		return nil, fmt.Errorf("prd %q is a template; instantiate it first", prdID)
	}
	backend := prd.Backend
	if backend == "" {
		backend = m.cfg.DecompositionBackend
	}
	effort := prd.Effort
	if effort == "" {
		effort = Effort(m.cfg.DecompositionEffort)
	}
	// Mark in-flight so the operator can see it from /api/autonomous/prds/{id}.
	prd.Status = PRDDecomposing
	prd.UpdatedAt = time.Now()
	_ = m.store.SavePRD(prd)

	prompt := fmt.Sprintf(DecompositionPrompt, prd.Spec)
	raw, err := m.decompose(DecomposeRequest{Spec: prompt, Backend: backend, Effort: effort})
	if err != nil {
		// Roll back to draft so the operator can re-trigger.
		prd.Status = PRDDraft
		prd.UpdatedAt = time.Now()
		_ = m.store.SavePRD(prd)
		return nil, fmt.Errorf("LLM decompose: %w", err)
	}
	title, stories, err := ParseDecomposition(raw)
	if err != nil {
		prd.Status = PRDDraft
		prd.UpdatedAt = time.Now()
		_ = m.store.SavePRD(prd)
		return nil, err
	}
	if title != "" {
		prd.Title = title
	}
	// BL191 Q1: operator review/approve gate. Status lands in needs_review
	// rather than active/running. Approve / Reject / RequestRevision are
	// the explicit transitions out.
	prd.Status = PRDNeedsReview
	prd.Decisions = append(prd.Decisions, Decision{
		At:            time.Now(),
		Kind:          "decompose",
		Backend:       backend,
		PromptChars:   len(prompt),
		ResponseChars: len(raw),
		Actor:         "autonomous",
	})
	if err := m.store.SetStories(prdID, stories); err != nil {
		return nil, err
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Approve transitions a PRD from needs_review → approved so the loop /
// Manager.Run is allowed to start workers. BL191 Q1.
func (m *Manager) Approve(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is not approvable", prdID, prd.Status)
	}
	now := time.Now()
	prd.Status = PRDApproved
	prd.ApprovedBy = actor
	prd.ApprovedAt = &now
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "approve", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Reject marks a PRD as terminally rejected. The decomposition stays
// stored for inspection but the loop will never run it.
func (m *Manager) Reject(prdID, actor, reason string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status == PRDCompleted {
		return nil, fmt.Errorf("prd %q already completed; nothing to reject", prdID)
	}
	now := time.Now()
	prd.Status = PRDRejected
	prd.RejectionReason = reason
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "reject", Actor: actor, Note: reason})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// RequestRevision asks the operator (or a follow-up Decompose call)
// for a new decomposition. The PRD lands back in revisions_asked;
// calling Decompose again resets it to needs_review.
func (m *Manager) RequestRevision(prdID, actor, note string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDApproved && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q does not accept revisions", prdID, prd.Status)
	}
	now := time.Now()
	prd.Status = PRDRevisionsAsked
	prd.RevisionsRequested++
	prd.UpdatedAt = now
	prd.Decisions = append(prd.Decisions, Decision{At: now, Kind: "request_revision", Actor: actor, Note: note})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// EditTaskSpec lets the operator rewrite an LLM-decomposed task's spec
// before approving the PRD. Only allowed in needs_review or
// revisions_asked. Records a Decision so the timeline shows the edit.
func (m *Manager) EditTaskSpec(prdID, taskID, newSpec, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked accept task edits", prdID, prd.Status)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].Spec = newSpec
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "edit_task", Actor: actor,
		Note: fmt.Sprintf("task=%s spec_chars=%d", taskID, len(newSpec)),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetTaskLLM (BL203, v5.4.0) lets the operator override a task's
// worker LLM (backend / effort / model) before approval. Empty string
// clears the override (falls back to PRD-level then global). Allowed
// in needs_review / revisions_asked only — once approved or running
// the worker is locked in.
func (m *Manager) SetTaskLLM(prdID, taskID, backend, effort, model, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status != PRDNeedsReview && prd.Status != PRDRevisionsAsked {
		return nil, fmt.Errorf("prd %q status %q is locked; only needs_review / revisions_asked accept LLM overrides", prdID, prd.Status)
	}
	found := false
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == taskID {
				prd.Story[si].Tasks[ti].Backend = backend
				prd.Story[si].Tasks[ti].Effort = Effort(effort)
				prd.Story[si].Tasks[ti].Model = model
				prd.Story[si].Tasks[ti].UpdatedAt = time.Now()
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("task %q not found in prd %q", taskID, prdID)
	}
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_task_llm", Actor: actor,
		Note: fmt.Sprintf("task=%s backend=%s effort=%s model=%s", taskID, backend, effort, model),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// SetPRDLLM (BL203, v5.4.0) overrides the PRD-level worker LLM defaults
// (backend / effort / model). Only allowed pre-Run. Tasks without
// per-task overrides will inherit these.
func (m *Manager) SetPRDLLM(prdID, backend, effort, model, actor string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if prd.Status == PRDRunning || prd.Status == PRDCompleted {
		return nil, fmt.Errorf("prd %q status %q is locked; PRD LLM overrides only apply pre-Run", prdID, prd.Status)
	}
	prd.Backend = backend
	prd.Effort = Effort(effort)
	prd.Model = model
	prd.UpdatedAt = time.Now()
	prd.Decisions = append(prd.Decisions, Decision{
		At: time.Now(), Kind: "set_prd_llm", Actor: actor,
		Note: fmt.Sprintf("backend=%s effort=%s model=%s", backend, effort, model),
	})
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// InstantiateTemplate (BL191 Q2) takes a template PRD + caller-supplied
// vars and stores a fresh executable PRD with substitutions applied to
// spec, title, and per-task spec strings. Required vars without a value
// are an error; defaults fill missing optionals.
func (m *Manager) InstantiateTemplate(templateID string, vars map[string]string, actor string) (*PRD, error) {
	tmpl, ok := m.store.GetPRD(templateID)
	if !ok {
		return nil, fmt.Errorf("template %q not found", templateID)
	}
	if !tmpl.IsTemplate {
		return nil, fmt.Errorf("prd %q is not a template", templateID)
	}
	resolved := make(map[string]string, len(tmpl.TemplateVars))
	for _, v := range tmpl.TemplateVars {
		if val, ok := vars[v.Name]; ok && val != "" {
			resolved[v.Name] = val
			continue
		}
		if v.Default != "" {
			resolved[v.Name] = v.Default
			continue
		}
		if v.Required {
			return nil, fmt.Errorf("template var %q is required but missing", v.Name)
		}
	}
	subst := func(s string) string {
		out := s
		for name, val := range resolved {
			out = replaceAll(out, "{{"+name+"}}", val)
		}
		return out
	}
	newPRD, err := m.store.CreatePRD(subst(tmpl.Spec), tmpl.ProjectDir, tmpl.Backend, tmpl.Effort)
	if err != nil {
		return nil, err
	}
	newPRD.Title = subst(tmpl.Title)
	newPRD.TemplateOf = tmpl.ID
	// Copy stories with substitutions applied to per-task specs.
	for _, st := range tmpl.Story {
		st2 := st
		tasks2 := make([]Task, 0, len(st.Tasks))
		for _, t := range st.Tasks {
			t2 := t
			t2.Spec = subst(t.Spec)
			t2.Title = subst(t.Title)
			tasks2 = append(tasks2, t2)
		}
		st2.Tasks = tasks2
		newPRD.Story = append(newPRD.Story, st2)
	}
	if len(newPRD.Story) > 0 {
		// Pre-decomposed from template — land in needs_review so operator
		// can confirm before Run.
		newPRD.Status = PRDNeedsReview
	}
	newPRD.Decisions = append(newPRD.Decisions, Decision{
		At: time.Now(), Kind: "template_instantiate", Actor: actor,
		Note: fmt.Sprintf("from=%s vars=%d", tmpl.ID, len(resolved)),
	})
	if err := m.store.SavePRD(newPRD); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(newPRD.ID)
	return updated, nil
}

// replaceAll is a small allocation-friendly substring replace; bypasses
// strings.ReplaceAll only because we want zero-package-import in this
// hot path.
func replaceAll(s, old, new string) string {
	for {
		i := indexOf(s, old)
		if i < 0 {
			return s
		}
		s = s[:i] + new + s[i+len(old):]
	}
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Status returns a snapshot of the loop state.
func (m *Manager) Status() LoopStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status
	st.Running = m.ctx != nil && m.ctx.Err() == nil
	for _, p := range m.store.ListPRDs() {
		if p.Status == PRDActive || p.Status == PRDRunning {
			st.ActivePRDs++
		}
		for _, s := range p.Story {
			for _, t := range s.Tasks {
				switch t.Status {
				case TaskQueued:
					st.QueuedTasks++
				case TaskInProgress, TaskRunningTests, TaskVerifying:
					st.RunningTasks++
				}
			}
		}
	}
	return st
}

// Start kicks the background loop. Idempotent.
func (m *Manager) Start(parent context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx != nil && m.ctx.Err() == nil {
		return // already running
	}
	if !m.cfg.Enabled {
		return
	}
	m.ctx, m.cancel = context.WithCancel(parent)
	go m.run()
}

// Stop signals the loop to exit (does not block).
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
		m.ctx = nil
	}
}

// run is the loop body. v1 is intentionally minimal: marks newly-
// active PRDs so external schedulers (e.g. main daemon's
// pipeline.Executor or operator-driven `datawatch autonomous run`)
// can pick them up. The actual Task → Session dispatch is done by
// the executor in executor.go (called from REST handler).
func (m *Manager) run() {
	interval := time.Duration(m.cfg.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-t.C:
			m.mu.Lock()
			m.status.LastTickAt = time.Now()
			m.mu.Unlock()
			// Future: scan for stuck tasks and surface to operator.
		}
	}
}
