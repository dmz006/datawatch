package autonomous

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/capacity"
)

const pinOtherModel = "ollama/other-model:7b"

// pinFake is a ModelHostCheckFn over a small fleet: the registered nodes and
// the models each serves. A node argument pins the check to that node, and an
// unregistered node comes back refused with NodeUnknown set, as the daemon's
// check does. It records every call.
type pinFake struct {
	mu     sync.Mutex
	serves map[string][]string
	calls  []routeCall
}

func newPinFake() *pinFake {
	return &pinFake{serves: map[string][]string{
		"gpu-a": {routeServedModel},
		"gpu-b": {pinOtherModel},
		"gpu-c": {routeServedModel, pinOtherModel},
	}}
}

func (f *pinFake) check(_ context.Context, backend, model, node string) ModelHostVerdict {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, routeCall{backend, model, node})
	v := ModelHostVerdict{Applicable: true, Model: model, Node: node}
	if node != "" {
		models, ok := f.serves[node]
		if !ok {
			v.Refused, v.NodeUnknown = true, true
			v.Message = "not a registered node"
			return v
		}
		if model != "" && !slices.Contains(models, model) {
			v.Refused = true
			// The wording does not name the node, so the pin check must.
			v.Message = "no registered node serves model " + model
		}
		return v
	}
	if model == "" {
		return v
	}
	for _, models := range f.serves {
		if slices.Contains(models, model) {
			return v
		}
	}
	v.Refused = true
	v.Message = "no registered node serves model " + model
	return v
}

func (f *pinFake) lastCall(t *testing.T) routeCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("the model host check was never called")
	}
	return f.calls[len(f.calls)-1]
}

// newPinManager returns a manager holding a pi Automaton in review whose worker
// holds routeServedModel, wired to a pinFake and the given refusal mode.
func newPinManager(t *testing.T, mode, model string) (*Manager, *PRD, *pinFake) {
	t.Helper()
	m := newRefusalManager(t)
	prd := seedRefusalPRD(t, m, "pi", model)
	f := newPinFake()
	m.SetModelHostCheck(f.check)
	m.SetModelRoutingSettings(func() ModelRoutingSettings {
		return ModelRoutingSettings{RefusalMode: mode, AlertOnRefusal: true, VerifierFollowsWorkerNode: true}
	})
	return m, prd, f
}

func pinnedNodes(t *testing.T, m *Manager, prdID string) (task, story string) {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("prd %q not found", prdID)
	}
	tk, st := findTaskInPRD(prd, "t1")
	return tk.PinnedNode, st.PinnedNode
}

func pinDecisionKinds(t *testing.T, m *Manager, prdID string) []string {
	t.Helper()
	var kinds []string
	for _, d := range routeDecisionKinds(t, m, prdID) {
		kinds = append(kinds, d.Kind)
	}
	return kinds
}

func TestModelRoute_NodePinNamesNodeThatDoesNotServeModel(t *testing.T) {
	m, prd, f := newPinManager(t, "refuse", routeServedModel)
	before := len(routeDecisionKinds(t, m, prd.ID))

	for _, pin := range []struct {
		name string
		do   func() error
	}{
		{"task", func() error { _, err := m.SetTaskNode(prd.ID, "t1", "gpu-b", "tester"); return err }},
		{"story", func() error { _, err := m.SetStoryNode(prd.ID, "s1", "gpu-b", "tester"); return err }},
	} {
		err := pin.do()
		if err == nil {
			t.Fatalf("%s: pinning a node that does not serve the model was accepted", pin.name)
		}
		if !errors.Is(err, ErrModelNotServed) {
			t.Fatalf("%s: error %v does not wrap ErrModelNotServed", pin.name, err)
		}
		if !strings.Contains(err.Error(), "gpu-b") || !strings.Contains(err.Error(), routeServedModel) {
			t.Fatalf("%s: error %q must name the node and the model", pin.name, err)
		}
	}
	if call := f.lastCall(t); call.node != "gpu-b" || call.model != routeServedModel || call.backend != "pi" {
		t.Fatalf("the check ran with %+v, want backend pi, model %s, node gpu-b", call, routeServedModel)
	}
	if task, story := pinnedNodes(t, m, prd.ID); task != "" || story != "" {
		t.Fatalf("a refused pin was stored: task=%q story=%q", task, story)
	}
	if after := len(routeDecisionKinds(t, m, prd.ID)); after != before {
		t.Fatalf("a refused pin recorded %d Decision(s)", after-before)
	}
}

