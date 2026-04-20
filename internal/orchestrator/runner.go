// BL117 — Runner: walks the graph, dispatches PRD execution via the
// injected PRDRunFn (wired to BL24 autonomous.Manager.Run in main.go),
// spawns guardrails after each PRD node completes, aggregates
// verdicts, blocks on first `block` outcome.
//
// The PRDRunFn / GuardrailFn indirections keep this package
// test-friendly and cycle-free with internal/autonomous + the
// session/BL103 validator wiring (which lives higher up the stack).

package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// PRDRunFn is the bridge to internal/autonomous.Manager.Run.
// Returns (summary, error) — summary goes into the guardrail context.
type PRDRunFn func(ctx context.Context, prdID string) (summary string, err error)

// GuardrailRequest is the input to a guardrail.
type GuardrailRequest struct {
	GraphID    string
	NodeID     string
	PRDID      string   // the PRD this guardrail is attesting
	Guardrail  string   // "rules" | "security" | …
	Summary    string   // output of the PRD run
	ProjectDir string
}

// GuardrailFn runs one guardrail and returns its Verdict. Wired to a
// BL103 validator agent spawn (or a plugin `on_guardrail` hook) in
// main.go. Tests inject fakes.
type GuardrailFn func(ctx context.Context, req GuardrailRequest) (Verdict, error)

