package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/observer"
	"github.com/dmz006/datawatch/internal/server"
)

const advisorGB = uint64(1) << 30

// advisorPeers answers the observer payload lookup the advisor reads for each
// node's live memory and disk; every other PeerRegistryAPI method is unused.
type advisorPeers struct {
	server.PeerRegistryAPI
	payloads map[string]*observer.StatsResponse
}

func (p advisorPeers) LastPayload(name string) *observer.StatsResponse { return p.payloads[name] }

// advisorSnap is an observer payload with the given total/used memory and
// total/used disk, in GiB.
func advisorSnap(memTotal, memUsed, diskTotal, diskUsed uint64) *observer.StatsResponse {
	return &observer.StatsResponse{
		Mem:  observer.Mem{TotalBytes: memTotal * advisorGB, UsedBytes: memUsed * advisorGB},
		Disk: []observer.Disk{{TotalBytes: diskTotal * advisorGB, UsedBytes: diskUsed * advisorGB}},
	}
}

// tagsNode starts an ollama stand-in whose /api/tags lists the given models.
func tagsNode(t *testing.T, models ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		var items []string
		for _, m := range models {
			items = append(items, fmt.Sprintf(`{"name":%q}`, m))
		}
		_, _ = fmt.Fprintf(w, `{"models":[%s]}`, strings.Join(items, ","))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newAdvisorUnderTest builds the daemon's placement advisor over a compute
// registry holding nodes, one LLM entry that lists them in order, and the
// observer payloads.
func newAdvisorUnderTest(t *testing.T, cfg *config.Config, nodes []*compute.Node, peers map[string]*observer.StatsResponse) *placementAdvisor {
	t.Helper()
	creg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, n := range nodes {
		if err := creg.Add(n); err != nil {
			t.Fatalf("add node %s: %v", n.Name, err)
		}
		names = append(names, n.Name)
	}
	return &placementAdvisor{
		cfg: cfg,
		entry: func(backend, model string) inference.LLM {
			return inference.LLM{Name: "ollama", Kind: "ollama", ComputeNodes: names}
		},
		nodes: func() *compute.Registry { return creg },
		peers: func() server.PeerRegistryAPI { return advisorPeers{payloads: peers} },
		now:   func() time.Time { return time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC) },
	}
}

func advisorNode(name, addr string) *compute.Node {
	return &compute.Node{Name: name, Kind: compute.KindLocal, Address: addr}
}

const advisorModel = "ollama/qwen3:8b"

// A node whose /api/tags lists the model serves it: placement chooses it, marks
// it serving, and the missing_model policy has nothing to decide.
func TestPlacementAdvisorReadsServedModelsFromTags(t *testing.T) {
	alpha := tagsNode(t, "llama3:70b")
	beta := tagsNode(t, "qwen3:8b")
	a := newAdvisorUnderTest(t, &config.Config{},
		[]*compute.Node{advisorNode("alpha", alpha.URL), advisorNode("beta", beta.URL)}, nil)
	plan := a.Plan("pi", advisorModel, "")
	if !plan.Relevant || plan.Chosen != "beta" || !plan.Serving {
		t.Fatalf("beta serves the model per /api/tags, got %+v", plan)
	}
}

// With no node serving the model, the plan is the planner's fit on observed
// memory and disk, not serving, and carries the live policy.
func TestPlacementAdvisorPlansMissingModelFromObserverPayloads(t *testing.T) {
	alpha := tagsNode(t, "llama3:70b")
	beta := tagsNode(t)
	cfg := &config.Config{ComputePlacement: config.ComputePlacementConfig{MissingModel: config.ComputePlacementAutoLoad, AssumeModelSizeGB: 10}}
	a := newAdvisorUnderTest(t, cfg,
		[]*compute.Node{advisorNode("alpha", alpha.URL), advisorNode("beta", beta.URL)},
		map[string]*observer.StatsResponse{
			"alpha": advisorSnap(32, 30, 500, 100), // 2 GB free: does not fit
			"beta":  advisorSnap(64, 8, 500, 100),  // 56 GB free: fits
		})
	plan := a.Plan("pi", advisorModel, "")
	if !plan.Relevant || plan.Chosen != "beta" || plan.Serving {
		t.Fatalf("beta fits and is not serving, got %+v", plan)
	}
	if plan.Policy != config.ComputePlacementAutoLoad {
		t.Errorf("policy = %q, want the live auto_load", plan.Policy)
	}
	// When nothing fits, the planner's refusal lines come back, one per node.
	small := newAdvisorUnderTest(t, cfg,
		[]*compute.Node{advisorNode("alpha", alpha.URL), advisorNode("beta", beta.URL)},
		map[string]*observer.StatsResponse{"alpha": advisorSnap(32, 30, 500, 100), "beta": advisorSnap(32, 31, 500, 100)})
	none := small.Plan("pi", advisorModel, "")
	if none.Chosen != "" || len(none.Refused) != 2 || !strings.HasPrefix(none.Refused[0], "alpha: ") || !strings.HasPrefix(none.Refused[1], "beta: ") {
		t.Fatalf("want a refusal line per node, got %+v", none)
	}
}

