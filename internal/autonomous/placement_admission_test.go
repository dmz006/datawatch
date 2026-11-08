package autonomous

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/config"
)

// fakePlacementAdvisory answers the load gate's planner with a fixed verdict.
type fakePlacementAdvisor struct {
	plan    PlacementPlan
	timeout time.Duration
}

func (f *fakePlacementAdvisor) Plan(backend, model string) PlacementPlan { return f.plan }

func (f *fakePlacementAdvisor) AskTimeout() time.Duration {
	if f.timeout <= 0 {
		return 30 * time.Minute
	}
	return f.timeout
}

// placementRig is a nodeLoadRig with a placement advisor (and, for ask
// tests, a question store) wired the same way the daemon wires them.
type placementRig struct {
	rig *nodeLoadRig
	store *QuestionStore
}

func newPlacementRig(t *testing.T, order []string, nodes map[string]*fakeOllamaNode, plan PlacementPlan, timeout time.Duration) *placementRig {
	t.Helper()
	rig := newNodeLoadRig(t, order, nodes)
	if plan.Relevant {
		rig.m.SetPlacementAdvisor(&fakePlacementAdvisor{plan: plan, timeout: timeout})
	}
	p := &placementRig{rig: rig}
	t.Cleanup(func() { p.rig.m.SetPlacementAdvisor(nil) })
	return p
}

// withQuestionStore wires a question store the way SetDecisionGate does, so
// the placement ask has a channel.
func (p *placementRig) withQuestionStore(t *testing.T) *QuestionStore {
	t.Helper()
	store, err := NewQuestionStore(filepath.Join(t.TempDir(), "questions.json"))
	if err != nil {
		t.Fatalf("NewQuestionStore: %v", err)
	}
	p.rig.m.SetDecisionGate(store, nil)
	p.store = store
	return store
}

// answerFirstPlacementQuestion answers the newest open placement question with
// the given option id, retrying until the question exists.
func (p *placementRig) answerFirstPlacementQuestion(t *testing.T, optionID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		qs := p.store.List(QuestionFilter{Asker: placementQuestionAsker, State: QuestionOpen})
		if len(qs) > 0 {
			if _, aerr := p.store.Answer(qs[0].ID, Answer{Actor: "test-operator", ChosenOptionID: optionID}); aerr != nil {
				t.Fatalf("answer %q: %v", optionID, aerr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the placement question never went open")
}

// TestPlacementGateNoAdvisorKeepsOldBehaviour asserts that without an advisor
// wired the load path is exactly what it was: failed node alerts and fails
// over, no placement refusal anywhere.
func TestPlacementGateNoAdvisorKeepsOldBehaviour(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	rig := newNodeLoadRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta})
	rig.admit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rig.load(ctx); err != nil {
		t.Fatalf("with no advisor the failover load must succeed, got %v", err)
	}
	if got := rig.led.LeaseOf(rig.task.ID); got == nil || got.Node != "beta" {
		t.Fatalf("lease should land on beta, got %+v", got)
	}
}

// TestPlacementAutoLoadProceedsOnChosenNode: auto_load with the planner's
// choice equal to the leased node loads on that node.
func TestPlacementAutoLoadProceedsOnChosenNode(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "alpha", Policy: config.ComputePlacementAutoLoad}, 0)
	p.rig.admit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.rig.load(ctx); err != nil {
		t.Fatalf("auto_load on the planned node must succeed, got %v", err)
	}
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "alpha" {
		t.Fatalf("lease should stay on alpha, got %+v", got)
	}
	if n := len(p.rig.sink.snapshot()); n != 0 {
		t.Fatalf("no alert should be raised, got %d", n)
	}
}

// TestPlacementAutoLoadReroutesToChosenNode: auto_load with the planner's
// choice on another node moves the lease there and loads it — the leased
// node is never loaded.
func TestPlacementAutoLoadReroutesToChosenNode(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAutoLoad}, 0)
	p.rig.admit(t)
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "alpha" {
		t.Fatalf("admission should land on alpha first, got %+v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.rig.load(ctx); err != nil {
		t.Fatalf("auto_load reroute to beta must succeed, got %v", err)
	}
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "beta" {
		t.Fatalf("after the reroute the lease must be on beta, got %+v", got)
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("the leased-but-rejected alpha must never be asked to load, got %d loads", loads)
	}
	if n := len(p.rig.sink.snapshot()); n != 0 {
		t.Fatalf("an approved reroute raises no alert, got %d", n)
	}
}

// TestPlacementAutoLoadRerouteWithNoNodeLeftFails: the planner chose a node
// the LLM entry does not pool; no reroute target exists, so the load fails.
func TestPlacementAutoLoadRerouteWithNoNodeLeftFails(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "gamma", Policy: config.ComputePlacementAutoLoad}, 0)
	p.rig.admit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.rig.load(ctx)
	if err == nil {
		t.Fatal("with no reroute target left the load must fail")
	}
	if p.rig.led.Held(p.rig.task.ID) {
		t.Error("the lease must be released")
	}
}

