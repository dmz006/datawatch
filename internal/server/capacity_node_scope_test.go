// v8.36.0 — two capacity-card fixes, confirmed against live data on
// PRD a2833a5e: (1) a registered compute node with no explicit
// max_concurrent_sessions was entirely invisible (SetLimit(pool, 0) deletes
// the pool rather than showing it unconfigured) even though it's real
// hardware; (2) /api/capacity had no PRD scoping at all — a Claude-only
// PRD's capacity card showed node:datawatch purely because some unrelated
// PRD/task on the machine was using it.

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

// TestHandleCapacity_ListsUnconfiguredNodes verifies every registered
// compute node appears in /api/capacity even with no configured limit and
// no current activity — previously invisible because SetLimit(pool, 0)
// deletes rather than zeroes the pool entry.
func TestHandleCapacity_ListsUnconfiguredNodes(t *testing.T) {
	s := &Server{}
	s.SetCapacity(capacity.New(capacity.Options{}))

	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if err := reg.Add(&compute.Node{Name: "configured", Kind: "ollama", Address: "http://configured:11434", MaxConcurrentSessions: 1}); err != nil {
		t.Fatalf("Add configured: %v", err)
	}
	if err := reg.Add(&compute.Node{Name: "unconfigured", Kind: "ollama", Address: "http://unconfigured:11434"}); err != nil {
		t.Fatalf("Add unconfigured: %v", err)
	}
	s.computeReg = reg
	// Simulate capacity_wiring.go's syncPools: "configured" got an explicit
	// limit, "unconfigured" never did (MaxConcurrentSessions is the zero
	// value, so SetLimit deletes rather than keeps it).
	s.capacityLedger.SetLimit("node:configured", 1)
	s.capacityLedger.SetLimit("node:unconfigured", 0)

	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var st capacity.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range st.Pools {
		names[p.Name] = true
	}
	if !names["node:configured"] {
		t.Error("node:configured missing from /api/capacity")
	}
	if !names["node:unconfigured"] {
		t.Error("node:unconfigured missing from /api/capacity — real hardware invisible just because no limit was set")
	}
}

// prdBackendSpy returns a canned map[string]any PRD shape (matching what
// GetPRD's callers already type-assert against elsewhere, e.g. the
// hard-delete memory_strategy handler) for prdRelevantPools to resolve.
type prdBackendSpy struct {
	fakeOrchAutonomous
	prd map[string]any
}

func (p *prdBackendSpy) GetPRD(id string) (any, bool) {
	if p.prd == nil {
		return nil, false
	}
	return p.prd, true
}

// TestHandleCapacity_PRDScoping verifies ?prd_id= filters the pool list down
// to "host" plus only the llm:/node: pools this PRD's own backend (and any
// per-story/per-task override) actually resolves to — a Claude-only PRD
// must not show an unrelated Ollama node's pool.
func TestHandleCapacity_PRDScoping(t *testing.T) {
	s := &Server{}
	l := capacity.New(capacity.Options{})
	l.SetLimit("node:datawatch", 1)
	s.SetCapacity(l)

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "ollama-datawatch", Kind: "ollama", ComputeNodes: []string{"datawatch"}}); err != nil {
		t.Fatalf("Add llm: %v", err)
	}
	s.inferenceReg = reg
	s.autonomousMgr = &prdBackendSpy{prd: map[string]any{
		"id":      "a2833a5e",
		"backend": "claude-code", // no matching inference registry entry — resolves to no llm:/node: pool
	}}

	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity?prd_id=a2833a5e", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var st capacity.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Pools {
		if p.Name == "node:datawatch" {
			t.Fatalf("Claude-only PRD's scoped capacity view includes node:datawatch, an unrelated node's pool: %+v", st.Pools)
		}
	}

	// Now give the PRD an actual task override pointing at ollama-datawatch —
	// node:datawatch must appear this time.
	s.autonomousMgr = &prdBackendSpy{prd: map[string]any{
		"id":      "a2833a5e",
		"backend": "claude-code",
		"stories": []any{
			map[string]any{
				"tasks": []any{
					map[string]any{"backend": "ollama-datawatch"},
				},
			},
		},
	}}
	rec = httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity?prd_id=a2833a5e", nil))
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range st.Pools {
		if p.Name == "node:datawatch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PRD with a task override on ollama-datawatch should include node:datawatch, got: %+v", st.Pools)
	}
}
