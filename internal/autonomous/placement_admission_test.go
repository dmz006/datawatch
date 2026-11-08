package autonomous

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/audit"
	"github.com/dmz006/datawatch/internal/capacity"
	"github.com/dmz006/datawatch/internal/config"
)

// fakePlacementAdvisory answers the load gate's planner with a fixed verdict.
type fakePlacementAdvisor struct {
	plan    PlacementPlan
	timeout time.Duration
	gotPin  string // records the pinnedTo argument from the last Plan call
	// followPin makes the fake behave like the real planner: a task pin
	// restricts the choice to the pinned node.
	followPin bool
}

func (f *fakePlacementAdvisor) Plan(backend, model, pinnedTo string) PlacementPlan {
	f.gotPin = pinnedTo
	plan := f.plan
	if f.followPin && pinnedTo != "" {
		plan.Chosen = pinnedTo
	}
	return plan
}

func (f *fakePlacementAdvisor) AskTimeout() time.Duration {
	if f.timeout <= 0 {
		return 30 * time.Minute
	}
	return f.timeout
}

// placementRig is a nodeLoadRig with a placement advisor (and, for ask
// tests, a question store) wired the same way the daemon wires them.
type placementRig struct {
	rig   *nodeLoadRig
	store *QuestionStore
}

func newPlacementRig(t *testing.T, order []string, nodes map[string]*fakeOllamaNode, plan PlacementPlan, timeout time.Duration) *placementRig {
	t.Helper()
	rig := newNodeLoadRig(t, order, nodes)
	if plan.Relevant {
		// The advisor rides the existing admission wiring: SetCapacity.
		rig.m.SetCapacity(rig.led, rig.keys, &fakePlacementAdvisor{plan: plan, timeout: timeout})
	}
	return &placementRig{rig: rig}
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
	err := p.rig.admitErr(t)
	if err != nil {
		t.Fatalf("auto_load reroute to beta must succeed, got %v", err)
	}
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "beta" {
		t.Fatalf("the lease must be on beta, got %+v", got)
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
	err := p.rig.admitErr(t)
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
	err := p.rig.admitErr(t)
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		task := p.rig.task
		done <- p.rig.m.admit(ctx, p.rig.prd, &task, "pi", nodeLoadModel, capacity.RoleWorker)
	}()

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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		task := p.rig.task
		done <- p.rig.m.admit(ctx, p.rig.prd, &task, "pi", nodeLoadModel, capacity.RoleWorker)
	}()

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

	start := time.Now()
	err := p.rig.admitErr(t)
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
	err := p.rig.admitErr(t)
	if !IsPlacementRefused(err) {
		t.Fatalf("an ask with no question channel must decline, got %v", err)
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("no load may be attempted, got %d loads", loads)
	}
}

// TestPlacementPinsTaskNode: the gate passes the task's leased node as the
// pin to the planner, so the planner can honour an existing placement.
func TestPlacementPinsTaskNode(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "alpha", Policy: config.ComputePlacementAutoLoad}, 0)
	// Set the task's ComputeNode to "alpha" — this is the pin.
	p.rig.task.ComputeNode = "alpha"
	// The fake advisor ignores the pin and returns its fixed plan (alpha).
	p.rig.admit(t)
	// Verify the pin was passed to the planner.
	adv := p.rig.m.getPlacementAdvisor()
	if adv == nil {
		t.Fatal("no advisor wired")
	}
	fake := adv.(*fakePlacementAdvisor)
	if fake.gotPin != "alpha" {
		t.Fatalf("the gate should pass the task's leased node as the pin, got %q", fake.gotPin)
	}
	// The lease should land on alpha (the planner's choice, which matches the pin).
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "alpha" {
		t.Fatalf("the lease should land on alpha, got %+v", got)
	}
}

// placementAuditCapture records the audit events the gate emits.
type placementAuditCapture struct {
	mu   sync.Mutex
	recs []placementAuditRecord
}

type placementAuditRecord struct {
	action  string
	details map[string]any
}

func (c *placementAuditCapture) fn(action string, details map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recs = append(c.recs, placementAuditRecord{action, details})
}

func (c *placementAuditCapture) snapshot() []placementAuditRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]placementAuditRecord(nil), c.recs...)
}

func (c *placementAuditCapture) actions() []string {
	var out []string
	for _, r := range c.snapshot() {
		out = append(out, r.action)
	}
	return out
}

