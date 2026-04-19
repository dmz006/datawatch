// F10 sprint 7 (S7.1) — orchestrator core.
//
// An Orchestrator walks a DAG of OrchestratorNodes and spawns one
// agent per node. Each node depends on zero or more upstream nodes;
// downstream nodes start only after their upstream nodes have
// reported a non-failed AgentResult.
//
// Why bake this into agents:
//   * The DAG terminology + spawn budgets + workspace lock + token
//     broker all live in this package — the orchestrator is the
//     consumer of those pieces, naturally adjacent
//   * pipelines.Executor (F15) covers session-DAG-on-the-same-host;
//     this is its multi-container peer. BL105 wires the two together.
//
// Failure semantics:
//   * Node spawn-fail → mark node Failed, downstream nodes Skipped
//   * Node spawned but RecordResult.Status != "ok" → same as above
//   * Cycle detected at Run start → abort with descriptive error
//   * Concurrent fanout: nodes whose deps are all Done run in
//     parallel (bounded by the orchestrator's MaxConcurrent)

package agents

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// OrchestratorNode describes one DAG vertex.
type OrchestratorNode struct {
	// ID is operator-assigned, unique within the plan. Used by
	// downstream nodes to declare dependencies.
	ID string `json:"id"`

	// ProjectProfile + ClusterProfile + Task feed the SpawnRequest
	// the orchestrator builds when this node's deps are satisfied.
	ProjectProfile string `json:"project_profile"`
	ClusterProfile string `json:"cluster_profile"`
	Task           string `json:"task"`
	Branch         string `json:"branch,omitempty"` // optional override

	// DependsOn lists the node IDs that must reach state=Done before
	// this node spawns. Empty = root node.
	DependsOn []string `json:"depends_on,omitempty"`
}

// OrchestratorPlan is the input to Orchestrator.Run.
type OrchestratorPlan struct {
	Nodes []OrchestratorNode `json:"nodes"`
}

// NodeState tracks the orchestrator's runtime view of one node.
type NodeState string

const (
	NodePending NodeState = "pending"
	NodeRunning NodeState = "running"
	NodeDone    NodeState = "done"
	NodeFailed  NodeState = "failed"
	NodeSkipped NodeState = "skipped" // upstream failed
)

// NodeRun is a per-node runtime record.
type NodeRun struct {
	NodeID      string
	State       NodeState
	AgentID     string       // populated after spawn
	Result      *AgentResult // populated after RecordResult
	Error       string       // spawn or wait failure
	StartedAt   time.Time
	FinishedAt  time.Time
}

// PlanRun is the aggregate result of Orchestrator.Run.
type PlanRun struct {
	Nodes map[string]*NodeRun
	StartedAt  time.Time
	FinishedAt time.Time
}

// Orchestrator coordinates DAG execution against a Manager.
type Orchestrator struct {
	Manager *Manager
	// MaxConcurrent caps how many node spawns run in parallel.
	// Defaults to 4 when zero. Per-cluster concurrency is naturally
	// bounded by the workspace lock + recursion gates already in
	// place — this is a global ceiling on top of those.
	MaxConcurrent int

	// PollInterval is how often the orchestrator polls for
	// node completion (an agent's Result field becoming non-nil).
	// Defaults to 200ms.
	PollInterval time.Duration

	// Now overridable for tests.
	Now func() time.Time
}

// Run executes plan synchronously. Returns when every node has
// finished (Done / Failed / Skipped) OR ctx is cancelled.
func (o *Orchestrator) Run(ctx context.Context, plan OrchestratorPlan) (*PlanRun, error) {
	if o.Manager == nil {
		return nil, errors.New("orchestrator: Manager required")
	}
	if err := validatePlan(plan); err != nil {
		return nil, err
	}
	maxConc := o.MaxConcurrent
	if maxConc <= 0 {
		maxConc = 4
	}
	pollInterval := o.PollInterval
	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}

	now := o.now()
	run := &PlanRun{
		Nodes:     make(map[string]*NodeRun, len(plan.Nodes)),
		StartedAt: now,
	}
	for i := range plan.Nodes {
		n := plan.Nodes[i]
		run.Nodes[n.ID] = &NodeRun{NodeID: n.ID, State: NodePending}
	}
	plannedByID := make(map[string]OrchestratorNode, len(plan.Nodes))
	for _, n := range plan.Nodes {
		plannedByID[n.ID] = n
	}

	// Concurrency gate.
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	var mu sync.Mutex // guards run.Nodes mutations from spawn goroutines

	updateState := func(id string, mut func(*NodeRun)) {
		mu.Lock()
		defer mu.Unlock()
		mut(run.Nodes[id])
	}

	// readyToSpawn returns nodes in Pending state whose every
	// dependency is in NodeDone.
	readyToSpawn := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for _, n := range plan.Nodes {
			rec := run.Nodes[n.ID]
			if rec.State != NodePending {
				continue
			}
			ready := true
			for _, dep := range n.DependsOn {
				dr := run.Nodes[dep]
				switch dr.State {
				case NodeDone:
					// good, keep checking other deps
				case NodeFailed, NodeSkipped:
					rec.State = NodeSkipped
					rec.Error = "upstream " + dep + " " + string(dr.State)
					rec.FinishedAt = o.now()
					ready = false
				default:
					ready = false // dep still pending/running
				}
			}
			if ready {
				out = append(out, n.ID)
			}
		}
		return out
	}

	// allDone returns true when no node is Pending or Running.
	allDone := func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, rec := range run.Nodes {
			if rec.State == NodePending || rec.State == NodeRunning {
				return false
			}
		}
		return true
	}

	for {
		if ctx.Err() != nil {
			break
		}
		if allDone() {
			break
		}
		ready := readyToSpawn()
		for _, nodeID := range ready {
			node := plannedByID[nodeID]
			updateState(nodeID, func(r *NodeRun) {
				r.State = NodeRunning
				r.StartedAt = o.now()
			})
			wg.Add(1)
			sem <- struct{}{}
			go func(n OrchestratorNode) {
				defer wg.Done()
				defer func() { <-sem }()
				o.runNode(ctx, n, run, &mu, pollInterval)
			}(node)
		}
		// Brief wait so we don't spin while nodes execute.
		select {
		case <-ctx.Done():
		case <-time.After(pollInterval):
		}
	}

	wg.Wait()
	run.FinishedAt = o.now()
	return run, nil
}