func TestModelRoute_NodePinAcceptedWhenNodeServesModel(t *testing.T) {
	m, prd, f := newPinManager(t, "refuse", routeServedModel)

	updated, err := m.SetTaskNode(prd.ID, "t1", "  gpu-a ", "tester")
	if err != nil {
		t.Fatalf("SetTaskNode: %v", err)
	}
	task, _ := findTaskInPRD(updated, "t1")
	if task.PinnedNode != "gpu-a" {
		t.Fatalf("task pin = %q, want gpu-a (trimmed)", task.PinnedNode)
	}
	if call := f.lastCall(t); call.node != "gpu-a" || call.model != routeServedModel {
		t.Fatalf("the check ran with %+v", call)
	}
	if _, err := m.SetStoryNode(prd.ID, "s1", "gpu-c", "tester"); err != nil {
		t.Fatalf("SetStoryNode: %v", err)
	}
	taskPin, storyPin := pinnedNodes(t, m, prd.ID)
	if taskPin != "gpu-a" || storyPin != "gpu-c" {
		t.Fatalf("stored pins task=%q story=%q", taskPin, storyPin)
	}
	kinds := pinDecisionKinds(t, m, prd.ID)
	if !slices.Contains(kinds, "set_task_node") || !slices.Contains(kinds, "set_story_node") {
		t.Fatalf("decisions %v lack set_task_node / set_story_node", kinds)
	}
	if slices.Contains(kinds, modelNotServedWarningKind) {
		t.Fatalf("an accepted pin recorded a warning: %v", kinds)
	}
	// The pin survives the store round trip.
	reloaded, _ := m.Store().GetPRD(prd.ID)
	if tk, st := findTaskInPRD(reloaded, "t1"); tk.PinnedNode != "gpu-a" || st.PinnedNode != "gpu-c" {
		t.Fatalf("reloaded pins task=%q story=%q", tk.PinnedNode, st.PinnedNode)
	}
}

func TestModelRoute_NodePinWarnModeAppliesAndRecordsWarning(t *testing.T) {
	m, prd, _ := newPinManager(t, "warn", routeServedModel)

	if _, err := m.SetTaskNode(prd.ID, "t1", "gpu-b", "tester"); err != nil {
		t.Fatalf("warn mode refused the pin: %v", err)
	}
	if task, _ := pinnedNodes(t, m, prd.ID); task != "gpu-b" {
		t.Fatalf("warn mode did not apply the pin: %q", task)
	}
	kinds := pinDecisionKinds(t, m, prd.ID)
	if !slices.Contains(kinds, "set_task_node") || !slices.Contains(kinds, modelNotServedWarningKind) {
		t.Fatalf("decisions %v lack set_task_node and %s", kinds, modelNotServedWarningKind)
	}
	for _, d := range routeDecisionKinds(t, m, prd.ID) {
		if d.Kind == modelNotServedWarningKind && (!strings.Contains(d.Note, "gpu-b") || !strings.Contains(d.Note, routeServedModel)) {
			t.Fatalf("warning %q must name the node and the model", d.Note)
		}
	}
}

func TestNodePin_UnknownNodeRefused(t *testing.T) {
	for _, mode := range []string{"refuse", "warn"} {
		for _, model := range []string{routeServedModel, ""} {
			m, prd, _ := newPinManager(t, mode, model)
			before := len(routeDecisionKinds(t, m, prd.ID))

			_, terr := m.SetTaskNode(prd.ID, "t1", "ghost-node", "tester")
			_, serr := m.SetStoryNode(prd.ID, "s1", "ghost-node", "tester")
			for _, err := range []error{terr, serr} {
				if !errors.Is(err, ErrNodeUnknown) {
					t.Fatalf("mode %s model %q: error %v does not wrap ErrNodeUnknown", mode, model, err)
				}
				if !strings.Contains(err.Error(), "ghost-node") {
					t.Fatalf("mode %s: error %q must name the node", mode, err)
				}
			}
			if task, story := pinnedNodes(t, m, prd.ID); task != "" || story != "" {
				t.Fatalf("mode %s model %q: an unregistered node was stored: task=%q story=%q", mode, model, task, story)
			}
			if after := len(routeDecisionKinds(t, m, prd.ID)); after != before {
				t.Fatalf("mode %s: a refused pin recorded %d Decision(s)", mode, after-before)
			}
		}
	}
}

