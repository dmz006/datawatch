// A relaunched task goes through placement like a new request. The fixture is
// shaped like production for a pi-prompt entry that lists two compute nodes:
// the resolver names the pools {host, llm:pi-prompt} with no node, the choices
// list both nodes at the same priority, and each node admits one session.
// Without the release in the relaunch block, Acquire returns at once for a
// holder that is already admitted, so the relaunch keeps its previous node and
// a node can read 2 of 1 while the other sits idle.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run 'TestRelaunchPlacement'
package autonomous

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/pipeline"
)

const (
	relaunchNodeFirst  = "datawatch"
	relaunchNodeSecond = "local-node"
)

// relaunchRig is one relaunch-shaped task on the two-node fixture.
type relaunchRig struct {
	m    *Manager
	led  *capacity.Ledger
	prd  *PRD
	task *Task

	mu         sync.Mutex
	spawnNodes []string
	atSpawn    capacity.Status
}

// newRelaunchRig builds the fixture and presets the task to the relaunch
// shape: verifying, a session of the previous attempt that is no longer alive,
// no completion marker, and prevNode recorded as the node it ran on.
func newRelaunchRig(t *testing.T, prevNode string) *relaunchRig {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: time.Second, Clock: newFakeCapClock()})
	led.SetLimit("host", 4)
	led.SetLimit("llm:pi-prompt", 2)
	led.SetLimit("node:"+relaunchNodeFirst, 1)
	led.SetLimit("node:"+relaunchNodeSecond, 1)
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		if backend != "pi-prompt" {
			return nil, ""
		}
		return []string{"host", "llm:pi-prompt"}, ""
	})
	m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
		if backend != "pi-prompt" {
			return nil
		}
		return []capacity.NodeChoice{{Name: relaunchNodeFirst, Priority: 50}, {Name: relaunchNodeSecond, Priority: 50}}
	})
	m.SetSessionAliveFn(func(string) bool { return false })

	prd, _ := m.CreatePRD("s", "/w/proj", "pi-prompt", "", EffortNormal)
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{
		{Title: "T", Spec: "write docs/x.md", Status: TaskVerifying, SessionID: "old-sess", ComputeNode: prevNode},
	}}})
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	_ = m.Store().SavePRD(cur)
	cur, _ = m.Store().GetPRD(cur.ID)
	return &relaunchRig{m: m, led: led, prd: cur, task: &cur.Story[0].Tasks[0]}
}

// holdOn gives holder a live session on node, the way adoptCapacity records a
// session that survived a restart: the lease names the node and the ledger adds
// the node pool.
func (r *relaunchRig) holdOn(holder, node string) {
	r.led.Adopt(capacity.Request{
		Holder: holder, PRDID: r.prd.ID, Pools: []string{"host", "llm:pi-prompt"}, Node: node,
	}, holder+"-sess")
}

func (r *relaunchRig) spawn(_ context.Context, req SpawnRequest) (SpawnResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spawnNodes = append(r.spawnNodes, req.ComputeNode)
	r.atSpawn = r.led.Snapshot()
	return SpawnResult{SessionID: "new-sess"}, nil
}

func (r *relaunchRig) spawned() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.spawnNodes...)
}

func (r *relaunchRig) ledgerAtSpawn() capacity.Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.atSpawn
}

// start runs executeOne for the task in the background. The returned channel
// carries its result.
func (r *relaunchRig) start(t *testing.T, ctx context.Context) <-chan error {
	t.Helper()
	verify := func(context.Context, *PRD, *Task) (VerificationResult, error) {
		return VerificationResult{OK: true}, nil
	}
	done := make(chan error, 1)
	go func() {
		done <- r.m.executeOne(ctx, r.prd, r.task, r.spawn, verify, 0, pipeline.QualityGateConfig{}, nil)
	}()
	return done
}

func (r *relaunchRig) finish(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeOne: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("executeOne did not return")
	}
}

