package pipeline

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// SessionStarter is the interface for creating sessions from the pipeline.
type SessionStarter interface {
	StartSession(task, projectDir, backend string) (string, error)
	GetSessionState(sessionID string) string
}

// Executor manages pipeline lifecycle and dispatches tasks as sessions.
type Executor struct {
	mu        sync.RWMutex
	pipelines map[string]*Pipeline
	starter   SessionStarter
	backend   string
	onUpdate  func(*Pipeline) // callback for state changes
}

// NewExecutor creates a pipeline executor.
func NewExecutor(starter SessionStarter, defaultBackend string) *Executor {
	return &Executor{
		pipelines: make(map[string]*Pipeline),
		starter:   starter,
		backend:   defaultBackend,
	}
}

// SetOnUpdate sets a callback invoked when pipeline state changes.
func (e *Executor) SetOnUpdate(fn func(*Pipeline)) {
	e.onUpdate = fn
}

// Start validates and begins executing a pipeline.
func (e *Executor) Start(p *Pipeline) error {
	// Validate — check for cycles (BL39)
	if cycle := DetectCycles(p.Tasks); cycle != nil {
		return fmt.Errorf("circular dependency detected: %v", cycle)
	}

	e.mu.Lock()
	e.pipelines[p.ID] = p
	p.State = StateRunning
	e.mu.Unlock()

	log.Printf("[pipeline] started %s: %s (%d tasks)", p.ID, p.Name, len(p.Tasks))
	go e.run(p)
	return nil
}

// run is the main execution loop for a pipeline.
func (e *Executor) run(p *Pipeline) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C

		if p.State != StateRunning {
			return
		}

		// Check if any running tasks completed
		for _, t := range p.Tasks {
			if t.State == StateRunning && t.SessionID != "" {
				state := e.starter.GetSessionState(t.SessionID)
				if state == "complete" {
					p.MarkCompleted(t.ID)
					log.Printf("[pipeline] task %s completed (session %s)", t.ID, t.SessionID)
					if e.onUpdate != nil {
						e.onUpdate(p)
					}
				} else if state == "failed" || state == "killed" {
					p.MarkFailed(t.ID, "session "+state)
					log.Printf("[pipeline] task %s failed (session %s: %s)", t.ID, t.SessionID, state)
					if e.onUpdate != nil {
						e.onUpdate(p)
					}
					return
				}
			}
		}

		// Check if pipeline is complete
		if p.IsComplete() {
			p.State = StateCompleted
			log.Printf("[pipeline] %s completed: all %d tasks done", p.ID, len(p.Tasks))
			if e.onUpdate != nil {
				e.onUpdate(p)
			}
			return
		}

		// Launch ready tasks (up to max parallel)
		ready := p.ReadyTasks()
		running := p.RunningCount()
		for _, t := range ready {
			if running >= p.MaxParallel {
				break
			}
			sessionID, err := e.starter.StartSession(t.Title, p.ProjectDir, e.backend)
			if err != nil {
				p.MarkFailed(t.ID, err.Error())
				if e.onUpdate != nil {
					e.onUpdate(p)
				}
				return
			}
			p.MarkRunning(t.ID, sessionID)
			running++
			log.Printf("[pipeline] launched task %s: %s (session %s)", t.ID, t.Title, sessionID)
			if e.onUpdate != nil {
				e.onUpdate(p)
			}
		}
	}
}

// Get returns a pipeline by ID.
func (e *Executor) Get(id string) *Pipeline {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.pipelines[id]
}

// List returns all pipelines.
func (e *Executor) List() []*Pipeline {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]*Pipeline, 0, len(e.pipelines))
	for _, p := range e.pipelines {
		result = append(result, p)
	}
	return result
}

// Cancel cancels a pipeline by ID.
func (e *Executor) Cancel(id string) error {
	e.mu.RLock()
	p, ok := e.pipelines[id]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("pipeline %s not found", id)
	}
	p.Cancel()
	log.Printf("[pipeline] %s cancelled", id)
	return nil
}