func TestNodePin_UnitWithoutModelIsCheckedForRegistrationOnly(t *testing.T) {
	m, prd, _ := newPinManager(t, "refuse", "")
	if _, err := m.SetTaskNode(prd.ID, "t1", "gpu-b", "tester"); err != nil {
		t.Fatalf("a registered node was refused for a unit with no model: %v", err)
	}
	if task, _ := pinnedNodes(t, m, prd.ID); task != "gpu-b" {
		t.Fatalf("pin = %q, want gpu-b", task)
	}
}

func TestNodePin_ClearingNeverAsksTheCheck(t *testing.T) {
	m, prd, f := newPinManager(t, "refuse", routeServedModel)
	if _, err := m.SetTaskNode(prd.ID, "t1", "gpu-a", "tester"); err != nil {
		t.Fatal(err)
	}
	calls := len(f.calls)
	if _, err := m.SetTaskNode(prd.ID, "t1", "", "tester"); err != nil {
		t.Fatalf("clearing the pin: %v", err)
	}
	if len(f.calls) != calls {
		t.Fatal("clearing a pin asked the model host check")
	}
	if task, _ := pinnedNodes(t, m, prd.ID); task != "" {
		t.Fatalf("pin after clearing = %q", task)
	}
}

func TestNodePin_SettersFollowTheTaskOverrideRule(t *testing.T) {
	m, prd, _ := newPinManager(t, "refuse", routeServedModel)
	stored, _ := m.Store().GetPRD(prd.ID)
	stored.Status = PRDRunning
	if err := m.Store().SavePRD(stored); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetTaskNode(prd.ID, "t1", "gpu-a", "tester"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("SetTaskNode on a running Automaton: %v", err)
	}
	if _, err := m.SetStoryNode(prd.ID, "s1", "gpu-a", "tester"); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("SetStoryNode on a running Automaton: %v", err)
	}
	stored.Status = PRDNeedsReview
	_ = m.Store().SavePRD(stored)
	if _, err := m.SetTaskNode(prd.ID, "missing", "gpu-a", "tester"); err == nil {
		t.Fatal("SetTaskNode accepted an unknown task")
	}
	if _, err := m.SetStoryNode(prd.ID, "missing", "gpu-a", "tester"); err == nil {
		t.Fatal("SetStoryNode accepted an unknown story")
	}
}

