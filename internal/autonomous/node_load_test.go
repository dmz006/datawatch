package autonomous

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

const nodeLoadModel = "qwen3.8:27b"

// fakeNodeOpts sets how a fake ollama node answers.
type fakeNodeOpts struct {
	// Resident reports the model as loaded from the start.
	Resident bool
	// FailLoad refuses load requests the way ollama does when memory is short.
	FailLoad bool
	// Gate, when set, keeps a requested load from showing as resident until
	// the channel is closed. The load request itself still succeeds.
	Gate chan struct{}
}

// fakeOllamaNode is an httptest ollama endpoint that reports the model it has
// loaded (GET /api/ps) and loads it on request (POST /api/generate).
type fakeOllamaNode struct {
	srv  *httptest.Server
	opts fakeNodeOpts

	mu       sync.Mutex
	loaded   bool
	psChecks int
	loads    int
}

func newFakeOllamaNode(t *testing.T, opts fakeNodeOpts) *fakeOllamaNode {
	t.Helper()
	f := &fakeOllamaNode{opts: opts, loaded: opts.Resident}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOllamaNode) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/api/ps":
		f.psChecks++
		if f.loaded && f.gateOpen() {
			_, _ = fmt.Fprintf(w, `{"models":[{"name":%q,"model":%q}]}`, nodeLoadModel, nodeLoadModel)
			return
		}
		_, _ = fmt.Fprint(w, `{"models":[]}`)
	case "/api/generate":
		f.loads++
		if f.opts.FailLoad {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{"error":"model requires more system memory"}`)
			return
		}
		f.loaded = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"done":true,"done_reason":"load"}`)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// gateOpen reports whether a requested load may show as resident. It is
// called with f.mu held.
func (f *fakeOllamaNode) gateOpen() bool {
	if f.opts.Gate == nil {
		return true
	}
	select {
	case <-f.opts.Gate:
		return true
	default:
		return false
	}
}

// counts returns the running-model checks and the load requests seen so far.
func (f *fakeOllamaNode) counts() (psChecks, loads int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.psChecks, f.loads
}

// recordingNodeAlerts is the fake alert sink: it keeps a copy of each alert.
type recordingNodeAlerts struct {
	mu     sync.Mutex
	raised []alerts.Alert
}

func (r *recordingNodeAlerts) Add(level alerts.Level, title, body, sessionID string) *alerts.Alert {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := alerts.Alert{Level: level, Title: title, Body: body, SessionID: sessionID}
	r.raised = append(r.raised, a)
	return &a
}

func (r *recordingNodeAlerts) snapshot() []alerts.Alert {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]alerts.Alert(nil), r.raised...)
}

// ollamaNodeLoader is the NodeLoader the tests wire: it loads through the real
// inference.ModelLoader against the fake nodes, keyed by node name.
func ollamaNodeLoader(nodes map[string]*fakeOllamaNode) NodeLoader {
	return func(ctx context.Context, name, model string, wait func(reason string)) error {
		f, ok := nodes[name]
		if !ok {
			return &inference.NodeLoadError{Node: name, Model: model, Cause: errors.New("not in the compute registry")}
		}
		loader := &inference.ModelLoader{Wait: 10 * time.Second, Poll: 5 * time.Millisecond}
		return loader.Ensure(ctx, &compute.Node{Name: name, Address: f.srv.URL}, model, wait)
	}
}

// nodeLoadRig is a Manager with an admission ledger over the named nodes, one
// PRD with one task, and the fake alert sink wired.
type nodeLoadRig struct {
	m    *Manager
	led  *capacity.Ledger
	prd  *PRD
	task Task
	sink *recordingNodeAlerts
}

// newNodeLoadRig builds the rig. order lists the nodes highest priority first.
// One name makes a single-node entry, which has no load step.
func newNodeLoadRig(t *testing.T, order []string, nodes map[string]*fakeOllamaNode) *nodeLoadRig {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	choices := make([]capacity.NodeChoice, 0, len(order))
	for i, n := range order {
		led.SetLimit("node:"+n, 1)
		choices = append(choices, capacity.NodeChoice{Name: n, Priority: 100 - i})
	}
	keys := func(backend, model string) ([]string, string) {
		if len(order) == 1 {
			return []string{"host", "node:" + order[0]}, order[0]
		}
		return []string{"host"}, ""
	}
	m.SetCapacity(led, keys)
	m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice { return choices })
	sink := &recordingNodeAlerts{}
	m.SetNodeLoad(ollamaNodeLoader(nodes), sink)
	prd := newPRDOnBackend(t, m, "pi", "ollama/"+nodeLoadModel)
	snap, _ := m.Store().SnapshotPRD(prd.ID)
	task := snap.Story[0].Tasks[0]
	if task.ID == "" {
		t.Fatal("fixture task has no id")
	}
	return &nodeLoadRig{m: m, led: led, prd: prd, task: task, sink: sink}
}

