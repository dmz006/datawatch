package main

// Restart adoption of pi-prompt workers on a two-node entry.
//
// Path that put both workers on the first node: the pi-prompt entry lists two
// nodes but its models list pairs only the first node with the model, so the
// resolver's serving filter collapses the candidates to that one node and the
// resolver returns it as a default node (and a node:<first> pool). A worker
// whose session says nothing the node resolver can match (no ComputeNodeRef,
// and an endpoint that equals no registered node address) kept that default,
// so every such worker was adopted onto the first node and the second node
// sat idle at 0 of 1.

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

const (
	piNodeFirst   = "datawatch"
	piNodeSecond  = "local-node"
	piAddrFirst   = "http://10.0.0.1:11434"
	piAddrSecond  = "http://10.0.0.2:11434"
	piBackendName = "pi-prompt"
)

// piPromptRegistries builds the production-shaped registries: two compute
// nodes with distinct explicit addresses and a pi-prompt entry that lists both.
// The ollama entry is the engine entry the pi backend's provider resolves to.
// servesBoth says whether the models list leaves both nodes serving the model;
// when false only the first node is paired with it.
func piPromptRegistries(t *testing.T, servesBoth bool) (*inference.Registry, *compute.Registry) {
	t.Helper()
	creg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []*compute.Node{
		{Name: piNodeFirst, Kind: compute.KindOllama, Address: piAddrFirst},
		{Name: piNodeSecond, Kind: compute.KindOllama, Address: piAddrSecond},
	} {
		if err := creg.Add(n); err != nil {
			t.Fatal(err)
		}
	}
	models := []inference.EnabledModel{
		{Node: piNodeFirst, Model: "qwen3.8:27b"},
		{Node: piNodeSecond, Model: "qwen3.8:27b"},
	}
	if !servesBoth {
		models[1].Model = "llama3.3:70b"
	}
	reg := inference.NewRegistry()
	for _, l := range []*inference.LLM{
		{Name: "ollama", Kind: "ollama", ComputeNodes: []string{piNodeFirst, piNodeSecond}},
		{Name: piBackendName, Kind: inference.KindPiPrompt, ComputeNodes: []string{piNodeFirst, piNodeSecond}, Models: models},
	} {
		if err := reg.Add(l); err != nil {
			t.Fatalf("add llm %s: %v", l.Name, err)
		}
	}
	return reg, creg
}

// piRestart runs the boot-time resume pass for two running tasks whose
// surviving sessions are given, on a fresh ledger with one engine per node, and
// returns the settled rig and the two task IDs.
func piRestart(t *testing.T, servesBoth bool, one, two *session.Session) (*restartRig, string, string) {
	t.Helper()
	reg, creg := piPromptRegistries(t, servesBoth)
	sessions := map[string]*session.Session{"sess-one": one, "sess-two": two}
	r := newRestartRigWith(t, sessions, reg, creg, map[string]int{"node:" + piNodeFirst: 1, "node:" + piNodeSecond: 1})
	a := r.runningTask("one", "sess-one")
	b := r.runningTask("two", "sess-two")
	r.api.SetExecutors(func(_ context.Context, req autonomous.SpawnRequest) (autonomous.SpawnResult, error) {
		t.Errorf("task %s was spawned, want its surviving session adopted", req.TaskID)
		return autonomous.SpawnResult{}, context.Canceled
	}, func(ctx context.Context, _ *autonomous.PRD, _ *autonomous.Task) (autonomous.VerificationResult, error) {
		<-ctx.Done()
		return autonomous.VerificationResult{}, ctx.Err()
	})
	r.waitFor("both sessions to be adopted", func() bool { return r.led.LeaseOf(a) != nil && r.led.LeaseOf(b) != nil })
	return r, a, b
}

// assertNoNodeOverLimit fails when a node pool holds more than its limit.
func assertNoNodeOverLimit(t *testing.T, st capacity.Status) {
	t.Helper()
	for _, p := range st.Pools {
		if len(p.Name) > 5 && p.Name[:5] == "node:" && p.Limit > 0 && p.Held > p.Limit {
			t.Errorf("pool %s holds %d, over its limit %d", p.Name, p.Held, p.Limit)
		}
	}
}

// placedSession is a surviving worker session the way real placement leaves
// it for a node: the explicit reference, the node address and the Ollama URL.
func placedSession(node, addr string) *session.Session {
	return &session.Session{ComputeNodeRef: node, NodeAddress: addr, OllamaURL: addr}
}