// waitForWaiting blocks until the task shows TaskWaitingCapacity and returns
// the reason it shows.
func (r *relaunchRig) waitForWaiting(t *testing.T) string {
	t.Helper()
	var reason string
	capWaitUntil(t, func() bool {
		cur, ok := r.m.Store().SnapshotPRD(r.prd.ID)
		if !ok {
			return false
		}
		tk := cur.Story[0].Tasks[0]
		reason = tk.WaitReason
		return tk.Status == TaskWaitingCapacity
	})
	return reason
}

// relaunch runs the relaunch to the spawn. When release names a holder, the
// task is expected to wait first; that holder is released once it does.
func (r *relaunchRig) relaunch(t *testing.T, release string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := r.start(t, ctx)
	if release != "" {
		r.waitForWaiting(t)
		r.led.Release(release)
	}
	r.finish(t, done)
}

func poolOf(st capacity.Status, name string) (capacity.PoolStatus, bool) {
	for _, p := range st.Pools {
		if p.Name == name {
			return p, true
		}
	}
	return capacity.PoolStatus{}, false
}

func holdersOf(t *testing.T, st capacity.Status, pool string) []string {
	t.Helper()
	p, ok := poolOf(st, pool)
	if !ok {
		t.Fatalf("pool %s missing from the snapshot", pool)
	}
	return p.Holders
}

// A live session X holds the previous node and the previous attempt left a
// lease adopted there too, so that node reads 2 of 1. The relaunch moves to the
// free node and the first node is left holding X alone.
func TestRelaunchPlacement_PreviousNodeHeldMovesToOtherNode(t *testing.T) {
	r := newRelaunchRig(t, relaunchNodeFirst)
	r.holdOn("live-x", relaunchNodeFirst)
	r.holdOn(r.task.ID, relaunchNodeFirst)
	if p, _ := poolOf(r.led.Snapshot(), "node:"+relaunchNodeFirst); p.Held != 2 {
		t.Fatalf("fixture: node %s holds %d, want the incident shape of 2", relaunchNodeFirst, p.Held)
	}

	r.relaunch(t, "")

	if got := r.spawned(); len(got) != 1 || got[0] != relaunchNodeSecond {
		t.Fatalf("spawned on %v, want one spawn on %s", got, relaunchNodeSecond)
	}
	st := r.ledgerAtSpawn()
	p, _ := poolOf(st, "node:"+relaunchNodeFirst)
	if p.Held != 1 || p.Limit != 1 {
		t.Fatalf("node %s reads %d of %d, want 1 of 1", relaunchNodeFirst, p.Held, p.Limit)
	}
	if h := holdersOf(t, st, "node:"+relaunchNodeFirst); len(h) != 1 || h[0] != "live-x" {
		t.Fatalf("node %s holders = %v, want live-x only", relaunchNodeFirst, h)
	}
}

// Nothing else runs and the previous node is the one listed second. Load is
// equal, so the previous node wins the tie rather than the first listed.
func TestRelaunchPlacement_FreePreviousNodeStaysThere(t *testing.T) {
	r := newRelaunchRig(t, relaunchNodeSecond)

	r.relaunch(t, "")

	if got := r.spawned(); len(got) != 1 || got[0] != relaunchNodeSecond {
		t.Fatalf("spawned on %v, want one spawn on %s", got, relaunchNodeSecond)
	}
}

// Both nodes are busy, so the relaunch waits like a new request: it shows the
// waiting status with a reason, holds no lease and spawns nothing. When the
// second node frees it lands there, never on the busy first node.
func TestRelaunchPlacement_BothNodesBusyWaitsLikeNewRequest(t *testing.T) {
	r := newRelaunchRig(t, relaunchNodeFirst)
	r.holdOn("live-x", relaunchNodeFirst)
	r.holdOn("live-y", relaunchNodeSecond)
	r.holdOn(r.task.ID, relaunchNodeFirst)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := r.start(t, ctx)

	reason := r.waitForWaiting(t)
	for _, n := range []string{relaunchNodeFirst, relaunchNodeSecond} {
		if !strings.Contains(reason, n) {
			t.Errorf("wait reason %q does not name node %s", reason, n)
		}
	}
	if r.led.Held(r.task.ID) || r.led.Held(VerifyHolder(r.task.ID)) {
		t.Error("the waiting relaunch still holds a lease")
	}
	if got := r.spawned(); len(got) != 0 {
		t.Fatalf("spawned %v while both nodes were busy", got)
	}

	r.led.Release("live-y")
	r.finish(t, done)

	if got := r.spawned(); len(got) != 1 || got[0] != relaunchNodeSecond {
		t.Fatalf("spawned on %v, want one spawn on %s", got, relaunchNodeSecond)
	}
	if h := holdersOf(t, r.ledgerAtSpawn(), "node:"+relaunchNodeFirst); len(h) != 1 || h[0] != "live-x" {
		t.Fatalf("node %s holders = %v, want live-x only", relaunchNodeFirst, h)
	}
}

