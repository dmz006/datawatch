package autonomous

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

const (
	routeServedModel   = "ollama/qwen3.8:27b"
	routeUnservedModel = "ollama/ghost-model:7b"
	routeNotServedMsg  = `no registered node serves model "ollama/ghost-model:7b"; nodes checked: gpu-a (serves 1 model, none is ollama/ghost-model:7b), gpu-b (could not read models: connection refused)`
)

// routeFake is a ModelHostCheckFn that serves routeServedModel and refuses
// anything else, recording every call. Each call also tries the manager lock:
// the check must never run with m.mu held.
type routeFake struct {
	t       *testing.T
	m       *Manager
	mu      sync.Mutex
	calls   []routeCall
	applies bool
}

type routeCall struct{ backend, model, node string }

func (f *routeFake) check(_ context.Context, backend, model, node string) ModelHostVerdict {
	f.t.Helper()
	if !f.m.mu.TryLock() {
		f.t.Error("the model host check ran with the manager lock held")
	} else {
		f.m.mu.Unlock()
	}
	f.mu.Lock()
	f.calls = append(f.calls, routeCall{backend, model, node})
	f.mu.Unlock()
	v := ModelHostVerdict{Applicable: f.applies, Model: model, Node: node}
	if model != routeServedModel {
		v.Refused = true
		v.Message = strings.ReplaceAll(routeNotServedMsg, routeUnservedModel, model)
	}
	return v
}

func (f *routeFake) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// routeAlert records the alerts the manager raises.
type routeAlert struct {
	mu     sync.Mutex
	titles []string
	bodies []string
	levels []string
}

func (a *routeAlert) sink(level, title, body string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.levels = append(a.levels, level)
	a.titles = append(a.titles, title)
	a.bodies = append(a.bodies, body)
	return "alert-1"
}

func (a *routeAlert) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.titles)
}

// newRouteManager returns a manager holding a pi Automaton in review whose
// worker, and every verifier level, already hold routeServedModel.
func newRouteManager(t *testing.T, mode string, alertOn bool) (*Manager, *PRD, *routeFake, *routeAlert) {
	t.Helper()
	m := newRefusalManager(t)
	prd := seedRefusalPRD(t, m, "pi", routeServedModel)
	f := &routeFake{t: t, m: m, applies: true}
	m.SetModelHostCheck(f.check)
	m.SetModelRoutingSettings(func() ModelRoutingSettings {
		return ModelRoutingSettings{RefusalMode: mode, AlertOnRefusal: alertOn, VerifierFollowsWorkerNode: true}
	})
	al := &routeAlert{}
	m.SetAlertSink(al.sink)
	return m, prd, f, al
}

// routeSetter is one pin path: it pins model and reports which field of the
// record that pin writes.
type routeSetter struct {
	name string
	pin  func(m *Manager, prdID, model string) error
	read func(s refusalSnapshot) string
}

func routeSetters() []routeSetter {
	return []routeSetter{
		{"set_task_llm", func(m *Manager, id, model string) error {
			_, _, err := m.SetTaskLLM(id, "t1", "", "", model, "test")
			return err
		}, func(s refusalSnapshot) string { return s.taskModel }},
		{"set_story_llm", func(m *Manager, id, model string) error {
			_, _, err := m.SetStoryLLM(id, "s1", "pi", "", model, "test")
			return err
		}, func(s refusalSnapshot) string { return s.storyModel }},
		{"set_prd_llm", func(m *Manager, id, model string) error {
			_, _, err := m.SetPRDLLM(id, nil, nil, strp(model), nil, nil, "test")
			return err
		}, func(s refusalSnapshot) string { return s.prdModel }},
		{"set_task_verification_llm", func(m *Manager, id, model string) error {
			_, err := m.SetTaskVerificationLLM(id, "t1", strp("pi"), strp(model), nil, "test")
			return err
		}, func(s refusalSnapshot) string { return s.taskVerificationModel }},
		{"set_story_verification_llm", func(m *Manager, id, model string) error {
			_, err := m.SetStoryVerificationLLM(id, "s1", strp("pi"), strp(model), nil, "test")
			return err
		}, func(s refusalSnapshot) string { return s.storyVerificationModel }},
		{"set_prd_verification_llm", func(m *Manager, id, model string) error {
			_, err := m.SetPRDVerificationLLM(id, strp("pi"), strp(model), nil, "test")
			return err
		}, func(s refusalSnapshot) string { return s.prdVerificationModel }},
	}
}

func routeDecisionKinds(t *testing.T, m *Manager, prdID string) []Decision {
	t.Helper()
	prd, ok := m.Store().GetPRD(prdID)
	if !ok {
		t.Fatalf("prd %q not found", prdID)
	}
	return prd.Decisions
}