func TestPiPromptRestartKeepsOneWorkerPerNode(t *testing.T) {
	for _, servesBoth := range []bool{true, false} {
		name := "both nodes serve the model"
		if !servesBoth {
			name = "serving filter leaves the first node"
		}
		t.Run(name, func(t *testing.T) {
			t.Run("explicit ComputeNodeRef", func(t *testing.T) {
				r, a, b := piRestart(t, servesBoth,
					placedSession(piNodeFirst, piAddrFirst), placedSession(piNodeSecond, piAddrSecond))
				assertOnePerNode(t, r, a, b)
			})

			t.Run("endpoint equals the node address", func(t *testing.T) {
				r, a, b := piRestart(t, servesBoth,
					&session.Session{NodeAddress: piAddrFirst, OllamaURL: piAddrFirst},
					&session.Session{NodeAddress: "HTTP://10.0.0.2:11434/", OllamaURL: piAddrSecond})
				assertOnePerNode(t, r, a, b)
			})

			t.Run("endpoint matches nothing never defaults to a node", func(t *testing.T) {
				stray := func() *session.Session {
					return &session.Session{NodeAddress: "http://10.9.9.9:11434", OllamaURL: "http://10.9.9.9:11434"}
				}
				r, a, b := piRestart(t, servesBoth, stray(), stray())
				st := r.led.Snapshot()
				for _, id := range []string{a, b} {
					ls := r.led.LeaseOf(id)
					if ls.Node != "" {
						t.Errorf("task %s adopted on node %q, want no node", id, ls.Node)
					}
				}
				for _, n := range []string{piNodeFirst, piNodeSecond} {
					if p, ok := poolNamed(st, "node:"+n); ok && p.Held != 0 {
						t.Errorf("pool node:%s = %+v, want idle (no worker can be placed on it by default)", n, p)
					}
				}
				if p, ok := poolNamed(st, "llm:"+piBackendName); !ok || p.Held != 2 {
					t.Errorf("pool llm:%s = %+v (found %v), want 2 held", piBackendName, p, ok)
				}
			})
		})
	}
}

func assertOnePerNode(t *testing.T, r *restartRig, a, b string) {
	t.Helper()
	st := r.led.Snapshot()
	want := map[string]string{a: piNodeFirst, b: piNodeSecond}
	got := leaseNodes(st)
	for id, node := range want {
		if got[id] != node {
			t.Errorf("task %s lease node = %q, want %q (leases %v)", id, got[id], node, got)
		}
	}
	for _, n := range []string{piNodeFirst, piNodeSecond} {
		if p, ok := poolNamed(st, "node:"+n); !ok || p.Held != 1 {
			t.Errorf("pool node:%s = %+v (found %v), want 1 held", n, p, ok)
		}
	}
	if p, ok := poolNamed(st, "llm:"+piBackendName); !ok || p.Held != 2 {
		t.Errorf("pool llm:%s = %+v (found %v), want 2 held", piBackendName, p, ok)
	}
	assertNoNodeOverLimit(t, st)
}

// entryNodeCount counts the nodes the hosting entry lists whatever the serving
// filter leaves, so adoption can tell a single-node entry from a collapsed one.
func TestEntryNodeCountIgnoresTheServingFilter(t *testing.T) {
	for _, servesBoth := range []bool{true, false} {
		reg, _ := piPromptRegistries(t, servesBoth)
		count := entryNodeCount(func() *inference.Registry { return reg })
		if n := count(piBackendName, restartModel); n != 2 {
			t.Errorf("servesBoth=%v: pi-prompt entry count = %d, want 2", servesBoth, n)
		}
		if n := count("claude-code", "opus"); n != 0 {
			t.Errorf("servesBoth=%v: hosted backend count = %d, want 0", servesBoth, n)
		}
	}

	solo, _ := nodeTestRegistries(t,
		[]*inference.LLM{{Name: "ollama", Kind: "ollama", ComputeNodes: []string{"alpha"}}},
		map[string]int{"alpha": 50})
	if n := entryNodeCount(func() *inference.Registry { return solo })("ollama", restartModel); n != 1 {
		t.Errorf("single-node entry count = %d, want 1", n)
	}
	if n := entryNodeCount(nil)(piBackendName, restartModel); n != 0 {
		t.Errorf("nil registry count = %d, want 0", n)
	}
}

// strayEndpointSession is a surviving worker whose endpoint matches no node
// address and which carries no ComputeNodeRef.
func strayEndpointSession(addr string) *session.Session {
	return &session.Session{NodeAddress: addr, OllamaURL: addr}
}

