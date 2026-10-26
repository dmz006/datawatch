package main

// End-to-end proof for node placement across a daemon restart. Two worker
// sessions that both live on node alpha survive the restart; the boot-time
// resume pass of a fresh manager and ledger adopts them. Only the edges are
// fakes: the session lookup answers from a map, and the nodes are compute
// registry entries with no server behind them.

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	"github.com/dmz006/datawatch/internal/session"
)

const restartModel = "ollama/qwen3.8:27b"

// restartRig is a daemon-shaped rig built after a restart: a fresh ledger with
// one engine per node and a fresh manager wired the way wireCapacity wires it.
type restartRig struct {
	t       *testing.T
	led     *capacity.Ledger
	mgr     *autonomous.Manager
	api     *autonomous.API
	reg     *inference.Registry
	resolve func(backend, model string) ([]string, string, []capacity.NodeChoice)
}

func newRestartRig(t *testing.T, sessions map[string]*session.Session) *restartRig {
	t.Helper()
	reg, creg := nodeTestRegistries(t,
		[]*inference.LLM{{Name: storyProvider, Kind: storyProvider, ComputeNodes: []string{"alpha", "beta"}}},
		map[string]int{"alpha": 50, "beta": 50})
	led := capacity.New(capacity.Options{PollInterval: time.Hour})
	led.SetLimit("node:alpha", 1)
	led.SetLimit("node:beta", 1)

	regFn := func() *inference.Registry { return reg }
	nodesFn := func() *compute.Registry { return creg }
	resolve := capacityResolver(regFn, nodesFn)
	keys, choices := capacityManagerKeys(resolve)

	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.Config{Enabled: true, ResumeMaxAgeSeconds: -1}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetCapacity(led, keys)
	mgr.SetCapacityChoices(choices)
	mgr.SetSessionNode(sessionComputeNode(func(id string) (*session.Session, bool) { s, ok := sessions[id]; return s, ok }, nodesFn))
	mgr.SetSessionAliveFn(func(id string) bool { _, ok := sessions[id]; return ok })
	reg.SetChangeFn(replanOnPlacementChange(led, regFn, nodesFn))

	api := autonomous.NewAPI(mgr)
	t.Cleanup(func() {
		if err := api.WaitIdle(storyWait); err != nil {
			t.Errorf("executors did not stop: %v", err)
		}
		mgr.Stop()
	})
	return &restartRig{t: t, led: led, mgr: mgr, api: api, reg: reg, resolve: resolve}
}

// runningTask saves a running PRD whose one task is in progress on sessionID,
// the state a daemon restart leaves behind, and returns the task ID.
func (r *restartRig) runningTask(name, sessionID string) string {
	r.t.Helper()
	dir := filepath.Join(r.t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.t.Fatal(err)
	}
	st := r.mgr.Store()
	prd, err := st.CreatePRD(name, dir, storyBackend, restartModel, autonomous.EffortNormal)
	if err != nil {
		r.t.Fatalf("CreatePRD %s: %v", name, err)
	}
	if err := st.SetStories(prd.ID, []autonomous.Story{{
		Title: "story " + name,
		Tasks: []autonomous.Task{{Title: "task " + name, Spec: "work for " + name}},
	}}); err != nil {
		r.t.Fatalf("SetStories %s: %v", name, err)
	}
	prd, _ = st.GetPRD(prd.ID)
	prd.Status = autonomous.PRDRunning
	task := &prd.Story[0].Tasks[0]
	task.Status = autonomous.TaskInProgress
	task.SessionID = sessionID
	task.UpdatedAt = time.Now()
	if err := st.SavePRD(prd); err != nil {
		r.t.Fatalf("SavePRD %s: %v", name, err)
	}
	return task.ID
}

func (r *restartRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(storyWait)
	for !cond() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// request builds a blocking admission the way wireCapacity does. node is the
// node a recorded placement names, empty for none.
func (r *restartRig) request(holder, node string) capacity.Request {
	pools, resolved, choices := r.resolve(storyProvider, restartModel)
	if node == "" {
		node = resolved
	}
	req := capacity.Request{Holder: holder, PRDID: "prd-" + holder, Pools: pools, Node: node, Model: restartModel}
	req = withNodeChoices(req, choices)
	req.Refresh = resolveRefresh(r.resolve, req, storyProvider, restartModel)
	return req
}

func (r *restartRig) admit(req capacity.Request) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- r.led.Acquire(context.Background(), req, 0, func() bool { return false }, func(string) {})
	}()
	return done
}

