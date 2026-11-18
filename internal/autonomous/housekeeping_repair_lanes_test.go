package autonomous

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

type fakeLaneFillOps struct {
	lanes    []LaneFillLaneView
	capFull  bool
	paused   bool
	started  []string
	startErr error
}

func (f *fakeLaneFillOps) Lanes(context.Context) []LaneFillLaneView { return f.lanes }
func (f *fakeLaneFillOps) CapacityFull(context.Context) bool        { return f.capFull }
func (f *fakeLaneFillOps) UsagePaused(context.Context) bool         { return f.paused }
func (f *fakeLaneFillOps) Start(_ context.Context, id string) error {
	f.started = append(f.started, id)
	return f.startErr
}

type laneHarness struct {
	r      *Repairer
	ops    *fakeLaneFillOps
	logs   []string
	alerts []string
}

func newLaneHarness(mode string, grace int, ops *fakeLaneFillOps, now func() time.Time) *laneHarness {
	h := &laneHarness{ops: ops}
	cfg := config.HousekeepingRepairsConfig{LaneFill: mode, GraceSeconds: &grace}
	if now == nil {
		now = func() time.Time { return time.Unix(1000, 0) }
	}
	h.r = NewRepairer(RepairerHooks{
		Settings: func() config.HousekeepingRepairsConfig { return cfg },
		Log:      func(l string) { h.logs = append(h.logs, l) },
		Alert:    func(_, title, _ string) { h.alerts = append(h.alerts, title) },
		Now:      now,
	}, NewLaneFillRepairs(ops)...)
	return h
}

func idleLane(cands ...LaneFillCandidateView) LaneFillLaneView {
	return LaneFillLaneView{Name: "gpu", Idle: true, Candidates: cands}
}

func elig(id string) LaneFillCandidateView {
	return LaneFillCandidateView{ID: id, Title: id, Eligible: true}
}

func TestNewLaneFillRepairsNil(t *testing.T) {
	if NewLaneFillRepairs(nil) != nil {
		t.Fatal("nil ops should give none")
	}
	if got := NewLaneFillRepairs(&fakeLaneFillOps{}); len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
}

func TestLaneFillStartsNextEligible(t *testing.T) {
	held := LaneFillCandidateView{ID: "held", Eligible: false}
	ops := &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(held, elig("a1"), elig("a2"))}}
	h := newLaneHarness(config.HousekeepingModeOn, 0, ops, nil)
	recs := h.r.Run(context.Background())
	if len(ops.started) != 1 || ops.started[0] != "a1" {
		t.Fatalf("started = %v", ops.started)
	}
	if len(recs) != 1 || recs[0].Kind != RepairDid || len(h.logs) != 1 || len(h.alerts) != 0 {
		t.Fatalf("recs=%v logs=%v alerts=%v", recs, h.logs, h.alerts)
	}
	if !strings.Contains(h.logs[0], "undo:") || !strings.Contains(recs[0].Done, "not changed") {
		t.Fatalf("log line missing undo or model note: %v", h.logs)
	}
}

func TestLaneFillReportModeActsOnNothing(t *testing.T) {
	ops := &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(elig("a1"))}}
	h := newLaneHarness(config.HousekeepingModeReport, 0, ops, nil)
	recs := h.r.Run(context.Background())
	if len(ops.started) != 0 || len(recs) != 1 || recs[0].Kind != RepairWould {
		t.Fatalf("started=%v recs=%v", ops.started, recs)
	}
}

func TestLaneFillOffModeSkips(t *testing.T) {
	ops := &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(elig("a1"))}}
	h := newLaneHarness(config.HousekeepingModeOff, 0, ops, nil)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.started) != 0 {
		t.Fatalf("recs=%v started=%v", recs, ops.started)
	}
}

func TestLaneFillGracePeriod(t *testing.T) {
	now := time.Unix(1000, 0)
	ops := &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(elig("a1"))}}
	h := newLaneHarness(config.HousekeepingModeOn, 60, ops, func() time.Time { return now })
	h.r.Run(context.Background())
	if len(ops.started) != 0 {
		t.Fatal("started before the grace period passed")
	}
	now = now.Add(61 * time.Second)
	h.r.Run(context.Background())
	if len(ops.started) != 1 {
		t.Fatalf("started = %v", ops.started)
	}
}

