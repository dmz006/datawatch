// BL105 — bridge from pipeline.Pipeline → agents.OrchestratorPlan.
//
// F15's pipelines.Executor runs DAGs of single-host sessions. F10's
// orchestrator runs DAGs of multi-container agent spawns. They share
// the same shape (tasks, dependencies, max-parallel) so a Pipeline
// task with ProjectProfile + ClusterProfile set can be translated
// into an OrchestratorNode and dispatched through agents.Manager
// instead of the local SessionStarter.
//
// The bridge is one-way: pipeline → orchestrator. The reverse direction
// (orchestrator results → pipeline state) flows through the existing
// agents.RecordResult + a future poller (BL105-followup).

package agents

import (
	"fmt"

	"github.com/dmz006/datawatch/internal/pipeline"
)

// OrchestratorPlanFromPipeline converts a pipeline.Pipeline into an
// OrchestratorPlan suitable for Orchestrator.Run. Tasks without
// both ProjectProfile and ClusterProfile are returned as the second
// slice — those still need single-host execution and the caller
// should hand them to pipelines.Executor as before.
//
// The split lets a single pipeline mix legacy single-host tasks
// with new multi-container agent spawns.
func OrchestratorPlanFromPipeline(p *pipeline.Pipeline) (*OrchestratorPlan, []*pipeline.Task, error) {
	if p == nil {
		return nil, nil, fmt.Errorf("OrchestratorPlanFromPipeline: pipeline required")
	}
	plan := &OrchestratorPlan{Nodes: make([]OrchestratorNode, 0, len(p.Tasks))}
	var legacy []*pipeline.Task
	for _, t := range p.Tasks {
		if t.ProjectProfile == "" || t.ClusterProfile == "" {
			legacy = append(legacy, t)
			continue
		}
		plan.Nodes = append(plan.Nodes, OrchestratorNode{
			ID:             t.ID,
			ProjectProfile: t.ProjectProfile,
			ClusterProfile: t.ClusterProfile,
			Task:           t.Title,
			Branch:         t.Branch,
			DependsOn:      append([]string(nil), t.DependsOn...),
		})
	}
	return plan, legacy, nil
}
