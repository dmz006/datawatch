// BL105 — pipeline → orchestrator translator tests.

package agents

import (
	"testing"

	"github.com/dmz006/datawatch/internal/pipeline"
)

func TestOrchestratorPlanFromPipeline_SplitsTasks(t *testing.T) {
	p := &pipeline.Pipeline{
		ID: "p", Name: "n",
		Tasks: []*pipeline.Task{
			{ID: "build", Title: "make build",
				ProjectProfile: "alpha", ClusterProfile: "k8s-prod"},
			{ID: "test", Title: "go test ./...",
				DependsOn: []string{"build"}},
			{ID: "deploy", Title: "kubectl apply",
				ProjectProfile: "alpha", ClusterProfile: "k8s-prod",
				DependsOn: []string{"test"}, Branch: "main"},
		},
	}
	plan, legacy, err := OrchestratorPlanFromPipeline(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 2 {
		t.Errorf("plan.Nodes=%d want 2 (build+deploy)", len(plan.Nodes))
	}
	if len(legacy) != 1 || legacy[0].ID != "test" {
		t.Errorf("legacy=%v want [test]", legacy)
	}
	// Verify dependency carried over for the deploy node.
	for _, n := range plan.Nodes {
		if n.ID == "deploy" {
			if len(n.DependsOn) != 1 || n.DependsOn[0] != "test" {
				t.Errorf("deploy.DependsOn=%v want [test]", n.DependsOn)
			}
			if n.Branch != "main" {
				t.Errorf("deploy.Branch=%q want main", n.Branch)
			}
		}
	}
}

func TestOrchestratorPlanFromPipeline_NoProfiles_AllLegacy(t *testing.T) {
	p := &pipeline.Pipeline{
		Tasks: []*pipeline.Task{
			{ID: "a", Title: "echo a"},
			{ID: "b", Title: "echo b"},
		},
	}
	plan, legacy, err := OrchestratorPlanFromPipeline(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Nodes) != 0 {
		t.Errorf("plan.Nodes=%d want 0", len(plan.Nodes))
	}
	if len(legacy) != 2 {
		t.Errorf("legacy=%d want 2", len(legacy))
	}
}

func TestOrchestratorPlanFromPipeline_NilGuard(t *testing.T) {
	if _, _, err := OrchestratorPlanFromPipeline(nil); err == nil {
		t.Error("expected error for nil pipeline")
	}
}

func TestOrchestratorPlanFromPipeline_PartialProfileFallsBack(t *testing.T) {
	// Project set but cluster empty → still legacy. Operator must
	// supply BOTH for orchestrator dispatch.
	p := &pipeline.Pipeline{Tasks: []*pipeline.Task{
		{ID: "x", Title: "t", ProjectProfile: "alpha"},
		{ID: "y", Title: "t", ClusterProfile: "k8s"},
	}}
	plan, legacy, _ := OrchestratorPlanFromPipeline(p)
	if len(plan.Nodes) != 0 {
		t.Errorf("plan.Nodes=%d want 0 (partial profile must fall back)", len(plan.Nodes))
	}
	if len(legacy) != 2 {
		t.Errorf("legacy=%d want 2", len(legacy))
	}
}