// Config mirrors the YAML orchestrator: block.
type Config struct {
	Enabled             bool     `json:"enabled"`
	DefaultGuardrails   []string `json:"default_guardrails,omitempty"`
	GuardrailTimeoutMs  int      `json:"guardrail_timeout_ms,omitempty"`
	GuardrailBackend    string   `json:"guardrail_backend,omitempty"`
	MaxParallelPRDs     int      `json:"max_parallel_prds,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Enabled:            false,
		DefaultGuardrails:  []string{"rules", "security", "release-readiness", "docs-diagrams-architecture"},
		GuardrailTimeoutMs: 120000,
		MaxParallelPRDs:    2,
	}
}

// Runner composes Store + Config + the injected fns.
type Runner struct {
	mu    sync.Mutex
	cfg   Config
	store *Store
	run   PRDRunFn
	guard GuardrailFn
}

func NewRunner(store *Store, cfg Config, run PRDRunFn, guard GuardrailFn) *Runner {
	return &Runner{cfg: cfg, store: store, run: run, guard: guard}
}

func (r *Runner) Store() *Store { return r.store }

func (r *Runner) Config() Config {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg
}

func (r *Runner) SetConfig(c Config) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg = c
}

// Plan builds the Node list for a Graph from prd_ids + optional
// explicit dependencies. Dependencies is a map prdID → list of prdIDs
// that must complete first. Each PRD gets one NodeKindPRD plus one
// NodeKindGuardrail per configured guardrail (depending on the PRD).
// Idempotent — calling Plan twice replaces the node set.
func (r *Runner) Plan(graphID string, deps map[string][]string) (*Graph, error) {
	g, ok := r.store.GetGraph(graphID)
	if !ok {
		return nil, fmt.Errorf("graph %q not found", graphID)
	}
	guards := r.cfg.DefaultGuardrails
	now := time.Now()
	prdNodeID := map[string]string{}
	var nodes []Node
	// PRD nodes first.
	for _, pid := range g.PRDIDs {
		n := Node{
			ID: newID(), GraphID: g.ID, Kind: NodeKindPRD, PRDID: pid,
			Status: NodePending, CreatedAt: now, UpdatedAt: now,
		}
		if ds, ok := deps[pid]; ok {
			for _, dep := range ds {
				if id, ok := prdNodeID[dep]; ok {
					n.DependsOn = append(n.DependsOn, id)
				}
			}
		}
		prdNodeID[pid] = n.ID
		nodes = append(nodes, n)
	}
	// One guardrail node per (PRD × guardrail).
	for _, pid := range g.PRDIDs {
		for _, gr := range guards {
			gn := Node{
				ID: newID(), GraphID: g.ID, Kind: NodeKindGuardrail,
				PRDID: pid, Guardrail: gr, Status: NodePending,
				DependsOn: []string{prdNodeID[pid]},
				CreatedAt: now, UpdatedAt: now,
			}
			nodes = append(nodes, gn)
		}
	}
	g.Nodes = nodes
	g.Status = GraphActive
	if err := r.store.SaveGraph(g); err != nil {
		return nil, err
	}
	return g, nil
}

// Run walks the graph in dependency order. Returns when the graph is
// completed, blocked, or the context is cancelled.
func (r *Runner) Run(ctx context.Context, graphID string) error {
	g, ok := r.store.GetGraph(graphID)
	if !ok {
		return fmt.Errorf("graph %q not found", graphID)
	}
	if len(g.Nodes) == 0 {
		if _, err := r.Plan(graphID, nil); err != nil {
			return err
		}
		g, _ = r.store.GetGraph(graphID)
	}

	order, err := topoSort(g.Nodes)
	if err != nil {
		return err
	}
	blocked := false
	for _, id := range order {
		if err := ctx.Err(); err != nil {
			return err
		}
		node := lookupNode(g, id)
		if node == nil || node.Status == NodeCompleted || node.Status == NodeBlocked {
			continue
		}
		// Skip when any dependency blocked.
		if depBlocked(g, node) {
			node.Status = NodeCancelled
			_ = saveNode(r.store, g, node)
			continue
		}
		if node.Kind == NodeKindPRD {
			if err := r.runPRD(ctx, g, node); err != nil {
				node.Status = NodeFailed
				node.Error = err.Error()
				_ = saveNode(r.store, g, node)
				blocked = true
				continue
			}
		} else {
			if err := r.runGuardrail(ctx, g, node); err != nil {
				node.Status = NodeFailed
				node.Error = err.Error()
				_ = saveNode(r.store, g, node)
				continue
			}
			if node.Verdict != nil && node.Verdict.Outcome == "block" {
				node.Status = NodeBlocked
				blocked = true
				_ = saveNode(r.store, g, node)
			}
		}
	}
	if blocked {
		g.Status = GraphBlocked
	} else {
		g.Status = GraphCompleted
	}
	return r.store.SaveGraph(g)
}

func (r *Runner) runPRD(ctx context.Context, g *Graph, n *Node) error {
	markStart(n)
	_ = saveNode(r.store, g, n)
	summary := ""
	if r.run != nil {
		s, err := r.run(ctx, n.PRDID)
		if err != nil {
			return err
		}
		summary = s
	}
	// Stash summary via Verdict (PRD node's verdict is "pass" — the
	// Summary field is the PRD output.)
	n.Verdict = &Verdict{
		Outcome:   "pass",
		Summary:   summary,
		VerdictAt: time.Now(),
	}
	markDone(n, NodeCompleted)
	return saveNode(r.store, g, n)
}

func (r *Runner) runGuardrail(ctx context.Context, g *Graph, n *Node) error {
	markStart(n)
	_ = saveNode(r.store, g, n)
	// Pull PRD summary from the PRD node's verdict (produced above).
	var prdSummary string
	for i := range g.Nodes {
		if g.Nodes[i].Kind == NodeKindPRD && g.Nodes[i].PRDID == n.PRDID && g.Nodes[i].Verdict != nil {
			prdSummary = g.Nodes[i].Verdict.Summary
			break
		}
	}
	if r.guard == nil {
		// No guardrail fn wired — treat as informational pass so the
		// graph can still progress in test/dev environments.
		n.Verdict = &Verdict{Outcome: "pass", Summary: "no guardrail fn configured", VerdictAt: time.Now()}
		markDone(n, NodeCompleted)
		return saveNode(r.store, g, n)
	}
	timeout := time.Duration(r.cfg.GuardrailTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	v, err := r.guard(cctx, GuardrailRequest{
		GraphID:    g.ID,
		NodeID:     n.ID,
		PRDID:      n.PRDID,
		Guardrail:  n.Guardrail,
		Summary:    prdSummary,
		ProjectDir: g.ProjectDir,
	})
	if err != nil {
		return err
	}
	n.Verdict = &v
	// `block` handled by caller; everything else completes the node.
	if v.Outcome != "block" {
		markDone(n, NodeCompleted)
	}
	return saveNode(r.store, g, n)
}

func markStart(n *Node) {
	now := time.Now()
	n.StartedAt = &now
	n.Status = NodeInProgress
}

func markDone(n *Node, s NodeStatus) {
	now := time.Now()
	n.FinishedAt = &now
	n.Status = s
}

func saveNode(store *Store, g *Graph, n *Node) error {
	n.UpdatedAt = time.Now()
	// Replace in slice.
	for i := range g.Nodes {
		if g.Nodes[i].ID == n.ID {
			g.Nodes[i] = *n
			break
		}
	}
	return store.SaveGraph(g)
}

func lookupNode(g *Graph, id string) *Node {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i]
		}
	}
	return nil
}

func depBlocked(g *Graph, n *Node) bool {
	for _, dep := range n.DependsOn {
		for _, x := range g.Nodes {
			if x.ID == dep && (x.Status == NodeBlocked || x.Status == NodeFailed) {
				return true
			}
		}
	}
	return false
}

// topoSort returns node IDs in dependency order. Returns an error on cycle.
func topoSort(nodes []Node) ([]string, error) {
	indeg := map[string]int{}
	idx := map[string]Node{}
	for _, n := range nodes {
		idx[n.ID] = n
		if _, seen := indeg[n.ID]; !seen {
			indeg[n.ID] = 0
		}
	}
	for _, n := range nodes {
		for _, dep := range n.DependsOn {
			if _, ok := idx[dep]; ok {
				indeg[n.ID]++
			}
		}
	}
	var ready []string
	for id, d := range indeg {
		if d == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	var out []string
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		out = append(out, id)
		for _, n := range nodes {
			for _, dep := range n.DependsOn {
				if dep == id {
					indeg[n.ID]--
					if indeg[n.ID] == 0 {
						ready = append(ready, n.ID)
					}
				}
			}
		}
		sort.Strings(ready)
	}
	if len(out) != len(nodes) {
		return nil, fmt.Errorf("orchestrator graph has a cycle")
	}
	return out, nil
}
