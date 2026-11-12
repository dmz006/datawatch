package main

// End-to-end proof for node balancing. Two pi-prompt workers run one LLM
// entry that pools two Ollama nodes through the real autonomous executor
// (Manager.Run): the real capacity ledger admits them, the node loader makes
// the model resident on the leased node, and the pi-prompt backend builds its
// launch command. Only the edges are fakes: tmux captures the command instead
// of typing it, pi is a shell script, and each Ollama node is a loopback
// httptest server. Nothing here starts a real tmux, runs the real pi, or
// reaches past loopback.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
	pibackend "github.com/dmz006/datawatch/internal/llm/backends/pi"
)

const (
	storyBackend  = "pi-prompt"
	storyProvider = "ollama"
	storyModel    = "ollama/qwen3.8:27b"
	storyWait     = 10 * time.Second
)

// fakeTmuxScript stands in for tmux. send-keys records the command it was
// given, in the capture directory named by DW_CAPTURE_DIR, under the session
// name; nothing is typed anywhere.
const fakeTmuxScript = `#!/bin/sh
if [ "$1" = send-keys ]; then
  printf '%s' "$4" > "$DW_CAPTURE_DIR/$3.cmd"
fi
`

// fakePiScript stands in for pi. It reads the provider base URL the launch
// wrote into the per-session agent directory, then makes one request there.
const fakePiScript = `#!/bin/sh
base=$(sed -n 's/.*"baseUrl": *"\([^"]*\)".*/\1/p' "$PI_CODING_AGENT_DIR/models.json" | head -n 1)
if [ -z "$base" ]; then
  echo "fake pi: no baseUrl in $PI_CODING_AGENT_DIR/models.json" >&2
  exit 2
fi
exec curl -fsS -X POST -H 'Content-Type: application/json' \
  -d '{"model":"qwen3.8:27b","messages":[{"role":"user","content":"ping"}]}' \
  "$base/chat/completions"
`

// storyPiModels is the source provider table the per-session agent directory
// is built from. Its baseUrl points at a closed loopback port, so a launch
// that ignored the override would fail instead of reaching a test server.
const storyPiModels = `{"providers":{"ollama":{"api":"openai-completions","apiKey":"ollama","baseUrl":"http://127.0.0.1:9/v1","models":[{"id":"qwen3.8:27b"}]}}}`

// loopbackOllama is a loopback httptest server that answers the calls a node
// load makes (GET /api/ps, POST /api/generate) and the single request the fake
// pi makes (POST /v1/chat/completions). It records each request by method and
// path so a test can count them.
type loopbackOllama struct {
	name     string
	srv      *httptest.Server
	failLoad bool

	mu       sync.Mutex
	resident map[string]bool
	seen     []string
}

func newLoopbackOllama(t *testing.T, name string, failLoad bool) *loopbackOllama {
	t.Helper()
	o := &loopbackOllama{name: name, failLoad: failLoad, resident: map[string]bool{}}
	o.srv = httptest.NewServer(http.HandlerFunc(o.serve))
	t.Cleanup(o.srv.Close)
	return o
}

