// A daemon restart rebuilds the ledger from the workers that survived. Each
// adopted lease must land on the node recorded for its worker, never on a node
// chosen by list order, and a node that cannot be told is left unknown rather
// than guessed. The fixture is the production shape used by the relaunch
// placement tests: a pi-prompt entry that lists two compute nodes, a resolver
// that names the pools {host, llm:pi-prompt} with no node, and one engine per
// node.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run 'TestAdoptRestart'
package autonomous

import (
	"context"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// adoptRestartRig is a two-task PRD on the two-node fixture. Both tasks look
// like workers whose sessions survived a restart: in progress, with a session.
type adoptRestartRig struct {
	m     *Manager
	led   *capacity.Ledger
	prd   *PRD
	tasks []*Task
}

// newAdoptRestartRig builds the fixture. recorded gives the ComputeNode of each
// task in order; its length is the task count.
func newAdoptRestartRig(t *testing.T, recorded ...string) *adoptRestartRig {
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
	m.SetEntryNodes(func(backend, model string) int {
		if backend != "pi-prompt" {
			return 0
		}
		return 2
	})

	prd, _ := m.CreatePRD("s", "/w/proj", "pi-prompt", "", EffortNormal)
	tasks := make([]Task, len(recorded))
	for i, node := range recorded {
		tasks[i] = Task{Title: "T", Spec: "write docs/x.md", Status: TaskInProgress, SessionID: "sess-" + string(rune('a'+i)), ComputeNode: node}
	}
	_ = m.Store().SetStories(prd.ID, []Story{{Title: "S", Tasks: tasks}})
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDRunning
	_ = m.Store().SavePRD(cur)
	cur, _ = m.Store().GetPRD(cur.ID)
	r := &adoptRestartRig{m: m, led: led, prd: cur}
	for i := range cur.Story[0].Tasks {
		r.tasks = append(r.tasks, &cur.Story[0].Tasks[i])
	}
	return r
}

func (r *adoptRestartRig) requireNodeReads(t *testing.T, node string, held int) {
	t.Helper()
	p, ok := poolOf(r.led.Snapshot(), "node:"+node)
	if !ok {
		t.Fatalf("pool node:%s missing from the snapshot", node)
	}
	if p.Held != held || p.Limit != 1 {
		t.Errorf("node %s reads %d of %d, want %d of 1", node, p.Held, p.Limit, held)
	}
}

func (r *adoptRestartRig) leaseCountFor(taskID string) int {
	n := 0
	for _, l := range r.led.Snapshot().Leases {
		if l.Holder == taskID || l.Holder == VerifyHolder(taskID) {
			n++
		}
	}
	return n
}

// Two workers survive a restart, each on the node recorded for it. They are
// adopted in the opposite order from the resolver's candidate list, so a node
// picked by list position would swap them. Each lease keeps its own node and
// each node reads 1 of 1; a second pass changes nothing.
func TestAdoptRestart_EachWorkerKeepsRecordedNode(t *testing.T) {
	r := newAdoptRestartRig(t, relaunchNodeSecond, relaunchNodeFirst)
	one, two := r.tasks[0], r.tasks[1]

	// The resolver lists relaunchNodeFirst first; adopt the task on the second
	// node first.
	r.m.adoptCapacity(r.prd, one)
	r.m.adoptCapacity(r.prd, two)

	for _, c := range []struct {
		task *Task
		node string
	}{{one, relaunchNodeSecond}, {two, relaunchNodeFirst}} {
		ls := leaseOrFail(t, r.led, c.task.ID)
		if ls.Node != c.node {
			t.Errorf("task %s lease on node %q, want its recorded node %q", c.task.ID, ls.Node, c.node)
		}
		if ls.NodeUnknown {
			t.Errorf("task %s lease flagged node-unknown despite a recorded node", c.task.ID)
		}
	}
	r.requireNodeReads(t, relaunchNodeFirst, 1)
	r.requireNodeReads(t, relaunchNodeSecond, 1)
	if h := holdersOf(t, r.led.Snapshot(), "node:"+relaunchNodeFirst); len(h) != 1 || h[0] != two.ID {
		t.Errorf("node %s holders = %v, want only task %s", relaunchNodeFirst, h, two.ID)
	}
	if h := holdersOf(t, r.led.Snapshot(), "node:"+relaunchNodeSecond); len(h) != 1 || h[0] != one.ID {
		t.Errorf("node %s holders = %v, want only task %s", relaunchNodeSecond, h, one.ID)
	}

	// Adopting again, as the boot pass and the skip-spawn path both may, is a
	// no-op: still one lease per task, on the same node.
	r.m.adoptCapacity(r.prd, one)
	r.m.adoptCapacity(r.prd, two)

	for _, c := range []struct {
		task *Task
		node string
	}{{one, relaunchNodeSecond}, {two, relaunchNodeFirst}} {
		if n := r.leaseCountFor(c.task.ID); n != 1 {
			t.Errorf("task %s holds %d leases after a second adopt, want 1", c.task.ID, n)
		}
		if ls := leaseOrFail(t, r.led, c.task.ID); ls.Node != c.node {
			t.Errorf("task %s moved to node %q after a second adopt, want %q", c.task.ID, ls.Node, c.node)
		}
	}
	r.requireNodeReads(t, relaunchNodeFirst, 1)
	r.requireNodeReads(t, relaunchNodeSecond, 1)
}

// No node is recorded on the task, so the node the session itself reports is
// the one the lease names.
func TestAdoptRestart_SessionReportedNodeWhenNoneRecorded(t *testing.T) {
	r := newAdoptRestartRig(t, "")
	task := r.tasks[0]
	r.m.SetSessionNode(func(sessionID string) string {
		if sessionID == task.SessionID {
			return relaunchNodeSecond
		}
		return ""
	})

	r.m.adoptCapacity(r.prd, task)

	ls := leaseOrFail(t, r.led, task.ID)
	if ls.Node != relaunchNodeSecond {
		t.Fatalf("lease on node %q, want the session's node %q", ls.Node, relaunchNodeSecond)
	}
	if ls.NodeUnknown {
		t.Error("lease flagged node-unknown although the session reported its node")
	}
	r.requireNodeReads(t, relaunchNodeSecond, 1)
	r.requireNodeReads(t, relaunchNodeFirst, 0)
}

// Nothing says where the worker runs and the entry lists two nodes. The lease
// holds no node and is flagged, rather than taking the first candidate.
func TestAdoptRestart_UnknownNodeIsNotTheFirstCandidate(t *testing.T) {
	r := newAdoptRestartRig(t, "")
	task := r.tasks[0]
	r.m.SetSessionNode(func(string) string { return "" })

	r.m.adoptCapacity(r.prd, task)

	ls := leaseOrFail(t, r.led, task.ID)
	if ls.Node != "" {
		t.Fatalf("lease on node %q, want no node", ls.Node)
	}
	if !ls.NodeUnknown {
		t.Error("lease with no node is not flagged node-unknown")
	}
	r.requireNodeReads(t, relaunchNodeFirst, 0)
	r.requireNodeReads(t, relaunchNodeSecond, 0)
}

// A worker adopted on the first node leaves the second as the only free one. A
// new admission lands there; a further admission waits rather than stacking on
// either node.
func TestAdoptRestart_NewRequestAvoidsAdoptedNode(t *testing.T) {
	r := newAdoptRestartRig(t, relaunchNodeFirst, "", "")
	adopted, fresh, extra := r.tasks[0], r.tasks[1], r.tasks[2]
	// Only the first task has a surviving worker; the other two are new.
	for _, nt := range []*Task{fresh, extra} {
		nt.SessionID = ""
		nt.Status = TaskPending
	}
	r.m.adoptCapacity(r.prd, adopted)
	r.requireNodeReads(t, relaunchNodeFirst, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.m.admit(ctx, r.prd, fresh, "pi-prompt", "", capacity.RoleWorker); err != nil {
		t.Fatalf("admit new task: %v", err)
	}
	if got := r.m.heldNode(fresh.ID); got != relaunchNodeSecond {
		t.Fatalf("new task placed on node %q, want %q (the adopted worker holds %q)", got, relaunchNodeSecond, relaunchNodeFirst)
	}
	if got := r.m.heldNode(adopted.ID); got != relaunchNodeFirst {
		t.Errorf("adopted worker moved to node %q, want %q", got, relaunchNodeFirst)
	}

	done := make(chan error, 1)
	go func() {
		done <- r.m.admit(ctx, r.prd, extra, "pi-prompt", "", capacity.RoleWorker)
	}()
	capWaitUntil(t, func() bool {
		cur, ok := r.m.Store().SnapshotPRD(r.prd.ID)
		return ok && cur.Story[0].Tasks[2].Status == TaskWaitingCapacity
	})
	if r.led.Held(extra.ID) {
		t.Error("the third admission holds a lease although both nodes are full")
	}
	r.requireNodeReads(t, relaunchNodeFirst, 1)
	r.requireNodeReads(t, relaunchNodeSecond, 1)

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("waiting admission did not return after cancel")
	}
}
