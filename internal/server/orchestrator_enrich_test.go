// S13 follow — orchestrator graph ObserverSummary enrichment.

package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/observer"
)

// fakeOrchAutonomous implements just the SessionIDsForPRD method we
// need; everything else satisfies the AutonomousAPI interface with
// stubs so the test compiles.
type fakeOrchAutonomous struct {
	prdSessions map[string][]string
}

func (f *fakeOrchAutonomous) Config() any                         { return nil }
func (f *fakeOrchAutonomous) SetConfig(any) error                 { return nil }
func (f *fakeOrchAutonomous) Status() any                         { return nil }
func (f *fakeOrchAutonomous) CreatePRD(string, string, string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) GetPRD(string) (any, bool)            { return nil, false }
func (f *fakeOrchAutonomous) ListPRDs() []any                      { return nil }
func (f *fakeOrchAutonomous) Decompose(string) (any, error)        { return nil, nil }
func (f *fakeOrchAutonomous) Run(string) error                     { return nil }
func (f *fakeOrchAutonomous) Cancel(string) error                  { return nil }
func (f *fakeOrchAutonomous) ListLearnings() []any                 { return nil }
func (f *fakeOrchAutonomous) SessionIDsForPRD(prdID string) []string {
	return f.prdSessions[prdID]
}

// BL191 (v5.2.0) — review/approve gate stubs so the fake satisfies the
// expanded AutonomousAPI interface.
func (f *fakeOrchAutonomous) Archive(string) (any, error)                        { return nil, nil }
func (f *fakeOrchAutonomous) Approve(string, string, string) (any, error)         { return nil, nil }
func (f *fakeOrchAutonomous) Reject(string, string, string) (any, error)          { return nil, nil }
func (f *fakeOrchAutonomous) RequestRevision(string, string, string) (any, error) { return nil, nil }
func (f *fakeOrchAutonomous) EditTaskSpec(string, string, string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) EditStory(string, string, string, string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) SetStoryProfile(string, string, string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) ApproveStory(string, string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) RejectStory(string, string, string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) SetStoryFiles(string, string, []string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) SetTaskFiles(string, string, []string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) InstantiateTemplate(string, map[string]string, string) (any, error) {
	return nil, nil
}

// BL203 (v5.4.0) — flexible LLM override stubs.
func (f *fakeOrchAutonomous) SetPRDLLM(string, string, string, string, string) (any, error) {
	return nil, nil
}
func (f *fakeOrchAutonomous) SetTaskLLM(string, string, string, string, string, string) (any, error) {
	return nil, nil
}

// BL191 Q4 (v5.9.0) — child PRD list stub.
func (f *fakeOrchAutonomous) ListChildPRDs(string) []any { return nil }

// v5.19.0 — full CRUD stubs.
func (f *fakeOrchAutonomous) DeletePRD(string) error                            { return nil }
func (f *fakeOrchAutonomous) EditPRDFields(string, string, string, string) (any, error) { return nil, nil }

// v5.26.19 — F10 profile attachment stub.
func (f *fakeOrchAutonomous) SetPRDProfiles(string, string, string) error { return nil }

// BL221 (v6.2.0) — TemplateStore CRUD stubs.
func (f *fakeOrchAutonomous) ListTemplates() []any                                              { return nil }
func (f *fakeOrchAutonomous) CreateTemplate(string, string, string, string, []string) (any, error) { return nil, nil }
func (f *fakeOrchAutonomous) GetTemplate(string) (any, bool)                                    { return nil, false }
func (f *fakeOrchAutonomous) UpdateTemplate(string, string, string, string, string, []string) (any, error) { return nil, nil }
func (f *fakeOrchAutonomous) DeleteTemplate(string) error                                       { return nil }
func (f *fakeOrchAutonomous) CloneToTemplate(string, string, string) (any, error)               { return nil, nil }
func (f *fakeOrchAutonomous) InstantiateFromTemplateStore(string, map[string]string, string, string, string) (any, error) { return nil, nil }

// fakeObserverForEnrich satisfies the bits of ObserverAPI the
// enrichment touches. EnvelopeSummary is the only meaningful method.
type fakeObserverForEnrich struct {
	envs map[string]struct {
		cpu float64
		rss uint64
	}
}

func (f *fakeObserverForEnrich) Config() any                            { return nil }
func (f *fakeObserverForEnrich) SetConfig(any) error                    { return nil }
func (f *fakeObserverForEnrich) Stats() any                             { return nil }
func (f *fakeObserverForEnrich) Envelopes() any                         { return nil }
func (f *fakeObserverForEnrich) EnvelopeTree(string) any                { return nil }
func (f *fakeObserverForEnrich) Start(context.Context)                  {}
func (f *fakeObserverForEnrich) Stop()                                  {}
func (f *fakeObserverForEnrich) EnvelopeSummary(id string) (float64, uint64, bool) {
	if v, ok := f.envs[id]; ok {
		return v.cpu, v.rss, true
	}
	return 0, 0, false
}

// fakePeerRegForEnrich exposes one peer with a synthetic snapshot.
type fakePeerRegForEnrich struct {
	peers       []observer.PeerEntry
	lastPayload map[string]*observer.StatsResponse
}