// admit takes the worker lease, as the executor does before the load step.
func (r *nodeLoadRig) admit(t *testing.T) {
	t.Helper()
	task := r.task
	if err := r.m.admit(context.Background(), r.prd, &task, "pi", nodeLoadModel, capacity.RoleWorker); err != nil {
		t.Fatalf("admit: %v", err)
	}
}

// load runs the load step the executor runs after admit.
func (r *nodeLoadRig) load(ctx context.Context) error {
	task := r.task
	return r.m.loadWorkerNode(ctx, r.prd, &task, "pi", nodeLoadModel)
}

// waitState returns the task's status and wait reason from the store.
func (r *nodeLoadRig) waitState() (TaskStatus, string) {
	snap, _ := r.m.Store().SnapshotPRD(r.prd.ID)
	tk := snap.Story[0].Tasks[0]
	return tk.Status, tk.WaitReason
}

func TestNodeLoadFailureRaisesAlertAndFailsOver(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	rig := newNodeLoadRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta})
	rig.admit(t)
	if got := rig.led.LeaseOf(rig.task.ID); got == nil || got.Node != "alpha" {
		t.Fatalf("first lease should land on the higher-priority alpha, got %+v", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rig.load(ctx); err != nil {
		t.Fatalf("failover to beta should succeed, got %v", err)
	}
	if got := rig.led.LeaseOf(rig.task.ID); got == nil || got.Node != "beta" {
		t.Fatalf("after the alpha failure the lease must move to beta (excluded alpha), got %+v", got)
	}
	raised := rig.sink.snapshot()
	if len(raised) != 1 {
		t.Fatalf("want one alert for the failed node, got %d", len(raised))
	}
	a := raised[0]
	if a.Level != alerts.LevelError {
		t.Errorf("alert level = %q, want error", a.Level)
	}
	for _, want := range []string{"alpha", nodeLoadModel, "requires more system memory"} {
		if !strings.Contains(a.Title+" "+a.Body, want) {
			t.Errorf("alert %q / %q does not name %q", a.Title, a.Body, want)
		}
	}
	if _, loads := alpha.counts(); loads != 1 {
		t.Errorf("alpha should have been asked to load once, got %d", loads)
	}
}

func TestNodeLoadWaitsForModel(t *testing.T) {
	gate := make(chan struct{})
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Gate: gate})
	rig := newNodeLoadRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha})
	rig.admit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- rig.load(ctx) }()

	want := fmt.Sprintf("loading %s on node alpha", nodeLoadModel)
	pollUntil(t, 5*time.Second, "task waiting on the node load", func() bool {
		status, reason := rig.waitState()
		return status == TaskWaitingCapacity && strings.Contains(reason, want)
	})
	if len(rig.sink.snapshot()) != 0 {
		t.Fatal("a load still in progress must not raise an alert")
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatalf("load should finish once the model shows, got %v", err)
	}
	if status, reason := rig.waitState(); status == TaskWaitingCapacity || reason != "" {
		t.Fatalf("the wait must clear once the load ends, got status=%s reason=%q", status, reason)
	}
	if got := rig.led.LeaseOf(rig.task.ID); got == nil || got.Node != "alpha" {
		t.Fatalf("a slow load keeps the lease on alpha, got %+v", got)
	}
	if psChecks, loads := alpha.counts(); loads != 1 || psChecks < 2 {
		t.Fatalf("want one load request and a waited poll loop, got loads=%d ps=%d", loads, psChecks)
	}
}

func TestSingleNodeEntryHasNoLoadStep(t *testing.T) {
	// The node would refuse a load and alert. A single-node entry must never
	// reach it, so neither a poll nor a load request may land there.
	alpha := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
	rig := newNodeLoadRig(t, []string{"alpha"}, map[string]*fakeOllamaNode{"alpha": alpha})
	rig.admit(t)
	if got := rig.led.LeaseOf(rig.task.ID); got == nil || got.Node != "alpha" {
		t.Fatalf("single-node entry should hold its node lease on alpha, got %+v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rig.load(ctx); err != nil {
		t.Fatalf("single-node entry has no load step, got %v", err)
	}
	if psChecks, loads := alpha.counts(); psChecks != 0 || loads != 0 {
		t.Fatalf("single-node entry must not touch its node, got ps=%d loads=%d", psChecks, loads)
	}
	if n := len(rig.sink.snapshot()); n != 0 {
		t.Fatalf("single-node entry must raise no load alert, got %d", n)
	}
}

func TestNoOtherNodeKeepsFailure(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
	rig := newNodeLoadRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta})
	rig.admit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := rig.load(ctx)
	var nodeErr *inference.NodeLoadError
	if !errors.As(err, &nodeErr) {
		t.Fatalf("with no node left the load failure must be returned, got %v", err)
	}
	if nodeErr.Node != "beta" {
		t.Errorf("the returned failure should be the last node tried (beta), got %q", nodeErr.Node)
	}
	if rig.led.Held(rig.task.ID) {
		t.Error("the lease must be released when no node is left")
	}
	if n := len(rig.sink.snapshot()); n != 2 {
		t.Errorf("each failed node should raise an alert, want 2 got %d", n)
	}
}
