package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

type fakePlanningOps struct {
	drafts    []PlanningDraftView
	replanned []string
	err       error
}

func (f *fakePlanningOps) Drafts(context.Context) []PlanningDraftView { return f.drafts }
func (f *fakePlanningOps) Replan(_ context.Context, id string) error {
	f.replanned = append(f.replanned, id)
	return f.err
}

type planningHarness struct {
	r      *Repairer
	logs   []string
	alerts []string
}

func newPlanningHarness(mode string, ops PlanningRepairOps) *planningHarness {
	h := &planningHarness{}
	grace := 0
	cfg := config.HousekeepingRepairsConfig{Planning: mode, GraceSeconds: &grace}
	h.r = NewRepairer(RepairerHooks{
		Settings: func() config.HousekeepingRepairsConfig { return cfg },
		Log:      func(l string) { h.logs = append(h.logs, l) },
		Alert:    func(_, title, _ string) { h.alerts = append(h.alerts, title) },
	}, NewPlanningRepairs(ops)...)
	return h
}

func stuckDraft(id, spec string) PlanningDraftView {
	return PlanningDraftView{ID: id, Title: "Demo", Draft: true, Failed: true, Source: "release-version-fixed", Detail: "bad", Spec: spec}
}

const correctableSpec = "Fix BL123 using /usr/local/bin/jq today."
const uncorrectableSpec = "Do a clear and ordinary thing."

func TestHousekeepingPlanningNil(t *testing.T) {
	if NewPlanningRepairs(nil) != nil {
		t.Fatal("nil ops should give none")
	}
}

func TestHousekeepingPlanningReportModeActsOnNothing(t *testing.T) {
	ops := &fakePlanningOps{drafts: []PlanningDraftView{stuckDraft("d1", correctableSpec)}}
	h := newPlanningHarness(config.HousekeepingModeReport, ops)
	recs := h.r.Run(context.Background())
	if len(ops.replanned) != 0 || len(recs) != 1 || recs[0].Kind != RepairWould || len(h.alerts) != 0 {
		t.Fatalf("replanned=%v recs=%v alerts=%v", ops.replanned, recs, h.alerts)
	}
}

func TestHousekeepingPlanningOnReplansCorrectable(t *testing.T) {
	ops := &fakePlanningOps{drafts: []PlanningDraftView{stuckDraft("d1", correctableSpec)}}
	h := newPlanningHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	if len(ops.replanned) != 1 || ops.replanned[0] != "d1" {
		t.Fatalf("replanned=%v", ops.replanned)
	}
	if len(recs) != 1 || recs[0].Kind != RepairDid || len(h.logs) != 1 || len(h.alerts) != 0 {
		t.Fatalf("recs=%v logs=%v alerts=%v", recs, h.logs, h.alerts)
	}
	if !strings.Contains(h.logs[0], "undo:") || !strings.Contains(recs[0].Done, "no session was touched") {
		t.Fatalf("log = %v", h.logs)
	}
}

func TestHousekeepingPlanningNotCorrectableReportedOnce(t *testing.T) {
	ops := &fakePlanningOps{drafts: []PlanningDraftView{stuckDraft("d1", uncorrectableSpec)}}
	h := newPlanningHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	h.r.Run(context.Background())
	if len(ops.replanned) != 0 {
		t.Fatalf("replanned %v", ops.replanned)
	}
	if len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe || len(h.alerts) != 1 {
		t.Fatalf("recs=%v alerts=%v", recs, h.alerts)
	}
}

func TestHousekeepingPlanningOffDoesNothing(t *testing.T) {
	ops := &fakePlanningOps{drafts: []PlanningDraftView{stuckDraft("d1", correctableSpec)}}
	h := newPlanningHarness(config.HousekeepingModeOff, ops)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.replanned) != 0 || len(h.alerts) != 0 {
		t.Fatalf("recs=%v replanned=%v", recs, ops.replanned)
	}
}

func TestHousekeepingPlanningIgnoresOtherDrafts(t *testing.T) {
	retry := stuckDraft("a", correctableSpec)
	retry.Retryable = true
	withStories := stuckDraft("b", correctableSpec)
	withStories.Stories = 2
	notDraft := stuckDraft("c", correctableSpec)
	notDraft.Draft = false
	noFail := stuckDraft("d", correctableSpec)
	noFail.Failed = false
	ops := &fakePlanningOps{drafts: []PlanningDraftView{retry, withStories, notDraft, noFail}}
	h := newPlanningHarness(config.HousekeepingModeOn, ops)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.replanned) != 0 {
		t.Fatalf("recs=%v replanned=%v", recs, ops.replanned)
	}
}

func TestHousekeepingPlanningReplanErrorRecorded(t *testing.T) {
	ops := &fakePlanningOps{drafts: []PlanningDraftView{stuckDraft("d1", correctableSpec)}, err: errors.New("boom")}
	h := newPlanningHarness(config.HousekeepingModeOn, ops)
	recs := h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairFailed || !strings.Contains(recs[0].Done, "boom") {
		t.Fatalf("recs=%v", recs)
	}
}

func TestHousekeepingPlanningApplyRechecks(t *testing.T) {
	ops := &fakePlanningOps{drafts: []PlanningDraftView{stuckDraft("d1", correctableSpec)}}
	rep := NewPlanningRepairs(ops)[0]
	f := rep.Detect(context.Background())[0]
	ops.drafts[0].Stories = 1
	if _, err := rep.Apply(context.Background(), f); err == nil || len(ops.replanned) != 0 {
		t.Fatalf("err=%v replanned=%v", err, ops.replanned)
	}
	ops.drafts = nil
	if _, err := rep.Apply(context.Background(), f); err == nil {
		t.Fatal("want error for a gone draft")
	}
}

// The four existing groups keep their own modes: setting planning does not
// change them, and an unset planning mode reports like the others.
func TestHousekeepingPlanningModeDoesNotAffectOtherGroups(t *testing.T) {
	c := config.HousekeepingRepairsConfig{Planning: "on"}
	for _, g := range []string{"task_chain", "advance_judge", "lane_fill", "leftovers"} {
		if m := c.ModeFor(g); m != config.HousekeepingModeReport {
			t.Fatalf("%s mode = %s", g, m)
		}
	}
	if c.ModeFor("planning") != config.HousekeepingModeOn || (config.HousekeepingRepairsConfig{}).ModeFor("planning") != config.HousekeepingModeReport {
		t.Fatal("planning mode wrong")
	}
	if c2, ok := config.SetHousekeepingRepairsField(config.HousekeepingRepairsConfig{}, "planning", "off"); !ok || c2.Planning != "off" {
		t.Fatal("set planning failed")
	}
}
