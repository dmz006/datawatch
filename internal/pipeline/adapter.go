package pipeline

import (
	"fmt"
	"strings"
)

// RouterAdapter adapts the Executor for the router's PipelineExecutor interface.
type RouterAdapter struct {
	exec *Executor
}

// NewRouterAdapter creates a router adapter for the pipeline executor.
func NewRouterAdapter(exec *Executor) *RouterAdapter {
	return &RouterAdapter{exec: exec}
}

func (a *RouterAdapter) StartPipeline(spec, projectDir string, _ []string, maxParallel int) (string, error) {
	tasks := ParsePipelineSpec(spec)
	if len(tasks) == 0 {
		return "", fmt.Errorf("no tasks in pipeline spec")
	}
	p, err := NewPipeline(spec, projectDir, tasks, maxParallel)
	if err != nil {
		return "", err
	}
	if err := a.exec.Start(p); err != nil {
		return "", err
	}
	return p.Summary(), nil
}

func (a *RouterAdapter) GetStatus(id string) string {
	p := a.exec.Get(id)
	if p == nil {
		return fmt.Sprintf("Pipeline %s not found.", id)
	}
	return p.Summary()
}

func (a *RouterAdapter) Cancel(id string) error {
	return a.exec.Cancel(id)
}

func (a *RouterAdapter) ListAll() string {
	pipelines := a.exec.List()
	if len(pipelines) == 0 {
		return "No pipelines."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Pipelines (%d):\n", len(pipelines))
	for _, p := range pipelines {
		fmt.Fprintf(&b, "  %s\n", p.Summary())
	}
	return b.String()
}

func (a *RouterAdapter) ListJSON() []map[string]interface{} {
	pipelines := a.exec.List()
	result := make([]map[string]interface{}, 0, len(pipelines))
	for _, p := range pipelines {
		result = append(result, map[string]interface{}{
			"id":           p.ID,
			"name":         p.Name,
			"state":        p.State,
			"project_dir":  p.ProjectDir,
			"max_parallel": p.MaxParallel,
			"tasks":        p.Tasks,
			"created_at":   p.CreatedAt,
			"error":        p.Error,
		})
	}
	return result
}