// The task's node pin reaches the planner: only the pinned node is considered,
// and a pin on a node outside the entry is refused rather than ignored.
func TestPlacementAdvisorPassesTheTaskPinToThePlanner(t *testing.T) {
	alpha := tagsNode(t)
	beta := tagsNode(t)
	cfg := &config.Config{ComputePlacement: config.ComputePlacementConfig{AssumeModelSizeGB: 10}}
	a := newAdvisorUnderTest(t, cfg,
		[]*compute.Node{advisorNode("alpha", alpha.URL), advisorNode("beta", beta.URL)},
		map[string]*observer.StatsResponse{
			"alpha": advisorSnap(128, 0, 500, 0), // the roomier node
			"beta":  advisorSnap(64, 0, 500, 0),
		})
	if plan := a.Plan("pi", advisorModel, ""); plan.Chosen != "alpha" {
		t.Fatalf("unpinned, the roomier alpha wins, got %+v", plan)
	}
	if plan := a.Plan("pi", advisorModel, "beta"); plan.Chosen != "beta" {
		t.Fatalf("pinned to beta, the planner must choose beta, got %+v", plan)
	}
	plan := a.Plan("pi", advisorModel, "ghost")
	if plan.Chosen != "" || len(plan.Refused) != 1 || !strings.Contains(plan.Refused[0], "pinned by the task") {
		t.Fatalf("a pin outside the entry is refused, got %+v", plan)
	}
}

// A node pinned to another model is never offered the model, whatever its
// allow_model_swap says: placement never replaces a pin.
func TestPlacementAdvisorNeverReplacesANodePin(t *testing.T) {
	alpha := tagsNode(t, "other:1b")
	pinned := advisorNode("alpha", alpha.URL)
	pinned.NodePinnedModel = "other:1b"
	pinned.AllowModelSwap = true
	cfg := &config.Config{ComputePlacement: config.ComputePlacementConfig{AssumeModelSizeGB: 10}}
	a := newAdvisorUnderTest(t, cfg, []*compute.Node{pinned},
		map[string]*observer.StatsResponse{"alpha": advisorSnap(128, 0, 500, 0)})
	plan := a.Plan("pi", advisorModel, "")
	if plan.Chosen != "" || len(plan.Refused) != 1 || !strings.Contains(plan.Refused[0], `pinned to model "other:1b"`) {
		t.Fatalf("a pinned node must be refused with its pin named, got %+v", plan)
	}
	// The registry's node is untouched: the planner got a copy.
	if !pinned.AllowModelSwap {
		t.Error("placement must not change the node's allow_model_swap")
	}
	// A node pinned to the very model asked for serves it.
	same := advisorNode("beta", tagsNode(t).URL)
	same.NodePinnedModel = "qwen3:8b"
	b := newAdvisorUnderTest(t, cfg, []*compute.Node{same}, nil)
	if plan := b.Plan("pi", advisorModel, ""); plan.Chosen != "beta" || !plan.Serving {
		t.Fatalf("a node pinned to the model serves it, got %+v", plan)
	}
}

// An entry that lists no compute node is not placement's business.
func TestPlacementAdvisorNotRelevantWithoutNodes(t *testing.T) {
	a := newAdvisorUnderTest(t, &config.Config{}, nil, nil)
	if plan := a.Plan("pi", advisorModel, ""); plan.Relevant {
		t.Fatalf("no nodes: placement has no opinion, got %+v", plan)
	}
}