func (o *loopbackOllama) serve(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	o.seen = append(o.seen, r.Method+" "+r.URL.Path)
	o.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/ps":
		o.mu.Lock()
		models := []map[string]string{}
		for m := range o.resident {
			models = append(models, map[string]string{"name": m, "model": m})
		}
		o.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"models": models})
	case r.Method == http.MethodPost && r.URL.Path == "/api/generate":
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if o.failLoad {
			http.Error(w, "model failed to load", http.StatusInternalServerError)
			return
		}
		o.mu.Lock()
		o.resident[body.Model] = true
		o.mu.Unlock()
		_, _ = fmt.Fprint(w, `{"done":true}`)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"answer from %s"}}]}`, o.name)
	default:
		http.NotFound(w, r)
	}
}

// calls returns the requests the server has seen since the last reset.
func (o *loopbackOllama) calls() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.seen...)
}

func (o *loopbackOllama) reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seen = nil
}

// recordingAlerts is the alert sink a node load raises into.
type recordingAlerts struct {
	mu  sync.Mutex
	got []nodeAlert
}

type nodeAlert struct {
	level alerts.Level
	title string
	body  string
}

func (r *recordingAlerts) Add(level alerts.Level, title, body, _ string) *alerts.Alert {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, nodeAlert{level: level, title: title, body: body})
	return nil
}

func (r *recordingAlerts) snapshot() []nodeAlert {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]nodeAlert(nil), r.got...)
}

type storyNode struct {
	name     string
	priority int
	failLoad bool
}

// nodeStory is one daemon-shaped rig: a compute registry of loopback Ollama
// nodes, one LLM entry over them, the real capacity ledger, and an autonomous
// manager running PRDs through its real executor.
type nodeStory struct {
	t        *testing.T
	root     string
	piBin    string
	captures string
	creg     *compute.Registry
	led      *capacity.Ledger
	mgr      *autonomous.Manager
	servers  map[string]*loopbackOllama
	alerts   *recordingAlerts
	keys     autonomous.CapacityKeysFn
	resolve  func(backend, model string) ([]string, string, []capacity.NodeChoice)
	ctx      context.Context
	cancel   context.CancelFunc

	mu      sync.Mutex
	gates   map[string]chan struct{}
	open    map[string]bool
	spawned map[string]bool
	runs    []chan error
}

// newNodeStory builds the rig. Every node gets a capacity limit of one engine
// on its node pool. llmNodes is the compute_nodes list of the one LLM entry.
func newNodeStory(t *testing.T, nodes []storyNode, llmNodes []string) *nodeStory {
	t.Helper()
	root := t.TempDir()

	// Keep every scratch file the launch writes inside this test's temp dir.
	t.Setenv("TMPDIR", t.TempDir())

	binDir := t.TempDir()
	writeStoryScript(t, filepath.Join(binDir, "tmux"), fakeTmuxScript)
	piBin := filepath.Join(binDir, "pi")
	writeStoryScript(t, piBin, fakePiScript)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	captures := t.TempDir()
	t.Setenv("DW_CAPTURE_DIR", captures)

	piSource := t.TempDir()
	if err := os.WriteFile(filepath.Join(piSource, "models.json"), []byte(storyPiModels), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", piSource)

	creg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	servers := map[string]*loopbackOllama{}
	for _, n := range nodes {
		o := newLoopbackOllama(t, n.name, n.failLoad)
		servers[n.name] = o
		if err := creg.Add(&compute.Node{Name: n.name, Kind: compute.KindOllama, Address: o.srv.URL, SchedulingPriority: n.priority}); err != nil {
			t.Fatalf("add node %s: %v", n.name, err)
		}
	}

	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: storyProvider, Kind: storyProvider, ComputeNodes: llmNodes}); err != nil {
		t.Fatalf("add llm: %v", err)
	}

	led := capacity.New(capacity.Options{})
	for _, n := range nodes {
		led.SetLimit("node:"+n.name, 1)
	}

	regFn := func() *inference.Registry { return reg }
	nodesFn := func() *compute.Registry { return creg }
	resolve := capacityResolver(regFn, nodesFn)
	keys, choices := capacityManagerKeys(resolve)

	sink := &recordingAlerts{}
	mgr, err := autonomous.NewManager(t.TempDir(), autonomous.Config{Enabled: true}, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	mgr.SetCapacity(led, keys)
	mgr.SetCapacityChoices(choices)
	mgr.SetNodeLoad(nodeLoader(nodesFn), sink)

	s := &nodeStory{
		t: t, root: root, piBin: piBin, captures: captures, creg: creg, led: led, mgr: mgr,
		servers: servers, alerts: sink, keys: keys, resolve: resolve,
		gates: map[string]chan struct{}{}, open: map[string]bool{}, spawned: map[string]bool{},
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	t.Cleanup(s.stop)
	return s
}

func writeStoryScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// stop ends every executor run. It is registered before the temp dirs are
// created, so it runs before they are removed.
func (s *nodeStory) stop() {
	s.cancel()
	s.mu.Lock()
	runs := append([]chan error(nil), s.runs...)
	s.mu.Unlock()
	for _, done := range runs {
		select {
		case <-done:
		case <-time.After(storyWait):
			s.t.Errorf("executor run did not stop")
		}
	}
}

// startWorker queues one approved PRD with one task and runs it on the real
// executor in its own goroutine. The task's verify step blocks until the test
// releases it, so the worker keeps its node until then. Returns the task ID.
func (s *nodeStory) startWorker(name string) string {
	s.t.Helper()
	projDir := filepath.Join(s.root, name)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		s.t.Fatal(err)
	}
	st := s.mgr.Store()
	prd, err := st.CreatePRD(name, projDir, storyBackend, storyModel, autonomous.EffortNormal)
	if err != nil {
		s.t.Fatalf("CreatePRD %s: %v", name, err)
	}
	if err := st.SetStories(prd.ID, []autonomous.Story{{
		Title: "story " + name,
		Tasks: []autonomous.Task{{Title: "task " + name, Spec: "work for " + name}},
	}}); err != nil {
		s.t.Fatalf("SetStories %s: %v", name, err)
	}
	prd, ok := st.GetPRD(prd.ID)
	if !ok {
		s.t.Fatalf("GetPRD %s: not found", name)
	}
	prd.Status = autonomous.PRDApproved
	if err := st.SavePRD(prd); err != nil {
		s.t.Fatalf("SavePRD %s: %v", name, err)
	}
	taskID := prd.Story[0].Tasks[0].ID

	gate := make(chan struct{})
	done := make(chan error, 1)
	s.mu.Lock()
	s.gates[taskID] = gate
	s.runs = append(s.runs, done)
	s.mu.Unlock()
	go func() { done <- s.mgr.Run(s.ctx, prd.ID, s.spawn, s.verify) }()
	return taskID
}

// spawn is the worker's spawn step. It builds the pi-prompt backend the way
// the session path does (model, then the leased node's address) and sends its
// launch command through the fake tmux.
func (s *nodeStory) spawn(ctx context.Context, req autonomous.SpawnRequest) (autonomous.SpawnResult, error) {
	if req.Backend != storyBackend {
		return autonomous.SpawnResult{}, fmt.Errorf("task %s spawned on backend %q, want %q", req.TaskID, req.Backend, storyBackend)
	}
	lease := s.led.LeaseOf(req.TaskID)
	if lease == nil || lease.Node == "" {
		return autonomous.SpawnResult{}, fmt.Errorf("task %s spawned without a node lease", req.TaskID)
	}
	node, err := s.creg.Get(lease.Node)
	if err != nil {
		return autonomous.SpawnResult{}, err
	}
	backend := pibackend.NewPrompt(s.piBin)
	if m, ok := backend.(interface{ SetModel(string) }); ok {
		m.SetModel(req.Model)
	}
	if nh, ok := backend.(interface{ SetNodeHost(string) }); ok {
		nh.SetNodeHost(node.Address)
	}
	session := "dw-" + req.TaskID
	if err := backend.Launch(ctx, req.Spec, session, req.ProjectDir, ""); err != nil {
		return autonomous.SpawnResult{}, err
	}
	s.mu.Lock()
	s.spawned[req.TaskID] = true
	s.mu.Unlock()
	return autonomous.SpawnResult{SessionID: session}, nil
}

// verify holds the worker on its node until the test releases the task.
func (s *nodeStory) verify(ctx context.Context, _ *autonomous.PRD, task *autonomous.Task) (autonomous.VerificationResult, error) {
	s.mu.Lock()
	gate := s.gates[task.ID]
	s.mu.Unlock()
	select {
	case <-gate:
	case <-ctx.Done():
		return autonomous.VerificationResult{}, ctx.Err()
	}
	return autonomous.VerificationResult{OK: true, Summary: "ok"}, nil
}

func (s *nodeStory) release(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.open[taskID] {
		close(s.gates[taskID])
		s.open[taskID] = true
	}
}

func (s *nodeStory) waitFor(what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(storyWait)
	for !cond() {
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (s *nodeStory) waitLease(taskID string) *capacity.Lease {
	s.t.Helper()
	s.waitFor("lease for "+taskID, func() bool { return s.led.LeaseOf(taskID) != nil })
	return s.led.LeaseOf(taskID)
}

func (s *nodeStory) waitSpawned(taskID string) {
	s.t.Helper()
	s.waitFor("spawn of "+taskID, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.spawned[taskID]
	})
}

func (s *nodeStory) captureFile(taskID string) string {
	return filepath.Join(s.captures, "dw-"+taskID+".cmd")
}

// launchCommand returns the command the fake tmux captured for a task, once
// its spawn has completed.
func (s *nodeStory) launchCommand(taskID string) string {
	s.t.Helper()
	s.waitSpawned(taskID)
	raw, err := os.ReadFile(s.captureFile(taskID))
	if err != nil {
		s.t.Fatalf("read captured launch for %s: %v", taskID, err)
	}
	return string(raw)
}

// runLaunch runs a captured launch command the way the shell would, with the
// tmux environment removed.
func runLaunch(t *testing.T, cmd string) string {
	t.Helper()
	out, err := exec.Command("env", "-u", "TMUX", "-u", "TMUX_TMPDIR", "bash", "-c", cmd).CombinedOutput()
	if err != nil {
		t.Fatalf("launch command failed: %v\n%s", err, out)
	}
	return string(out)
}

func poolNamed(st capacity.Status, name string) (capacity.PoolStatus, bool) {
	for _, p := range st.Pools {
		if p.Name == name {
			return p, true
		}
	}
	return capacity.PoolStatus{}, false
}

func waiterOf(st capacity.Status, holder string) *capacity.Waiter {
	for i := range st.Waiting {
		if st.Waiting[i].Holder == holder {
			return &st.Waiting[i]
		}
	}
	return nil
}

// leaseNodes maps each holder in the snapshot to the node its lease names.
func leaseNodes(st capacity.Status) map[string]string {
	out := map[string]string{}
	for _, l := range st.Leases {
		out[l.Holder] = l.Node
	}
	return out
}

func storyNodes(failAlpha bool) []storyNode {
	return []storyNode{
		{name: "alpha", priority: 80, failLoad: failAlpha},
		{name: "beta", priority: 20},
	}
}

var storyBothNodes = []string{"alpha", "beta"}

func TestTwoPiWorkersHoldDifferentNodes(t *testing.T) {
	s := newNodeStory(t, storyNodes(false), storyBothNodes)

	first := s.startWorker("first")
	if n := s.waitLease(first).Node; n != "alpha" {
		t.Fatalf("first worker holds %q, want alpha (the higher-priority node)", n)
	}
	second := s.startWorker("second")
	if n := s.waitLease(second).Node; n != "beta" {
		t.Fatalf("second worker holds %q, want beta (alpha is taken)", n)
	}

	st := s.led.Snapshot()
	for _, node := range []string{"alpha", "beta"} {
		p, ok := poolNamed(st, "node:"+node)
		if !ok || p.Limit != 1 || p.Held != 1 {
			t.Fatalf("pool node:%s = %+v (found %v), want limit 1 held 1", node, p, ok)
		}
	}
	want := map[string]string{first: "alpha", second: "beta"}
	if got := leaseNodes(st); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot leases = %v, want each worker beside its node %v", got, want)
	}

	third := s.startWorker("third")
	s.waitFor("third worker to wait for a node", func() bool {
		w := waiterOf(s.led.Snapshot(), third)
		return w != nil && w.Reason != ""
	})
	w := waiterOf(s.led.Snapshot(), third)
	for _, node := range []string{"alpha", "beta"} {
		if !strings.Contains(w.Reason, node) {
			t.Fatalf("wait reason %q does not name node %s", w.Reason, node)
		}
	}
	if !reflect.DeepEqual(w.Nodes, []string{"alpha", "beta"}) {
		t.Fatalf("waiter nodes = %v, want [alpha beta]", w.Nodes)
	}
	if _, err := os.Stat(s.captureFile(third)); !os.IsNotExist(err) {
		t.Fatalf("waiting worker was spawned before a node freed (stat err %v)", err)
	}

	s.release(first)
	s.waitFor("first worker to release its node", func() bool { return s.led.LeaseOf(first) == nil })
	if n := s.waitLease(third).Node; n != "alpha" {
		t.Fatalf("third worker took %q, want alpha, the node the first worker released", n)
	}
	if n := s.led.LeaseOf(second).Node; n != "beta" {
		t.Fatalf("second worker moved to %q while it still runs, want beta", n)
	}
	st = s.led.Snapshot()
	want = map[string]string{second: "beta", third: "alpha"}
	if got := leaseNodes(st); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot leases after release = %v, want %v", got, want)
	}
}

func TestTwoPiWorkersConnectToDifferentHosts(t *testing.T) {
	s := newNodeStory(t, storyNodes(false), storyBothNodes)

	first := s.startWorker("first")
	firstNode := s.waitLease(first).Node
	second := s.startWorker("second")
	secondNode := s.waitLease(second).Node
	if firstNode == secondNode {
		t.Fatalf("both workers hold %q", firstNode)
	}

	firstCmd := s.launchCommand(first)
	secondCmd := s.launchCommand(second)

	// Only the launch commands' own requests should count.
	for _, o := range s.servers {
		o.reset()
	}
	firstOut := runLaunch(t, firstCmd)
	secondOut := runLaunch(t, secondCmd)

	for _, c := range []struct{ node, out string }{{firstNode, firstOut}, {secondNode, secondOut}} {
		if !strings.Contains(c.out, "DATAWATCH_COMPLETE") {
			t.Fatalf("launch on %s did not complete:\n%s", c.node, c.out)
		}
		if want := "answer from " + c.node; !strings.Contains(c.out, want) {
			t.Fatalf("launch on %s did not reach that host (want %q):\n%s", c.node, want, c.out)
		}
	}
	for name, o := range s.servers {
		want := []string{"POST /v1/chat/completions"}
		if got := o.calls(); !reflect.DeepEqual(got, want) {
			t.Fatalf("server %s received %v from the fake pi, want exactly one request %v", name, got, want)
		}
	}

	s.release(first)
	s.release(second)
}

func TestOneNodeEntryKeepsTodayPoolsAndHost(t *testing.T) {
	s := newNodeStory(t, []storyNode{{name: "alpha", priority: 70}}, []string{"alpha"})

	pools, node, choices := s.resolve(storyBackend, storyModel)
	if want := []string{"host", "node:alpha"}; !reflect.DeepEqual(pools, want) || node != "alpha" || choices != nil {
		t.Fatalf("one-node resolve = %v %q %v, want %v alpha nil", pools, node, choices, want)
	}
	if kp, kn := s.keys(storyBackend, storyModel); !reflect.DeepEqual(kp, pools) || kn != node {
		t.Fatalf("keys = %v %q, want the resolver's pools and node", kp, kn)
	}

	// A single-node worker takes that node's lease and makes no load call.
	id := s.startWorker("solo")
	lease := s.waitLease(id)
	if lease.Node != "alpha" || !reflect.DeepEqual(lease.Pools, pools) {
		t.Fatalf("lease = node %q pools %v, want alpha %v", lease.Node, lease.Pools, pools)
	}
	s.waitSpawned(id)
	if got := s.servers["alpha"].calls(); len(got) != 0 {
		t.Fatalf("single-node worker made node calls %v, want none", got)
	}
	s.release(id)
}

func TestNodeLoadFailureRaisesAlertAndMovesWorkerToOtherNode(t *testing.T) {
	s := newNodeStory(t, storyNodes(true), storyBothNodes)

	id := s.startWorker("first")
	s.waitFor("worker to move to beta after alpha fails to load", func() bool {
		l := s.led.LeaseOf(id)
		return l != nil && l.Node == "beta"
	})

	got := s.alerts.snapshot()
	if len(got) != 1 {
		t.Fatalf("alerts = %+v, want exactly one node load alert", got)
	}
	if got[0].level != alerts.LevelError || !strings.Contains(got[0].title, "alpha") || !strings.Contains(got[0].title, storyModel) {
		t.Fatalf("alert = %+v, want an error naming alpha and %s", got[0], storyModel)
	}
	if got := s.servers["alpha"].calls(); !slices.Contains(got, "GET /api/ps") || !slices.Contains(got, "POST /api/generate") {
		t.Fatalf("alpha received %v, want the residency check and the load attempt", got)
	}

	cmd := s.launchCommand(id)
	for _, o := range s.servers {
		o.reset()
	}
	out := runLaunch(t, cmd)
	if !strings.Contains(out, "DATAWATCH_COMPLETE") || !strings.Contains(out, "answer from beta") {
		t.Fatalf("launch did not complete on beta:\n%s", out)
	}
	if got := s.servers["alpha"].calls(); len(got) != 0 {
		t.Fatalf("failed node alpha received fake pi requests %v, want none", got)
	}
	if got, want := s.servers["beta"].calls(), []string{"POST /v1/chat/completions"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("beta received %v, want exactly one request %v", got, want)
	}

	s.release(id)
}