func TestModelRoute_RefusesWhenNoNodeServesModel(t *testing.T) {
	m, prd, f, _ := newRouteManager(t, "refuse", true)
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeServedModel, "seed"); err != nil {
		t.Fatalf("seed pin refused: %v", err)
	}
	before := takeRefusalSnapshot(t, m, prd.ID)

	_, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test")
	if !errors.Is(err, ErrModelNotServed) {
		t.Fatalf("err = %v, want one wrapping ErrModelNotServed", err)
	}
	after := takeRefusalSnapshot(t, m, prd.ID)
	assertSnapshotUnchanged(t, before, after)
	if after.taskModel != routeServedModel {
		t.Fatalf("task model = %q, want the old %q", after.taskModel, routeServedModel)
	}
	if f.callCount() == 0 {
		t.Fatal("the check was never asked")
	}
}

func TestModelRoute_AcceptsWhenANodeServesModel(t *testing.T) {
	m, prd, f, al := newRouteManager(t, "refuse", true)
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeServedModel, "test"); err != nil {
		t.Fatalf("pin of a served model refused: %v", err)
	}
	if got := takeRefusalSnapshot(t, m, prd.ID).taskModel; got != routeServedModel {
		t.Fatalf("task model = %q, want %q", got, routeServedModel)
	}
	if al.count() != 0 {
		t.Fatalf("a served model raised %d alert(s)", al.count())
	}
	if f.callCount() != 1 {
		t.Fatalf("check calls = %d, want 1", f.callCount())
	}
	// The effective backend is resolved the way a run does: the task names
	// none, so it inherits the Automaton's pi.
	if got := f.calls[0]; got.backend != "pi" || got.model != routeServedModel || got.node != "" {
		t.Fatalf("check call = %+v, want backend pi, the pinned model and no node", got)
	}
}

func TestModelRoute_NotApplicableAndUnwiredAndClearedPass(t *testing.T) {
	m, prd, f, al := newRouteManager(t, "refuse", true)
	f.applies = false
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test"); err != nil {
		t.Fatalf("a not applicable verdict refused the pin: %v", err)
	}
	f.applies = true
	// Clearing a pin (empty model) is never checked.
	before := f.callCount()
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", "", "test"); err != nil {
		t.Fatalf("clearing the pin refused: %v", err)
	}
	if f.callCount() != before {
		t.Fatal("clearing a pin asked the check")
	}
	if al.count() != 0 {
		t.Fatalf("%d alert(s) raised", al.count())
	}
	// No check wired lets everything through.
	m.SetModelHostCheck(nil)
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test"); err != nil {
		t.Fatalf("pin refused with no check wired: %v", err)
	}
}

func TestModelRoute_WarnModeProceedsAndRecords(t *testing.T) {
	m, prd, _, _ := newRouteManager(t, "warn", true)
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "operator"); err != nil {
		t.Fatalf("warn mode refused the pin: %v", err)
	}
	if got := takeRefusalSnapshot(t, m, prd.ID).taskModel; got != routeUnservedModel {
		t.Fatalf("task model = %q, want the pinned %q", got, routeUnservedModel)
	}
	var warn *Decision
	for _, d := range routeDecisionKinds(t, m, prd.ID) {
		if d.Kind == "model_not_served_warning" {
			d := d
			warn = &d
		}
	}
	if warn == nil {
		t.Fatal("no model_not_served_warning decision recorded")
	}
	if warn.Note != routeNotServedMsg {
		t.Fatalf("warning note = %q, want the verdict message", warn.Note)
	}
}

func TestModelRoute_RefusalRaisesAlertWhenConfigured(t *testing.T) {
	t.Run("alert on", func(t *testing.T) {
		m, prd, _, al := newRouteManager(t, "refuse", true)
		if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test"); !errors.Is(err, ErrModelNotServed) {
			t.Fatalf("err = %v", err)
		}
		if al.count() != 1 {
			t.Fatalf("alerts = %d, want exactly 1", al.count())
		}
		if al.titles[0] != "Automaton model not served by any node" {
			t.Fatalf("alert title = %q", al.titles[0])
		}
		if !strings.Contains(al.bodies[0], routeNotServedMsg) {
			t.Fatalf("alert body does not carry the message: %q", al.bodies[0])
		}
		if strings.Contains(al.bodies[0], "PRD") {
			t.Fatalf("alert body says PRD: %q", al.bodies[0])
		}
	})
	t.Run("alert off", func(t *testing.T) {
		m, prd, _, al := newRouteManager(t, "refuse", false)
		if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test"); !errors.Is(err, ErrModelNotServed) {
			t.Fatalf("err = %v", err)
		}
		if al.count() != 0 {
			t.Fatalf("alerts = %d, want none with alert_on_refusal off", al.count())
		}
	})
	t.Run("warn mode alerts too", func(t *testing.T) {
		m, prd, _, al := newRouteManager(t, "warn", true)
		if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test"); err != nil {
			t.Fatal(err)
		}
		if al.count() != 1 {
			t.Fatalf("alerts = %d, want 1", al.count())
		}
	})
	t.Run("no sink wired", func(t *testing.T) {
		m, prd, _, _ := newRouteManager(t, "refuse", true)
		m.SetAlertSink(nil)
		if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test"); !errors.Is(err, ErrModelNotServed) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestModelRoute_RefusalNamesModelAndNodesChecked(t *testing.T) {
	m, prd, _, _ := newRouteManager(t, "refuse", true)
	_, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test")
	if err == nil {
		t.Fatal("pin was not refused")
	}
	for _, want := range []string{routeUnservedModel, "gpu-a", "gpu-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err.Error(), want)
		}
	}
	if err.Error() != routeNotServedMsg {
		t.Fatalf("error text = %q, want the verdict message", err.Error())
	}
}

