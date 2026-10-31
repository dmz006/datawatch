// REST tests for the served-models route and the node pin actions. Nodes are
// read through a fake lister; nothing here touches the network.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/server/multiserver"
)

const restRoutedModel = "ollama/qwen3.8:27b"

// restFleet is a fake fleet: the models each node serves, and a count of how
// many times a node was read.
type restFleet struct {
	mu     sync.Mutex
	serves map[string][]string
	fail   map[string]string
	reads  atomic.Int32
}

func (f *restFleet) lister(_ context.Context, n *compute.Node, _ time.Duration) ([]string, error) {
	f.reads.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if msg, bad := f.fail[n.Name]; bad {
		return nil, &fleetError{msg}
	}
	return append([]string{}, f.serves[n.Name]...), nil
}

type fleetError struct{ msg string }

func (e *fleetError) Error() string { return e.msg }

// restModelHostCheck mirrors the daemon's check over the same cache: a named
// node is checked on its own, otherwise every enabled node is.
func restModelHostCheck(reg *compute.Registry, cache *compute.ServedModelsCache) autonomous.ModelHostCheckFn {
	return func(ctx context.Context, backend, model, node string) autonomous.ModelHostVerdict {
		v := autonomous.ModelHostVerdict{Applicable: true, Model: model, Node: node}
		var nodes []*compute.Node
		for _, n := range reg.List() {
			if !n.Disabled {
				nodes = append(nodes, n)
			}
		}
		if node != "" {
			if _, err := reg.Get(node); err != nil {
				v.Refused, v.NodeUnknown = true, true
				v.Message = "node " + node + " does not serve model " + model + ": it is not a registered node"
				return v
			}
		}
		res := compute.CheckModel(model, node, cache.GetAll(ctx, nodes, false), "assume_serves")
		v.Refused, v.NodeUnknown = res.Refused, res.NodeUnknown
		v.ServedBy = append([]string{}, res.Served...)
		for _, c := range res.Checked {
			v.Checked = append(v.Checked, autonomous.ModelHostNodeState{Node: c.Node, State: c.State, Reason: c.Reason})
		}
		if res.Refused {
			v.Message = (&compute.NoNodeServesModelError{Result: res}).Error()
		}
		return v
	}
}

type restRoutingEnv struct {
	srv   *Server
	fleet *restFleet
	mgr   *autonomous.Manager
	prdID string
}

// newRestRoutingEnv builds a server with three registered nodes (gpu-a serves
// the routed model, gpu-b serves another, gpu-off is disabled) and an
// Automaton in review with one story and one task whose worker is the routed
// model on the pi backend.
func newRestRoutingEnv(t *testing.T) *restRoutingEnv {
	t.Helper()
	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	for _, n := range []*compute.Node{
		{Name: "gpu-a", Kind: compute.KindOllama, Address: "http://gpu-a.invalid:11434"},
		{Name: "gpu-b", Kind: compute.KindOllama, Address: "http://gpu-b.invalid:11434"},
		{Name: "gpu-off", Kind: compute.KindOllama, Address: "http://gpu-off.invalid:11434", Disabled: true},
	} {
		if err := reg.Add(n); err != nil {
			t.Fatalf("Add %s: %v", n.Name, err)
		}
	}
	fleet := &restFleet{
		serves: map[string][]string{
			"gpu-a":   {"qwen3.8:27b"},
			"gpu-b":   {"llama3:8b"},
			"gpu-off": {"qwen3.8:27b"},
		},
		fail: map[string]string{},
	}
	cache := compute.NewServedModelsCache(fleet.lister, func() time.Duration { return 300 * time.Second }, nil, nil)
	check := restModelHostCheck(reg, cache)

	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetModelHostCheck(check)
	mgr.SetModelRoutingSettings(func() autonomous.ModelRoutingSettings {
		return autonomous.ModelRoutingSettings{RefusalMode: "refuse", AlertOnRefusal: true, VerifierFollowsWorkerNode: true}
	})
	prd, err := mgr.CreatePRD("routing spec", t.TempDir(), "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	prd.Backend, prd.Model = "pi", restRoutedModel
	prd.Story = []autonomous.Story{{ID: "s1", Title: "story", Tasks: []autonomous.Task{{ID: "t1", Title: "task"}}}}
	prd.Status = autonomous.PRDNeedsReview
	if err := mgr.Store().SavePRD(prd); err != nil {
		t.Fatalf("SavePRD: %v", err)
	}

	srv := &Server{autonomousMgr: newTestAPI(mgr), computeReg: reg}
	srv.SetServedModelsCache(cache)
	srv.SetModelHostCheck(check)
	srv.cfg = &config.Config{}
	return &restRoutingEnv{srv: srv, fleet: fleet, mgr: mgr, prdID: prd.ID}
}

func (e *restRoutingEnv) get(t *testing.T, query string, peer *multiserver.Entry) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/compute/served-models"+query, nil)
	if peer != nil {
		req = req.WithContext(context.WithValue(req.Context(), fedPeerKey, peer))
	}
	rr := httptest.NewRecorder()
	e.srv.handleServedModels(rr, req)
	return rr
}

