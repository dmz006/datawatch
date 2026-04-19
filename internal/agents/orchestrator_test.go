// F10 sprint 7 (S7.1) — orchestrator core tests.

package agents

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/profile"
)

// orchestratorFixture wires a Manager with one project + one cluster
// profile + a tiny "auto-completer" that, after each Spawn, fires a
// goroutine to RecordResult so the orchestrator's poll loop unblocks.
func orchestratorFixture(t *testing.T) (*Orchestrator, *Manager) {
	t.Helper()
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})

	m := NewManager(ps, cs)
	m.RegisterDriver(&autoCompleter{kind: "docker", manager: m, status: "ok"})
	return &Orchestrator{
		Manager:       m,
		MaxConcurrent: 4,
		PollInterval:  20 * time.Millisecond,
	}, m
}

// autoCompleter is a Driver that auto-RecordResult's the agent
// shortly after Spawn so orchestrator's poll loop has something to
// observe. status="fail" simulates a worker reporting failure.
type autoCompleter struct {
	kind    string
	manager *Manager
	status  string // "ok" | "fail" | "" → "ok"
}

func (a *autoCompleter) Kind() string { return a.kind }
func (a *autoCompleter) Spawn(_ context.Context, ag *Agent) error {
	ag.DriverInstance = "fake-" + ag.ID
	mgr := a.manager
	st := a.status
	if st == "" {
		st = "ok"
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		_ = mgr.RecordResult(ag.ID, &AgentResult{Status: st, Summary: "auto-" + st})
	}()
	return nil
}
func (a *autoCompleter) Status(_ context.Context, _ *Agent) (State, error) {
	return StateReady, nil
}
func (a *autoCompleter) Logs(_ context.Context, _ *Agent, _ int) (string, error) { return "", nil }
func (a *autoCompleter) Terminate(_ context.Context, _ *Agent) error              { return nil }