func TestNodePin_TaskOverridesStory(t *testing.T) {
	story := &Story{PinnedNode: " gpu-a "}
	if got := effectiveNodePin(&Task{}, story); got != "gpu-a" {
		t.Fatalf("story pin = %q, want gpu-a", got)
	}
	if got := effectiveNodePin(&Task{PinnedNode: "gpu-c "}, story); got != "gpu-c" {
		t.Fatalf("task pin = %q, want gpu-c over the story's", got)
	}
	if got := effectiveNodePin(&Task{PinnedNode: "   "}, story); got != "gpu-a" {
		t.Fatalf("a blank task pin = %q, want the story's gpu-a", got)
	}
	if got := effectiveNodePin(nil, nil); got != "" {
		t.Fatalf("no unit gave %q", got)
	}
	if got := effectiveNodePin(&Task{}, nil); got != "" {
		t.Fatalf("an unpinned task gave %q", got)
	}

	m, prd, _ := newPinManager(t, "refuse", routeServedModel)
	if _, err := m.SetStoryNode(prd.ID, "s1", "gpu-a", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetTaskNode(prd.ID, "t1", "gpu-c", "tester"); err != nil {
		t.Fatal(err)
	}
	cur, _ := m.Store().GetPRD(prd.ID)
	task, st := findTaskInPRD(cur, "t1")
	if got := effectiveNodePin(task, st); got != "gpu-c" {
		t.Fatalf("stored task pin = %q, want gpu-c", got)
	}
	if _, err := m.SetTaskNode(prd.ID, "t1", "", "tester"); err != nil {
		t.Fatal(err)
	}
	cur, _ = m.Store().GetPRD(prd.ID)
	task, st = findTaskInPRD(cur, "t1")
	if got := effectiveNodePin(task, st); got != "gpu-a" {
		t.Fatalf("after clearing the task pin = %q, want the story's gpu-a", got)
	}
}

func TestNodePin_ModelChangeIsCheckedAgainstThePin(t *testing.T) {
	m, prd, f := newPinManager(t, "refuse", routeServedModel)
	// gpu-a serves only routeServedModel; gpu-c serves the other model too.
	if _, err := m.SetTaskNode(prd.ID, "t1", "gpu-a", "tester"); err != nil {
		t.Fatal(err)
	}
	// Some node serves pinOtherModel, but the pinned one does not.
	_, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", pinOtherModel, "tester")
	if !errors.Is(err, ErrModelNotServed) || !strings.Contains(err.Error(), "gpu-a") {
		t.Fatalf("SetTaskLLM on a task pinned to gpu-a: %v, want ErrModelNotServed naming gpu-a", err)
	}
	if call := f.lastCall(t); call.node != "gpu-a" || call.model != pinOtherModel {
		t.Fatalf("the model check ran with %+v, want node gpu-a", call)
	}
	if cur, _ := m.Store().GetPRD(prd.ID); func() bool { tk, _ := findTaskInPRD(cur, "t1"); return tk.Model != "" }() {
		t.Fatal("a refused model was stored")
	}

	// A story pin reaches the task, and the story setter checks against it.
	if _, err := m.SetTaskNode(prd.ID, "t1", "", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetStoryNode(prd.ID, "s1", "gpu-a", "tester"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", pinOtherModel, "tester"); !errors.Is(err, ErrModelNotServed) {
		t.Fatalf("SetTaskLLM under a story pin: %v", err)
	}
	if _, _, err := m.SetStoryLLM(prd.ID, "s1", "pi", "", pinOtherModel, "tester"); !errors.Is(err, ErrModelNotServed) {
		t.Fatalf("SetStoryLLM on a pinned story: %v", err)
	}
	// A model the pinned node serves is accepted.
	if _, _, err := m.SetStoryLLM(prd.ID, "s1", "pi", "", routeServedModel, "tester"); err != nil {
		t.Fatalf("SetStoryLLM with a served model: %v", err)
	}

	// The Automaton-level setter checks the pins of the tasks that inherit.
	if _, _, err := m.SetStoryLLM(prd.ID, "s1", "", "", "", "tester"); err != nil {
		t.Fatalf("clearing the story worker: %v", err)
	}
	if _, _, err := m.SetPRDLLM(prd.ID, nil, nil, strp(pinOtherModel), nil, nil, "tester"); !errors.Is(err, ErrModelNotServed) || !strings.Contains(err.Error(), "gpu-a") {
		t.Fatalf("SetPRDLLM with a pinned inheriting task: %v, want ErrModelNotServed naming gpu-a", err)
	}
	if cur, _ := m.Store().GetPRD(prd.ID); cur.Model != routeServedModel {
		t.Fatalf("Automaton model changed to %q after a refusal", cur.Model)
	}
}

// pinPlacement is a ledger with two candidate nodes, slow and fast, both with
// room for two workers, and a resolver whose candidate list a test can change.
type pinPlacement struct {
	m   *Manager
	led *capacity.Ledger
	mu  sync.Mutex
	// candidates is what the resolver reports.
	candidates []capacity.NodeChoice
}

func newPinPlacement(t *testing.T) (*pinPlacement, *PRD) {
	t.Helper()
	m, _, _, _ := apiFixture(t)
	led := capacity.New(capacity.Options{PollInterval: 10 * time.Millisecond})
	for _, n := range []string{"slow", "fast", "extra"} {
		led.SetLimit("node:"+n, 2)
	}
	p := &pinPlacement{m: m, led: led, candidates: []capacity.NodeChoice{{Name: "slow", Priority: 10}, {Name: "fast", Priority: 90}}}
	m.SetCapacity(led, func(backend, model string) ([]string, string) { return []string{"host", "llm:pool"}, "" })
	m.SetCapacityChoices(func(backend, model string) []capacity.NodeChoice {
		p.mu.Lock()
		defer p.mu.Unlock()
		return slices.Clone(p.candidates)
	})
	prd := newPRDWithTasks(t, m, 3)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Status = PRDNeedsReview
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatal(err)
	}
	cur, _ = m.Store().GetPRD(prd.ID)
	return p, cur
}

func (p *pinPlacement) setCandidates(c ...capacity.NodeChoice) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.candidates = c
}