func (e *restRoutingEnv) action(t *testing.T, action, body string, peer *multiserver.Entry) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/autonomous/prds/"+e.prdID+"/"+action, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if peer != nil {
		req = req.WithContext(context.WithValue(req.Context(), fedPeerKey, peer))
	}
	rr := httptest.NewRecorder()
	e.srv.handleAutonomousPRDs(rr, req)
	return rr
}

type servedModelsAnswer struct {
	CacheSeconds int    `json:"cache_seconds"`
	RefusalMode  string `json:"refusal_mode"`
	Nodes        []struct {
		Node       string   `json:"node"`
		Kind       string   `json:"kind"`
		Models     []string `json:"models"`
		Known      bool     `json:"known"`
		Cached     bool     `json:"cached"`
		AgeSeconds int      `json:"age_seconds"`
		FetchedAt  string   `json:"fetched_at"`
		Error      string   `json:"error"`
		Disabled   bool     `json:"disabled"`
	} `json:"nodes"`
	Check *struct {
		Model       string   `json:"model"`
		Refused     bool     `json:"refused"`
		NodeUnknown bool     `json:"node_unknown"`
		ServedBy    []string `json:"served_by"`
		Checked     []struct {
			Node   string `json:"node"`
			State  string `json:"state"`
			Reason string `json:"reason"`
		} `json:"checked"`
		Message string `json:"message"`
	} `json:"check"`
}

func decodeServed(t *testing.T, rr *httptest.ResponseRecorder) servedModelsAnswer {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body %s", rr.Code, rr.Body.String())
	}
	var a servedModelsAnswer
	if err := json.Unmarshal(rr.Body.Bytes(), &a); err != nil {
		t.Fatalf("bad json %q: %v", rr.Body.String(), err)
	}
	return a
}

func TestModelRoutingREST_ServedModelsListsEveryNode(t *testing.T) {
	e := newRestRoutingEnv(t)
	e.fleet.fail["gpu-b"] = "connection refused"

	a := decodeServed(t, e.get(t, "", nil))
	if a.CacheSeconds != 0 && a.CacheSeconds != 300 {
		t.Fatalf("cache_seconds %d", a.CacheSeconds)
	}
	if a.RefusalMode != "refuse" {
		t.Fatalf("refusal_mode %q, want refuse", a.RefusalMode)
	}
	if a.Check != nil {
		t.Fatalf("a request with no model carried a check: %+v", a.Check)
	}
	names := []string{}
	for _, n := range a.Nodes {
		names = append(names, n.Node)
	}
	if !slices.Contains(names, "gpu-a") || !slices.Contains(names, "gpu-b") || !slices.Contains(names, "gpu-off") || len(names) != 3 {
		t.Fatalf("nodes %v, want gpu-a, gpu-b and gpu-off", names)
	}
	for _, n := range a.Nodes {
		switch n.Node {
		case "gpu-a":
			if !n.Known || n.Kind != "ollama" || !slices.Equal(n.Models, []string{"qwen3.8:27b"}) || n.FetchedAt == "" {
				t.Fatalf("gpu-a = %+v", n)
			}
			if _, err := time.Parse(time.RFC3339, n.FetchedAt); err != nil {
				t.Fatalf("fetched_at %q is not RFC 3339", n.FetchedAt)
			}
		case "gpu-b":
			if n.Known || n.Error == "" || len(n.Models) != 0 {
				t.Fatalf("an unreadable node must be unknown with an error: %+v", n)
			}
		case "gpu-off":
			if !n.Disabled || n.Known || len(n.Models) != 0 {
				t.Fatalf("a disabled node must be listed as disabled and not read: %+v", n)
			}
		}
	}
	// The disabled node was never read: two enabled nodes, one read each.
	if got := e.fleet.reads.Load(); got != 2 {
		t.Fatalf("%d node reads, want 2 (the disabled node is not read)", got)
	}

	// One node by name; an unknown name is 404.
	one := decodeServed(t, e.get(t, "?node=gpu-a", nil))
	if len(one.Nodes) != 1 || one.Nodes[0].Node != "gpu-a" {
		t.Fatalf("?node=gpu-a returned %+v", one.Nodes)
	}
	if rr := e.get(t, "?node=nope", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown node: status %d, want 404", rr.Code)
	}
	// Only GET is served.
	req := httptest.NewRequest(http.MethodPost, "/api/compute/served-models", nil)
	rr := httptest.NewRecorder()
	e.srv.handleServedModels(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d, want 405", rr.Code)
	}
}