// TestPiPromptRestartUnknownNodeHoldsNoNode proves that a surviving worker
// whose node cannot be determined holds no node pool and is flagged, that no
// guess from a host name, DNS or part of a node name is made, and that a
// worker whose node is knowable in the same run still lands on it.
func TestPiPromptRestartUnknownNodeHoldsNoNode(t *testing.T) {
	const piNodeFirstPort = "http://10.0.0.1:9999"
	const piNodeLookalike = "http://datawatch-gpu.example:11434"

	t.Run("two workers on no known node", func(t *testing.T) {
		for _, servesBoth := range []bool{true, false} {
			// Same host as the first node, other port; and a host that only
			// resembles a node name.
			r, a, b := piRestart(t, servesBoth, strayEndpointSession(piNodeFirstPort), strayEndpointSession(piNodeLookalike))
			st := r.led.Snapshot()
			for _, id := range []string{a, b} {
				ls := r.led.LeaseOf(id)
				if ls.Node != "" || !ls.NodeUnknown {
					t.Errorf("servesBoth=%v task %s lease = %+v, want no node and node_unknown", servesBoth, id, ls)
				}
			}
			for _, n := range []string{piNodeFirst, piNodeSecond} {
				if p, ok := poolNamed(st, "node:"+n); ok && p.Held != 0 {
					t.Errorf("servesBoth=%v pool node:%s = %+v, want held 0", servesBoth, n, p)
				}
			}
			if p, ok := poolNamed(st, "llm:"+piBackendName); !ok || p.Held != 2 {
				t.Errorf("servesBoth=%v pool llm:%s = %+v (found %v), want 2 held", servesBoth, piBackendName, p, ok)
			}
			assertNoNodeOverLimit(t, st)

			// The /api/capacity-shaped body flags both leases.
			body, err := json.Marshal(map[string]any{"pools": st.Pools, "leases": st.Leases})
			if err != nil {
				t.Fatal(err)
			}
			var shaped struct {
				Leases []map[string]any `json:"leases"`
			}
			if err := json.Unmarshal(body, &shaped); err != nil {
				t.Fatal(err)
			}
			if len(shaped.Leases) != 2 {
				t.Fatalf("want 2 leases in the body, got %s", body)
			}
			for _, ls := range shaped.Leases {
				if ls["node_unknown"] != true {
					t.Errorf("lease %v is not flagged in the capacity body", ls)
				}
			}
			text, err := autonomous.FormatCapacityText(body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(text, "Leases with unknown node: 2") {
				t.Errorf("status text should count both leases:\n%s", text)
			}
		}
	})

	t.Run("a knowable node still lands on it", func(t *testing.T) {
		reg, creg := piPromptRegistries(t, true)
		sessions := map[string]*session.Session{
			"sess-lost":  strayEndpointSession(piNodeFirstPort),
			"sess-known": placedSession(piNodeSecond, piAddrSecond),
		}
		r := newRestartRigWith(t, sessions, reg, creg, map[string]int{"node:" + piNodeFirst: 1, "node:" + piNodeSecond: 1})
		lost := r.runningTask("lost", "sess-lost")
		known := r.runningTask("known", "sess-known")
		r.api.SetExecutors(func(_ context.Context, req autonomous.SpawnRequest) (autonomous.SpawnResult, error) {
			t.Errorf("task %s was spawned, want its surviving session adopted", req.TaskID)
			return autonomous.SpawnResult{}, context.Canceled
		}, func(ctx context.Context, _ *autonomous.PRD, _ *autonomous.Task) (autonomous.VerificationResult, error) {
			<-ctx.Done()
			return autonomous.VerificationResult{}, ctx.Err()
		})
		r.waitFor("both sessions to be adopted", func() bool { return r.led.LeaseOf(lost) != nil && r.led.LeaseOf(known) != nil })

		if ls := r.led.LeaseOf(lost); ls.Node != "" || !ls.NodeUnknown {
			t.Errorf("lost worker lease = %+v, want no node and node_unknown", ls)
		}
		if ls := r.led.LeaseOf(known); ls.Node != piNodeSecond || ls.NodeUnknown {
			t.Errorf("known worker lease = %+v, want node %s and no flag", ls, piNodeSecond)
		}
		st := r.led.Snapshot()
		if p, ok := poolNamed(st, "node:"+piNodeFirst); ok && p.Held != 0 {
			t.Errorf("pool node:%s = %+v, want held 0", piNodeFirst, p)
		}
		if p, ok := poolNamed(st, "node:"+piNodeSecond); !ok || p.Held != 1 {
			t.Errorf("pool node:%s = %+v (found %v), want held 1", piNodeSecond, p, ok)
		}
		if p, ok := poolNamed(st, "llm:"+piBackendName); !ok || p.Held != 2 {
			t.Errorf("pool llm:%s = %+v (found %v), want 2 held", piBackendName, p, ok)
		}
	})
}

// sessionEndpoint reads the endpoint adoption logs for a worker on no known
// node: the node address, else the engine URL, and "" for an unknown session.
func TestSessionEndpointReadsNodeAddressThenEngineURL(t *testing.T) {
	sessions := map[string]*session.Session{
		"a": {NodeAddress: "http://n:1", OllamaURL: "http://o:2"},
		"b": {OllamaURL: "http://o:2"},
	}
	read := sessionEndpoint(func(id string) (*session.Session, bool) { s, ok := sessions[id]; return s, ok })
	for id, want := range map[string]string{"a": "http://n:1", "b": "http://o:2", "gone": ""} {
		if got := read(id); got != want {
			t.Errorf("endpoint(%s) = %q, want %q", id, got, want)
		}
	}
	if got := sessionEndpoint(nil)("a"); got != "" {
		t.Errorf("no lookup wired: endpoint = %q, want empty", got)
	}
	nilSess := sessionEndpoint(func(string) (*session.Session, bool) { return nil, true })
	if got := nilSess("a"); got != "" {
		t.Errorf("nil session: endpoint = %q, want empty", got)
	}
}