func (f *fakePeerRegForEnrich) Register(string, string, string, map[string]any) (string, error) {
	return "", nil
}
func (f *fakePeerRegForEnrich) Verify(string, string) (*observer.PeerEntry, error) { return nil, nil }
func (f *fakePeerRegForEnrich) RecordPush(string, *observer.StatsResponse) error    { return nil }
func (f *fakePeerRegForEnrich) Get(name string) (observer.PeerEntry, bool) {
	for _, p := range f.peers {
		if p.Name == name {
			return p, true
		}
	}
	return observer.PeerEntry{}, false
}
func (f *fakePeerRegForEnrich) LastPayload(name string) *observer.StatsResponse {
	return f.lastPayload[name]
}
func (f *fakePeerRegForEnrich) List() []observer.PeerEntry { return f.peers }
func (f *fakePeerRegForEnrich) Delete(string) error        { return nil }

func TestEnrichGraphWithObserverSummary_LocalOnly(t *testing.T) {
	s := &Server{
		autonomousMgr: &fakeOrchAutonomous{
			prdSessions: map[string][]string{"prd-1": {"sess-a", "sess-b"}},
		},
		observerAPI: &fakeObserverForEnrich{
			envs: map[string]struct {
				cpu float64
				rss uint64
			}{
				"session:sess-a": {cpu: 12.5, rss: 100 * 1024 * 1024},
				"session:sess-b": {cpu: 7.5, rss: 50 * 1024 * 1024},
			},
		},
	}
	graph := map[string]any{
		"id": "g1",
		"nodes": []any{
			map[string]any{"id": "n1", "kind": "prd", "prd_id": "prd-1"},
			map[string]any{"id": "n2", "kind": "guardrail"}, // no prd_id
		},
	}
	enriched := s.enrichGraphWithObserverSummary(graph)

	// Round-trip through JSON to assert the shape.
	raw, _ := json.Marshal(enriched)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	nodes := got["nodes"].([]any)
	n1 := nodes[0].(map[string]any)
	summary, ok := n1["observer_summary"].(map[string]any)
	if !ok {
		t.Fatalf("n1 missing observer_summary: %+v", n1)
	}
	if summary["cpu_pct"].(float64) != 20.0 {
		t.Errorf("cpu_pct = %v want 20", summary["cpu_pct"])
	}
	if int(summary["rss_mb"].(float64)) != 150 {
		t.Errorf("rss_mb = %v want 150", summary["rss_mb"])
	}
	if int(summary["envelope_count"].(float64)) != 2 {
		t.Errorf("envelope_count = %v want 2", summary["envelope_count"])
	}
	// Guardrail node has no observer_summary.
	n2 := nodes[1].(map[string]any)
	if _, present := n2["observer_summary"]; present {
		t.Errorf("guardrail node should not have observer_summary")
	}
}

func TestEnrichGraphWithObserverSummary_PeerSnapshotFolds(t *testing.T) {
	now := time.Now().UTC()
	s := &Server{
		autonomousMgr: &fakeOrchAutonomous{
			prdSessions: map[string][]string{"prd-1": {"sess-x"}},
		},
		observerAPI: &fakeObserverForEnrich{envs: map[string]struct {
			cpu float64
			rss uint64
		}{}},
		peerRegistry: &fakePeerRegForEnrich{
			peers: []observer.PeerEntry{
				{Name: "agt-1", Shape: "A", LastPushAt: now},
			},
			lastPayload: map[string]*observer.StatsResponse{
				"agt-1": {
					V: 2,
					Envelopes: []observer.Envelope{
						{ID: "session:sess-x", CPUPct: 33.3, RSSBytes: 200 * 1024 * 1024},
					},
				},
			},
		},
	}
	graph := map[string]any{
		"nodes": []any{
			map[string]any{"id": "n1", "kind": "prd", "prd_id": "prd-1"},
		},
	}
	enriched := s.enrichGraphWithObserverSummary(graph)
	raw, _ := json.Marshal(enriched)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	n1 := got["nodes"].([]any)[0].(map[string]any)
	summary, ok := n1["observer_summary"].(map[string]any)
	if !ok {
		t.Fatalf("missing observer_summary: %+v", n1)
	}
	if summary["cpu_pct"].(float64) != 33.3 {
		t.Errorf("cpu_pct = %v want 33.3", summary["cpu_pct"])
	}
	if int(summary["rss_mb"].(float64)) != 200 {
		t.Errorf("rss_mb = %v want 200", summary["rss_mb"])
	}
	if _, ok := summary["last_push_at"].(string); !ok {
		t.Errorf("expected last_push_at string, got %v", summary["last_push_at"])
	}
}

func TestEnrichGraphWithObserverSummary_NoMatch_OmitsField(t *testing.T) {
	s := &Server{
		autonomousMgr: &fakeOrchAutonomous{
			prdSessions: map[string][]string{"prd-1": {"sess-z"}},
		},
		observerAPI: &fakeObserverForEnrich{envs: map[string]struct {
			cpu float64
			rss uint64
		}{}},
	}
	graph := map[string]any{
		"nodes": []any{
			map[string]any{"id": "n1", "kind": "prd", "prd_id": "prd-1"},
		},
	}
	enriched := s.enrichGraphWithObserverSummary(graph)
	raw, _ := json.Marshal(enriched)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	n1 := got["nodes"].([]any)[0].(map[string]any)
	if _, present := n1["observer_summary"]; present {
		t.Errorf("no matching envelope → observer_summary should be omitted")
	}
}

func TestEnrichGraphWithObserverSummary_NilDeps_NoOp(t *testing.T) {
	s := &Server{} // no autonomousMgr / observerAPI / peerRegistry
	graph := map[string]any{"nodes": []any{}}
	if got := s.enrichGraphWithObserverSummary(graph); got == nil {
		t.Errorf("nil deps shouldn't drop the graph; got nil")
	}
}