// Single-node plan runs to Done.
func TestOrchestrator_SingleNode_Done(t *testing.T) {
	o, _ := orchestratorFixture(t)
	plan := OrchestratorPlan{Nodes: []OrchestratorNode{
		{ID: "a", ProjectProfile: "p", ClusterProfile: "c", Task: "echo"},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	run, err := o.Run(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if run.Nodes["a"].State != NodeDone {
		t.Errorf("state=%s want done; err=%s", run.Nodes["a"].State, run.Nodes["a"].Error)
	}
}

// Linear chain runs each node once and respects ordering.
func TestOrchestrator_LinearChain(t *testing.T) {
	o, _ := orchestratorFixture(t)
	plan := OrchestratorPlan{Nodes: []OrchestratorNode{
		{ID: "a", ProjectProfile: "p", ClusterProfile: "c", Task: "1", Branch: "ba"},
		{ID: "b", ProjectProfile: "p", ClusterProfile: "c", Task: "2", Branch: "bb", DependsOn: []string{"a"}},
		{ID: "c", ProjectProfile: "p", ClusterProfile: "c", Task: "3", Branch: "bc", DependsOn: []string{"b"}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, err := o.Run(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if run.Nodes[id].State != NodeDone {
			t.Errorf("node %s state=%s err=%s", id, run.Nodes[id].State, run.Nodes[id].Error)
		}
	}
	// b must start strictly after a finishes; c after b.
	if !run.Nodes["b"].StartedAt.After(run.Nodes["a"].FinishedAt) {
		t.Errorf("b started before a finished: a.fin=%v b.start=%v",
			run.Nodes["a"].FinishedAt, run.Nodes["b"].StartedAt)
	}
}

// Diamond fan-out: a → {b, c} → d. b + c run concurrently after a.
func TestOrchestrator_Diamond(t *testing.T) {
	o, _ := orchestratorFixture(t)
	plan := OrchestratorPlan{Nodes: []OrchestratorNode{
		{ID: "a", ProjectProfile: "p", ClusterProfile: "c", Task: "a", Branch: "ba"},
		{ID: "b", ProjectProfile: "p", ClusterProfile: "c", Task: "b", Branch: "bb", DependsOn: []string{"a"}},
		{ID: "c", ProjectProfile: "p", ClusterProfile: "c", Task: "c", Branch: "bc", DependsOn: []string{"a"}},
		{ID: "d", ProjectProfile: "p", ClusterProfile: "c", Task: "d", Branch: "bd", DependsOn: []string{"b", "c"}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	run, _ := o.Run(ctx, plan)
	for _, id := range []string{"a", "b", "c", "d"} {
		if run.Nodes[id].State != NodeDone {
			t.Errorf("node %s state=%s err=%s", id, run.Nodes[id].State, run.Nodes[id].Error)
		}
	}
}

// Failure of a midstream node cascades downstream as Skipped.
func TestOrchestrator_FailureCascadesAsSkipped(t *testing.T) {
	dir := t.TempDir()
	ps, _ := profile.NewProjectStore(filepath.Join(dir, "p.json"))
	cs, _ := profile.NewClusterStore(filepath.Join(dir, "c.json"))
	_ = ps.Create(&profile.ProjectProfile{
		Name: "p", Git: profile.GitSpec{URL: "https://g/y"},
		ImagePair: profile.ImagePair{Agent: "agent-claude"},
		Memory:    profile.MemorySpec{Mode: profile.MemorySyncBack},
	})
	_ = cs.Create(&profile.ClusterProfile{Name: "c", Kind: profile.ClusterDocker, Context: "x"})
	m := NewManager(ps, cs)
	// Driver fails every Spawn.
	m.RegisterDriver(&autoCompleter{kind: "docker", manager: m, status: "fail"})

	o := &Orchestrator{Manager: m, MaxConcurrent: 4, PollInterval: 20 * time.Millisecond}
	plan := OrchestratorPlan{Nodes: []OrchestratorNode{
		{ID: "a", ProjectProfile: "p", ClusterProfile: "c", Task: "a", Branch: "ba"},
		{ID: "b", ProjectProfile: "p", ClusterProfile: "c", Task: "b", Branch: "bb", DependsOn: []string{"a"}},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	run, err := o.Run(ctx, plan)
	if err != nil {
		t.Fatal(err)
	}
	if run.Nodes["a"].State != NodeFailed {
		t.Errorf("a state=%s want failed", run.Nodes["a"].State)
	}
	if run.Nodes["b"].State != NodeSkipped {
		t.Errorf("b state=%s want skipped", run.Nodes["b"].State)
	}
}

// Plan validation: cycle detected, descriptive error.
func TestOrchestrator_RejectsCycle(t *testing.T) {
	o, _ := orchestratorFixture(t)
	plan := OrchestratorPlan{Nodes: []OrchestratorNode{
		{ID: "a", ProjectProfile: "p", ClusterProfile: "c", Task: "a", DependsOn: []string{"b"}},
		{ID: "b", ProjectProfile: "p", ClusterProfile: "c", Task: "b", DependsOn: []string{"a"}},
	}}
	_, err := o.Run(context.Background(), plan)
	if err == nil {
		t.Fatal("expected cycle rejection")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error wording: %v", err)
	}
}

// Plan validation: missing dep.
func TestOrchestrator_RejectsMissingDep(t *testing.T) {
	o, _ := orchestratorFixture(t)
	plan := OrchestratorPlan{Nodes: []OrchestratorNode{
		{ID: "a", ProjectProfile: "p", ClusterProfile: "c", Task: "a", DependsOn: []string{"ghost"}},
	}}
	_, err := o.Run(context.Background(), plan)
	if err == nil {
		t.Fatal("expected missing-dep rejection")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should name the missing dep: %v", err)
	}
}

// Plan validation: duplicate ids.
func TestOrchestrator_RejectsDuplicateIDs(t *testing.T) {
	o, _ := orchestratorFixture(t)
	plan := OrchestratorPlan{Nodes: []OrchestratorNode{
		{ID: "a", ProjectProfile: "p", ClusterProfile: "c", Task: "x"},
		{ID: "a", ProjectProfile: "p", ClusterProfile: "c", Task: "y"},
	}}
	_, err := o.Run(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("expected duplicate-id rejection, got %v", err)
	}
}

// Plan validation: required fields.
func TestOrchestrator_RejectsMissingFields(t *testing.T) {
	o, _ := orchestratorFixture(t)
	cases := []OrchestratorNode{
		{ID: "", ProjectProfile: "p", ClusterProfile: "c"},
		{ID: "a", ClusterProfile: "c"},
		{ID: "a", ProjectProfile: "p"},
	}
	for i, n := range cases {
		_, err := o.Run(context.Background(), OrchestratorPlan{Nodes: []OrchestratorNode{n}})
		if err == nil {
			t.Errorf("case %d: expected validation error for node=%+v", i, n)
		}
	}
}

// Empty plan rejected.
func TestOrchestrator_RejectsEmptyPlan(t *testing.T) {
	o, _ := orchestratorFixture(t)
	if _, err := o.Run(context.Background(), OrchestratorPlan{}); err == nil {
		t.Error("expected empty-plan rejection")
	}
}
