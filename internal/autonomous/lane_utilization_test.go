package autonomous

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

var laneUtilNow = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func laneUtilCfg() LaneFillConfig {
	return LaneFillConfig{
		Mode: "shadow", GPULanes: 2, ClaudeLanes: 1,
		GPUBackend: "pi-prompt", ClaudeBackend: "claude-code",
	}
}

// ago is a time h hours before the fixed clock.
func ago(h float64) time.Time { return laneUtilNow.Add(-time.Duration(h * float64(time.Hour))) }

func utilTask(prd, backend string, startH, endH float64) laneUtilizationTask {
	t := laneUtilizationTask{prdID: prd, title: "T " + prd, stage: "stage", backend: backend, start: ago(startH)}
	if endH >= 0 {
		t.end = ago(endH)
	}
	return t
}

func laneByID(t *testing.T, r LaneUtilization, id string) LaneUtilizationLane {
	t.Helper()
	for _, l := range r.Lanes {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("lane %s not found in %+v", id, r.Lanes)
	return LaneUtilizationLane{}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestLaneUtilizationBusyIdle(t *testing.T) {
	cases := []struct {
		name     string
		tasks    []laneUtilizationTask
		lane     string
		busyH    float64
		runs     int
		bucketsN int
	}{
		{"all idle", nil, "gpu-1", 0, 0, 24},
		{"all busy", []laneUtilizationTask{utilTask("a", "pi-prompt", 30, 0)}, "gpu-1", 24, 1, 24},
		{"still running", []laneUtilizationTask{utilTask("a", "claude-code", 6, -1)}, "claude-1", 6, 1, 24},
		{"partial busy", []laneUtilizationTask{utilTask("a", "pi-prompt", 10, 8), utilTask("b", "pi-prompt", 5, 4)}, "gpu-1", 3, 2, 24},
		{"overlap counts once", []laneUtilizationTask{utilTask("a", "claude-code", 10, 6), utilTask("b", "claude-code", 8, 4)}, "claude-1", 6, 2, 24},
		{"crosses window start", []laneUtilizationTask{utilTask("a", "pi-prompt", 30, 20)}, "gpu-1", 4, 1, 24},
		{"ended before window", []laneUtilizationTask{utilTask("a", "pi-prompt", 40, 30)}, "gpu-1", 0, 0, 24},
		{"starts after now", []laneUtilizationTask{utilTask("a", "pi-prompt", -1, -1)}, "gpu-1", 0, 0, 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := buildLaneUtilization(laneUtilizationInput{cfg: laneUtilCfg(), tasks: tc.tasks}, 24*time.Hour, 0, laneUtilNow)
			if len(r.Lanes) != 3 {
				t.Fatalf("want 3 lanes, got %d", len(r.Lanes))
			}
			l := laneByID(t, r, tc.lane)
			if !near(l.BusySeconds, tc.busyH*3600) {
				t.Errorf("busy = %v, want %v", l.BusySeconds, tc.busyH*3600)
			}
			if !near(l.BusySeconds+l.IdleSeconds, 24*3600) || !near(l.BusyShare+l.IdleShare, 1) {
				t.Errorf("busy+idle must fill the window: %+v", l)
			}
			if len(l.Runs) != tc.runs {
				t.Errorf("runs = %d, want %d", len(l.Runs), tc.runs)
			}
			if len(l.Buckets) != tc.bucketsN || r.BucketCount != tc.bucketsN {
				t.Errorf("buckets = %d", len(l.Buckets))
			}
			var sum float64
			for _, b := range l.Buckets {
				sum += b.BusyShare
			}
			if !near(sum/float64(tc.bucketsN), l.BusyShare) {
				t.Errorf("bucket mean %v != busy share %v", sum/float64(tc.bucketsN), l.BusyShare)
			}
		})
	}
}

func TestLaneUtilizationSameStartOrder(t *testing.T) {
	tasks := []laneUtilizationTask{utilTask("b", "pi-prompt", 4, 3), utilTask("a", "pi-prompt", 4, 2)}
	r := buildLaneUtilization(laneUtilizationInput{cfg: laneUtilCfg(), tasks: tasks}, 24*time.Hour, 4, laneUtilNow)
	if g := laneByID(t, r, "gpu-1"); len(g.Runs) != 1 || g.Runs[0].PRDID != "a" {
		t.Fatalf("gpu-1 %+v", g.Runs)
	}
}

func TestLaneUtilizationLaneAssignment(t *testing.T) {
	// Two overlapping GPU runs go on separate GPU lanes; a third, once the
	// first has ended, reuses the first lane.
	tasks := []laneUtilizationTask{
		utilTask("a", "pi-prompt", 10, 6),
		utilTask("b", "pi-prompt", 9, 5),
		utilTask("c", "pi-prompt", 4, 2),
	}
	r := buildLaneUtilization(laneUtilizationInput{cfg: laneUtilCfg(), tasks: tasks}, 24*time.Hour, 6, laneUtilNow)
	if g1, g2 := laneByID(t, r, "gpu-1"), laneByID(t, r, "gpu-2"); len(g1.Runs) != 2 || len(g2.Runs) != 1 {
		t.Fatalf("gpu-1 %d runs, gpu-2 %d runs", len(g1.Runs), len(g2.Runs))
	}
	// Three overlapping runs on two lanes: the third lands on the lane that
	// frees first and still counts once.
	tasks = []laneUtilizationTask{
		utilTask("a", "pi-prompt", 10, 2),
		utilTask("b", "pi-prompt", 9, 5),
		utilTask("c", "pi-prompt", 8, 6),
	}
	r = buildLaneUtilization(laneUtilizationInput{cfg: laneUtilCfg(), tasks: tasks}, 24*time.Hour, 6, laneUtilNow)
	if g2 := laneByID(t, r, "gpu-2"); len(g2.Runs) != 2 || !near(g2.BusySeconds, 4*3600) {
		t.Fatalf("gpu-2 %+v", g2)
	}
}

func TestLaneUtilizationQueueWait(t *testing.T) {
	a := utilTask("a", "claude-code", 10, 8)
	a.queued = ago(10.5)
	b := utilTask("b", "claude-code", 6, 4)
	b.queued = ago(8)
	c := utilTask("c", "claude-code", 3, 2)
	c.queued = ago(2)                        // queued after start: clamps to zero
	d := utilTask("d", "claude-code", 30, 2) // started before the window: no wait counted
	d.queued = ago(31)
	e := utilTask("e", "claude-code", 1, 0) // unknown queue time
	r := buildLaneUtilization(laneUtilizationInput{cfg: laneUtilCfg(), tasks: []laneUtilizationTask{a, b, c, d, e}}, 24*time.Hour, 4, laneUtilNow)
	w := laneByID(t, r, "claude-1").QueueWait
	if w.Count != 3 || !near(w.MaxSeconds, 2*3600) || !near(w.MeanSeconds, (1800+7200+0)/3.0) {
		t.Fatalf("queue wait %+v", w)
	}
	if got := laneByID(t, r, "gpu-1").QueueWait; got.Count != 0 {
		t.Fatalf("idle lane has wait %+v", got)
	}
}

func TestLaneUtilizationMixedBackends(t *testing.T) {
	tasks := []laneUtilizationTask{
		utilTask("a", "pi-prompt", 12, 8),   // 4h gpu
		utilTask("b", "claude-code", 6, 4),  // 2h claude
		utilTask("c", "claude-code", 5, 3),  // overlaps b: only 1 extra hour
		utilTask("d", "opencode", 3, 2),     // 1h on a backend with no lane
		utilTask("e", "opencode", 2.5, 1.5), // overlaps d on its own lane: 0.5 extra
	}
	r := buildLaneUtilization(laneUtilizationInput{cfg: laneUtilCfg(), tasks: tasks}, 24*time.Hour, 4, laneUtilNow)
	if len(r.Lanes) != 4 {
		t.Fatalf("want 4 lanes (gpu 2, claude 1, other 1), got %d", len(r.Lanes))
	}
	oc := laneByID(t, r, "other-opencode")
	if oc.Type != "other" || !near(oc.BusySeconds, 1.5*3600) {
		t.Fatalf("other lane %+v", oc)
	}
	want := map[string]float64{"pi-prompt": 4, "claude-code": 3, "opencode": 1.5}
	var share float64
	for _, m := range r.BackendMix {
		if !near(m.BusySeconds, want[m.Backend]*3600) {
			t.Errorf("%s busy %v want %v", m.Backend, m.BusySeconds, want[m.Backend]*3600)
		}
		share += m.Share
	}
	if len(r.BackendMix) != 3 || !near(share, 1) || r.BackendMix[0].Backend != "pi-prompt" {
		t.Fatalf("mix %+v", r.BackendMix)
	}
}

func TestLaneUtilizationUnconfiguredLaneType(t *testing.T) {
	cfg := laneUtilCfg()
	cfg.ClaudeLanes = 0
	// ClaudeLanes 0 may read as "none" or as a default; either way a claude
	// run must land on a claude lane.
	r := buildLaneUtilization(laneUtilizationInput{cfg: cfg, tasks: []laneUtilizationTask{utilTask("a", "claude-code", 3, 1)}}, 24*time.Hour, 4, laneUtilNow)
	found := false
	for _, l := range r.Lanes {
		if l.Type == LaneTypeClaude && len(l.Runs) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("claude run not placed: %+v", r.Lanes)
	}
}

func TestLaneUtilizationIdleWithEligibleWork(t *testing.T) {
	in := laneUtilizationInput{
		cfg:           laneUtilCfg(),
		workSince:     map[string]time.Time{"gpu-2": ago(0.5), "claude-1": laneUtilNow.Add(time.Hour)},
		firstEligible: map[string]string{"gpu-2": "Build the thing (ab12cd34)"},
	}
	r := buildLaneUtilization(in, 24*time.Hour, 4, laneUtilNow)
	g := laneByID(t, r, "gpu-2")
	if !g.IdleWithEligibleWork || !near(g.IdleWithEligibleSeconds, 1800) || g.FirstEligibleItem != "Build the thing (ab12cd34)" {
		t.Fatalf("flag not set: %+v", g)
	}
	for _, id := range []string{"gpu-1", "claude-1"} {
		if l := laneByID(t, r, id); l.IdleWithEligibleWork || l.IdleWithEligibleSeconds != 0 || l.FirstEligibleItem != "" {
			t.Fatalf("flag set on %s: %+v", id, l)
		}
	}
	// Cleared: no episode recorded.
	in.workSince = map[string]time.Time{}
	r = buildLaneUtilization(in, 24*time.Hour, 4, laneUtilNow)
	if laneByID(t, r, "gpu-2").IdleWithEligibleWork {
		t.Fatal("flag not cleared")
	}
}

func TestLaneUtilizationEmptyHistory(t *testing.T) {
	r := buildLaneUtilization(laneUtilizationInput{cfg: laneUtilCfg()}, 0, 0, laneUtilNow)
	if r.WindowSeconds != 86400 || !r.WindowEnd.Equal(laneUtilNow) || !r.WindowStart.Equal(ago(24)) {
		t.Fatalf("window %+v", r)
	}
	if len(r.BackendMix) != 0 || r.BackendMix == nil {
		t.Fatalf("mix %+v", r.BackendMix)
	}
	if r.LaneFill.Decisions == nil || len(r.LaneFill.Decisions) != 0 || r.LaneFill.Mode != "shadow" {
		t.Fatalf("fill %+v", r.LaneFill)
	}
	if r.PlanUsage != (LaneUtilizationPlan{}) {
		t.Fatalf("plan %+v", r.PlanUsage)
	}
	for _, l := range r.Lanes {
		if l.BusySeconds != 0 || l.Runs == nil {
			t.Fatalf("lane %+v", l)
		}
	}
}

func TestLaneUtilizationClamp(t *testing.T) {
	wins := []struct{ in, want time.Duration }{
		{0, 24 * time.Hour}, {-time.Hour, 24 * time.Hour}, {time.Minute, time.Hour},
		{time.Hour, time.Hour}, {48 * time.Hour, 48 * time.Hour},
		{90 * 24 * time.Hour, 30 * 24 * time.Hour},
	}
	for _, c := range wins {
		if got := ClampLaneUtilizationWindow(c.in); got != c.want {
			t.Errorf("window %v -> %v, want %v", c.in, got, c.want)
		}
	}
	bks := []struct{ in, want int }{{0, 24}, {-3, 24}, {1, 1}, {100, 100}, {1000, 288}}
	for _, c := range bks {
		if got := ClampLaneUtilizationBuckets(c.in); got != c.want {
			t.Errorf("buckets %d -> %d, want %d", c.in, got, c.want)
		}
	}
	r := buildLaneUtilization(laneUtilizationInput{cfg: laneUtilCfg()}, 5*time.Minute, 1000, laneUtilNow)
	if r.WindowSeconds != 3600 || r.BucketCount != 288 || len(r.Lanes[0].Buckets) != 288 {
		t.Fatalf("clamped result %v %d", r.WindowSeconds, r.BucketCount)
	}
}

func TestLaneUtilizationDecisionsAndPlan(t *testing.T) {
	var ds []LaneFillDecision
	for i := 0; i < LaneUtilizationDecisionLimit+5; i++ {
		ds = append(ds, LaneFillDecision{LaneID: "gpu-1", Action: LaneFillActionStartPRD, PRDID: "p"})
	}
	in := laneUtilizationInput{
		cfg:    laneUtilCfg(),
		status: LaneFillStatus{Mode: "on", Modes: LaneFillKindModes{GPU: "on", Claude: "shadow"}, Decisions: ds},
		usage:  LaneUtilizationUsage{Points: 25, PlanPoints: 100, Tokens: 123456},
	}
	r := buildLaneUtilization(in, 24*time.Hour, 4, laneUtilNow)
	if r.LaneFill.Mode != "on" || r.LaneFill.Modes.Claude != "shadow" || len(r.LaneFill.Decisions) != LaneUtilizationDecisionLimit {
		t.Fatalf("fill %+v", r.LaneFill)
	}
	if r.PlanUsage.Points != 25 || r.PlanUsage.Tokens != 123456 || !near(r.PlanUsage.PercentOfPlan, 25) {
		t.Fatalf("plan %+v", r.PlanUsage)
	}
	in.usage.PlanPoints = 0
	if p := buildLaneUtilization(in, 24*time.Hour, 4, laneUtilNow).PlanUsage; p.PercentOfPlan != 0 {
		t.Fatalf("no plan must give 0 percent: %+v", p)
	}
}

func TestLaneUtilizationJSONHasNoMoneyKeys(t *testing.T) {
	tasks := []laneUtilizationTask{utilTask("a", "pi-prompt", 5, 1), utilTask("b", "opencode", 3, 2)}
	in := laneUtilizationInput{
		cfg: laneUtilCfg(), tasks: tasks,
		status:    LaneFillStatus{Mode: "shadow", Decisions: []LaneFillDecision{{LaneID: "gpu-1", Action: LaneFillActionIdle}}},
		usage:     LaneUtilizationUsage{Points: 5, PlanPoints: 100, Tokens: 9},
		workSince: map[string]time.Time{"gpu-2": ago(1)},
	}
	data, err := json.Marshal(buildLaneUtilization(in, 24*time.Hour, 4, laneUtilNow))
	if err != nil {
		t.Fatal(err)
	}
	var any interface{}
	if err := json.Unmarshal(data, &any); err != nil {
		t.Fatal(err)
	}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case map[string]interface{}:
			for k, c := range x {
				lk := strings.ToLower(k)
				for _, bad := range []string{"dollar", "usd", "cost"} {
					if strings.Contains(lk, bad) {
						t.Errorf("key %q contains %q", k, bad)
					}
				}
				walk(c)
			}
		case []interface{}:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(any)
}

type fakeLaneUtilUsage struct{ start, end time.Time }

func (f *fakeLaneUtilUsage) LaneUtilizationUsage(start, end time.Time) LaneUtilizationUsage {
	f.start, f.end = start, end
	return LaneUtilizationUsage{Points: 10, PlanPoints: 50, Tokens: 77}
}

func TestLaneUtilizationNilAndBareManager(t *testing.T) {
	var nilM *Manager
	nilM.SetLaneUtilizationUsageSource(&fakeLaneUtilUsage{})
	if r := nilM.LaneUtilization(0, 0, laneUtilNow); len(r.Lanes) == 0 || r.LaneFill.Mode == "" {
		t.Fatalf("nil manager: %+v", r)
	}
	bare := &Manager{}
	if r := bare.LaneUtilization(time.Hour, 2, laneUtilNow); r.BucketCount != 2 {
		t.Fatalf("bare manager: %+v", r)
	}
}

func TestLaneUtilizationFromManager(t *testing.T) {
	h := newLaneFillHarness(t, "shadow")
	usage := &fakeLaneUtilUsage{}
	h.m.SetLaneUtilizationUsageSource(usage)

	// A running Claude task, a finished GPU task, one that never started and one outside the window.
	p := h.addPRD("Util work", PRDApproved, "any", "")
	approved := ago(9)
	p.ApprovedAt = &approved
	p.AutomationID = "no-such-automation" // an unknown Automation resolves to none
	if err := h.m.store.SavePRD(p); err != nil {
		t.Fatal(err)
	}
	s, err := h.m.store.AddStory(p.ID, "Stage one", "")
	if err != nil {
		t.Fatal(err)
	}
	mk := func(title, backend string, status TaskStatus, started, completed *time.Time, updated time.Time) {
		tk, err := h.m.store.AddTask(p.ID, s.ID, title, "spec", nil)
		if err != nil {
			t.Fatal(err)
		}
		h.m.store.MutateTask(tk.ID, func(x *Task) {
			x.CreatedAt = ago(10)
			x.Backend, x.Status, x.StartedAt, x.CompletedAt, x.UpdatedAt = backend, status, started, completed, updated
			if title == "smoke" {
				x.Kind = "smoke"
			}
		})
	}
	tp := func(hh float64) *time.Time { v := ago(hh); return &v }
	mk("running", "claude-code", TaskInProgress, tp(3), nil, ago(1))
	mk("done", "pi-prompt", TaskCompleted, tp(8), tp(6), ago(6))
	mk("smoke", "pi-prompt", TaskCompleted, tp(5), tp(4), ago(4))
	mk("never", "pi-prompt", TaskPending, nil, nil, ago(1))
	mk("future", "pi-prompt", TaskInProgress, tp(-2), nil, ago(0))
	mk("old", "pi-prompt", TaskCompleted, tp(50), tp(49), ago(49))

	// Idle episode and alert state of the coordinator.
	h.c.lf.mu.Lock()
	h.c.lf.workSince["gpu-1"] = ago(0.25)
	h.c.lf.alerts = []LaneFillAlert{{LaneID: "gpu-1", Work: "Alert item (aa)"}}
	h.c.lf.mu.Unlock()

	r := h.m.LaneUtilization(24*time.Hour, 6, laneUtilNow)
	if !usage.end.Equal(laneUtilNow) || !usage.start.Equal(ago(24)) {
		t.Fatalf("usage window %v..%v", usage.start, usage.end)
	}
	if r.PlanUsage.Tokens != 77 || !near(r.PlanUsage.PercentOfPlan, 20) {
		t.Fatalf("plan %+v", r.PlanUsage)
	}
	c := laneByID(t, r, "claude-1")
	if len(c.Runs) != 1 || c.Runs[0].Title != "Util work" || c.Runs[0].Stage != "Stage one" || c.Runs[0].Kind != "task" || !near(c.BusySeconds, 3*3600) {
		t.Fatalf("claude lane %+v", c)
	}
	if c.QueueWait.Count != 1 || c.QueueWait.MaxSeconds < 6*3600-1 {
		t.Fatalf("claude wait %+v", c.QueueWait)
	}
	g := laneByID(t, r, "gpu-1")
	kinds := map[string]bool{}
	for _, run := range g.Runs {
		kinds[run.Kind] = true
	}
	if !kinds["smoke"] || !kinds["task"] || !g.IdleWithEligibleWork || g.FirstEligibleItem != "Alert item (aa)" {
		t.Fatalf("gpu lane %+v", g)
	}
	if !near(g.BusySeconds, 3*3600) { // done 6..8h ago (2h) + smoke 4..5h ago (1h)
		t.Fatalf("gpu busy %v", g.BusySeconds)
	}
}

func TestLaneUtilizationFirstEligible(t *testing.T) {
	st := LaneFillStatus{
		Alerts: []LaneFillAlert{{LaneID: "gpu-1", Work: "from alert"}, {LaneID: "gpu-1", Work: "older alert"}, {LaneID: "gpu-3"}},
		Decisions: []LaneFillDecision{
			{LaneID: "gpu-1", PRDID: "x", Title: "ignored, alert wins"},
			{LaneID: "", PRDID: "y", Title: "no lane"},
			{LaneID: "claude-1", PRDID: "", Title: "no automaton"},
			{LaneID: "claude-1", PRDID: "z", Title: "Decision item"},
			{LaneID: "claude-1", PRDID: "w", Title: "older decision"},
		},
	}
	got := laneUtilizationFirstEligible(st)
	if got["gpu-1"] != "from alert" || got["claude-1"] != "Decision item (z)" || len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestLaneUtilizationTaskEnd(t *testing.T) {
	st := ago(5)
	up := ago(3)
	done := ago(4)
	cases := []struct {
		name string
		task Task
		want time.Time
	}{
		{"running", Task{Status: TaskInProgress, StartedAt: &st}, laneUtilNow},
		{"completed", Task{Status: TaskCompleted, StartedAt: &st, CompletedAt: &done}, done},
		{"updated", Task{Status: TaskFailed, StartedAt: &st, UpdatedAt: up}, up},
		{"no end", Task{Status: TaskFailed, StartedAt: &st, UpdatedAt: ago(6)}, st},
		{"no start", Task{Status: TaskFailed}, laneUtilNow},
	}
	for _, c := range cases {
		if got := laneUtilizationTaskEnd(&c.task, laneUtilNow); !got.Equal(c.want) {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
	}
}

func TestLaneUtilizationIntervalHelpers(t *testing.T) {
	iv := func(a, b float64) laneInterval { return laneInterval{ago(a), ago(b)} }
	got := mergeIntervals([]laneInterval{iv(5, 4), iv(4, 3), iv(2, 1), iv(2.5, 0.5), iv(9, 8), iv(7, 7), iv(1, -3)}, ago(8.5), ago(0))
	if len(got) != 3 || !got[2].end.Equal(ago(0)) {
		t.Fatalf("merge %+v", got)
	}
	if s := intervalSeconds(got, ago(6), ago(4)); !near(s, 3600) {
		t.Fatalf("clipped seconds %v", s)
	}
	if s := intervalOverlapSeconds([]laneInterval{iv(5, 3)}, []laneInterval{iv(4, 2), iv(10, 9)}); !near(s, 3600) {
		t.Fatalf("overlap %v", s)
	}
}
