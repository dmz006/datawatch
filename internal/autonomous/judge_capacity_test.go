package autonomous

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/capacity"
)

// judgeAlerts records each node-load alert raised.
type judgeAlerts struct{ nodes []string }

func (a *judgeAlerts) Add(_ alerts.Level, _, _, _ string) *alerts.Alert {
	a.nodes = append(a.nodes, "")
	return nil
}

// recordingLoader makes a fake loader that records the nodes it was asked to
// load on, and fails the nodes in failOn.
func recordingLoader(failOn ...string) (NodeLoader, *[]string) {
	var seen []string
	loader := func(_ context.Context, node, _ string, _ func(string)) error {
		seen = append(seen, node)
		if slices.Contains(failOn, node) {
			return errors.New("load refused")
		}
		return nil
	}
	return loader, &seen
}

// TestVerifierHoldsNodeLease: a verifier whose entry pools two nodes takes a
// lease on one of them, hands that node back, and loads its model there.
func TestVerifierHoldsNodeLease(t *testing.T) {
	m, led := poolFixture(t, true)
	loader, seen := recordingLoader()
	m.SetNodeLoad(loader, &judgeAlerts{})

	node, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder("task-v"), PRDID: "prd-v", Backend: "pi", Model: "ollama/q",
		Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	if node != "fast" {
		t.Fatalf("judge node = %q, want fast (highest priority)", node)
	}
	ls := led.LeaseOf(VerifyHolder("task-v"))
	if ls == nil || ls.Node != "fast" || !slices.Contains(ls.Pools, "node:fast") {
		t.Fatalf("judge lease = %+v, want node fast with node:fast pool", ls)
	}
	if !slices.Equal(*seen, []string{"fast"}) {
		t.Fatalf("model loaded on %v, want [fast]", *seen)
	}
	release()
	if led.Held(VerifyHolder("task-v")) {
		t.Fatal("judge lease leaked after release")
	}
}

// TestVerifyHandoffKeepsNode: a worker holding node X hands its lease to the
// verifier with node X kept, and no release-and-re-wait happens.
func TestVerifyHandoffKeepsNode(t *testing.T) {
	m, led := poolFixture(t, true)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	if err := m.admit(context.Background(), cur, &task, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "fast" {
		t.Fatalf("worker lease = %+v, want node fast", ls)
	}

	node, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder(task.ID), WorkerHolder: task.ID, PRDID: prd.ID,
		Backend: "pi", Model: "ollama/q", Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "fast" {
		t.Fatalf("judge node = %q, want the worker's node fast", node)
	}
	if led.LeaseOf(task.ID) != nil {
		t.Fatal("worker lease still held after hand-off")
	}
	ls := led.LeaseOf(VerifyHolder(task.ID))
	if ls == nil || ls.Node != "fast" {
		t.Fatalf("verifier lease = %+v, want node fast kept", ls)
	}
}

// TestVerifyHandoffRefusesNodeOutsideCandidates: the worker's node is not a
// candidate of the judge's entry, so no hand-off happens and the judge takes
// its own lease.
func TestVerifyHandoffRefusesNodeOutsideCandidates(t *testing.T) {
	m, led := poolFixture(t, true)
	if err := led.Acquire(context.Background(), capacity.Request{
		Holder: "task-x", Pools: []string{"host", "llm:pool", "node:gone"}, Node: "gone",
	}, 0, nil, nil); err != nil {
		t.Fatal(err)
	}
	if m.HandoffVerifyLease("task-x", "pi", "ollama/q") {
		t.Fatal("hand-off accepted a node outside the judge's candidates")
	}
	if led.LeaseOf("task-x") == nil {
		t.Fatal("refused hand-off released the worker lease")
	}
}

// TestJudgeSingleNodeUnchanged: a judge on a one-node entry gets no node back,
// loads nothing, and holds the same lease it always held. With NodeOnly it
// takes no lease at all.
func TestJudgeSingleNodeUnchanged(t *testing.T) {
	m, led := poolFixture(t, false)
	loader, seen := recordingLoader()
	m.SetNodeLoad(loader, &judgeAlerts{})

	node, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder("task-s"), Backend: "pi", Model: "ollama/q", Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	if node != "" {
		t.Fatalf("single-node judge node = %q, want empty", node)
	}
	if ls := led.LeaseOf(VerifyHolder("task-s")); ls == nil || ls.Node != "solo" {
		t.Fatalf("single-node judge lease = %+v, want node solo", ls)
	}
	if len(*seen) != 0 {
		t.Fatalf("single-node judge loaded a model on %v, want none", *seen)
	}
	release()

	node, release, err = m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: "guardrail:g1", Backend: "pi", Model: "ollama/q", NodeOnly: true,
	})
	if err != nil || node != "" {
		t.Fatalf("NodeOnly single-node: node=%q err=%v, want empty and nil", node, err)
	}
	if led.Held("guardrail:g1") {
		t.Fatal("NodeOnly judge on a one-node entry took a lease")
	}
	release()
}

