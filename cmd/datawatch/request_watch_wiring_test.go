package main

import (
	"context"
	"testing"

	autonomouspkg "github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/compute"
	observerpkg "github.com/dmz006/datawatch/internal/observer"
	"github.com/dmz006/datawatch/internal/requestwatch"
	"github.com/dmz006/datawatch/internal/session"
)

// requestWatchFixture is a lister over in-memory sessions, Automata and nodes.
type requestWatchFixture struct {
	sessions []*session.Session
	prds     []*autonomouspkg.PRD
	nodes    map[string]*compute.Node
}

func (f *requestWatchFixture) lister() *requestWatchLister {
	return &requestWatchLister{
		sessions: func() []*session.Session { return f.sessions },
		prds:     func() []*autonomouspkg.PRD { return f.prds },
		node: func(name string) (*compute.Node, bool) {
			n, ok := f.nodes[name]
			return n, ok
		},
	}
}

func newRequestWatchFixture() *requestWatchFixture {
	return &requestWatchFixture{
		nodes: map[string]*compute.Node{
			"node-a": {Name: "node-a", Kind: compute.KindOllama, Address: "node-a:11434", ObserverPeer: "peer-a"},
		},
		prds: []*autonomouspkg.PRD{{
			ID: "prd1",
			Story: []autonomouspkg.Story{{
				ID: "st1",
				Tasks: []autonomouspkg.Task{
					{ID: "task-run", PRDID: "prd1", Status: autonomouspkg.TaskInProgress, SessionID: "host-w001"},
					{ID: "task-ver", PRDID: "prd1", Status: autonomouspkg.TaskVerifying},
					{ID: "task-done", PRDID: "prd1", Status: autonomouspkg.TaskCompleted, SessionID: "host-d001"},
				},
			}},
		}},
	}
}

func rwSession(fullID, name, node string, state session.State) *session.Session {
	return &session.Session{ID: fullID[len(fullID)-4:], FullID: fullID, Name: name, State: state, ComputeNodeRef: node, Model: "model-x"}
}

func rwTargets(t *testing.T, f *requestWatchFixture) []requestwatch.Target {
	t.Helper()
	return f.lister().Targets(context.Background())
}

func TestRequestWatchWorkerTarget(t *testing.T) {
	f := newRequestWatchFixture()
	f.sessions = []*session.Session{rwSession("host-w001", "worker", "node-a", session.StateRunning)}
	got := rwTargets(t, f)
	if len(got) != 1 {
		t.Fatalf("targets = %+v, want 1", got)
	}
	tg := got[0]
	if tg.SessionID != "host-w001" || tg.Role != requestwatch.RoleWorker || tg.AutomatonID != "prd1" || tg.TaskID != "task-run" {
		t.Fatalf("target = %+v", tg)
	}
	if tg.NodeName != "node-a" || tg.NodeAddress != "node-a:11434" || tg.NodeKind != "ollama" || tg.ObserverPeer != "peer-a" || tg.Model != "model-x" {
		t.Fatalf("node fields = %+v", tg)
	}
}

// The executor marks a task verifying right after its worker starts and keeps
// that status while the worker runs, so the running worker of a verifying task
// is a target; once the worker has ended it is not.
func TestRequestWatchWorkerOfVerifyingTaskIsTarget(t *testing.T) {
	f := newRequestWatchFixture()
	f.prds[0].Story[0].Tasks[1].SessionID = "host-w002"
	f.sessions = []*session.Session{rwSession("host-w002", "autonomous:worker", "node-a", session.StateRunning)}
	got := rwTargets(t, f)
	if len(got) != 1 {
		t.Fatalf("targets = %+v, want the running worker of the verifying task", got)
	}
	if got[0].SessionID != "host-w002" || got[0].Role != requestwatch.RoleWorker || got[0].TaskID != "task-ver" || got[0].AutomatonID != "prd1" {
		t.Fatalf("target = %+v", got[0])
	}
	f.sessions[0].State = session.StateComplete
	if got := rwTargets(t, f); len(got) != 0 {
		t.Fatalf("a worker that has ended must not be a target: %+v", got)
	}
}

func TestRequestWatchWorkerWaitingInputIsTarget(t *testing.T) {
	f := newRequestWatchFixture()
	f.sessions = []*session.Session{rwSession("host-w001", "worker", "node-a", session.StateWaitingInput)}
	if got := rwTargets(t, f); len(got) != 1 {
		t.Fatalf("a session waiting for input is a target: %+v", got)
	}
}

func TestRequestWatchWorkerShortTaskSessionID(t *testing.T) {
	f := newRequestWatchFixture()
	f.prds[0].Story[0].Tasks[0].SessionID = "w001"
	f.sessions = []*session.Session{rwSession("host-w001", "worker", "node-a", session.StateRunning)}
	if got := rwTargets(t, f); len(got) != 1 || got[0].TaskID != "task-run" {
		t.Fatalf("targets = %+v, want the worker matched by its short id", got)
	}
}

func TestRequestWatchVerifierTarget(t *testing.T) {
	f := newRequestWatchFixture()
	f.sessions = []*session.Session{rwSession("host-v001", verifierSessionNamePrefix+":task-ver", "node-a", session.StateRunning)}
	got := rwTargets(t, f)
	if len(got) != 1 {
		t.Fatalf("targets = %+v, want 1", got)
	}
	if got[0].Role != requestwatch.RoleVerifier || got[0].AutomatonID != "prd1" || got[0].TaskID != "task-ver" {
		t.Fatalf("target = %+v", got[0])
	}
}