func TestModelRoutingREST_RefreshBypassesCache(t *testing.T) {
	e := newRestRoutingEnv(t)

	decodeServed(t, e.get(t, "?node=gpu-a", nil))
	if got := e.fleet.reads.Load(); got != 1 {
		t.Fatalf("first request read the node %d times, want 1", got)
	}
	second := decodeServed(t, e.get(t, "?node=gpu-a", nil))
	if got := e.fleet.reads.Load(); got != 1 {
		t.Fatalf("second request read the node again (%d reads); it must come from the cache", got)
	}
	if !second.Nodes[0].Cached {
		t.Fatalf("second answer not marked cached: %+v", second.Nodes[0])
	}
	// The node gains a model; refresh=true must see it, the cache must not.
	e.fleet.mu.Lock()
	e.fleet.serves["gpu-a"] = []string{"qwen3.8:27b", "gemma3:12b"}
	e.fleet.mu.Unlock()
	stale := decodeServed(t, e.get(t, "?node=gpu-a", nil))
	if len(stale.Nodes[0].Models) != 1 {
		t.Fatalf("cached answer already shows the new model: %v", stale.Nodes[0].Models)
	}
	fresh := decodeServed(t, e.get(t, "?node=gpu-a&refresh=true", nil))
	if got := e.fleet.reads.Load(); got != 2 {
		t.Fatalf("refresh=true: %d reads, want 2", got)
	}
	if len(fresh.Nodes[0].Models) != 2 || fresh.Nodes[0].Cached {
		t.Fatalf("refresh=true answer = %+v, want 2 models and not cached", fresh.Nodes[0])
	}
}

func TestModelRoutingREST_ModelQueryReportsRefusal(t *testing.T) {
	e := newRestRoutingEnv(t)

	// Neither enabled node serves this model: refused, the message names the
	// model and the nodes checked.
	a := decodeServed(t, e.get(t, "?model=ollama/mistral:7b&backend=pi", nil))
	if a.Check == nil || !a.Check.Refused {
		t.Fatalf("check = %+v, want refused", a.Check)
	}
	if !strings.Contains(a.Check.Message, "ollama/mistral:7b") || !strings.Contains(a.Check.Message, "gpu-a") || !strings.Contains(a.Check.Message, "gpu-b") {
		t.Fatalf("message %q must name the model and the nodes", a.Check.Message)
	}
	if len(a.Check.ServedBy) != 0 || len(a.Check.Checked) != 2 {
		t.Fatalf("served_by %v checked %+v", a.Check.ServedBy, a.Check.Checked)
	}

	// gpu-a serves the routed model: not refused, and it is named as a host.
	ok := decodeServed(t, e.get(t, "?model="+restRoutedModel+"&backend=pi", nil))
	if ok.Check == nil || ok.Check.Refused {
		t.Fatalf("check = %+v, want not refused", ok.Check)
	}
	if !slices.Equal(ok.Check.ServedBy, []string{"gpu-a"}) {
		t.Fatalf("served_by %v, want [gpu-a]", ok.Check.ServedBy)
	}

	// A node that does not serve it, and a node that is not registered.
	b := decodeServed(t, e.get(t, "?model="+restRoutedModel+"&node=gpu-b", nil))
	if b.Check == nil || !b.Check.Refused || !strings.Contains(b.Check.Message, "gpu-b") {
		t.Fatalf("pinned check on gpu-b = %+v", b.Check)
	}

	// The REST answer agrees with a pin-time refusal for the same node.
	rr := e.action(t, "set_task_node", `{"task_id":"t1","node":"gpu-b"}`, nil)
	if rr.Code < 400 || rr.Code >= 500 {
		t.Fatalf("pin to gpu-b: status %d, want 4xx", rr.Code)
	}
	if strings.TrimSpace(rr.Body.String()) != b.Check.Message {
		t.Fatalf("pin refusal %q differs from the REST check message %q", strings.TrimSpace(rr.Body.String()), b.Check.Message)
	}
}

