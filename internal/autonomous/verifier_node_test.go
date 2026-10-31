package autonomous

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

const (
	vnWorkerModel   = "ollama/worker"
	vnVerifierModel = "ollama/verifier"
)

// verifierNodeFixture is a ledger with two candidate nodes, A and B, whose pools
// depend on the model (so a verifier with another model than its worker cannot
// take over the worker's lease), and a fake model host check.
type verifierNodeFixture struct {
	m   *Manager
	led *capacity.Ledger
	prd *PRD
	// notServed lists "node|model" pairs the fake host check refuses.
	notServed []string
}

func newVerifierNodeFixture(t *testing.T) *verifierNodeFixture {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for _, n := range []string{"A", "B"} {
		led.SetLimit("node:"+n, 3)
	}
	f := &verifierNodeFixture{m: m, led: led}
	m.SetCapacity(led, func(backend, model string) ([]string, string) {
		return []string{"host", "llm:" + model}, ""
	})
	m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
		return []capacity.NodeChoice{{Name: "A", Priority: 90}, {Name: "B", Priority: 10}}
	})
	m.SetModelHostCheck(func(_ context.Context, _, model, node string) ModelHostVerdict {
		return ModelHostVerdict{Applicable: true, Refused: slices.Contains(f.notServed, node+"|"+model), Model: model, Node: node}
	})
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDNeedsReview
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatal(err)
	}
	f.prd, _ = m.Store().GetPRD(prd.ID)
	return f
}

func (f *verifierNodeFixture) task() *Task {
	task := f.prd.Story[0].Tasks[0]
	return &task
}

// occupy holds one slot of node for holder, outside any task.
func (f *verifierNodeFixture) occupy(t *testing.T, holder, node string) {
	t.Helper()
	req := capacity.Request{Holder: holder, Pools: []string{"host", "llm:filler", "node:" + node}, Node: node}
	if err := f.led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy %s: %v", node, err)
	}
}

func (f *verifierNodeFixture) admitWorker(t *testing.T) string {
	t.Helper()
	task := f.task()
	if err := f.m.admit(context.Background(), f.prd, task, "pi", vnWorkerModel, capacity.RoleWorker); err != nil {
		t.Fatalf("admit worker: %v", err)
	}
	ls := f.led.LeaseOf(task.ID)
	if ls == nil {
		t.Fatal("worker holds no lease")
	}
	return ls.Node
}

func (f *verifierNodeFixture) admitVerifier(t *testing.T, wait time.Duration) (string, func(), error) {
	t.Helper()
	task := f.task()
	return f.m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: VerifyHolder(task.ID), WorkerHolder: task.ID, PRDID: f.prd.ID,
		Backend: "pi", Model: vnVerifierModel, Wait: wait,
	})
}

func (f *verifierNodeFixture) setFollow(on bool) {
	f.m.SetModelRoutingSettings(func() ModelRoutingSettings {
		return ModelRoutingSettings{RefusalMode: "refuse", AlertOnRefusal: true, VerifierFollowsWorkerNode: on}
	})
}

// workerOnBLeavingAIdleAndBBusy puts an unpinned worker on node B, then arranges
// the load so that normal placement of its verifier would choose A: A idle, B
// holding another holder.
func (f *verifierNodeFixture) workerOnBLeavingAIdleAndBBusy(t *testing.T) {
	t.Helper()
	f.occupy(t, "a-1", "A")
	f.occupy(t, "a-2", "A")
	if got := f.admitWorker(t); got != "B" {
		t.Fatalf("worker node = %q, want B while A is busy", got)
	}
	f.led.Release("a-1")
	f.led.Release("a-2")
	f.occupy(t, "b-1", "B")
}

func (f *verifierNodeFixture) notFollowedDecisions() []Decision {
	cur, _ := f.m.Store().GetPRD(f.prd.ID)
	var out []Decision
	for _, d := range cur.Decisions {
		if d.Kind == verifierNodeNotFollowedKind {
			out = append(out, d)
		}
	}
	return out
}