// The previous attempt's lease was handed to the verifier. It is freed with
// the relaunch, so nothing stays under the verifier's name and no node pool
// goes over its limit.
func TestRelaunchPlacement_VerifierHeldLeaseIsReleased(t *testing.T) {
	r := newRelaunchRig(t, relaunchNodeFirst)
	r.holdOn("live-x", relaunchNodeSecond)
	r.holdOn(VerifyHolder(r.task.ID), relaunchNodeFirst)

	r.relaunch(t, "")

	st := r.ledgerAtSpawn()
	for _, l := range st.Leases {
		if l.Holder == VerifyHolder(r.task.ID) {
			t.Fatalf("lease %s remains after the relaunch: %+v", l.Holder, l)
		}
	}
	for _, n := range []string{relaunchNodeFirst, relaunchNodeSecond} {
		if p, _ := poolOf(st, "node:"+n); p.Held > p.Limit {
			t.Errorf("node %s reads %d of %d", n, p.Held, p.Limit)
		}
	}
	if got := r.spawned(); len(got) != 1 || got[0] != relaunchNodeFirst {
		t.Fatalf("spawned on %v, want one spawn on %s (the free node)", got, relaunchNodeFirst)
	}
}

// After the relaunch is placed, no node pool is over its limit and the task
// holds exactly one lease, in each of the starting states above.
func TestRelaunchPlacement_NoNodeOverLimitAfterRelaunch(t *testing.T) {
	cases := []struct {
		name    string
		prev    string
		setup   func(r *relaunchRig)
		release string
	}{
		{"previous node held twice", relaunchNodeFirst, func(r *relaunchRig) {
			r.holdOn("live-x", relaunchNodeFirst)
			r.holdOn(r.task.ID, relaunchNodeFirst)
		}, ""},
		{"free previous node", relaunchNodeSecond, func(r *relaunchRig) {}, ""},
		{"both nodes busy", relaunchNodeFirst, func(r *relaunchRig) {
			r.holdOn("live-x", relaunchNodeFirst)
			r.holdOn("live-y", relaunchNodeSecond)
			r.holdOn(r.task.ID, relaunchNodeFirst)
		}, "live-y"},
		{"verifier holds previous lease", relaunchNodeFirst, func(r *relaunchRig) {
			r.holdOn("live-x", relaunchNodeSecond)
			r.holdOn(VerifyHolder(r.task.ID), relaunchNodeFirst)
		}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRelaunchRig(t, tc.prev)
			tc.setup(r)

			r.relaunch(t, tc.release)

			if got := r.spawned(); len(got) != 1 {
				t.Fatalf("spawned %v, want exactly one spawn", got)
			}
			st := r.ledgerAtSpawn()
			for _, n := range []string{relaunchNodeFirst, relaunchNodeSecond} {
				if p, _ := poolOf(st, "node:"+n); p.Held > p.Limit {
					t.Errorf("node %s reads %d of %d", n, p.Held, p.Limit)
				}
			}
			own := 0
			for _, l := range st.Leases {
				if l.Holder == r.task.ID || l.Holder == VerifyHolder(r.task.ID) {
					own++
				}
			}
			if own != 1 {
				t.Errorf("task holds %d leases, want exactly one: %+v", own, st.Leases)
			}
		})
	}
}