func TestLaneFillBusyLaneIgnored(t *testing.T) {
	l := idleLane(elig("a1"))
	l.Idle = false
	ops := &fakeLaneFillOps{lanes: []LaneFillLaneView{l}}
	h := newLaneHarness(config.HousekeepingModeOn, 0, ops, nil)
	if recs := h.r.Run(context.Background()); len(recs) != 0 || len(ops.started) != 0 {
		t.Fatalf("recs=%v", recs)
	}
}

func TestLaneFillRefusals(t *testing.T) {
	forbidden := LaneFillCandidateView{ID: "f1", Eligible: true, NeedsForbiddenBackend: true}
	capLane := idleLane(elig("a1"))
	capLane.InflightCapReached = true
	cases := []struct {
		name string
		ops  *fakeLaneFillOps
		want string
	}{
		{"no eligible work", &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(LaneFillCandidateView{ID: "h"})}}, "no eligible"},
		{"no candidates", &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane()}}, "no eligible"},
		{"capacity full", &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(elig("a1"))}, capFull: true}, "capacity is full"},
		{"inflight cap", &fakeLaneFillOps{lanes: []LaneFillLaneView{capLane}}, "inflight cap"},
		{"usage paused", &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(elig("a1"))}, paused: true}, "usage guard"},
		{"forbidden backend only", &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(forbidden)}}, "may not use"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newLaneHarness(config.HousekeepingModeOn, 0, tc.ops, nil)
			recs := h.r.Run(context.Background())
			if len(tc.ops.started) != 0 {
				t.Fatalf("started %v", tc.ops.started)
			}
			if len(recs) != 1 || recs[0].Kind != RepairSkippedUnsafe || !strings.Contains(recs[0].Done, tc.want) {
				t.Fatalf("recs=%v", recs)
			}
			if len(h.alerts) != 1 || len(h.logs) != 1 {
				t.Fatalf("alerts=%v logs=%v", h.alerts, h.logs)
			}
			// A second pass must not raise a second alert.
			h.r.Run(context.Background())
			if len(h.alerts) != 1 {
				t.Fatalf("alert repeated: %v", h.alerts)
			}
		})
	}
}

func TestLaneFillSkipsForbiddenPicksNextAllowed(t *testing.T) {
	forbidden := LaneFillCandidateView{ID: "f1", Eligible: true, NeedsForbiddenBackend: true}
	ops := &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(forbidden, elig("ok"))}}
	h := newLaneHarness(config.HousekeepingModeOn, 0, ops, nil)
	h.r.Run(context.Background())
	if len(ops.started) != 1 || ops.started[0] != "ok" {
		t.Fatalf("started = %v", ops.started)
	}
}

func TestLaneFillStartErrorRecordedFailed(t *testing.T) {
	ops := &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(elig("a1"))}, startErr: errors.New("boom")}
	h := newLaneHarness(config.HousekeepingModeOn, 0, ops, nil)
	recs := h.r.Run(context.Background())
	if len(recs) != 1 || recs[0].Kind != RepairFailed || !strings.Contains(recs[0].Done, "boom") {
		t.Fatalf("recs=%v", recs)
	}
}

func TestLaneFillApplyRechecksState(t *testing.T) {
	ops := &fakeLaneFillOps{lanes: []LaneFillLaneView{idleLane(elig("a1"))}}
	rep := NewLaneFillRepairs(ops)[0]
	f := rep.Detect(context.Background())[0]
	ops.paused = true // changed between detect and apply
	if _, err := rep.Apply(context.Background(), f); err == nil || len(ops.started) != 0 {
		t.Fatalf("err=%v started=%v", err, ops.started)
	}
	ops.paused = false
	ops.lanes = nil
	if _, err := rep.Apply(context.Background(), f); err == nil || len(ops.started) != 0 {
		t.Fatalf("gone lane: err=%v started=%v", err, ops.started)
	}
}