// waitForWaitReason polls the task's stored state until it shows the wanted
// wait reason, so a test can read the parked state while the ask is open.
func (r *nodeLoadRig) waitForWaitReason(t *testing.T, want string) TaskStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if status, reason := r.waitState(); reason == want {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, reason := r.waitState()
	t.Fatalf("the task never showed wait reason %q, last reason %q", want, reason)
	return ""
}

// TestPlacementServingNodeSkipsThePolicy: when the node placement chose already
// serves the model nothing is missing, so even the refuse policy has nothing to
// decide: admission proceeds, no ask is raised, no alert, no audit event.
func TestPlacementServingNodeSkipsThePolicy(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	for _, policy := range []string{config.ComputePlacementRefuse, config.ComputePlacementAsk, config.ComputePlacementAutoLoad} {
		p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
			PlacementPlan{Relevant: true, Serving: true, Chosen: "beta", Policy: policy}, 0)
		cap := &placementAuditCapture{}
		p.rig.m.SetAuditFn(cap.fn)
		store := p.withQuestionStore(t)
		if err := p.rig.admitErr(t); err != nil {
			t.Fatalf("policy %s: a served model must admit, got %v", policy, err)
		}
		if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil {
			t.Fatalf("policy %s: the task must hold a lease", policy)
		}
		if n := len(store.List(QuestionFilter{Asker: placementQuestionAsker})); n != 0 {
			t.Errorf("policy %s: no ask may be raised for a served model, got %d", policy, n)
		}
		if n := len(p.rig.sink.snapshot()); n != 0 {
			t.Errorf("policy %s: no alert may be raised, got %d", policy, n)
		}
		if acts := cap.actions(); len(acts) != 0 {
			t.Errorf("policy %s: no placement audit event for a served model, got %v", policy, acts)
		}
	}
}

// TestPlacementRefuseAuditsAndCarriesPlannerText: under refuse the failure
// carries the planner's refusal lines and the refused audit event records
// the policy and those lines.
func TestPlacementRefuseAuditsAndCarriesPlannerText(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{})
	beta := newFakeOllamaNode(t, fakeNodeOpts{})
	refused := []string{"alpha: free memory 1.0 GB, needs 20.0 GB plus 10% headroom", "beta: disabled"}
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Policy: config.ComputePlacementRefuse, Refused: refused}, 0)
	cap := &placementAuditCapture{}
	p.rig.m.SetAuditFn(cap.fn)
	err := p.rig.admitErr(t)
	if !IsPlacementRefused(err) {
		t.Fatalf("want a placement refusal, got %v", err)
	}
	for _, line := range refused {
		if !strings.Contains(err.Error(), line) {
			t.Errorf("the failure %q must carry the planner line %q", err.Error(), line)
		}
	}
	recs := cap.snapshot()
	if len(recs) != 1 || recs[0].action != audit.ActionPlacementRefused {
		t.Fatalf("want one %s event, got %v", audit.ActionPlacementRefused, cap.actions())
	}
	d := recs[0].details
	if d["policy"] != config.ComputePlacementRefuse || d["model"] != nodeLoadModel {
		t.Errorf("audit details %v miss the policy or model", d)
	}
	if r, _ := d["reason"].(string); !strings.Contains(r, "beta: disabled") {
		t.Errorf("audit reason %q must carry the planner lines", r)
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("refuse never loads, got %d loads on alpha", loads)
	}
}

// TestPlacementAskParksWithVisibleReasonThenApproves: while the ask is open
// the task shows as waiting with the exact reason "model placement awaiting
// approval"; the approval resumes admission, loads the model on the approved
// node before the lease, and the audit trail is asked, approved, loaded.
func TestPlacementAskParksWithVisibleReasonThenApproves(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{}) // missing: loads on request
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk}, 0)
	cap := &placementAuditCapture{}
	p.rig.m.SetAuditFn(cap.fn)
	p.withQuestionStore(t)

	done := make(chan error, 1)
	go func() { done <- p.rig.admitErr(t) }()

	if status := p.rig.waitForWaitReason(t, "model placement awaiting approval"); status != TaskWaitingCapacity {
		t.Errorf("a parked task is %s, want %s", status, TaskWaitingCapacity)
	}
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got != nil {
		t.Errorf("no lease may be held while the ask is open, got %+v", got)
	}
	p.answerFirstPlacementQuestion(t, placementOptionLoad)
	if err := <-done; err != nil {
		t.Fatalf("an approved ask must admit, got %v", err)
	}
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "beta" {
		t.Fatalf("the lease must be on the approved node beta, got %+v", got)
	}
	if _, loads := beta.counts(); loads != 1 {
		t.Errorf("the approved node must load the model once, got %d loads", loads)
	}
	want := []string{audit.ActionPlacementAsked, audit.ActionPlacementApproved, audit.ActionPlacementLoaded}
	if got := cap.actions(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("audit trail = %v, want %v", got, want)
	}
	for _, r := range cap.snapshot() {
		if r.details["node"] != "beta" || r.details["policy"] != config.ComputePlacementAsk || r.details["model"] != nodeLoadModel {
			t.Errorf("%s details %v miss node, model or policy", r.action, r.details)
		}
	}
}