// occupy holds one slot of node for holder, outside any task.
func (p *pinPlacement) occupy(t *testing.T, holder, node string) {
	t.Helper()
	req := capacity.Request{Holder: holder, Pools: []string{"host", "llm:pool", "node:" + node}, Node: node}
	if err := p.led.Acquire(context.Background(), req, time.Second, nil, nil); err != nil {
		t.Fatalf("occupy %s: %v", node, err)
	}
}

func (p *pinPlacement) leaseNode(taskID string) string {
	if ls := p.led.LeaseOf(taskID); ls != nil {
		return ls.Node
	}
	return ""
}

func (p *pinPlacement) task(t *testing.T, prdID string, i int) *Task {
	t.Helper()
	cur, _ := p.m.Store().GetPRD(prdID)
	task := cur.Story[0].Tasks[i]
	return &task
}

func TestNodePin_PlacementOnlyUsesPinnedNode(t *testing.T) {
	p, prd := newPinPlacement(t)
	// fast is the busier node, so unpinned placement would choose slow.
	p.occupy(t, "other-1", "fast")

	control := p.task(t, prd.ID, 0)
	if err := p.m.admit(context.Background(), prd, control, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if got := p.leaseNode(control.ID); got != "slow" {
		t.Fatalf("unpinned lease node = %q, want slow (the less loaded node)", got)
	}
	p.m.releaseCapacity(control.ID)

	if _, err := p.m.SetTaskNode(prd.ID, p.task(t, prd.ID, 1).ID, "fast", "tester"); err != nil {
		t.Fatal(err)
	}
	pinned := p.task(t, prd.ID, 1)
	if err := p.m.admit(context.Background(), prd, pinned, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if got := p.leaseNode(pinned.ID); got != "fast" {
		t.Fatalf("pinned lease node = %q, want fast although slow is less loaded", got)
	}
	if got := p.m.heldNode(pinned.ID); got != "fast" {
		t.Fatalf("heldNode = %q, the spawn must start on the pinned node", got)
	}
}

func TestNodePin_StoryPinReachesTasks(t *testing.T) {
	p, prd := newPinPlacement(t)
	p.occupy(t, "other-1", "fast")
	if _, err := p.m.SetStoryNode(prd.ID, prd.Story[0].ID, "fast", "tester"); err != nil {
		t.Fatal(err)
	}
	task := p.task(t, prd.ID, 0)
	if err := p.m.admit(context.Background(), prd, task, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if got := p.leaseNode(task.ID); got != "fast" {
		t.Fatalf("lease node = %q, want the story's pin fast", got)
	}
}

func TestNodePin_CandidateJoiningMidWaitIsStillExcluded(t *testing.T) {
	p, prd := newPinPlacement(t)
	// The pinned node is full, so the task waits.
	p.led.SetLimit("node:fast", 1)
	p.occupy(t, "other-1", "fast")
	if _, err := p.m.SetTaskNode(prd.ID, p.task(t, prd.ID, 0).ID, "fast", "tester"); err != nil {
		t.Fatal(err)
	}
	task := p.task(t, prd.ID, 0)

	done := make(chan error, 1)
	go func() {
		done <- p.m.admit(context.Background(), prd, task, "pi", "ollama/q", capacity.RoleWorker)
	}()

	// slow has room, yet the pinned task must not take it.
	time.Sleep(100 * time.Millisecond)
	if got := p.leaseNode(task.ID); got != "" {
		t.Fatalf("a task pinned to a full node was placed on %q", got)
	}
	// A new candidate joins while the task waits: it must be excluded as well.
	p.setCandidates(capacity.NodeChoice{Name: "slow", Priority: 10}, capacity.NodeChoice{Name: "fast", Priority: 90}, capacity.NodeChoice{Name: "extra", Priority: 50})
	time.Sleep(150 * time.Millisecond)
	if got := p.leaseNode(task.ID); got != "" {
		t.Fatalf("a candidate that joined mid-wait took the pinned task: node %q", got)
	}
	select {
	case err := <-done:
		t.Fatalf("admit returned early with %v", err)
	default:
	}

	p.led.Release("other-1")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("admit: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the pinned task was not admitted after its node freed up")
	}
	if got := p.leaseNode(task.ID); got != "fast" {
		t.Fatalf("lease node = %q, want fast", got)
	}
}

func TestNodePin_ClearingPinRestoresPlacement(t *testing.T) {
	p, prd := newPinPlacement(t)
	p.occupy(t, "other-1", "fast")
	taskID := p.task(t, prd.ID, 0).ID

	if _, err := p.m.SetTaskNode(prd.ID, taskID, "fast", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := p.m.admit(context.Background(), prd, p.task(t, prd.ID, 0), "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if got := p.leaseNode(taskID); got != "fast" {
		t.Fatalf("pinned lease node = %q, want fast", got)
	}
	p.m.releaseCapacity(taskID)

	if _, err := p.m.SetTaskNode(prd.ID, taskID, "", "tester"); err != nil {
		t.Fatal(err)
	}
	if err := p.m.admit(context.Background(), prd, p.task(t, prd.ID, 0), "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatal(err)
	}
	if got := p.leaseNode(taskID); got != "slow" {
		t.Fatalf("lease node after clearing the pin = %q, want slow (the less loaded node)", got)
	}
}

func TestNodePin_VerifierAdmissionIsNotPinned(t *testing.T) {
	p, prd := newPinPlacement(t)
	p.occupy(t, "other-1", "fast")
	taskID := p.task(t, prd.ID, 0).ID
	if _, err := p.m.SetTaskNode(prd.ID, taskID, "fast", "tester"); err != nil {
		t.Fatal(err)
	}
	verifier := p.task(t, prd.ID, 0)
	if err := p.m.admit(context.Background(), prd, verifier, "pi", "ollama/q", capacity.RoleVerifier); err != nil {
		t.Fatal(err)
	}
	if got := p.leaseNode(taskID); got != "slow" {
		t.Fatalf("a verifier admission was pinned: lease node %q, want slow", got)
	}
}

func TestNodePin_PinnedNodeUnavailableFailsWithTheNode(t *testing.T) {
	p, prd := newPinPlacement(t)
	taskID := p.task(t, prd.ID, 0).ID
	// Pinned directly: the node is not a candidate of this backend and model.
	stored, _ := p.m.Store().GetPRD(prd.ID)
	stored.Story[0].Tasks[0].PinnedNode = "ghost"
	if err := p.m.Store().SavePRD(stored); err != nil {
		t.Fatal(err)
	}
	err := p.m.admit(context.Background(), stored, p.task(t, prd.ID, 0), "pi", "ollama/q", capacity.RoleWorker)
	if !errors.Is(err, ErrPinnedNodeUnavailable) {
		t.Fatalf("admit = %v, want an error wrapping ErrPinnedNodeUnavailable", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("error %q must name the node", err)
	}
	if p.led.LeaseOf(taskID) != nil {
		t.Fatal("a task whose pin cannot be honoured holds a lease")
	}

	// The failover exclude list names the pinned node: the task must not move.
	stored.Story[0].Tasks[0].PinnedNode = "fast"
	if err := p.m.Store().SavePRD(stored); err != nil {
		t.Fatal(err)
	}
	err = p.m.acquireLease(context.Background(), stored, p.task(t, prd.ID, 0), "pi", "ollama/q", capacity.RoleWorker, []string{"fast"}, "")
	if !errors.Is(err, ErrPinnedNodeUnavailable) || !strings.Contains(err.Error(), "fast") {
		t.Fatalf("acquireLease with the pinned node excluded = %v", err)
	}
	if p.led.LeaseOf(taskID) != nil {
		t.Fatal("a failed-over pinned task holds a lease")
	}
}

func TestNodePin_SingleCandidateMustBeThePin(t *testing.T) {
	m, led := poolFixture(t, false)
	prd := newPRDWithTasks(t, m, 1)
	cur, _ := m.Store().GetPRD(prd.ID)
	cur.Story[0].Tasks[0].PinnedNode = "elsewhere"
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatal(err)
	}
	cur, _ = m.Store().GetPRD(prd.ID)
	task := cur.Story[0].Tasks[0]
	err := m.admit(context.Background(), cur, &task, "pi", "ollama/q", capacity.RoleWorker)
	if !errors.Is(err, ErrPinnedNodeUnavailable) || !strings.Contains(err.Error(), "elsewhere") {
		t.Fatalf("admit on a single node entry pinned elsewhere = %v", err)
	}
	if led.LeaseOf(task.ID) != nil {
		t.Fatal("the task holds a lease")
	}

	cur.Story[0].Tasks[0].PinnedNode = "solo"
	if err := m.Store().SavePRD(cur); err != nil {
		t.Fatal(err)
	}
	cur, _ = m.Store().GetPRD(prd.ID)
	task = cur.Story[0].Tasks[0]
	if err := m.admit(context.Background(), cur, &task, "pi", "ollama/q", capacity.RoleWorker); err != nil {
		t.Fatalf("admit pinned to the only node: %v", err)
	}
	if ls := led.LeaseOf(task.ID); ls == nil || ls.Node != "solo" {
		t.Fatalf("lease = %+v, want node solo", ls)
	}
}

func TestNodePin_ApplyNodePinExcludesEveryOtherCandidate(t *testing.T) {
	choices := []capacity.NodeChoice{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	req, err := applyNodePin(capacity.Request{NodeChoices: choices}, "b", []string{"z"})
	if err != nil {
		t.Fatal(err)
	}
	got := slices.Clone(req.ExcludeNodes)
	slices.Sort(got)
	if !slices.Equal(got, []string{"a", "c", "z"}) {
		t.Fatalf("ExcludeNodes = %v, want a, c and the failover z", got)
	}
	if same, err := applyNodePin(capacity.Request{NodeChoices: choices}, "", nil); err != nil || len(same.ExcludeNodes) != 0 {
		t.Fatalf("an empty pin changed the request: %+v, %v", same, err)
	}
	if _, err := applyNodePin(capacity.Request{NodeChoices: choices}, "d", nil); !errors.Is(err, ErrPinnedNodeUnavailable) {
		t.Fatalf("a pin outside the candidates: %v", err)
	}
	if _, err := applyNodePin(capacity.Request{NodeChoices: choices}, "b", []string{"b"}); !errors.Is(err, ErrPinnedNodeUnavailable) {
		t.Fatalf("a pin in the failover list: %v", err)
	}
}

func TestNodePin_UnpinnedPlacementIsUntouched(t *testing.T) {
	p, prd := newPinPlacement(t)
	p.occupy(t, "other-1", "fast")
	task := p.task(t, prd.ID, 0)
	req := capacity.Request{Holder: task.ID, Pools: []string{"host", "llm:pool"}, NodeChoices: p.candidates}
	got := p.m.placementRefresh(req, "pi", "ollama/q", nil)()
	if len(got.ExcludeNodes) != 0 || len(got.NodeChoices) != 2 {
		t.Fatalf("an unpinned refresh changed the request: %+v", got)
	}
}

// An unknown Automaton id is refused with a message that names the Automaton
// and starts in lower case, as Go error strings do.
func TestNodePin_UnknownAutomatonMessageNamesAutomaton(t *testing.T) {
	m, _, _ := newPinManager(t, "refuse", routeServedModel)
	for name, call := range map[string]func() error{
		"task":  func() error { _, err := m.SetTaskNode("missing-id", "t", "gpu-a", "tester"); return err },
		"story": func() error { _, err := m.SetStoryNode("missing-id", "s", "gpu-a", "tester"); return err },
	} {
		err := call()
		if err == nil {
			t.Fatalf("%s: want an error for an unknown Automaton", name)
		}
		msg := err.Error()
		if !strings.Contains(msg, "Automaton") || !strings.Contains(msg, "missing-id") || strings.Contains(msg, "PRD") {
			t.Fatalf("%s: message %q should name the Automaton and its id", name, msg)
		}
		if msg[0] < 'a' || msg[0] > 'z' {
			t.Fatalf("%s: message %q should start in lower case", name, msg)
		}
	}
}