// runNode spawns the node's agent + waits for its AgentResult.
func (o *Orchestrator) runNode(ctx context.Context, n OrchestratorNode, run *PlanRun, mu *sync.Mutex, poll time.Duration) {
	a, err := o.Manager.Spawn(ctx, SpawnRequest{
		ProjectProfile: n.ProjectProfile,
		ClusterProfile: n.ClusterProfile,
		Task:           n.Task,
		Branch:         n.Branch,
	})
	mu.Lock()
	rec := run.Nodes[n.ID]
	if a != nil {
		rec.AgentID = a.ID
	}
	mu.Unlock()
	if err != nil {
		mu.Lock()
		rec.State = NodeFailed
		rec.Error = "spawn: " + err.Error()
		rec.FinishedAt = o.now()
		mu.Unlock()
		return
	}

	// Poll for the agent's Result field (RecordResult populates it).
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			rec.State = NodeFailed
			rec.Error = "ctx cancelled"
			rec.FinishedAt = o.now()
			mu.Unlock()
			return
		case <-time.After(poll):
		}
		got := o.Manager.Get(a.ID)
		if got == nil {
			continue
		}
		if got.Result != nil {
			mu.Lock()
			rec.Result = got.Result
			if got.Result.Status == "ok" {
				rec.State = NodeDone
			} else {
				rec.State = NodeFailed
				rec.Error = "result.status=" + got.Result.Status
			}
			rec.FinishedAt = o.now()
			mu.Unlock()
			return
		}
		// Terminal state with no result = failed (worker died without posting).
		if got.State == StateStopped || got.State == StateFailed {
			mu.Lock()
			rec.State = NodeFailed
			if got.FailureReason != "" {
				rec.Error = got.FailureReason
			} else {
				rec.Error = "agent " + string(got.State) + " without RecordResult"
			}
			rec.FinishedAt = o.now()
			mu.Unlock()
			return
		}
	}
}

// validatePlan rejects empty plans, duplicate node IDs, missing deps,
// and cycles. Cheap up-front check — saves operators from discovering
// a cycle deep into a 30-minute Pod-spawning sequence.
func validatePlan(plan OrchestratorPlan) error {
	if len(plan.Nodes) == 0 {
		return errors.New("plan: at least one node required")
	}
	seen := map[string]struct{}{}
	for _, n := range plan.Nodes {
		if n.ID == "" {
			return errors.New("plan: node id required")
		}
		if _, dup := seen[n.ID]; dup {
			return fmt.Errorf("plan: duplicate node id %q", n.ID)
		}
		seen[n.ID] = struct{}{}
		if n.ProjectProfile == "" {
			return fmt.Errorf("plan: node %q missing project_profile", n.ID)
		}
		if n.ClusterProfile == "" {
			return fmt.Errorf("plan: node %q missing cluster_profile", n.ID)
		}
	}
	// Validate deps + detect cycles via DFS.
	for _, n := range plan.Nodes {
		for _, dep := range n.DependsOn {
			if _, ok := seen[dep]; !ok {
				return fmt.Errorf("plan: node %q depends on unknown %q", n.ID, dep)
			}
		}
	}
	if cycle := findCycle(plan); cycle != "" {
		return fmt.Errorf("plan: cycle detected involving %s", cycle)
	}
	return nil
}

// findCycle returns a comma-joined node-id chain when the DAG isn't
// acyclic; empty string on success. Standard DFS with white/grey/
// black colours.
func findCycle(plan OrchestratorPlan) string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	color := map[string]int{}
	adj := map[string][]string{}
	for _, n := range plan.Nodes {
		color[n.ID] = white
		adj[n.ID] = n.DependsOn
	}
	var path []string
	var dfs func(string) string
	dfs = func(u string) string {
		color[u] = grey
		path = append(path, u)
		for _, v := range adj[u] {
			if color[v] == grey {
				path = append(path, v)
				return joinIDs(path)
			}
			if color[v] == white {
				if c := dfs(v); c != "" {
					return c
				}
			}
		}
		color[u] = black
		path = path[:len(path)-1]
		return ""
	}
	for _, n := range plan.Nodes {
		if color[n.ID] == white {
			if c := dfs(n.ID); c != "" {
				return c
			}
		}
	}
	return ""
}

func joinIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += " → "
		}
		out += id
	}
	return out
}

func (o *Orchestrator) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now().UTC()
}