// TestPlacementAskTimeoutAuditsDenial: a timed-out ask is a refusal and the
// denied event is recorded; no load follows.
func TestPlacementAskTimeoutAuditsDenial(t *testing.T) {
	prev := placementWaitPoll
	placementWaitPoll = 10 * time.Millisecond
	t.Cleanup(func() { placementWaitPoll = prev })
	alpha := newFakeOllamaNode(t, fakeNodeOpts{})
	beta := newFakeOllamaNode(t, fakeNodeOpts{})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk}, 100*time.Millisecond)
	cap := &placementAuditCapture{}
	p.rig.m.SetAuditFn(cap.fn)
	p.withQuestionStore(t)
	if err := p.rig.admitErr(t); !IsPlacementRefused(err) {
		t.Fatalf("a timed-out ask is a refusal, got %v", err)
	}
	want := []string{audit.ActionPlacementAsked, audit.ActionPlacementDenied}
	if got := cap.actions(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("audit trail = %v, want %v", got, want)
	}
	if _, loads := beta.counts(); loads != 0 {
		t.Errorf("no load after a timeout, got %d", loads)
	}
}

// TestPlacementAskDedupesAcrossAttempts: two admission attempts of the same
// task, model and node raise one pending ask between them, both resume on
// the one answer, and a later attempt reuses the approval instead of asking
// again.
func TestPlacementAskDedupesAcrossAttempts(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk}, 0)
	store := p.withQuestionStore(t)
	cap := &placementAuditCapture{}
	p.rig.m.SetAuditFn(cap.fn)

	gate := func() (placementTarget, error) {
		task := p.rig.task
		return p.rig.m.placementGate(context.Background(), &task, "pi", nodeLoadModel, "alpha")
	}
	type result struct {
		target placementTarget
		err    error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			tg, err := gate()
			results <- result{tg, err}
		}()
	}
	// Wait until both attempts are parked on the single open question.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(store.List(QuestionFilter{Asker: placementQuestionAsker, State: QuestionOpen})) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if n := len(store.List(QuestionFilter{Asker: placementQuestionAsker})); n != 1 {
		t.Fatalf("two attempts of one (task, model, node) must raise one ask, got %d", n)
	}
	p.answerFirstPlacementQuestion(t, placementOptionLoad)
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err != nil || r.target.Node != "beta" {
			t.Fatalf("both attempts must resume on the one answer, got %+v", r)
		}
	}
	// A third attempt finds the approval and never asks again.
	tg, err := gate()
	if err != nil || tg.Node != "beta" {
		t.Fatalf("a later attempt must reuse the approval, got %+v %v", tg, err)
	}
	if n := len(store.List(QuestionFilter{Asker: placementQuestionAsker})); n != 1 {
		t.Errorf("a reused approval must not raise another ask, got %d questions", n)
	}
	asked := 0
	for _, a := range cap.actions() {
		if a == audit.ActionPlacementAsked {
			asked++
		}
	}
	if asked != 1 {
		t.Errorf("one ask must record one placement_asked event, got %d (%v)", asked, cap.actions())
	}
}