func TestModelRoutingREST_SetTaskNodeRefusesNodeNotServing(t *testing.T) {
	e := newRestRoutingEnv(t)

	for _, tc := range []struct {
		name, node, want string
		namesModel       bool
	}{
		{"node without the model", "gpu-b", "gpu-b", true},
		{"node that is not registered", "gpu-missing", "gpu-missing", false},
	} {
		rr := e.action(t, "set_task_node", `{"task_id":"t1","node":"`+tc.node+`"}`, nil)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400; body %s", tc.name, rr.Code, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, tc.want) || (tc.namesModel && !strings.Contains(body, restRoutedModel)) {
			t.Fatalf("%s: body %q must name the node (and the model for a node that lacks it)", tc.name, body)
		}
	}
	prd, _ := e.mgr.Store().GetPRD(e.prdID)
	if task, _ := findTaskForTest(prd, "t1"); task.PinnedNode != "" {
		t.Fatalf("a refused pin was stored: %q", task.PinnedNode)
	}

	// A node that serves the model is accepted and the pin shows in the JSON;
	// an empty node clears it.
	rr := e.action(t, "set_task_node", `{"task_id":"t1","node":"gpu-a","actor":"tester"}`, nil)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"pinned_node":"gpu-a"`) {
		t.Fatalf("pin to gpu-a: status %d body %s", rr.Code, rr.Body.String())
	}
	rr = e.action(t, "set_task_node", `{"task_id":"t1","node":""}`, nil)
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), `"pinned_node":"gpu-a"`) {
		t.Fatalf("clearing the pin: status %d body %s", rr.Code, rr.Body.String())
	}
	if rr := e.action(t, "set_task_node", `{"node":"gpu-a"}`, nil); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing task_id: status %d, want 400", rr.Code)
	}
}

func TestModelRoutingREST_SetStoryNodeAccepts(t *testing.T) {
	e := newRestRoutingEnv(t)

	rr := e.action(t, "set_story_node", `{"story_id":"s1","node":"gpu-a","actor":"tester"}`, nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, want 200; body %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"pinned_node":"gpu-a"`) {
		t.Fatalf("body %s does not carry the story pin", rr.Body.String())
	}
	prd, _ := e.mgr.Store().GetPRD(e.prdID)
	if prd.Story[0].PinnedNode != "gpu-a" {
		t.Fatalf("stored story pin %q, want gpu-a", prd.Story[0].PinnedNode)
	}
	// A node that does not serve the model is refused with a 4xx naming both.
	rr = e.action(t, "set_story_node", `{"story_id":"s1","node":"gpu-b"}`, nil)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "gpu-b") || !strings.Contains(rr.Body.String(), restRoutedModel) {
		t.Fatalf("refused story pin: status %d body %s", rr.Code, rr.Body.String())
	}
	// Clearing works, and an unknown story is a client error.
	if rr := e.action(t, "set_story_node", `{"story_id":"s1","node":""}`, nil); rr.Code != http.StatusOK {
		t.Fatalf("clearing: status %d", rr.Code)
	}
	if rr := e.action(t, "set_story_node", `{"story_id":"nope","node":"gpu-a"}`, nil); rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown story: status %d, want 400", rr.Code)
	}
}

func TestModelRoutingREST_NodeActionsNeedFederationWriteCapability(t *testing.T) {
	e := newRestRoutingEnv(t)
	readOnly := &multiserver.Entry{Name: "read-only-peer", Enabled: true, Federated: true}

	for _, tc := range []struct{ action, body string }{
		{"set_task_node", `{"task_id":"t1","node":"gpu-a"}`},
		{"set_story_node", `{"story_id":"s1","node":"gpu-a"}`},
	} {
		if rr := e.action(t, tc.action, tc.body, readOnly); rr.Code != http.StatusForbidden {
			t.Fatalf("%s from a peer without the capability: status %d, want 403", tc.action, rr.Code)
		}
		writer := &multiserver.Entry{Name: "writer", Enabled: true, Federated: true, Capabilities: []string{federation.CapAutonomousWrite}}
		if rr := e.action(t, tc.action, tc.body, writer); rr.Code != http.StatusOK {
			t.Fatalf("%s from a peer with the capability: status %d body %s", tc.action, rr.Code, rr.Body.String())
		}
	}
	prd, _ := e.mgr.Store().GetPRD(e.prdID)
	if task, _ := findTaskForTest(prd, "t1"); task.PinnedNode != "gpu-a" {
		t.Fatalf("the allowed peer's task pin was not stored: %q", task.PinnedNode)
	}

	// The read route needs the compute read capability.
	if rr := e.get(t, "", readOnly); rr.Code != http.StatusForbidden {
		t.Fatalf("served-models from a peer without compute:read: status %d, want 403", rr.Code)
	}
	reader := &multiserver.Entry{Name: "reader", Enabled: true, Federated: true, Capabilities: []string{federation.CapComputeRead}}
	if rr := e.get(t, "", reader); rr.Code != http.StatusOK {
		t.Fatalf("served-models from a peer with compute:read: status %d body %s", rr.Code, rr.Body.String())
	}
}

// findTaskForTest returns the task with the given id and its story.
func findTaskForTest(prd *autonomous.PRD, id string) (*autonomous.Task, *autonomous.Story) {
	for si := range prd.Story {
		for ti := range prd.Story[si].Tasks {
			if prd.Story[si].Tasks[ti].ID == id {
				return &prd.Story[si].Tasks[ti], &prd.Story[si]
			}
		}
	}
	return &autonomous.Task{}, &autonomous.Story{}
}