func TestRequestWatchLocalSessionNotObserved(t *testing.T) {
	f := newRequestWatchFixture()
	f.sessions = []*session.Session{
		// No node reference at all.
		rwSession("host-w001", "worker", "", session.StateRunning),
		// A node reference that is not in the compute registry.
		rwSession("host-v001", verifierSessionNamePrefix+":task-ver", "not-registered", session.StateRunning),
	}
	if got := rwTargets(t, f); len(got) != 0 {
		t.Fatalf("sessions off the compute registry were observed: %+v", got)
	}
}

func TestRequestWatchSessionWithoutTaskNotObserved(t *testing.T) {
	f := newRequestWatchFixture()
	f.sessions = []*session.Session{
		// A session that no Automaton task points at.
		rwSession("host-o001", "interactive", "node-a", session.StateRunning),
		// A verifier named for a task that does not exist.
		rwSession("host-v002", verifierSessionNamePrefix+":no-such-task", "node-a", session.StateRunning),
		// A session of a task that is no longer in progress.
		rwSession("host-d001", "worker", "node-a", session.StateRunning),
	}
	if got := rwTargets(t, f); len(got) != 0 {
		t.Fatalf("sessions with no Automaton task were observed: %+v", got)
	}
}

func TestRequestWatchFinishedSessionNotTarget(t *testing.T) {
	f := newRequestWatchFixture()
	for _, st := range []session.State{session.StateComplete, session.StateFailed, session.StateKilled, session.StateRateLimited} {
		f.sessions = []*session.Session{rwSession("host-w001", "worker", "node-a", st)}
		if got := rwTargets(t, f); len(got) != 0 {
			t.Fatalf("state %q must not be a target: %+v", st, got)
		}
	}
}

func TestRequestWatchListerWithoutManagers(t *testing.T) {
	if got := newRequestWatchLister(nil, nil, nil).Targets(context.Background()); len(got) != 0 {
		t.Fatalf("a lister with no managers has targets: %+v", got)
	}
}

func TestRequestWatchSourceSelector(t *testing.T) {
	sel := requestWatchSources(nil)
	if sel(string(compute.KindOllama)) == nil {
		t.Fatal("the ollama kind must have a source")
	}
	if sel(string(compute.KindOpenAICompat)) != nil {
		t.Fatal("a kind without a source must select none")
	}
}

func TestRequestWatchGPUFromObserverPeer(t *testing.T) {
	snap := &observerpkg.StatsResponse{GPU: []observerpkg.GPU{{UtilPct: 5}, {UtilPct: 72}}}
	g := requestWatchGPU{
		peerPayload: func(peer string) *observerpkg.StatsResponse {
			if peer == "peer-a" {
				return snap
			}
			return nil
		},
	}
	if pct, ok := g.Read(context.Background(), requestwatch.NodeRef{Name: "node-a", ObserverPeer: "peer-a"}); !ok || pct != 72 {
		t.Fatalf("gpu = %v ok=%v, want 72 true", pct, ok)
	}
	// No observer data for the peer.
	if _, ok := g.Read(context.Background(), requestwatch.NodeRef{Name: "node-b", ObserverPeer: "peer-b"}); ok {
		t.Fatal("a node with no observer data must report not ok")
	}
	// With no peer attached the peer is named like the node, as in the node detail route.
	if _, ok := g.Read(context.Background(), requestwatch.NodeRef{Name: "peer-a"}); !ok {
		t.Fatal("a node with no attached peer falls back to the peer named like it")
	}
	// A snapshot without a GPU figure.
	g.peerPayload = func(string) *observerpkg.StatsResponse { return &observerpkg.StatsResponse{} }
	if _, ok := g.Read(context.Background(), requestwatch.NodeRef{Name: "node-a", ObserverPeer: "peer-a"}); ok {
		t.Fatal("a snapshot with no GPU must report not ok")
	}
}

func TestRequestWatchGPULocalAndSwitch(t *testing.T) {
	local := &observerpkg.StatsResponse{GPU: []observerpkg.GPU{{UtilPct: 40}}}
	on := true
	g := requestWatchGPU{
		enabled:     func() bool { return on },
		peerPayload: func(string) *observerpkg.StatsResponse { return nil },
		localName:   "this-host",
		local:       func() *observerpkg.StatsResponse { return local },
	}
	node := requestwatch.NodeRef{Name: "node-a", ObserverPeer: "this-host"}
	if pct, ok := g.Read(context.Background(), node); !ok || pct != 40 {
		t.Fatalf("local gpu = %v ok=%v, want 40 true", pct, ok)
	}
	if _, ok := g.Read(context.Background(), requestwatch.NodeRef{Name: "node-a", ObserverPeer: "other"}); ok {
		t.Fatal("the local observer must answer only for the local peer")
	}
	on = false
	if _, ok := g.Read(context.Background(), node); ok {
		t.Fatal("use_gpu_signal off must report not ok")
	}
}

func TestRequestWatchCallbacksWiring(t *testing.T) {
	if cb := requestWatchCallbacks(nil); cb.OnNoProgress != nil || cb.OnRecovered != nil || cb.OnEnded != nil {
		t.Errorf("a nil manager must give no callbacks: %+v", cb)
	}
	amgr, err := autonomouspkg.NewManager(t.TempDir(), autonomouspkg.DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	cb := requestWatchCallbacks(amgr)
	if cb.OnNoProgress == nil || cb.OnRecovered == nil || cb.OnEnded == nil {
		t.Fatalf("all three callbacks must be wired: %+v", cb)
	}
	// With no Automaton and no question store these are quiet no-ops.
	o := requestwatch.Observation{SessionID: "s", AutomatonID: "a", TaskID: "t"}
	cb.OnNoProgress(o)
	cb.OnRecovered(o)
	cb.OnEnded(o)
}