func TestModelRoute_DefaultSettingsRefuseAndAlert(t *testing.T) {
	m, prd, _, al := newRouteManager(t, "refuse", true)
	m.SetModelRoutingSettings(nil)
	if _, _, err := m.SetTaskLLM(prd.ID, "t1", "", "", routeUnservedModel, "test"); !errors.Is(err, ErrModelNotServed) {
		t.Fatalf("err = %v, want a refusal with the default settings", err)
	}
	if al.count() != 1 {
		t.Fatalf("alerts = %d, want 1 with the default settings", al.count())
	}
}

// TestModelRoute_EverySetterRefusesAndWarns covers each setter that stores a
// worker or verifier model: a refusal changes nothing and records nothing, and
// a warning is applied and recorded.
func TestModelRoute_EverySetterRefusesAndWarns(t *testing.T) {
	for _, s := range routeSetters() {
		s := s
		t.Run(s.name+" refuses", func(t *testing.T) {
			m, prd, _, al := newRouteManager(t, "refuse", true)
			before := takeRefusalSnapshot(t, m, prd.ID)
			err := s.pin(m, prd.ID, routeUnservedModel)
			if !errors.Is(err, ErrModelNotServed) {
				t.Fatalf("err = %v, want one wrapping ErrModelNotServed", err)
			}
			assertSnapshotUnchanged(t, before, takeRefusalSnapshot(t, m, prd.ID))
			if got := s.read(takeRefusalSnapshot(t, m, prd.ID)); got == routeUnservedModel {
				t.Fatalf("the refused model was stored: %q", got)
			}
			for _, d := range routeDecisionKinds(t, m, prd.ID) {
				if d.Kind == "model_not_served_warning" {
					t.Fatal("a refusal recorded a warning")
				}
			}
			if al.count() != 1 {
				t.Fatalf("alerts = %d, want 1", al.count())
			}
		})
		t.Run(s.name+" accepts a served model", func(t *testing.T) {
			m, prd, _, al := newRouteManager(t, "refuse", true)
			if err := s.pin(m, prd.ID, routeServedModel); err != nil {
				t.Fatalf("served model refused: %v", err)
			}
			if got := s.read(takeRefusalSnapshot(t, m, prd.ID)); got != routeServedModel {
				t.Fatalf("stored model = %q, want %q", got, routeServedModel)
			}
			if al.count() != 0 {
				t.Fatalf("alerts = %d, want 0", al.count())
			}
		})
		t.Run(s.name+" warns", func(t *testing.T) {
			m, prd, _, _ := newRouteManager(t, "warn", true)
			if err := s.pin(m, prd.ID, routeUnservedModel); err != nil {
				t.Fatalf("warn mode refused: %v", err)
			}
			if got := s.read(takeRefusalSnapshot(t, m, prd.ID)); got != routeUnservedModel {
				t.Fatalf("stored model = %q, want the warned %q", got, routeUnservedModel)
			}
			found := false
			for _, d := range routeDecisionKinds(t, m, prd.ID) {
				if d.Kind == "model_not_served_warning" && strings.Contains(d.Note, routeUnservedModel) {
					found = true
				}
			}
			if !found {
				t.Fatal("no model_not_served_warning decision naming the model")
			}
		})
	}
}

// A call that leaves the model out is not checked, so an effort-only edit is
// never refused over a model it did not touch.
func TestModelRoute_CallWithoutModelIsNotChecked(t *testing.T) {
	m, prd, f, _ := newRouteManager(t, "refuse", true)
	if _, _, err := m.SetPRDLLM(prd.ID, nil, strp("high"), nil, nil, nil, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetTaskVerificationLLM(prd.ID, "t1", nil, nil, strp("high"), "test"); err != nil {
		t.Fatal(err)
	}
	if f.callCount() != 0 {
		t.Fatalf("check calls = %d, want 0", f.callCount())
	}
}
