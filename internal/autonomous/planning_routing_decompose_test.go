package autonomous

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// planCall is one fake decompose call.
type planCall struct{ backend, model, spec string }

// fakeDecomposer is the fake decompose function: it replies with the next
// scripted answer (the last one repeats) and records every call.
type fakeDecomposer struct {
	calls []planCall
	resp  []string
	err   error
}

func (f *fakeDecomposer) fn(req DecomposeRequest) (string, error) {
	f.calls = append(f.calls, planCall{req.Backend, req.Model, req.Spec})
	if f.err != nil {
		return "", f.err
	}
	i := len(f.calls) - 1
	if i >= len(f.resp) {
		i = len(f.resp) - 1
	}
	return f.resp[i], nil
}

func routingTestConfig(mut func(*config.PlanningRoutingConfig)) Config {
	cfg := DefaultConfig()
	cfg.PlanningBackend = "global-b"
	cfg.PlanningModel = "global-m"
	cfg.PlanningRouting = config.PlanningRoutingConfig{
		Enabled: true,
		Rules: []config.PlanningRule{{
			Name:     "r1",
			Planners: []config.PlannerRef{{Backend: "routed-b", Model: "routed-m"}, {Backend: "esc-b", Model: "esc-m"}},
		}},
	}
	if mut != nil {
		mut(&cfg.PlanningRouting)
	}
	return cfg
}

