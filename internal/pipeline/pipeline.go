// Package pipeline implements session chaining with DAG execution.
// A pipeline is a directed acyclic graph of tasks where each task
// is a datawatch session. Tasks execute in dependency order with
// parallelism where possible.
package pipeline

import (
	"fmt"
	"sync"
	"time"
)

// State represents the lifecycle state of a pipeline or task.
type State string

const (
	StatePending   State = "pending"
	StateRunning   State = "running"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Task is a single unit of work in a pipeline.
type Task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	DependsOn   []string `json:"depends_on,omitempty"`
	State       State    `json:"state"`
	SessionID   string   `json:"session_id,omitempty"` // datawatch session ID when running
	Error       string   `json:"error,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// BL105 — when ProjectProfile + ClusterProfile are set the
	// executor dispatches this task through agents.Orchestrator
	// (multi-container DAG) instead of the local SessionStarter.
	// Both empty = legacy single-host session execution.
	ProjectProfile string `json:"project_profile,omitempty"`
	ClusterProfile string `json:"cluster_profile,omitempty"`
	Branch         string `json:"branch,omitempty"`
}

// Pipeline is a DAG of tasks to execute.
type Pipeline struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ProjectDir  string    `json:"project_dir"`
	Tasks       []*Task   `json:"tasks"`
	State       State     `json:"state"`
	MaxParallel int       `json:"max_parallel"`
	CreatedAt   time.Time `json:"created_at"`
	Error       string    `json:"error,omitempty"`

	mu sync.Mutex
}

// NewPipeline creates a pipeline from a list of task definitions.
func NewPipeline(name, projectDir string, tasks []*Task, maxParallel int) *Pipeline {
	if maxParallel <= 0 {
		maxParallel = 3
	}
	id := fmt.Sprintf("pipe-%d", time.Now().UnixMilli()%100000)
	return &Pipeline{
		ID:          id,
		Name:        name,
		ProjectDir:  projectDir,
		Tasks:       tasks,
		State:       StatePending,
		MaxParallel: maxParallel,
		CreatedAt:   time.Now(),
	}
}

// TaskByID returns a task by ID, or nil.
func (p *Pipeline) TaskByID(id string) *Task {
	for _, t := range p.Tasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// ReadyTasks returns tasks whose dependencies are all completed.
func (p *Pipeline) ReadyTasks() []*Task {
	p.mu.Lock()
	defer p.mu.Unlock()

	completed := make(map[string]bool)
	for _, t := range p.Tasks {
		if t.State == StateCompleted {
			completed[t.ID] = true
		}
	}

	var ready []*Task
	for _, t := range p.Tasks {
		if t.State != StatePending {
			continue
		}
		allDepsComplete := true
		for _, dep := range t.DependsOn {
			if !completed[dep] {
				allDepsComplete = false
				break
			}
		}
		if allDepsComplete {
			ready = append(ready, t)
		}
	}
	return ready
}

// RunningCount returns the number of currently running tasks.
func (p *Pipeline) RunningCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, t := range p.Tasks {
		if t.State == StateRunning {
			count++
		}
	}
	return count
}

// MarkRunning marks a task as running with the given session ID.
func (p *Pipeline) MarkRunning(taskID, sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t := p.TaskByID(taskID); t != nil {
		t.State = StateRunning
		t.SessionID = sessionID
		now := time.Now()
		t.StartedAt = &now
	}
}

// MarkCompleted marks a task as completed.
func (p *Pipeline) MarkCompleted(taskID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t := p.TaskByID(taskID); t != nil {
		t.State = StateCompleted
		now := time.Now()
		t.CompletedAt = &now
	}
}

// MarkFailed marks a task as failed with an error.
func (p *Pipeline) MarkFailed(taskID, err string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t := p.TaskByID(taskID); t != nil {
		t.State = StateFailed
		t.Error = err
		now := time.Now()
		t.CompletedAt = &now
	}
	p.State = StateFailed
	p.Error = fmt.Sprintf("task %s failed: %s", taskID, err)
}

// IsComplete returns true if all tasks are completed.
func (p *Pipeline) IsComplete() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.Tasks {
		if t.State != StateCompleted {
			return false
		}
	}
	return true
}

// Cancel marks all pending tasks as cancelled.
func (p *Pipeline) Cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range p.Tasks {
		if t.State == StatePending {
			t.State = StateCancelled
		}
	}
	p.State = StateCancelled
}

// Summary returns a text summary of pipeline status.
func (p *Pipeline) Summary() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	counts := map[State]int{}
	for _, t := range p.Tasks {
		counts[t.State]++
	}
	return fmt.Sprintf("[%s] %s: %d tasks (%d done, %d running, %d pending, %d failed)",
		p.ID, p.Name, len(p.Tasks),
		counts[StateCompleted], counts[StateRunning],
		counts[StatePending], counts[StateFailed])
}

// DetectCycles checks for circular dependencies using Kahn's algorithm (BL39).
// Returns the cycle path if found, or nil if the DAG is valid.
func DetectCycles(tasks []*Task) []string {
	// Build adjacency and in-degree
	inDegree := make(map[string]int)
	adj := make(map[string][]string)
	for _, t := range tasks {
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		for _, dep := range t.DependsOn {
			adj[dep] = append(adj[dep], t.ID)
			inDegree[t.ID]++
		}
	}

	// Kahn's algorithm
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	sorted := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted++
		for _, next := range adj[node] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if sorted < len(tasks) {
		// Cycle detected — find the cycle path
		var cycle []string
		for id, deg := range inDegree {
			if deg > 0 {
				cycle = append(cycle, id)
			}
		}
		return cycle
	}
	return nil
}

// ParsePipelineSpec parses a pipeline specification string.
// Format: "task1: do thing -> task2: do next -> task3: do last"
// All tasks depend on the previous one (linear chain).
func ParsePipelineSpec(spec string) []*Task {
	var tasks []*Task
	parts := splitPipelineSpec(spec)
	for i, part := range parts {
		id := fmt.Sprintf("t%d", i+1)
		task := &Task{
			ID:    id,
			Title: part,
			State: StatePending,
		}
		if i > 0 {
			task.DependsOn = []string{fmt.Sprintf("t%d", i)}
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func splitPipelineSpec(spec string) []string {
	// Split on " -> " or " → "
	var parts []string
	for _, sep := range []string{" -> ", " → ", " | "} {
		if len(parts) == 0 {
			parts = splitOn(spec, sep)
			if len(parts) > 1 {
				return parts
			}
		}
	}
	// Single task
	return []string{spec}
}

func splitOn(s, sep string) []string {
	var result []string
	for {
		idx := indexOf(s, sep)
		if idx < 0 {
			result = append(result, trim(s))
			break
		}
		result = append(result, trim(s[:idx]))
		s = s[idx+len(sep):]
	}
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') { start++ }
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') { end-- }
	return s[start:end]
}