// TestModelRoute_VerifierFollowsPinnedNode: a task pinned to node B whose
// verifier uses another model than the worker, with node A the less loaded
// node: the verifier lease lands on B.
func TestModelRoute_VerifierFollowsPinnedNode(t *testing.T) {
	f := newVerifierNodeFixture(t)
	f.occupy(t, "b-1", "B")
	if _, err := f.m.SetTaskNode(f.prd.ID, f.task().ID, "B", "tester"); err != nil {
		t.Fatal(err)
	}
	if got := f.admitWorker(t); got != "B" {
		t.Fatalf("pinned worker node = %q, want B", got)
	}

	node, release, err := f.admitVerifier(t, time.Second)
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "B" {
		t.Fatalf("verifier node = %q, want the pinned node B although A is less loaded", node)
	}
	if ls := f.led.LeaseOf(VerifyHolder(f.task().ID)); ls == nil || ls.Node != "B" {
		t.Fatalf("verifier lease = %+v, want node B", ls)
	}
	if f.led.LeaseOf(f.task().ID) != nil {
		t.Fatal("worker lease still held")
	}
	if d := f.notFollowedDecisions(); len(d) != 0 {
		t.Fatalf("a followed node recorded %d verifier_node_not_followed decisions", len(d))
	}
}

// TestVerifierNode_FollowsWorkerNodeByDefault: no pin, the worker ran on B and
// the default settings are wired: the verifier goes to B although A is the
// less loaded node.
func TestVerifierNode_FollowsWorkerNodeByDefault(t *testing.T) {
	f := newVerifierNodeFixture(t)
	f.workerOnBLeavingAIdleAndBBusy(t)

	node, release, err := f.admitVerifier(t, time.Second)
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "B" {
		t.Fatalf("verifier node = %q, want the worker's node B", node)
	}
}

// TestVerifierNode_FollowOffUsesNormalPlacement: with the key false the least
// loaded node wins, as it did before, and nothing is recorded.
func TestVerifierNode_FollowOffUsesNormalPlacement(t *testing.T) {
	f := newVerifierNodeFixture(t)
	f.setFollow(false)
	f.workerOnBLeavingAIdleAndBBusy(t)

	node, release, err := f.admitVerifier(t, time.Second)
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "A" {
		t.Fatalf("verifier node = %q, want the least loaded node A with the key off", node)
	}
	if d := f.notFollowedDecisions(); len(d) != 0 {
		t.Fatalf("the key off recorded %d verifier_node_not_followed decisions", len(d))
	}
}

// TestVerifierNode_WorkerNodeNotServingVerifierModelFallsBack: node B does not
// serve the verifier's model, so normal placement applies and the Automaton
// records why.
func TestVerifierNode_WorkerNodeNotServingVerifierModelFallsBack(t *testing.T) {
	f := newVerifierNodeFixture(t)
	f.notServed = []string{"B|" + vnVerifierModel}
	f.workerOnBLeavingAIdleAndBBusy(t)

	node, release, err := f.admitVerifier(t, time.Second)
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "A" {
		t.Fatalf("verifier node = %q, want A by normal placement", node)
	}
	d := f.notFollowedDecisions()
	if len(d) != 1 {
		t.Fatalf("got %d verifier_node_not_followed decisions, want 1", len(d))
	}
	for _, want := range []string{f.task().ID, `"B"`, "does not serve"} {
		if !contains(d[0].Note, want) {
			t.Fatalf("decision note %q lacks %q", d[0].Note, want)
		}
	}
}

