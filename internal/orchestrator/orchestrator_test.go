// BL117 — orchestrator package unit tests.

package orchestrator

import (
	"context"
	"encoding/json"
	"testing"
)

// helper: fresh runner against a temp store, with optional fns.
func newTest(t *testing.T, run PRDRunFn, guard GuardrailFn) *Runner {
	t.Helper()
	st, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return NewRunner(st, DefaultConfig(), run, guard)
}

// --- store ---

func TestStore_CreateGraph_RoundTrip(t *testing.T) {
	r := newTest(t, nil, nil)
	g, err := r.Store().CreateGraph("demo", "/proj", []string{"p1", "p2"})
	if err != nil {
		t.Fatalf("CreateGraph: %v", err)
	}
	if g.ID == "" || len(g.PRDIDs) != 2 || g.Status != GraphDraft {
		t.Fatalf("graph: %+v", g)
	}
	got, ok := r.Store().GetGraph(g.ID)
	if !ok || got.Title != "demo" {
		t.Fatalf("GetGraph: %+v ok=%v", got, ok)
	}
}

func TestStore_CreateGraph_RejectsEmptyPRDIDs(t *testing.T) {
	r := newTest(t, nil, nil)
	if _, err := r.Store().CreateGraph("x", "", nil); err == nil {
		t.Fatalf("expected error on empty prd_ids")
	}
}

// --- planning ---

func TestPlan_GeneratesPRDAndGuardrailNodesWithDeps(t *testing.T) {
	r := newTest(t, nil, nil)
	g, _ := r.Store().CreateGraph("p", "", []string{"a", "b"})
	// a must run before b.
	planned, err := r.Plan(g.ID, map[string][]string{"b": {"a"}})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	guards := DefaultConfig().DefaultGuardrails
	wantNodes := 2 + 2*len(guards) // 2 PRD + 2*guardrails
	if len(planned.Nodes) != wantNodes {
		t.Fatalf("want %d nodes, got %d: %+v", wantNodes, len(planned.Nodes), planned.Nodes)
	}
	// Find PRD nodes
	var aNodeID, bNodeID string
	for _, n := range planned.Nodes {
		if n.Kind == NodeKindPRD && n.PRDID == "a" {
			aNodeID = n.ID
		}
		if n.Kind == NodeKindPRD && n.PRDID == "b" {
			bNodeID = n.ID
		}
	}
	if aNodeID == "" || bNodeID == "" {
		t.Fatalf("PRD nodes missing")
	}
	// b should depend on a.
	for _, n := range planned.Nodes {
		if n.ID == bNodeID {
			found := false
			for _, d := range n.DependsOn {
				if d == aNodeID {
					found = true
				}
			}
			if !found {
				t.Fatalf("b did not get dep on a: %+v", n.DependsOn)
			}
		}
	}
}

// --- topological order ---

func TestTopoSort_CycleDetection(t *testing.T) {
	nodes := []Node{
		{ID: "x", DependsOn: []string{"y"}},
		{ID: "y", DependsOn: []string{"x"}},
	}
	if _, err := topoSort(nodes); err == nil {
		t.Fatalf("expected cycle error")
	}
}

func TestTopoSort_ChainOrder(t *testing.T) {
	nodes := []Node{
		{ID: "c", DependsOn: []string{"b"}},
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
	}
	got, err := topoSort(nodes)
	if err != nil {
		t.Fatalf("topoSort: %v", err)
	}
	if got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("order broken: %v", got)
	}
}

// --- runner ---

func TestRun_PRDSummaryReachesGuardrail(t *testing.T) {
	prdRun := func(_ context.Context, prdID string) (string, error) {
		return "result for " + prdID, nil
	}
	var seenSummary string
	guard := func(_ context.Context, req GuardrailRequest) (Verdict, error) {
		seenSummary = req.Summary
		return Verdict{Outcome: "pass", Summary: "ok"}, nil
	}
	r := newTest(t, prdRun, guard)
	// One guardrail only, keep the graph small.
	cfg := DefaultConfig()
	cfg.DefaultGuardrails = []string{"rules"}
	r.SetConfig(cfg)

	g, _ := r.Store().CreateGraph("g", "/p", []string{"p1"})
	if _, err := r.Plan(g.ID, nil); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if err := r.Run(context.Background(), g.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := r.Store().GetGraph(g.ID)
	if got.Status != GraphCompleted {
		t.Fatalf("graph not completed: %s", got.Status)
	}
	if seenSummary != "result for p1" {
		t.Fatalf("guardrail didn't receive PRD summary, got %q", seenSummary)
	}
}

func TestRun_BlockVerdictHaltsGraph(t *testing.T) {
	prdRun := func(_ context.Context, _ string) (string, error) { return "ok", nil }
	guard := func(_ context.Context, req GuardrailRequest) (Verdict, error) {
		if req.Guardrail == "security" {
			return Verdict{Outcome: "block", Severity: "high", Summary: "bad finding"}, nil
		}
		return Verdict{Outcome: "pass", Summary: "ok"}, nil
	}
	r := newTest(t, prdRun, guard)
	cfg := DefaultConfig()
	cfg.DefaultGuardrails = []string{"rules", "security", "docs"}
	r.SetConfig(cfg)
	g, _ := r.Store().CreateGraph("g", "", []string{"p1"})
	_, _ = r.Plan(g.ID, nil)
	if err := r.Run(context.Background(), g.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := r.Store().GetGraph(g.ID)
	if got.Status != GraphBlocked {
		t.Fatalf("graph should be blocked, got %s", got.Status)
	}
	// Count block verdicts.
	blocks := 0
	for _, n := range got.Nodes {
		if n.Verdict != nil && n.Verdict.Outcome == "block" {
			blocks++
		}
	}
	if blocks != 1 {
		t.Fatalf("expected exactly 1 block verdict, got %d", blocks)
	}
}

func TestRun_NoGuardrailFn_StillProgressesAsPass(t *testing.T) {
	prdRun := func(_ context.Context, _ string) (string, error) { return "ok", nil }
	r := newTest(t, prdRun, nil)
	cfg := DefaultConfig()
	cfg.DefaultGuardrails = []string{"rules"}
	r.SetConfig(cfg)
	g, _ := r.Store().CreateGraph("g", "", []string{"p1"})
	_, _ = r.Plan(g.ID, nil)
	if err := r.Run(context.Background(), g.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := r.Store().GetGraph(g.ID)
	if got.Status != GraphCompleted {
		t.Fatalf("without guardrail fn graph should still complete: %s", got.Status)
	}
}

// --- API adapter ---

func TestAPI_SetConfig_UnmarshalsRawJSON(t *testing.T) {
	r := newTest(t, nil, nil)
	a := NewAPI(r)
	raw := json.RawMessage(`{"enabled":true,"max_parallel_prds":5}`)
	if err := a.SetConfig(raw); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	got := r.Config()
	if !got.Enabled || got.MaxParallelPRDs != 5 {
		t.Fatalf("config not applied: %+v", got)
	}
}
