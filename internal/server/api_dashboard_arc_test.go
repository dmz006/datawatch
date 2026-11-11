package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeArcs []arcInfo

func (f fakeArcs) Arcs() []arcInfo { return f }

type fakeArcLanes struct {
	lanes  []arcLane
	queued []arcQueued
}

func (f fakeArcLanes) Lanes() ([]arcLane, error)    { return f.lanes, nil }
func (f fakeArcLanes) Queued() ([]arcQueued, error) { return f.queued, nil }

type fakeArcUsage struct {
	u   arcPlanUsage
	err error
}

func (f fakeArcUsage) PlanUsage(context.Context) (arcPlanUsage, error) { return f.u, f.err }

type fakeArcTokens struct{ in, out int64 }

func (f fakeArcTokens) WindowTokens(context.Context) (int64, int64, error) { return f.in, f.out, nil }

func arcFixture() arcBuilder {
	return arcBuilder{
		Arcs: fakeArcs{{ID: "a1", Name: "Release arc", Members: []arcMember{
			{Stage: "Final", Status: "pending"},
			{Stage: "features", Status: "running"},
			{Stage: "features", Status: "completed"},
			{Stage: "features", Status: "pending"},
			{Stage: "bugs", Status: "completed"},
			{Stage: "bugs", Status: "completed"},
			{Stage: "bugs", Status: "blocked", Title: "Fix the crash", BlockedReason: "the guardrail refused the change"},
			{Stage: "maintenance", Status: "running"},
			{Stage: "maintenance", Status: "pending"},
		}}},
		Lanes: fakeArcLanes{
			lanes: []arcLane{{Kind: "claude", InFlight: 1, Cap: 2}, {Kind: "pi", InFlight: 2, Cap: 2}},
			queued: []arcQueued{
				{Lane: "claude", Priority: 1, Count: 3},
				{Lane: "claude", Priority: 5, Count: 2},
				{Lane: "pi", Priority: 0, Count: 4},
			},
		},
		Usage:  fakeArcUsage{u: arcPlanUsage{PointsUsed: 40, PointsAllowance: 100}},
		Tokens: fakeArcTokens{in: 1000, out: 250},
	}
}

func TestArcStageOrder(t *testing.T) {
	resp := arcFixture().Build(context.Background(), "")
	var got []string
	for _, s := range resp.Arc.Stages {
		got = append(got, s.Stage)
	}
	if strings.Join(got, ",") != "bugs,features,final" {
		t.Fatalf("stage order = %v, want bugs,features,final", got)
	}
}

func TestArcPerStageProgress(t *testing.T) {
	st := arcFixture().Build(context.Background(), "a1").Arc.Stages
	bugs, feats := st[0], st[1]
	if bugs.Total != 3 || bugs.Done != 2 || bugs.Blocked != 1 || bugs.Running != 0 || bugs.Percent != 66.7 {
		t.Fatalf("bugs = %+v", bugs)
	}
	if feats.Total != 3 || feats.Done != 1 || feats.Running != 1 || feats.Percent != 33.3 {
		t.Fatalf("features = %+v", feats)
	}
	if st[2].Total != 1 || st[2].Percent != 0 {
		t.Fatalf("final = %+v", st[2])
	}
}

func TestArcBlockedStageReason(t *testing.T) {
	arc := arcFixture().Build(context.Background(), "").Arc
	if arc.BlockedStage == nil || arc.BlockedStage.Stage != "bugs" {
		t.Fatalf("blocked_stage = %+v", arc.BlockedStage)
	}
	if !strings.Contains(arc.BlockedStage.Reason, "Fix the crash") || !strings.Contains(arc.BlockedStage.Reason, "guardrail") {
		t.Fatalf("reason not plain/specific: %q", arc.BlockedStage.Reason)
	}
	b := arcFixture()
	b.Arcs = fakeArcs{{ID: "x", Members: []arcMember{{Stage: "features", Status: "completed"}}}}
	if got := b.Build(context.Background(), "").Arc; got.BlockedStage != nil {
		t.Fatalf("unblocked arc reported blocked_stage %+v", got.BlockedStage)
	}
}

func TestArcMaintenanceLaneIsSeparate(t *testing.T) {
	arc := arcFixture().Build(context.Background(), "").Arc
	for _, s := range arc.Stages {
		if s.Stage == "maintenance" {
			t.Fatal("maintenance appeared as a stage")
		}
	}
	m := arc.MaintenanceQueue
	if m == nil || m.Total != 2 || m.Running != 1 || m.Done != 0 || m.Blocked != 0 {
		t.Fatalf("maintenance_queue = %+v", m)
	}
	// Maintenance must not leak into the stage counts.
	if arc.Stages[1].Total != 3 {
		t.Fatalf("features total = %d, want 3", arc.Stages[1].Total)
	}
}

func TestArcQueuedByPriority(t *testing.T) {
	l := arcFixture().Build(context.Background(), "").Lanes
	if !l.Available || len(l.Lanes) != 2 || l.Lanes[0].Cap != 2 || l.Lanes[1].InFlight != 2 {
		t.Fatalf("lanes = %+v", l)
	}
	if len(l.Queued) != 3 || l.Queued[0].Lane != "claude" || l.Queued[0].Priority != 5 || l.Queued[1].Priority != 1 {
		t.Fatalf("queued = %+v (want claude by priority high-first)", l.Queued)
	}
}

func TestArcPointsAndTokensPresent(t *testing.T) {
	u := arcFixture().Build(context.Background(), "").Usage
	if !u.Available || u.Points == nil || u.Points.Used != 40 || u.Points.Remaining != 60 || u.Points.Percent != 40 {
		t.Fatalf("points = %+v", u.Points)
	}
	if u.Tokens == nil || u.Tokens.Total != 1250 {
		t.Fatalf("tokens = %+v", u.Tokens)
	}
}

func TestArcMissingSubsystemsNeverFail(t *testing.T) {
	resp := arcBuilder{Usage: fakeArcUsage{err: errors.New("no readings")}}.Build(context.Background(), "")
	if resp.Arc.Available || resp.Arc.Reason == "" || resp.Lanes.Available || resp.Lanes.Reason == "" || resp.Usage.Available || resp.Usage.Reason == "" {
		t.Fatalf("expected unavailable parts with reasons: %+v", resp)
	}
}

func TestArcHandlerOKWhenNothingWired(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleDashboardArc(rr, httptest.NewRequest(http.MethodGet, "/api/dashboard/arc", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestArcJSONHasNoDollarOrBilledCostKey(t *testing.T) {
	raw, err := json.Marshal(arcFixture().Build(context.Background(), ""))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(n any) {
		switch x := n.(type) {
		case map[string]any:
			for k, c := range x {
				lk := strings.ToLower(k)
				if strings.Contains(lk, "usd") || strings.Contains(lk, "dollar") || strings.Contains(lk, "cost") || strings.Contains(lk, "billed") || strings.Contains(lk, "price") {
					t.Errorf("forbidden money key %q in arc JSON", k)
				}
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(v)
}