// TestPlacementRefusePolicyStopsBeforeLoad: the refuse policy stops the load
// on the leased node before anything is loaded, fails the start with a
// placement refusal, and raises the deferred alert.
func TestPlacementRefusePolicyStopsBeforeLoad(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	refused := []string{"alpha: no room for the model (mem)", "beta: no room for the model (disk)"}
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "", Policy: config.ComputePlacementRefuse, Refused: refused}, 0)
	p.rig.admit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := p.rig.load(ctx)
	if !IsPlacementRefused(err) {
		t.Fatalf("the refuse policy must fail the start with a placement refusal, got %v", err)
	}
	if p.rig.led.Held(p.rig.task.ID) {
		t.Error("the lease must be released on a refusal")
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("the refuse policy must never ask the node to load, got %d loads", loads)
	}
	raised := p.rig.sink.snapshot()
	if len(raised) != 1 {
		t.Fatalf("want one deferred alert, got %d", len(raised))
	}
	a := raised[0]
	if a.Level != alerts.LevelWarn {
		t.Errorf("alert level = %q, want warn", a.Level)
	}
	for _, want := range []string{nodeLoadModel, "alpha", "no room for the model (disk)"} {
		if !strings.Contains(a.Title+" "+a.Body, want) {
			t.Errorf("alert %q / %q does not name %q", a.Title, a.Body, want)
		}
	}
}

// TestPlacementAskApproveLoads: the ask policy raises the operator question;
// an approved load proceeds (here to the planner's other node via reroute).
func TestPlacementAskApproveLoads(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{FailLoad: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk}, 0)
	p.withQuestionStore(t)
	p.rig.admit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.rig.load(ctx) }()

	p.answerFirstPlacementQuestion(t, placementOptionLoad)
	if err := <-done; err != nil {
		t.Fatalf("an approved ask should load (beta), got %v", err)
	}
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "beta" {
		t.Fatalf("the approved load should land on beta, got %+v", got)
	}
	if status, _ := p.rig.waitState(); status == TaskWaitingCapacity {
		t.Errorf("the waiting-for-decision state must clear after the answer, got %s", status)
	}
}

// TestPlacementAskRefuseStops: the operator's refusal fails the start with a
// placement refusal that names the question, and the question is answered (
// not withdrawn — the operator closed it).
func TestPlacementAskRefuseStops(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk}, 0)
	p.withQuestionStore(t)
	p.rig.admit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.rig.load(ctx) }()

	p.answerFirstPlacementQuestion(t, placementOptionRefuse)
	err := <-done
	if !IsPlacementRefused(err) {
		t.Fatalf("a refused ask must fail the start with a placement refusal, got %v", err)
	}
	var refused *PlacementRefused
	if !errors.As(err, &refused) || refused.QuestionID == "" {
		t.Errorf("the refusal should name the raised question, got %v", err)
	}
	qs := p.store.List(QuestionFilter{Asker: placementQuestionAsker})
	if len(qs) != 1 || qs[0].State != QuestionAnswered {
		t.Fatalf("the question should be answered by the operator (not withdrawn), got %+v", qs)
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("a refused ask must never load the leased node, got %d loads", loads)
	}
	raised := p.rig.sink.snapshot()
	if len(raised) != 1 || raised[0].Level != alerts.LevelWarn {
		t.Fatalf("want one warn deferred alert, got %+v", raised)
	}
}

// TestPlacementAskTimeoutWithholdsAndDeclines: an unanswered ask is withdrawn
// when its timeout lands and the load declines.
func TestPlacementAskTimeoutWithholdsAndDeclines(t *testing.T) {
	prev := placementWaitPoll
	placementWaitPoll = 10 * time.Millisecond
	t.Cleanup(func() { placementWaitPoll = prev })

	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk}, 300*time.Millisecond)
	p.withQuestionStore(t)
	p.rig.admit(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	err := p.rig.load(ctx)
	elapsed := time.Since(start)
	if !IsPlacementRefused(err) {
		t.Fatalf("a timed-out ask must decline the load, got %v", err)
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("the gate should have waited out its timeout, returned after %s", elapsed)
	}
	qs := p.store.List(QuestionFilter{Asker: placementQuestionAsker})
	if len(qs) != 1 || qs[0].State != QuestionWithdrawn {
		t.Fatalf("a timed-out ask must be withdrawn, got %+v", qs)
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("a timed-out ask must never load, got %d loads", loads)
	}
}

// TestPlacementAskWithoutStoreDeclines: with no question channel wired (no
// operator reachable) the ask declines the load instead of hanging.
func TestPlacementAskWithoutStoreDeclines(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk}, 0)
	// No question store wired: decisionStore stays nil.
	p.rig.admit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.rig.load(ctx)
	if !IsPlacementRefused(err) {
		t.Fatalf("an ask with no question channel must decline, got %v", err)
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("no load may be attempted, got %d loads", loads)
	}
}