// TestPlacementCancelDuringAskIsNotARefusal: a task cancelled (or a context
// ended) while the ask is open ends the admission as a cancel: no placement
// refusal, no deferred alert, and the open question is withdrawn.
func TestPlacementCancelDuringAskIsNotARefusal(t *testing.T) {
	cases := []struct {
		name   string
		end    func(p *placementRig, cancel context.CancelFunc)
		wantIs error
	}{
		{"task cancelled", func(p *placementRig, cancel context.CancelFunc) {
			p.rig.m.store.MutateTask(p.rig.task.ID, func(c *Task) { c.Status = TaskCancelled })
		}, errCancelledWhileWaiting},
		{"context ended", func(p *placementRig, cancel context.CancelFunc) { cancel() }, context.Canceled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prev := placementWaitPoll
			placementWaitPoll = 10 * time.Millisecond
			t.Cleanup(func() { placementWaitPoll = prev })
			alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
			beta := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
			p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
				PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAsk}, 0)
			cap := &placementAuditCapture{}
			p.rig.m.SetAuditFn(cap.fn)
			store := p.withQuestionStore(t)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				task := p.rig.task
				done <- p.rig.m.admit(ctx, p.rig.prd, &task, "pi", nodeLoadModel, capacity.RoleWorker)
			}()
			p.rig.waitForWaitReason(t, "model placement awaiting approval")
			c.end(p, cancel)
			err := <-done
			if IsPlacementRefused(err) {
				t.Fatalf("a cancel is not a refusal, got %v", err)
			}
			if !errors.Is(err, c.wantIs) {
				t.Fatalf("err = %v, want %v", err, c.wantIs)
			}
			if n := len(p.rig.sink.snapshot()); n != 0 {
				t.Errorf("a cancel raises no deferred alert, got %d", n)
			}
			for _, a := range cap.actions() {
				if a == audit.ActionPlacementDenied || a == audit.ActionPlacementRefused {
					t.Errorf("a cancel records no %s event", a)
				}
			}
			qs := store.List(QuestionFilter{Asker: placementQuestionAsker})
			if len(qs) != 1 || qs[0].State != QuestionWithdrawn {
				t.Errorf("the open ask must be withdrawn on a cancel, got %+v", qs)
			}
		})
	}
}

// TestPlacementAutoLoadLoadsMissingModelBeforeLease: auto_load pulls and loads
// the model on the node placement chose, records the loaded event, and then
// admits the task on that node only, even though the other node ranks higher.
func TestPlacementAutoLoadLoadsMissingModelBeforeLease(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{}) // missing
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "beta", Policy: config.ComputePlacementAutoLoad}, 0)
	cap := &placementAuditCapture{}
	p.rig.m.SetAuditFn(cap.fn)
	if err := p.rig.admitErr(t); err != nil {
		t.Fatalf("auto_load must admit, got %v", err)
	}
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "beta" {
		t.Fatalf("the lease must be on beta, where the model was loaded, got %+v", got)
	}
	if _, loads := beta.counts(); loads != 1 {
		t.Errorf("the chosen node must load once, got %d", loads)
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("the other node must not be touched, got %d loads", loads)
	}
	recs := cap.snapshot()
	if len(recs) != 1 || recs[0].action != audit.ActionPlacementLoaded || recs[0].details["node"] != "beta" || recs[0].details["policy"] != config.ComputePlacementAutoLoad {
		t.Fatalf("want one placement_loaded event for beta under auto_load, got %+v", recs)
	}
	// The load step the executor runs next finds the model resident: no second load.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.rig.load(ctx); err != nil {
		t.Fatalf("the post-lease load step: %v", err)
	}
	if _, loads := beta.counts(); loads != 1 {
		t.Errorf("the post-lease step must not load again, got %d loads", loads)
	}
}

// TestPlacementTaskPinIsRespected: a task pinned to a node reaches the planner
// as the pin, the planner's answer is that node, and the task is admitted
// there even though another node ranks higher; the unpinned node is never
// loaded.
func TestPlacementTaskPinIsRespected(t *testing.T) {
	alpha := newFakeOllamaNode(t, fakeNodeOpts{Resident: true})
	beta := newFakeOllamaNode(t, fakeNodeOpts{})
	p := newPlacementRig(t, []string{"alpha", "beta"}, map[string]*fakeOllamaNode{"alpha": alpha, "beta": beta},
		PlacementPlan{Relevant: true, Chosen: "alpha", Policy: config.ComputePlacementAutoLoad}, 0)
	p.rig.m.getPlacementAdvisor().(*fakePlacementAdvisor).followPin = true
	p.rig.task.ComputeNode = "beta"
	if err := p.rig.admitErr(t); err != nil {
		t.Fatalf("a pinned task must admit, got %v", err)
	}
	if got := p.rig.m.getPlacementAdvisor().(*fakePlacementAdvisor).gotPin; got != "beta" {
		t.Fatalf("the planner must receive the task pin, got %q", got)
	}
	if got := p.rig.led.LeaseOf(p.rig.task.ID); got == nil || got.Node != "beta" {
		t.Fatalf("the task must be admitted on its pinned node beta, got %+v", got)
	}
	if _, loads := alpha.counts(); loads != 0 {
		t.Errorf("the unpinned node must not be loaded, got %d", loads)
	}
}