// TestJudgeRequestCarriesPriority: a judge's own lease request keeps its
// priority and verifier role, and the candidate nodes only for two or more.
func TestJudgeRequestCarriesPriority(t *testing.T) {
	choices := []capacity.NodeChoice{{Name: "a", Priority: 1}, {Name: "b", Priority: 2}}
	r := judgeRequest(JudgeAdmit{Holder: "verify:t", PRDID: "prd", Priority: 5}, []string{"host"}, "", choices, nil)
	if r.Priority != 5 || r.Role != capacity.RoleVerifier || r.PRDID != "prd" || len(r.NodeChoices) != 2 {
		t.Fatalf("judge request = %+v", r)
	}
	one := judgeRequest(JudgeAdmit{Holder: "verify:t"}, []string{"host"}, "solo", choices[:1], nil)
	if one.NodeChoices != nil || one.Node != "solo" {
		t.Fatalf("one-node request = %+v, want no choices and node solo", one)
	}
}

// TestJudgeLoadFailoverAlertsAndMovesNode: a judge whose model will not load
// on its node raises the alert, releases the lease and lands on the next node.
func TestJudgeLoadFailoverAlertsAndMovesNode(t *testing.T) {
	m, led := poolFixture(t, true)
	loader, seen := recordingLoader("fast")
	sink := &judgeAlerts{}
	m.SetNodeLoad(loader, sink)

	node, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder("task-f"), Backend: "pi", Model: "ollama/q", Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "slow" {
		t.Fatalf("judge node after failover = %q, want slow", node)
	}
	if !slices.Equal(*seen, []string{"fast", "slow"}) {
		t.Fatalf("loads tried on %v, want [fast slow]", *seen)
	}
	if len(sink.nodes) != 1 {
		t.Fatalf("alerts raised = %d, want 1", len(sink.nodes))
	}
	if ls := led.LeaseOf(VerifyHolder("task-f")); ls == nil || ls.Node != "slow" {
		t.Fatalf("judge lease = %+v, want node slow", ls)
	}
}

// TestAdmitJudgeHandoffDifferingPoolsFallsBack: the verifier's backend
// resolves to a different pool set than the worker lease covers, so the
// hand-off is refused, the worker's slot is released first (no stacking) and
// the verifier takes its own lease on its own node. When the pool sets match,
// the worker's lease is handed over on the same node.
func TestAdmitJudgeHandoffDifferingPoolsFallsBack(t *testing.T) {
	m, _, led := handoffTwoPoolFixture(t)
	acquireWorker := func(holder string) {
		t.Helper()
		if err := led.Acquire(context.Background(), capacity.Request{
			Holder: holder, Pools: []string{"node:n1"}, Node: "n1", Role: capacity.RoleWorker,
		}, 0, nil, nil); err != nil {
			t.Fatal(err)
		}
	}

	// Same pool set: handed over, kept on n1.
	acquireWorker("task-a")
	_, release, err := m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder("task-a"), WorkerHolder: "task-a", Backend: "workerY", Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	if ls := led.LeaseOf(VerifyHolder("task-a")); ls == nil || ls.Node != "n1" || led.Held("task-a") {
		t.Fatalf("hand-off: verifier lease = %+v, worker held = %v, want n1 and worker gone", ls, led.Held("task-a"))
	}
	release()

	// Differing pool set: fallback releases the worker lease, verifier on n2.
	acquireWorker("task-b")
	_, release, err = m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder("task-b"), WorkerHolder: "task-b", Backend: "verifierX", Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge fallback: %v", err)
	}
	defer release()
	if led.Held("task-b") {
		t.Fatal("fallback kept the worker lease: the task would hold two slots")
	}
	if ls := led.LeaseOf(VerifyHolder("task-b")); ls == nil || !slices.Contains(ls.Pools, "node:n2") {
		t.Fatalf("fallback verifier lease = %+v, want node:n2", ls)
	}
	if n := len(led.Snapshot().Leases); n != 1 {
		t.Fatalf("leases = %d, want exactly the verifier's", n)
	}
}
