package autonomous

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// leaseRig wires a manager whose capacity resolver behaves like the daemon's:
// one local-engine entry ("ollama") lists two compute nodes, a session is gated
// on a node pool only when LocalEngineGate resolves its model's provider, and
// every model the resolver is asked about is recorded.
type leaseRig struct {
	m      *Manager
	led    *capacity.Ledger
	mu     sync.Mutex
	models []string
}

// seen returns the distinct models the capacity resolver was asked about.
func (r *leaseRig) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, m := range r.models {
		dup := false
		for _, o := range out {
			dup = dup || o == m
		}
		if !dup {
			out = append(out, m)
		}
	}
	return out
}

// newLeaseRig builds the rig. defaultModel is what the pi-family default
// lookup reports for both pi backends; empty wires no lookup at all.
func newLeaseRig(t *testing.T, defaultModel string) *leaseRig {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for _, n := range []string{"gpu-a", "gpu-b"} {
		led.SetLimit("node:"+n, 1)
	}
	r := &leaseRig{m: m, led: led}
	engines := map[string][]string{"ollama": {"gpu-a", "gpu-b"}}
	resolve := func(backend, model string) (pools []string, node string, choices []capacity.NodeChoice) {
		r.mu.Lock()
		r.models = append(r.models, model)
		r.mu.Unlock()
		pools = []string{"host"}
		_, gated := LocalEngineGate(backend, model, func(provider string) (string, bool) {
			nodes, ok := engines[provider]
			if !ok {
				return "", false
			}
			return nodes[0], true
		})
		if !gated {
			return pools, "", nil
		}
		nodes := engines["ollama"]
		return pools, "", []capacity.NodeChoice{{Name: nodes[0], Priority: 10}, {Name: nodes[1], Priority: 90}}
	}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		pools, node, _ := resolve(backend, model)
		return pools, node
	})
	m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
		_, _, c := resolve(backend, model)
		return c
	})
	if defaultModel != "" {
		m.SetPiDefaultModel(func(string) string { return defaultModel })
	}
	return r
}

// spawned is what one worker SpawnRequest carried, plus the node its lease
// was granted on at that moment.
type spawned struct {
	model, computeNode, leaseNode string
}

// runOne runs a one-task PRD on backend and model through Run and returns the
// SpawnRequest values the worker saw.
func (r *leaseRig) runOne(t *testing.T, backend, model string) spawned {
	t.Helper()
	prd, err := r.m.CreatePRD("s", "/w/proj", backend, model, EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T0", Spec: "write docs/x.md"}}}})
	cur, _ := r.m.Store().GetPRD(prd.ID)
	cur.Status = PRDApproved
	_ = r.m.Store().SavePRD(cur)

	var got spawned
	spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
		got.model, got.computeNode = req.Model, req.ComputeNode
		if ls := r.led.LeaseOf(req.TaskID); ls != nil {
			got.leaseNode = ls.Node
		}
		return SpawnResult{SessionID: "s-" + req.TaskID}, nil
	}
	verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
		return VerificationResult{OK: true, Summary: "ok"}, nil
	}
	if err := r.m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return got
}

// TestPiFamilyWorkerHoldsNodeItRunsOn checks that a pi-family worker whose
// model carries no provider prefix is admitted on one of the engine's nodes,
// that the spawn names that same node, and that admission, the spawn and the
// session start all see one provider-qualified model.
func TestPiFamilyWorkerHoldsNodeItRunsOn(t *testing.T) {
	const qualified = "ollama/qwen3.8:27b"
	for _, backend := range []string{"pi-prompt", "pi"} {
		for _, tc := range []struct{ name, model string }{
			{"bare model", "qwen3.8:27b"},
			{"provider-qualified model", qualified},
			{"no model uses the launch default", ""},
		} {
			t.Run(backend+"/"+tc.name, func(t *testing.T) {
				r := newLeaseRig(t, qualified)
				got := r.runOne(t, backend, tc.model)
				if got.leaseNode == "" {
					t.Fatalf("lease holds no node: admission was not gated on the engine's nodes (models seen by admission: %v)", r.seen())
				}
				if got.computeNode != got.leaseNode {
					t.Fatalf("spawn ComputeNode = %q, lease node = %q; the spawn must carry the leased node", got.computeNode, got.leaseNode)
				}
				if got.leaseNode != "gpu-a" && got.leaseNode != "gpu-b" {
					t.Fatalf("lease node = %q, want one of the engine's nodes", got.leaseNode)
				}
				if got.model != qualified {
					t.Fatalf("SpawnRequest.Model = %q, want %q", got.model, qualified)
				}
				if seen := r.seen(); len(seen) != 1 || seen[0] != qualified {
					t.Fatalf("admission resolved models %v, want only %q", seen, qualified)
				}
			})
		}
	}
}

// TestModelQualificationLeavesOtherSetupsAlone checks the cases that must
// behave as before: a hosted backend, an install with no pi default to take a
// provider from, and a model that already names a different provider.
func TestModelQualificationLeavesOtherSetupsAlone(t *testing.T) {
	t.Run("hosted backend keeps its bare model and holds no node", func(t *testing.T) {
		r := newLeaseRig(t, "ollama/qwen3.8:27b")
		got := r.runOne(t, "claude-code", "claude-opus-5-5")
		if got.model != "claude-opus-5-5" || got.computeNode != "" || got.leaseNode != "" {
			t.Fatalf("claude-code spawn = %+v, want the bare model and no node", got)
		}
		if seen := r.seen(); len(seen) != 1 || seen[0] != "claude-opus-5-5" {
			t.Fatalf("admission resolved models %v, want only the bare model", seen)
		}
	})

	t.Run("no pi default leaves a bare model as written", func(t *testing.T) {
		r := newLeaseRig(t, "")
		got := r.runOne(t, "pi-prompt", "qwen3.8:27b")
		if got.model != "qwen3.8:27b" || got.computeNode != "" {
			t.Fatalf("spawn = %+v, want the bare model and no node", got)
		}
	})

	t.Run("a default with no provider changes nothing", func(t *testing.T) {
		r := newLeaseRig(t, "qwen3.8:27b")
		got := r.runOne(t, "pi-prompt", "qwen3.8:32b")
		if got.model != "qwen3.8:32b" {
			t.Fatalf("SpawnRequest.Model = %q, want the model as written", got.model)
		}
	})

	t.Run("another provider is kept", func(t *testing.T) {
		r := newLeaseRig(t, "ollama/qwen3.8:27b")
		got := r.runOne(t, "pi-prompt", "anthropic/claude-haiku-5-5")
		if got.model != "anthropic/claude-haiku-5-5" || got.computeNode != "" {
			t.Fatalf("spawn = %+v, want the model as written and no node", got)
		}
	})
}
