// BL24 — central coordinator. Wires the store, decomposer, and the
// background loop. Mirrors nightwire/autonomous/manager.py shape.

package autonomous

import (
	"context"
	"fmt"
	"sync"
	"time"
)

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
}

// DefaultConfig returns sane defaults — autonomous OFF until operator opts in.
func DefaultConfig() Config {
	return Config{
		Enabled:             false,
		PollIntervalSeconds: 30,
		MaxParallelTasks:    3,
		AutoFixRetries:      1,
		SecurityScan:        true,
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
func (m *Manager) Decompose(prdID string) (*PRD, error) {
	prd, ok := m.store.GetPRD(prdID)
	if !ok {
		return nil, fmt.Errorf("prd %q not found", prdID)
	}
	if m.decompose == nil {
		return nil, fmt.Errorf("decompose fn not configured")
	}
	backend := prd.Backend
	if backend == "" {
		backend = m.cfg.DecompositionBackend
	}
	effort := prd.Effort
	if effort == "" {
		effort = Effort(m.cfg.DecompositionEffort)
	}
	prompt := fmt.Sprintf(DecompositionPrompt, prd.Spec)
	raw, err := m.decompose(DecomposeRequest{Spec: prompt, Backend: backend, Effort: effort})
	if err != nil {
		return nil, fmt.Errorf("LLM decompose: %w", err)
	}
	title, stories, err := ParseDecomposition(raw)
	if err != nil {
		return nil, err
	}
	if title != "" {
		prd.Title = title
	}
	prd.Status = PRDActive
	if err := m.store.SetStories(prdID, stories); err != nil {
		return nil, err
	}
	if err := m.store.SavePRD(prd); err != nil {
		return nil, err
	}
	updated, _ := m.store.GetPRD(prdID)
	return updated, nil
}

// Status returns a snapshot of the loop state.
func (m *Manager) Status() LoopStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status
	st.Running = m.ctx != nil && m.ctx.Err() == nil
	for _, p := range m.store.ListPRDs() {
		if p.Status == PRDActive {
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