func newRoutingManager(t *testing.T, cfg Config, f *fakeDecomposer) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir(), cfg, f.fn)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func newRoutingPRD(t *testing.T, m *Manager) *PRD {
	t.Helper()
	prd, err := m.CreatePRD(ladderTestSpec, "", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	return prd
}

func TestPlanningRoutingExplicitPRDFieldWinsOverRouting(t *testing.T) {
	cfg := routingTestConfig(nil)
	prd := &PRD{DecompositionProfile: "prd-b", DecompositionModel: "prd-m", Spec: ladderTestSpec}
	rp := routedPlanForDecompose(prd, nil, cfg)
	if rp.Decision != nil || rp.UseLadder {
		t.Fatalf("an explicit PRD planner must skip routing, got %+v", rp)
	}
	if rp.Backend != "global-b" || rp.Model != "global-m" {
		t.Fatalf("PRD fields are applied by the caller over the global pair, got %s/%s", rp.Backend, rp.Model)
	}

	f := &fakeDecomposer{resp: []string{ladderFixedPlanJSON}}
	m := newRoutingManager(t, cfg, f)
	p := newRoutingPRD(t, m)
	p.DecompositionProfile, p.DecompositionModel = "prd-b", "prd-m"
	if err := m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	got, err := m.Decompose(p.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0].backend != "prd-b" || f.calls[0].model != "prd-m" {
		t.Fatalf("explicit PRD planner must be used once, got %+v", f.calls)
	}
	if got.PlanningRecord != nil {
		t.Fatalf("routing was not consulted, record must be nil, got %+v", got.PlanningRecord)
	}
}

func TestPlanningRoutingAutomatonFieldWinsOverRouting(t *testing.T) {
	cfg := routingTestConfig(nil)
	prd := &PRD{Spec: ladderTestSpec}
	for _, auto := range []*Automation{
		{PlanningBackend: "auto-b", PlanningModel: "auto-m"},
		{PlanningModel: "auto-m"},
	} {
		rp := routedPlanForDecompose(prd, auto, cfg)
		wantB, wantM := effectivePlanning(auto, cfg)
		if rp.Decision != nil || rp.UseLadder || rp.Backend != wantB || rp.Model != wantM {
			t.Fatalf("Automaton planner must win over routing: got %+v want %s/%s", rp, wantB, wantM)
		}
	}
}

func TestPlanningRoutingDisabledMatchesOldResolution(t *testing.T) {
	cfg := routingTestConfig(func(r *config.PlanningRoutingConfig) { r.Enabled = false })
	prd := &PRD{Spec: ladderTestSpec}
	rp := routedPlanForDecompose(prd, nil, cfg)
	wantB, wantM := effectivePlanning(nil, cfg)
	if rp.Backend != wantB || rp.Model != wantM || rp.Decision != nil || rp.UseLadder {
		t.Fatalf("disabled routing must equal effectivePlanning, got %+v", rp)
	}

	f := &fakeDecomposer{resp: []string{ladderFixedPlanJSON}}
	m := newRoutingManager(t, cfg, f)
	p := newRoutingPRD(t, m)
	got, err := m.Decompose(p.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0].backend != "global-b" || f.calls[0].model != "global-m" {
		t.Fatalf("disabled routing must use the global planner, got %+v", f.calls)
	}
	if got.PlanningRecord != nil {
		t.Fatalf("no record when routing is off, got %+v", got.PlanningRecord)
	}
}

func TestPlanningRoutingShadowKeepsPlannerAndRecordsRoute(t *testing.T) {
	cfg := routingTestConfig(func(r *config.PlanningRoutingConfig) { r.Shadow = true })
	f := &fakeDecomposer{resp: []string{ladderFixedPlanJSON}}
	m := newRoutingManager(t, cfg, f)
	p := newRoutingPRD(t, m)
	got, err := m.Decompose(p.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if len(f.calls) != 1 || f.calls[0].backend != "global-b" || f.calls[0].model != "global-m" {
		t.Fatalf("shadow mode must plan exactly as before, got %+v", f.calls)
	}
	rec := got.PlanningRecord
	if rec == nil || !rec.Shadow || rec.Fallback || rec.RuleName != "r1" {
		t.Fatalf("shadow decision must be recorded, got %+v", rec)
	}
	if rec.PlannerBackend != "global-b" || rec.PlannerModel != "global-m" || len(rec.Attempts) != 0 {
		t.Fatalf("shadow record names the planner actually used, got %+v", rec)
	}
}

func TestPlanningRoutingEnabledRunsLadder(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		cfg := routingTestConfig(nil)
		f := &fakeDecomposer{resp: []string{ladderFlaggedPlanJSON, ladderFixedPlanJSON}}
		m := newRoutingManager(t, cfg, f)
		p := newRoutingPRD(t, m)
		var got *PRD
		var err error
		var cbStories int
		if streaming {
			got, err = m.decomposeStreamingCore(p.ID, func(i, n int, s Story) { cbStories++ })
		} else {
			got, err = m.Decompose(p.ID)
		}
		if err != nil {
			t.Fatalf("streaming=%v decompose: %v", streaming, err)
		}
		if len(f.calls) != 2 {
			t.Fatalf("streaming=%v: ladder = plan then touch-up, got %d calls", streaming, len(f.calls))
		}
		for _, c := range f.calls {
			if c.backend != "routed-b" || c.model != "routed-m" {
				t.Fatalf("streaming=%v: the routed planner must run, got %+v", streaming, f.calls)
			}
		}
		if !strings.Contains(f.calls[1].spec, "TOUCH-UP") {
			t.Fatalf("second call must be the touch-up, got %q", f.calls[1].spec)
		}
		if got.Status != PRDNeedsReview {
			t.Fatalf("streaming=%v: status %s", streaming, got.Status)
		}
		rec := got.PlanningRecord
		if rec == nil || rec.Shadow || rec.Fallback || rec.RuleName != "r1" || rec.PlannerBackend != "routed-b" {
			t.Fatalf("streaming=%v: ladder record wrong: %+v", streaming, rec)
		}
		if len(rec.Attempts) != 2 || rec.Attempts[0].Kind != LadderAttemptPlan || rec.Attempts[1].Kind != LadderAttemptTouchUp || rec.FindingCount != 0 {
			t.Fatalf("streaming=%v: attempts wrong: %+v", streaming, rec)
		}
		if streaming && cbStories != 1 {
			t.Fatalf("streaming callback must see the stored stories, got %d", cbStories)
		}
	}
}

func TestPlanningRoutingRecordStoredOnFallbackAndFailure(t *testing.T) {
	// Fallback: no rule matches, the normal planner runs, the decision is stored.
	cfg := routingTestConfig(func(r *config.PlanningRoutingConfig) { r.Rules[0].Match.Kind = []string{"no-such-kind"} })
	f := &fakeDecomposer{resp: []string{ladderFixedPlanJSON}}
	m := newRoutingManager(t, cfg, f)
	p := newRoutingPRD(t, m)
	got, err := m.Decompose(p.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	if f.calls[0].backend != "global-b" || got.PlanningRecord == nil || !got.PlanningRecord.Fallback {
		t.Fatalf("fallback must keep the planner and store the record: %+v %+v", f.calls, got.PlanningRecord)
	}
	if stored, _ := m.store.GetPRD(p.ID); stored.PlanningRecord == nil {
		t.Fatal("record must be persisted on the PRD")
	}

	// Ladder failure path: every planner returns garbage.
	f2 := &fakeDecomposer{resp: []string{"not json"}}
	m2 := newRoutingManager(t, routingTestConfig(nil), f2)
	p2 := newRoutingPRD(t, m2)
	if _, err := m2.Decompose(p2.ID); err == nil {
		t.Fatal("garbage plans must fail the decompose")
	}
	stored, _ := m2.store.GetPRD(p2.ID)
	if stored.Status != PRDDraft || stored.PlanningRecord == nil || len(stored.PlanningRecord.Attempts) == 0 {
		t.Fatalf("failure path must store the record with its attempts: %s %+v", stored.Status, stored.PlanningRecord)
	}
	if n := len(stored.PlanningRecord.Attempts); n > PlanningRecordMaxAttempts {
		t.Fatalf("attempts capped at %d, got %d", PlanningRecordMaxAttempts, n)
	}

	// Shadow failure path: single attempt fails, record still stored.
	cfg3 := routingTestConfig(func(r *config.PlanningRoutingConfig) { r.Shadow = true })
	f3 := &fakeDecomposer{resp: []string{"not json"}}
	m3 := newRoutingManager(t, cfg3, f3)
	p3 := newRoutingPRD(t, m3)
	if _, err := m3.Decompose(p3.ID); err == nil {
		t.Fatal("garbage plan must fail")
	}
	if s3, _ := m3.store.GetPRD(p3.ID); s3.PlanningRecord == nil || !s3.PlanningRecord.Shadow {
		t.Fatalf("shadow failure must store the record, got %+v", s3.PlanningRecord)
	}
}

func TestPlanningRoutingLadderPlannerErrorsRecorded(t *testing.T) {
	// A planner error escalates inside the ladder rather than ending it.
	f := &fakeDecomposer{err: errors.New("boom")}
	m := newRoutingManager(t, routingTestConfig(nil), f)
	p := newRoutingPRD(t, m)
	if _, err := m.Decompose(p.ID); err == nil {
		t.Fatal("must fail when every planner errors")
	}
	stored, _ := m.store.GetPRD(p.ID)
	if stored.PlanningRecord == nil || stored.PlanningRecord.Attempts[0].Outcome != LadderOutcomeError {
		t.Fatalf("errors are recorded as attempts: %+v", stored.PlanningRecord)
	}
}

func TestPlanningRoutingSupervisorEditsIncrementCounter(t *testing.T) {
	f := &fakeDecomposer{resp: []string{ladderFixedPlanJSON}}
	m := newRoutingManager(t, routingTestConfig(nil), f)
	p := newRoutingPRD(t, m)
	got, err := m.Decompose(p.ID)
	if err != nil {
		t.Fatalf("Decompose: %v", err)
	}
	a := &API{M: m}
	if _, err := a.AddStory(p.ID, "Extra", "added by the supervisor", "supervisor"); err != nil {
		t.Fatalf("AddStory: %v", err)
	}
	cur, _ := m.store.GetPRD(p.ID)
	if cur.PlanningRecord.SupervisorEdits != 1 {
		t.Fatalf("add story must count, got %d", cur.PlanningRecord.SupervisorEdits)
	}
	storyID := cur.Story[len(cur.Story)-1].ID
	if _, err := a.AddTask(p.ID, storyID, "Extra task", "do a thing", "supervisor", nil); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if _, err := a.EditStory(p.ID, storyID, "Renamed", "", "supervisor"); err != nil {
		t.Fatalf("EditStory: %v", err)
	}
	if _, err := a.RemoveStory(p.ID, storyID, "supervisor"); err != nil {
		t.Fatalf("RemoveStory: %v", err)
	}
	cur, _ = m.store.GetPRD(p.ID)
	if cur.PlanningRecord.SupervisorEdits != 4 {
		t.Fatalf("four edits expected, got %d", cur.PlanningRecord.SupervisorEdits)
	}
	// A failed edit must not count.
	if _, err := a.RemoveStory(p.ID, "no-such-story", "supervisor"); err == nil {
		t.Fatal("removing a missing story must fail")
	}
	cur, _ = m.store.GetPRD(p.ID)
	if cur.PlanningRecord.SupervisorEdits != 4 {
		t.Fatalf("failed edit counted: %d", cur.PlanningRecord.SupervisorEdits)
	}
	// A PRD with no record is untouched.
	plain := newRoutingPRD(t, m)
	m.NotePlanningEdit(plain.ID)
	if q, _ := m.store.GetPRD(plain.ID); q.PlanningRecord != nil {
		t.Fatal("no record must be created by an edit")
	}
	_ = got
}

func TestPlanningRoutingRecordAttemptCap(t *testing.T) {
	lr := LadderResult{Planner: config.PlannerRef{Backend: "b", Model: "m"}}
	for i := 1; i <= 14; i++ {
		lr.Attempts = append(lr.Attempts, AttemptRecord{Index: i})
	}
	rec := NewPlanningRecordFromLadder(RouteDecision{RuleName: "r"}, lr, lr.Attempts[0].At)
	if len(rec.Attempts) != PlanningRecordMaxAttempts || rec.Attempts[0].Index != 5 {
		t.Fatalf("cap must keep the latest %d attempts, got %d starting at %d", PlanningRecordMaxAttempts, len(rec.Attempts), rec.Attempts[0].Index)
	}
}
