// Adoption node order: the node recorded on the task, then the node the
// session reports, then the resolver's node only for a single-node entry.
//
// Run with: env -u TMUX -u TMUX_TMPDIR go test ./internal/autonomous -run 'TestAdoptCapacity|TestTaskComputeNode'
package autonomous

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

// adoptNodeRig is a manager whose resolver names a default node "first" with
// its node pool, the way it does when an entry's serving filter collapses to
// one candidate, plus a task holding session "live-sess".
func adoptNodeRig(t *testing.T) (*Manager, *capacity.Ledger, *PRD, *Task) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for _, n := range []string{"first", "second", "third"} {
		led.SetLimit("node:"+n, 1)
	}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"host", "llm:pool", "node:first"}, "first"
	})
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := &cur.Story[0].Tasks[0]
	task.SessionID = "live-sess"
	return m, led, cur, task
}

func leaseOrFail(t *testing.T, led *capacity.Ledger, holder string) *capacity.Lease {
	t.Helper()
	ls := led.LeaseOf(holder)
	if ls == nil {
		t.Fatalf("no lease for %s", holder)
	}
	return ls
}

func TestAdoptCapacityPrefersRecordedTaskNode(t *testing.T) {
	m, led, prd, task := adoptNodeRig(t)
	m.SetSessionNode(func(string) string { return "second" })
	task.ComputeNode = "third"

	m.adoptCapacity(prd, task)

	ls := leaseOrFail(t, led, task.ID)
	if ls.Node != "third" {
		t.Fatalf("lease node = %q, want the recorded node third (not the session's second or the resolver's first)", ls.Node)
	}
	if want := []string{"host", "llm:pool", "node:third"}; !reflect.DeepEqual(ls.Pools, want) {
		t.Fatalf("lease pools = %v, want %v", ls.Pools, want)
	}
}

func TestAdoptCapacityMultiNodeEntryNeverDefaultsToFirstNode(t *testing.T) {
	m, led, prd, task := adoptNodeRig(t)
	m.SetSessionNode(func(string) string { return "" })
	// The entry lists two nodes; the serving filter collapsed the candidates to
	// "first", which the resolver returns as its default.
	m.SetEntryNodes(func(string, string) int { return 2 })

	m.adoptCapacity(prd, task)

	ls := leaseOrFail(t, led, task.ID)
	if ls.Node != "" {
		t.Fatalf("lease node = %q, want none for a multi-node entry with no record", ls.Node)
	}
	if want := []string{"host", "llm:pool"}; !reflect.DeepEqual(ls.Pools, want) {
		t.Fatalf("lease pools = %v, want %v (no node pool)", ls.Pools, want)
	}
	for _, p := range led.Snapshot().Pools {
		if p.Name == "node:first" && p.Held != 0 {
			t.Fatalf("pool node:first = %+v, want idle", p)
		}
	}
}

func TestAdoptCapacitySessionNodeBeatsResolverDefault(t *testing.T) {
	m, led, prd, task := adoptNodeRig(t)
	m.SetSessionNode(func(id string) string {
		if id == "live-sess" {
			return "second"
		}
		return ""
	})
	m.SetEntryNodes(func(string, string) int { return 2 })

	m.adoptCapacity(prd, task)

	ls := leaseOrFail(t, led, task.ID)
	if ls.Node != "second" {
		t.Fatalf("lease node = %q, want second", ls.Node)
	}
	// The resolver's default node pool must not be held beside the real one.
	if want := []string{"host", "llm:pool", "node:second"}; !reflect.DeepEqual(ls.Pools, want) {
		t.Fatalf("lease pools = %v, want %v", ls.Pools, want)
	}
	for _, p := range led.Snapshot().Pools {
		if p.Name == "node:first" && p.Held != 0 {
			t.Fatalf("pool node:first = %+v, want idle", p)
		}
	}
}

func TestAdoptCapacitySingleNodeEntryUsesResolverNode(t *testing.T) {
	for name, count := range map[string]EntryNodesFn{
		"entry lists one node":   func(string, string) int { return 1 },
		"entry count not wired":  nil,
		"entry runs on no nodes": func(string, string) int { return 0 },
	} {
		t.Run(name, func(t *testing.T) {
			m, led, prd, task := adoptNodeRig(t)
			m.SetEntryNodes(count)
			// No session resolver wired: only the resolver can name a node.
			m.adoptCapacity(prd, task)

			ls := leaseOrFail(t, led, task.ID)
			wantNode := "first"
			if name == "entry runs on no nodes" {
				wantNode = ""
			}
			if ls.Node != wantNode {
				t.Fatalf("lease node = %q, want %q", ls.Node, wantNode)
			}
		})
	}
}

func TestAdoptCapacityRecordedNodeNeedsNoSession(t *testing.T) {
	m, led, prd, task := adoptNodeRig(t)
	task.SessionID = ""
	task.ComputeNode = "second"
	m.SetEntryNodes(func(string, string) int { return 2 })

	m.adoptCapacity(prd, task)

	if ls := leaseOrFail(t, led, task.ID); ls.Node != "second" {
		t.Fatalf("lease node = %q, want second", ls.Node)
	}
}