func (r *restartRig) waitQueued(holder string) {
	r.t.Helper()
	r.waitFor(holder+" to queue", func() bool { return waiterOf(r.led.Snapshot(), holder) != nil })
}

func (r *restartRig) expectGranted(done <-chan error, holder, node string) {
	r.t.Helper()
	select {
	case err := <-done:
		if err != nil {
			r.t.Fatalf("%s: Acquire: %v", holder, err)
		}
	case <-time.After(storyWait):
		r.t.Fatalf("%s still waiting, want it granted on %s", holder, node)
	}
	if ls := r.led.LeaseOf(holder); ls == nil || ls.Node != node {
		r.t.Fatalf("%s lease = %+v, want node %s", holder, ls, node)
	}
}

func TestRestartAdoptsSessionsOntoTheirNodeAndPlacesTheRestElsewhere(t *testing.T) {
	sessions := map[string]*session.Session{
		"sess-one": {ComputeNodeRef: "alpha"},
		"sess-two": {ComputeNodeRef: "alpha"},
	}
	r := newRestartRig(t, sessions)
	one := r.runningTask("one", "sess-one")
	two := r.runningTask("two", "sess-two")

	// The boot-time resume pass; nothing may be spawned, both sessions live.
	r.api.SetExecutors(func(_ context.Context, req autonomous.SpawnRequest) (autonomous.SpawnResult, error) {
		t.Errorf("task %s was spawned, want its surviving session adopted", req.TaskID)
		return autonomous.SpawnResult{}, context.Canceled
	}, func(ctx context.Context, _ *autonomous.PRD, _ *autonomous.Task) (autonomous.VerificationResult, error) {
		<-ctx.Done()
		return autonomous.VerificationResult{}, ctx.Err()
	})
	r.waitFor("both sessions to be adopted", func() bool { return r.led.LeaseOf(one) != nil && r.led.LeaseOf(two) != nil })

	st := r.led.Snapshot()
	want := map[string]string{one: "alpha", two: "alpha"}
	if got := leaseNodes(st); !reflect.DeepEqual(got, want) {
		t.Fatalf("adopted leases = %v, want both on alpha %v", got, want)
	}
	if p, ok := poolNamed(st, "node:alpha"); !ok || p.Held != 2 {
		t.Fatalf("pool node:alpha = %+v (found %v), want 2 engines in use", p, ok)
	}
	if p, ok := poolNamed(st, "node:beta"); ok && p.Held != 0 {
		t.Fatalf("pool node:beta = %+v, want idle", p)
	}

	// A third request cannot use alpha and is granted on beta.
	r.expectGranted(r.admit(r.request("third", "")), "third", "beta")

	// A fourth request, last placed on alpha, waits: alpha is over its limit
	// and beta holds the third.
	fourth := r.admit(r.request("fourth", "alpha"))
	r.waitQueued("fourth")

	// Removing alpha from the list clears the stale node on the waiter, and
	// beta freeing is what places it. Nothing is rejected on the way.
	llm, err := r.reg.Get(storyProvider)
	if err != nil {
		t.Fatal(err)
	}
	llm.ComputeNodes = []string{"beta"}
	if err := r.reg.Update(llm); err != nil {
		t.Fatalf("removing alpha from compute_nodes: %v", err)
	}
	select {
	case err := <-fourth:
		t.Fatalf("fourth was granted (err %v) while beta was still held", err)
	case <-time.After(50 * time.Millisecond):
	}
	if w := waiterOf(r.led.Snapshot(), "fourth"); w == nil || slices.Contains(w.Nodes, "alpha") {
		t.Fatalf("fourth waiter = %+v, want it queued without alpha", w)
	}
	r.led.Release("third")
	r.expectGranted(fourth, "fourth", "beta")
}