// TestVerifierNode_WaitsForWorkerNodeInsteadOfMoving: the worker's node is full
// and the other node is free; the dry run and a real admission wait for the
// worker's node when following, and take the free node when not.
func TestVerifierNode_WaitsForWorkerNodeInsteadOfMoving(t *testing.T) {
	f := newVerifierNodeFixture(t)
	f.led.SetLimit("node:B", 1)
	f.occupy(t, "b-1", "B")
	// The worker's lease is gone by the time a verifier waits; its node is the
	// one recorded on the task.
	f.m.Store().MutateTask(f.task().ID, func(c *Task) { c.ComputeNode = "B" })
	j := JudgeAdmit{
		Holder: VerifyHolder(f.task().ID), WorkerHolder: f.task().ID, PRDID: f.prd.ID,
		Backend: "pi", Model: vnVerifierModel, Wait: 100 * time.Millisecond,
	}

	if f.m.JudgeFits(j) {
		t.Fatal("JudgeFits says the verifier fits although it must wait for the worker's full node B")
	}
	_, _, err := f.m.AdmitJudge(context.Background(), j)
	if !errors.Is(err, capacity.ErrWaitTimeout) {
		t.Fatalf("AdmitJudge = %v, want a wait timeout instead of a move to node A", err)
	}
	if ls := f.led.LeaseOf(j.Holder); ls != nil {
		t.Fatalf("verifier moved to %+v", ls)
	}

	f.setFollow(false)
	if !f.m.JudgeFits(j) {
		t.Fatal("JudgeFits says the verifier waits with the key off and node A free")
	}
}

// TestVerifierNode_GuardrailJudgeKeepsPlacement: a guardrail judge is not a
// verifier and places freely whatever the setting says.
func TestVerifierNode_GuardrailJudgeKeepsPlacement(t *testing.T) {
	f := newVerifierNodeFixture(t)
	f.m.Store().MutateTask(f.task().ID, func(c *Task) { c.ComputeNode = "B" })
	f.occupy(t, "b-1", "B")

	node, release, err := f.m.AdmitJudge(context.Background(), JudgeAdmit{
		Holder: JudgeHolder(f.task().ID), PRDID: f.prd.ID, Backend: "pi", Model: vnVerifierModel, Wait: time.Second,
	})
	if err != nil {
		t.Fatalf("AdmitJudge: %v", err)
	}
	defer release()
	if node != "A" {
		t.Fatalf("guardrail judge node = %q, want A by normal placement", node)
	}
}

// TestVerifierNode_DecisionRule covers the pure rule.
func TestVerifierNode_DecisionRule(t *testing.T) {
	cands := []capacity.NodeChoice{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	on := ModelRoutingSettings{VerifierFollowsWorkerNode: true}
	cases := []struct {
		name       string
		in         verifierNodeInput
		followed   bool
		wantExcl   []string
		wantReason string
	}{
		{"follows", verifierNodeInput{Settings: on, WorkerNode: "B", Candidates: cands, ServesModel: true}, true, []string{"A", "C"}, "follows"},
		{"off", verifierNodeInput{Settings: ModelRoutingSettings{}, WorkerNode: "B", Candidates: cands, ServesModel: true}, false, nil, "off"},
		{"unknown worker node", verifierNodeInput{Settings: on, Candidates: cands, ServesModel: true}, false, nil, "not known"},
		{"not a candidate", verifierNodeInput{Settings: on, WorkerNode: "Z", Candidates: cands, ServesModel: true}, false, nil, "not a candidate"},
		{"model not served", verifierNodeInput{Settings: on, WorkerNode: "B", Candidates: cands}, false, nil, "does not serve"},
	}
	for _, c := range cases {
		got := decideVerifierNode(c.in)
		if got.Followed != c.followed || !slices.Equal(got.ExcludeNodes, c.wantExcl) || !contains(got.Reason, c.wantReason) {
			t.Errorf("%s: got %+v, want followed=%v exclude=%v reason containing %q", c.name, got, c.followed, c.wantExcl, c.wantReason)
		}
	}
	if got := resolveWorkerNode("P", "L", "C"); got != "P" {
		t.Errorf("pin must win, got %q", got)
	}
	if got := resolveWorkerNode("", "L", "C"); got != "L" {
		t.Errorf("lease must come before the recorded node, got %q", got)
	}
	if got := resolveWorkerNode("", "", " C "); got != "C" {
		t.Errorf("recorded node = %q", got)
	}
}