func TestAdoptCapacityKeepsHostedPoolsUntouched(t *testing.T) {
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"host", "llm:claude"}, "" })
	m.SetEntryNodes(func(string, string) int { return 0 })
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := &cur.Story[0].Tasks[0]
	task.SessionID = "live-sess"

	m.adoptCapacity(cur, task)

	ls := leaseOrFail(t, led, task.ID)
	if ls.Node != "" || !reflect.DeepEqual(ls.Pools, []string{"host", "llm:claude"}) {
		t.Fatalf("hosted lease = %+v, want pools host and llm:claude and no node", ls)
	}
}

func TestPoolsWithoutNodes(t *testing.T) {
	got := poolsWithoutNodes([]string{"host", "node:a", "llm:x", "node:b"})
	if want := []string{"host", "llm:x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("poolsWithoutNodes = %v, want %v", got, want)
	}
	if got := poolsWithoutNodes(nil); len(got) != 0 {
		t.Fatalf("poolsWithoutNodes(nil) = %v, want empty", got)
	}
}

// The spawn records the node the start handler placed the worker on, not the
// node the lease advised, and falls back to the lease's node when the spawn
// path reports none.
func TestTaskRecordsNodeTheWorkerRunsOn(t *testing.T) {
	cases := []struct {
		name     string
		reported string
		want     string
	}{
		{"handler re-placed the session", "slow", "slow"},
		{"spawn path reports no node", "", "fast"},
		{"handler kept the leased node", "fast", "fast"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := poolFixture(t, true)
			prd := newPRDWithTasks(t, m, 1)
			cur, _ := m.Store().GetPRD(prd.ID)
			cur.Status = PRDApproved
			_ = m.Store().SavePRD(cur)

			spawn := func(_ context.Context, req SpawnRequest) (SpawnResult, error) {
				if req.ComputeNode != "fast" {
					t.Errorf("spawn ComputeNode = %q, want the leased fast", req.ComputeNode)
				}
				return SpawnResult{SessionID: "s-" + req.TaskID, ComputeNode: tc.reported}, nil
			}
			verify := func(_ context.Context, _ *PRD, _ *Task) (VerificationResult, error) {
				return VerificationResult{OK: true, Summary: "ok"}, nil
			}
			if err := m.Run(context.Background(), prd.ID, spawn, verify); err != nil {
				t.Fatalf("Run: %v", err)
			}
			got, _ := m.Store().GetPRD(prd.ID)
			if n := got.Story[0].Tasks[0].ComputeNode; n != tc.want {
				t.Fatalf("task ComputeNode = %q, want %q", n, tc.want)
			}
		})
	}
}

// Task.ComputeNode is saved with the PRD store and read back after a reload.
func TestTaskComputeNodeSurvivesStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	prd, err := st.CreatePRD("spec", t.TempDir(), "pi-prompt", "ollama/qwen3.8:27b", EffortNormal)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStories(prd.ID, []Story{{Title: "S", Tasks: []Task{{Title: "T", Spec: "x"}}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetPRD(prd.ID)
	task := &got.Story[0].Tasks[0]
	task.ComputeNode = "local-node"
	if err := st.SaveTask(task); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, ok := reloaded.GetPRD(prd.ID)
	if !ok {
		t.Fatal("PRD missing after reload")
	}
	if n := again.Story[0].Tasks[0].ComputeNode; n != "local-node" {
		t.Fatalf("task ComputeNode after reload = %q, want local-node", n)
	}
}

// A session adopted without a link is a different worker from the one the task
// last recorded, so the recorded node is dropped before the lease is taken.
func TestAdoptUnlinkedSessionDropsStaleRecordedNode(t *testing.T) {
	m, prd, dir := adoptReconcileSetup(t, "sess-dead")
	task := &prd.Story[0].Tasks[0]
	task.ComputeNode = "stale-node"
	_ = m.Store().SaveTask(task)
	prd, _ = m.Store().GetPRD(prd.ID)
	task = &prd.Story[0].Tasks[0]

	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"host"}, "" })
	m.SetSessionNode(func(id string) string {
		if id == "host-fresh" {
			return "real-node"
		}
		return ""
	})
	sessions := []SessionInfo{{ID: "host-fresh", TaskID: task.ID, PRDID: prd.ID, ProjectDir: dir, CreatedAt: time.Now(), Alive: true}}

	if id, _ := m.adoptUnlinkedSession(prd, task, func() []SessionInfo { return sessions }); id != "host-fresh" {
		t.Fatalf("adopted %q, want host-fresh", id)
	}
	if task.ComputeNode != "" {
		t.Fatalf("task ComputeNode = %q, want it cleared for the new session", task.ComputeNode)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "real-node" {
		t.Fatalf("lease = %+v, want node real-node from the session", ls)
	}
}
