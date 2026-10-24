package autonomous

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var laneFillTestNow = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

// laneFillTestConfig is the shipped shape: 2 GPU lanes, 3 Claude lanes, the
// default backends.
func laneFillTestConfig() LaneFillConfig {
	return LaneFillConfig{
		Mode: "shadow", GPULanes: 2, ClaudeLanes: 3, DecomposeAheadMin: 2,
		GPUBackend: "pi-prompt", ClaudeBackend: "claude-code",
	}
}

func testLanes(gpu, claude int) []Lane {
	return BuildLanes(LaneFillConfig{GPULanes: gpu, ClaudeLanes: claude}, nil, LaneEvidence{}).Lanes
}

func occupy(lanes []Lane, id, prd, backend string) {
	for i := range lanes {
		if lanes[i].ID == id {
			lanes[i].Occupant = &LaneOccupant{PRDID: prd, Title: "running " + prd, Backend: backend}
		}
	}
}

func readyCand(id, pref string, prio int, age int) LaneFillCandidate {
	return LaneFillCandidate{
		ID: id, Title: "title " + id, Status: PRDApproved, LanePreference: pref,
		Backend: "pi-prompt", Priority: prio, BackendChangeAllowed: true,
		CreatedAt: laneFillTestNow.Add(time.Duration(age) * time.Minute),
	}
}

func draftCand(id, pref string, age int) LaneFillCandidate {
	return LaneFillCandidate{
		ID: id, Title: "draft " + id, Status: PRDDraft, LanePreference: pref,
		Backend: "pi-prompt", BackendChangeAllowed: true,
		CreatedAt: laneFillTestNow.Add(time.Duration(age) * time.Minute),
	}
}

func plan(cfg LaneFillConfig, lanes []Lane, cands []LaneFillCandidate) []LaneFillDecision {
	return PlanLaneFill(LaneFillInput{Config: cfg, Lanes: lanes, Candidates: cands, Now: laneFillTestNow})
}

// byLane returns the decision for a lane id.
func byLane(t *testing.T, ds []LaneFillDecision, lane string) LaneFillDecision {
	t.Helper()
	for _, d := range ds {
		if d.LaneID == lane {
			return d
		}
	}
	t.Fatalf("no decision for lane %s in %v", lane, summarize(ds))
	return LaneFillDecision{}
}

func summarize(ds []LaneFillDecision) []string {
	var out []string
	for _, d := range ds {
		out = append(out, fmt.Sprintf("%s/%s:%s:%s", d.LaneID, d.LaneType, d.Action, d.PRDID))
	}
	return out
}

func actions(ds []LaneFillDecision, a LaneFillAction) []LaneFillDecision {
	var out []LaneFillDecision
	for _, d := range ds {
		if d.Action == a {
			out = append(out, d)
		}
	}
	return out
}

func TestLaneFillPlanSourceOrder(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.ClaudeLanes = 0
	cfg.GPULanes = 1
	lanes := testLanes(1, 0)

	t.Run("ready beats draft", func(t *testing.T) {
		ds := plan(cfg, lanes, []LaneFillCandidate{draftCand("d1", "", 0), readyCand("r1", "", 0, 5)})
		d := byLane(t, ds, "gpu-1")
		if d.Action != LaneFillActionStartPRD || d.PRDID != "r1" {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("draft decomposed when nothing is ready", func(t *testing.T) {
		ds := plan(cfg, lanes, []LaneFillCandidate{draftCand("d1", "", 0)})
		d := byLane(t, ds, "gpu-1")
		if d.Action != LaneFillActionDecompose || d.PRDID != "d1" {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("merge is reported and the lane still takes a draft", func(t *testing.T) {
		merge := LaneFillCandidate{ID: "m1", Title: "t", Status: PRDCompleted, AllTasksDone: true, NeedsMergeFlag: true}
		ds := plan(cfg, lanes, []LaneFillCandidate{merge, draftCand("d1", "", 0)})
		if d := byLane(t, ds, "gpu-1"); d.Action != LaneFillActionDecompose || d.PRDID != "d1" {
			t.Fatalf("lane got %v", summarize(ds))
		}
		m := actions(ds, LaneFillActionReportMerge)
		if len(m) != 1 || m[0].PRDID != "m1" || m[0].LaneID != "" {
			t.Fatalf("merge reports = %v", summarize(ds))
		}
	})
	t.Run("nothing at all is idle with a reason", func(t *testing.T) {
		ds := plan(cfg, lanes, nil)
		d := byLane(t, ds, "gpu-1")
		if d.Action != LaneFillActionIdle || d.Blocker != "no eligible work" {
			t.Fatalf("got %+v", d)
		}
	})
	t.Run("priority then age then id", func(t *testing.T) {
		cands := []LaneFillCandidate{
			readyCand("r-old-low", "", 1, 0),
			readyCand("r-new-high", "", 5, 30),
			readyCand("r-old-high", "", 5, 10),
			readyCand("r-b-tie", "", 5, 10),
		}
		ds := plan(cfg, lanes, cands)
		if d := byLane(t, ds, "gpu-1"); d.PRDID != "r-b-tie" {
			t.Fatalf("got %v", summarize(ds))
		}
	})
}

func TestLaneFillPlanModeIndependent(t *testing.T) {
	cands := []LaneFillCandidate{
		readyCand("r1", "gpu", 1, 0), readyCand("r2", "claude", 1, 1), readyCand("r3", "", 0, 2),
		draftCand("d1", "", 3),
		{ID: "m1", Status: PRDCompleted, AllTasksDone: true, NeedsMergeFlag: true},
	}
	var base []LaneFillDecision
	for i, mode := range []string{"", "off", "shadow", "on"} {
		cfg := laneFillTestConfig()
		cfg.Mode = mode
		ds := plan(cfg, testLanes(2, 3), cands)
		want := LaneFillMode(mode)
		if mode == "" {
			want = LaneFillModeShadow
		}
		for j := range ds {
			if ds[j].Mode != want {
				t.Fatalf("mode %q: decision mode %q", mode, ds[j].Mode)
			}
			ds[j].Mode = ""
		}
		if i == 0 {
			base = ds
			continue
		}
		if !reflect.DeepEqual(base, ds) {
			t.Fatalf("mode %q changed the decisions:\n%v\nvs\n%v", mode, summarize(base), summarize(ds))
		}
	}
}

func TestLaneFillPlanHoldAndSequencing(t *testing.T) {
	lookup := func(ref string) (PRDStatus, bool) {
		switch ref {
		case "aaaa1111":
			return PRDCompleted, true
		case "bbbb2222":
			return PRDRunning, true
		}
		return "", false
	}
	cases := []struct {
		name      string
		spec      string
		wantStart bool
		wantSub   string
	}{
		{name: "HOLD line", spec: "HOLD until decided", wantSub: "HOLD"},
		{name: "do not run", spec: "please do not run", wantSub: "do not run"},
		{name: "sequenced after an unshipped ref", spec: "sequenced after bbbb2222", wantSub: "bbbb2222"},
		{name: "sequenced after an unknown ref", spec: "sequenced after cccc3333", wantSub: "cccc3333"},
		{name: "sequenced after a completed ref", spec: "sequenced after aaaa1111", wantStart: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := readyCand("held1", "", 0, 0)
			c.HoldReason = PRDHoldReason(&PRD{ID: c.ID, Spec: tc.spec}, lookup)
			ds := plan(laneFillTestConfig(), testLanes(1, 0), []LaneFillCandidate{c})
			d := byLane(t, ds, "gpu-1")
			if tc.wantStart {
				if d.Action != LaneFillActionStartPRD || d.PRDID != "held1" {
					t.Fatalf("got %v", summarize(ds))
				}
				return
			}
			if d.Action != LaneFillActionIdle {
				t.Fatalf("held work was planned: %v", summarize(ds))
			}
			if !strings.Contains(strings.ToLower(d.Blocker), strings.ToLower(tc.wantSub)) || !strings.Contains(d.Blocker, "held1") {
				t.Fatalf("blocker %q should name held1 and %q", d.Blocker, tc.wantSub)
			}
		})
	}
}

func TestLaneFillPlanAutomationMemberExcluded(t *testing.T) {
	member := readyCand("chain1", "", 9, 0)
	member.InAutomation = true
	other := readyCand("solo1", "", 1, 1)
	ds := plan(laneFillTestConfig(), testLanes(1, 0), []LaneFillCandidate{member, other})
	if d := byLane(t, ds, "gpu-1"); d.PRDID != "solo1" {
		t.Fatalf("got %v", summarize(ds))
	}
	// Alone, the member is never started and the lane says why.
	ds = plan(laneFillTestConfig(), testLanes(1, 0), []LaneFillCandidate{member})
	d := byLane(t, ds, "gpu-1")
	if d.Action != LaneFillActionIdle || !strings.Contains(d.Blocker, "chain1") || !strings.Contains(d.Blocker, "Automation") {
		t.Fatalf("got %+v", d)
	}
	// An already running Automaton is not started a second time.
	lanes := testLanes(2, 0)
	occupy(lanes, "gpu-1", "solo1", "pi-prompt")
	ds = plan(laneFillTestConfig(), lanes, []LaneFillCandidate{other})
	if got := actions(ds, LaneFillActionStartPRD); len(got) != 0 {
		t.Fatalf("started a running Automaton: %v", summarize(ds))
	}
}

func TestLaneFillPlanGPUTakesWorkWhileClaudeFull(t *testing.T) {
	lanes := testLanes(2, 3)
	for i := 1; i <= 3; i++ {
		occupy(lanes, fmt.Sprintf("claude-%d", i), fmt.Sprintf("run%d", i), "claude-code")
	}
	var cands []LaneFillCandidate
	for i := 0; i < 8; i++ {
		cands = append(cands, readyCand(fmt.Sprintf("q%d", i), "", 0, i))
	}
	ds := plan(laneFillTestConfig(), lanes, cands)
	starts := actions(ds, LaneFillActionStartPRD)
	if len(starts) != 2 || starts[0].LaneID != "gpu-1" || starts[1].LaneID != "gpu-2" {
		t.Fatalf("got %v", summarize(ds))
	}
	if starts[0].PRDID != "q0" || starts[1].PRDID != "q1" {
		t.Fatalf("got %v", summarize(ds))
	}
	for _, d := range ds {
		if strings.Contains(strings.ToLower(d.Blocker+d.Reason), "review") {
			t.Fatalf("a review backlog must never be a blocker: %+v", d)
		}
	}
}

func TestLaneFillPlanNoIdleLaneGivesBlockers(t *testing.T) {
	t.Run("all lanes full yields no lane decision", func(t *testing.T) {
		lanes := testLanes(2, 3)
		for _, l := range lanes {
			occupy(lanes, l.ID, "run-"+l.ID, "x")
		}
		ds := plan(laneFillTestConfig(), lanes, []LaneFillCandidate{readyCand("r1", "", 0, 0)})
		if got := actions(ds, LaneFillActionStartPRD); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("claude full and gpu work fixed on claude gives blockers", func(t *testing.T) {
		lanes := testLanes(1, 1)
		occupy(lanes, "claude-1", "run1", "claude-code")
		c := readyCand("fixed1", "", 0, 0)
		c.Backend, c.BackendChangeAllowed = "claude-code", false
		ds := plan(laneFillTestConfig(), lanes, []LaneFillCandidate{c})
		d := byLane(t, ds, "gpu-1")
		want := "PRD fixed1: backend fixed at claude-code, cannot move to the pool"
		if d.Action != LaneFillActionIdle || d.Blocker != want {
			t.Fatalf("got %+v, want blocker %q", d, want)
		}
	})
	t.Run("gpu full and claude work held gives a hold blocker on the claude lane", func(t *testing.T) {
		lanes := testLanes(1, 1)
		occupy(lanes, "gpu-1", "run1", "pi-prompt")
		c := readyCand("hold1", "claude", 0, 0)
		c.HoldReason = "a line starts with HOLD"
		ds := plan(laneFillTestConfig(), lanes, []LaneFillCandidate{c})
		d := byLane(t, ds, "claude-1")
		if d.Action != LaneFillActionIdle || !strings.Contains(d.Blocker, "hold1") {
			t.Fatalf("got %+v", d)
		}
	})
}

func TestLaneFillPlanPreferenceRouting(t *testing.T) {
	cfg := laneFillTestConfig()
	t.Run("gpu preference goes to the pool and claude preference to claude", func(t *testing.T) {
		cands := []LaneFillCandidate{readyCand("c1", "claude", 9, 0), readyCand("g1", "gpu", 1, 1)}
		ds := plan(cfg, testLanes(1, 1), cands)
		if d := byLane(t, ds, "gpu-1"); d.PRDID != "g1" || d.Action != LaneFillActionStartPRD {
			t.Fatalf("got %v", summarize(ds))
		}
		if d := byLane(t, ds, "claude-1"); d.PRDID != "c1" || d.Action != LaneFillActionStartPRD {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("own preference first even at lower priority", func(t *testing.T) {
		cands := []LaneFillCandidate{readyCand("any1", "", 9, 0), readyCand("g1", "gpu", 1, 1)}
		ds := plan(cfg, testLanes(1, 0), cands)
		if d := byLane(t, ds, "gpu-1"); d.PRDID != "g1" {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("gpu preference never starts on a claude lane", func(t *testing.T) {
		ds := plan(cfg, testLanes(0, 2), []LaneFillCandidate{readyCand("g1", "gpu", 0, 0)})
		if got := actions(ds, LaneFillActionStartPRD); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("claude preference never starts on the pool", func(t *testing.T) {
		ds := plan(cfg, testLanes(2, 0), []LaneFillCandidate{readyCand("c1", "claude", 0, 0)})
		if got := actions(ds, LaneFillActionStartPRD); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("any prefers an idle gpu lane", func(t *testing.T) {
		ds := plan(cfg, testLanes(1, 1), []LaneFillCandidate{readyCand("any1", "any", 0, 0)})
		if d := byLane(t, ds, "gpu-1"); d.PRDID != "any1" || d.Action != LaneFillActionStartPRD {
			t.Fatalf("got %v", summarize(ds))
		}
		if d := byLane(t, ds, "claude-1"); d.Action != LaneFillActionIdle {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("any falls back to a claude lane when the pool is full", func(t *testing.T) {
		lanes := testLanes(1, 1)
		occupy(lanes, "gpu-1", "run1", "pi-prompt")
		ds := plan(cfg, lanes, []LaneFillCandidate{readyCand("any1", "", 0, 0)})
		if d := byLane(t, ds, "claude-1"); d.PRDID != "any1" {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("a candidate fills one lane only", func(t *testing.T) {
		ds := plan(cfg, testLanes(2, 3), []LaneFillCandidate{readyCand("only", "", 0, 0)})
		if got := actions(ds, LaneFillActionStartPRD); len(got) != 1 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
}

func TestLaneFillPlanBackendFixed(t *testing.T) {
	cfg := laneFillTestConfig()
	fixedAny := func() LaneFillCandidate {
		c := readyCand("anyfix", "", 5, 0)
		c.Backend, c.BackendChangeAllowed = "claude-code", false
		return c
	}
	t.Run("any can move to the pool when the backend may change", func(t *testing.T) {
		c := fixedAny()
		c.BackendChangeAllowed = true
		ds := plan(cfg, testLanes(1, 0), []LaneFillCandidate{c})
		d := byLane(t, ds, "gpu-1")
		if d.PRDID != "anyfix" || !strings.Contains(d.Reason, "backend would change from claude-code to pi-prompt") {
			t.Fatalf("got %+v", d)
		}
	})
	t.Run("any already on the gpu backend needs no change", func(t *testing.T) {
		c := fixedAny()
		c.Backend = "pi-prompt"
		ds := plan(cfg, testLanes(1, 0), []LaneFillCandidate{c})
		d := byLane(t, ds, "gpu-1")
		if d.PRDID != "anyfix" || strings.Contains(d.Reason, "would change") {
			t.Fatalf("got %+v", d)
		}
	})
	t.Run("fixed any takes a claude lane and the pool records the blocker only when none is idle", func(t *testing.T) {
		ds := plan(cfg, testLanes(1, 1), []LaneFillCandidate{fixedAny()})
		if d := byLane(t, ds, "claude-1"); d.PRDID != "anyfix" || d.Action != LaneFillActionStartPRD {
			t.Fatalf("got %v", summarize(ds))
		}
		if d := byLane(t, ds, "gpu-1"); d.Action != LaneFillActionIdle || d.Blocker != "no eligible work" {
			t.Fatalf("pool blocker with the work placed elsewhere: %+v", d)
		}

		lanes := testLanes(1, 1)
		occupy(lanes, "claude-1", "run1", "claude-code")
		ds = plan(cfg, lanes, []LaneFillCandidate{fixedAny()})
		want := "PRD anyfix: backend fixed at claude-code, cannot move to the pool"
		if d := byLane(t, ds, "gpu-1"); d.Action != LaneFillActionIdle || d.Blocker != want {
			t.Fatalf("got %+v", d)
		}
		if got := actions(ds, LaneFillActionStartPRD); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("fixed gpu preference is blocked and never started on claude", func(t *testing.T) {
		c := readyCand("gpufix", "gpu", 5, 0)
		c.Backend, c.BackendChangeAllowed = "claude-code", false
		ds := plan(cfg, testLanes(1, 1), []LaneFillCandidate{c})
		if got := actions(ds, LaneFillActionStartPRD); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
		blocked := actions(ds, LaneFillActionBlocked)
		want := "PRD gpufix: backend fixed at claude-code, cannot move to the pool"
		if len(blocked) != 1 || blocked[0].PRDID != "gpufix" || blocked[0].Blocker != want || blocked[0].LaneType != LaneTypeGPU {
			t.Fatalf("got %v / %+v", summarize(ds), blocked)
		}
	})
	t.Run("fixed work does not hide the next usable item", func(t *testing.T) {
		ok := readyCand("usable", "", 1, 5)
		ds := plan(cfg, testLanes(1, 0), []LaneFillCandidate{fixedAny(), ok})
		if d := byLane(t, ds, "gpu-1"); d.PRDID != "usable" {
			t.Fatalf("got %v", summarize(ds))
		}
	})
}

func TestLaneFillPlanMergeFlagNeverStarts(t *testing.T) {
	cands := []LaneFillCandidate{
		{ID: "m1", Title: "merge one", Status: PRDApproved, AllTasksDone: true, NeedsMergeFlag: true, Priority: 99, BackendChangeAllowed: true},
		{ID: "m2", Title: "merge two", Status: PRDCompleted, AllTasksDone: true, NeedsMergeFlag: true},
	}
	ds := plan(laneFillTestConfig(), testLanes(2, 3), cands)
	if got := actions(ds, LaneFillActionStartPRD); len(got) != 0 {
		t.Fatalf("a merge candidate was started: %v", summarize(ds))
	}
	merges := actions(ds, LaneFillActionReportMerge)
	if len(merges) != 2 || merges[0].PRDID != "m1" || merges[1].PRDID != "m2" {
		t.Fatalf("got %v", summarize(ds))
	}
	for _, m := range merges {
		if m.Executed || m.LaneID != "" {
			t.Fatalf("merge report must be report-only: %+v", m)
		}
	}
	// The merge backlog is never the reason a lane is idle.
	for _, d := range actions(ds, LaneFillActionIdle) {
		if strings.Contains(d.Blocker, "merge") {
			t.Fatalf("merge named as a blocker: %+v", d)
		}
	}
}

func TestLaneFillPlanDecomposeAhead(t *testing.T) {
	cfg := laneFillTestConfig()
	cfg.DecomposeAheadMin = 2
	// Every lane busy, so only the decompose-ahead rule can act.
	full := func() []Lane {
		l := testLanes(2, 3)
		for _, x := range l {
			occupy(l, x.ID, "run-"+x.ID, "x")
		}
		return l
	}
	drafts := func() []LaneFillCandidate {
		return []LaneFillCandidate{draftCand("dnew", "", 20), draftCand("dold", "", 5), draftCand("dgpu", "gpu", 1), draftCand("dclaude", "claude", 2)}
	}
	t.Run("below the minimum picks the oldest fitting draft per type", func(t *testing.T) {
		ds := plan(cfg, full(), append(drafts(), readyCand("r1", "gpu", 0, 0)))
		ahead := actions(ds, LaneFillActionDecomposeAhead)
		if len(ahead) != 2 {
			t.Fatalf("got %v", summarize(ds))
		}
		if ahead[0].LaneType != LaneTypeGPU || ahead[0].PRDID != "dgpu" {
			t.Fatalf("gpu pick: %v", summarize(ds))
		}
		if ahead[1].LaneType != LaneTypeClaude || ahead[1].PRDID != "dclaude" {
			t.Fatalf("claude pick: %v", summarize(ds))
		}
	})
	t.Run("at the minimum does nothing", func(t *testing.T) {
		cands := append(drafts(), readyCand("r1", "", 0, 0), readyCand("r2", "", 0, 1))
		ds := plan(cfg, full(), cands)
		if got := actions(ds, LaneFillActionDecomposeAhead); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("above the minimum does nothing", func(t *testing.T) {
		cands := drafts()
		for i := 0; i < 5; i++ {
			cands = append(cands, readyCand(fmt.Sprintf("r%d", i), "", 0, i))
		}
		ds := plan(cfg, full(), cands)
		if got := actions(ds, LaneFillActionDecomposeAhead); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("one draft is never used for both types", func(t *testing.T) {
		ds := plan(cfg, full(), []LaneFillCandidate{draftCand("only", "", 0)})
		ahead := actions(ds, LaneFillActionDecomposeAhead)
		if len(ahead) != 1 || ahead[0].LaneType != LaneTypeGPU {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("a type with no fitting draft emits nothing for it", func(t *testing.T) {
		ds := plan(cfg, full(), []LaneFillCandidate{draftCand("dgpu", "gpu", 0)})
		ahead := actions(ds, LaneFillActionDecomposeAhead)
		if len(ahead) != 1 || ahead[0].LaneType != LaneTypeGPU {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("held draft is skipped", func(t *testing.T) {
		held := draftCand("dheld", "", 0)
		held.HoldReason = "do not run"
		ds := plan(cfg, full(), []LaneFillCandidate{held, draftCand("dok", "", 9)})
		ahead := actions(ds, LaneFillActionDecomposeAhead)
		if len(ahead) == 0 || ahead[0].PRDID != "dok" {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("in-flight decompositions at the lane count stop it", func(t *testing.T) {
		in := LaneFillInput{
			Config: cfg, Lanes: full(), Candidates: drafts(), Now: laneFillTestNow,
			InFlightDecomposeByType: map[LaneType]int{LaneTypeGPU: 2, LaneTypeClaude: 3},
		}
		if got := actions(PlanLaneFill(in), LaneFillActionDecomposeAhead); len(got) != 0 {
			t.Fatalf("got %v", summarize(got))
		}
		in.InFlightDecomposeByType = map[LaneType]int{LaneTypeGPU: 2, LaneTypeClaude: 2}
		got := actions(PlanLaneFill(in), LaneFillActionDecomposeAhead)
		if len(got) != 1 || got[0].LaneType != LaneTypeClaude {
			t.Fatalf("below the cap for claude only: %v", summarize(got))
		}
	})
	t.Run("a minimum of zero never decomposes ahead", func(t *testing.T) {
		z := cfg
		z.DecomposeAheadMin = 0
		ds := plan(z, full(), drafts())
		if got := actions(ds, LaneFillActionDecomposeAhead); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("a lane type with no lanes never decomposes ahead", func(t *testing.T) {
		l := testLanes(1, 0)
		occupy(l, "gpu-1", "run1", "pi-prompt")
		ds := plan(cfg, l, []LaneFillCandidate{draftCand("dclaude", "claude", 0)})
		if got := actions(ds, LaneFillActionDecomposeAhead); len(got) != 0 {
			t.Fatalf("got %v", summarize(ds))
		}
	})
	t.Run("an idle lane's own decompose counts toward the cap", func(t *testing.T) {
		l := testLanes(1, 1)
		occupy(l, "claude-1", "run1", "claude-code")
		ds := plan(cfg, l, []LaneFillCandidate{draftCand("d1", "", 0), draftCand("d2", "", 1)})
		if got := actions(ds, LaneFillActionDecompose); len(got) != 1 || got[0].PRDID != "d1" {
			t.Fatalf("got %v", summarize(ds))
		}
		for _, d := range actions(ds, LaneFillActionDecomposeAhead) {
			if d.LaneType == LaneTypeGPU {
				t.Fatalf("the gpu type is already at its cap: %v", summarize(ds))
			}
		}
	})
	t.Run("a decompose at the cap leaves the lane idle with a reason", func(t *testing.T) {
		in := LaneFillInput{
			Config: cfg, Lanes: testLanes(1, 0), Candidates: []LaneFillCandidate{draftCand("d1", "", 0)}, Now: laneFillTestNow,
			InFlightDecomposeByType: map[LaneType]int{LaneTypeGPU: 1},
		}
		d := byLane(t, PlanLaneFill(in), "gpu-1")
		if d.Action != LaneFillActionIdle || !strings.Contains(d.Blocker, "d1") {
			t.Fatalf("got %+v", d)
		}
	})
}

func TestLaneFillPlanEvidenceError(t *testing.T) {
	cfg := laneFillTestConfig()
	model := BuildLanes(cfg, nil, LaneEvidence{Err: "model host unreachable"})
	want := "pool evidence unavailable: model host unreachable"
	for _, l := range model.Lanes {
		if l.Type == LaneTypeGPU && l.Blocker != want {
			t.Fatalf("lane %s blocker = %q", l.ID, l.Blocker)
		}
		if l.Type == LaneTypeClaude && l.Blocker != "" {
			t.Fatalf("claude lane %s got blocker %q", l.ID, l.Blocker)
		}
	}
	cands := []LaneFillCandidate{readyCand("r1", "", 0, 0), readyCand("r2", "", 0, 1), readyCand("r3", "gpu", 0, 2), draftCand("d1", "", 3)}

	// Lanes carrying the blocker.
	ds := PlanLaneFill(LaneFillInput{Config: cfg, Lanes: model.Lanes, Candidates: cands, EvidenceErr: "model host unreachable", Now: laneFillTestNow})
	checkEvidenceBlocked(t, ds, want)

	// Lanes built without the blocker still must not be called idle.
	plain := testLanes(2, 3)
	ds = PlanLaneFill(LaneFillInput{Config: cfg, Lanes: plain, Candidates: cands, EvidenceErr: "model host unreachable", Now: laneFillTestNow})
	checkEvidenceBlocked(t, ds, want)
}

func checkEvidenceBlocked(t *testing.T, ds []LaneFillDecision, want string) {
	t.Helper()
	for _, id := range []string{"gpu-1", "gpu-2"} {
		d := byLane(t, ds, id)
		if d.Action != LaneFillActionBlocked || d.Blocker != want || d.PRDID != "" {
			t.Fatalf("lane %s: %+v", id, d)
		}
	}
	for _, d := range ds {
		if d.Action == LaneFillActionIdle && d.LaneType == LaneTypeGPU {
			t.Fatalf("a gpu lane was called idle: %+v", d)
		}
		if d.LaneType == LaneTypeGPU && (d.Action == LaneFillActionStartPRD || d.Action == LaneFillActionDecompose || d.Action == LaneFillActionDecomposeAhead) {
			t.Fatalf("gpu work was planned with unknown pool state: %+v", d)
		}
	}
	// Claude lanes are unaffected.
	if d := byLane(t, ds, "claude-1"); d.Action != LaneFillActionStartPRD {
		t.Fatalf("claude lane: %+v", d)
	}
}

func TestLaneFillPlanBuildLanes(t *testing.T) {
	cfg := laneFillTestConfig()
	run := func(id, backend string) LaneOccupant {
		return LaneOccupant{PRDID: id, Title: "t " + id, Backend: backend}
	}

	t.Run("lane counts and ids", func(t *testing.T) {
		m := BuildLanes(cfg, nil, LaneEvidence{})
		var ids []string
		for _, l := range m.Lanes {
			ids = append(ids, l.ID)
			if l.Occupant != nil || l.Blocker != "" {
				t.Fatalf("lane %s should be idle", l.ID)
			}
		}
		if !reflect.DeepEqual(ids, []string{"gpu-1", "gpu-2", "claude-1", "claude-2", "claude-3"}) {
			t.Fatalf("ids = %v", ids)
		}
	})
	t.Run("zero claude lanes", func(t *testing.T) {
		c := cfg
		c.ClaudeLanes = 0
		if m := BuildLanes(c, nil, LaneEvidence{}); len(m.Lanes) != 2 {
			t.Fatalf("lanes = %d", len(m.Lanes))
		}
	})
	t.Run("backends map to lane types", func(t *testing.T) {
		m := BuildLanes(cfg, []LaneOccupant{run("a", "pi-prompt"), run("b", "claude-code"), run("c", "opencode")}, LaneEvidence{})
		if m.Lanes[0].Occupant == nil || m.Lanes[0].Occupant.PRDID != "a" {
			t.Fatalf("gpu-1: %+v", m.Lanes[0])
		}
		if m.Lanes[2].Occupant == nil || m.Lanes[2].Occupant.PRDID != "b" {
			t.Fatalf("claude-1: %+v", m.Lanes[2])
		}
		if len(m.Unlaned) != 1 || m.Unlaned[0].PRDID != "c" {
			t.Fatalf("unlaned = %+v", m.Unlaned)
		}
		if lt, ok := LaneTypeForBackend(cfg, "opencode"); ok || lt != "" {
			t.Fatalf("opencode mapped to %q", lt)
		}
	})
	t.Run("extras never create an idle lane", func(t *testing.T) {
		m := BuildLanes(cfg, []LaneOccupant{run("a", "pi-prompt"), run("b", "pi-prompt"), run("c", "pi-prompt")}, LaneEvidence{})
		if len(m.Lanes) != 5 || len(m.Extra) != 1 || m.Extra[0].PRDID != "c" {
			t.Fatalf("lanes %d extra %+v", len(m.Lanes), m.Extra)
		}
		if m.Lanes[0].Occupant == nil || m.Lanes[1].Occupant == nil {
			t.Fatal("both gpu lanes should be taken")
		}
	})
	t.Run("evidence larger than the automata holds more lanes", func(t *testing.T) {
		m := BuildLanes(cfg, []LaneOccupant{run("a", "pi-prompt")}, LaneEvidence{GPUBusy: 2})
		if m.Lanes[1].Occupant == nil || m.Lanes[1].Occupant.PRDID != "" {
			t.Fatalf("gpu-2 should hold a session that belongs to no Automaton: %+v", m.Lanes[1])
		}
		ds := plan(cfg, m.Lanes, []LaneFillCandidate{readyCand("r1", "gpu", 0, 0)})
		if got := actions(ds, LaneFillActionStartPRD); len(got) != 0 {
			t.Fatalf("started onto a busy pool: %v", summarize(ds))
		}
	})
	t.Run("automata larger than the evidence is not double counted", func(t *testing.T) {
		m := BuildLanes(cfg, []LaneOccupant{run("a", "pi-prompt"), run("b", "pi-prompt")}, LaneEvidence{GPUBusy: 1})
		if len(m.Extra) != 0 || m.Lanes[0].Occupant.PRDID != "a" || m.Lanes[1].Occupant.PRDID != "b" {
			t.Fatalf("lanes %+v extra %+v", m.Lanes, m.Extra)
		}
	})
	t.Run("evidence with no automata is capped at the lane count", func(t *testing.T) {
		m := BuildLanes(cfg, nil, LaneEvidence{GPUBusy: 7})
		if len(m.Lanes) != 5 || m.Lanes[0].Occupant == nil || m.Lanes[1].Occupant == nil || m.Lanes[2].Occupant != nil {
			t.Fatalf("lanes %+v", m.Lanes)
		}
	})
	t.Run("an evidence error leaves no idle gpu lane", func(t *testing.T) {
		m := BuildLanes(cfg, []LaneOccupant{run("a", "pi-prompt")}, LaneEvidence{Err: "boom"})
		if m.Lanes[0].Blocker != "" {
			t.Fatalf("occupied lane got a blocker: %+v", m.Lanes[0])
		}
		if m.Lanes[1].Blocker != "pool evidence unavailable: boom" {
			t.Fatalf("gpu-2: %+v", m.Lanes[1])
		}
	})
	t.Run("input order does not change the placement", func(t *testing.T) {
		a := BuildLanes(cfg, []LaneOccupant{run("a", "pi-prompt"), run("b", "pi-prompt")}, LaneEvidence{})
		b := BuildLanes(cfg, []LaneOccupant{run("b", "pi-prompt"), run("a", "pi-prompt")}, LaneEvidence{})
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%+v vs %+v", a, b)
		}
	})
}

func TestLaneFillPlanDeterministicOrder(t *testing.T) {
	var cands []LaneFillCandidate
	for i := 0; i < 6; i++ {
		cands = append(cands, readyCand(fmt.Sprintf("r%d", i), []string{"", "gpu", "claude"}[i%3], i%2, i))
	}
	cands = append(cands,
		draftCand("d1", "", 7), draftCand("d2", "gpu", 8),
		LaneFillCandidate{ID: "m1", Status: PRDCompleted, AllTasksDone: true, NeedsMergeFlag: true},
		LaneFillCandidate{ID: "m2", Status: PRDCompleted, AllTasksDone: true, NeedsMergeFlag: true, Priority: 3},
	)
	c := readyCand("gfix", "gpu", 0, 9)
	c.Backend, c.BackendChangeAllowed = "claude-code", false
	cands = append(cands, c)

	cfg := laneFillTestConfig()
	lanes := testLanes(2, 3)
	occupy(lanes, "claude-3", "busy", "claude-code")
	base := plan(cfg, lanes, cands)
	if len(base) == 0 {
		t.Fatal("no decisions")
	}

	// Reversed and rotated inputs give the same decisions in the same order.
	rev := make([]LaneFillCandidate, len(cands))
	for i, c := range cands {
		rev[len(cands)-1-i] = c
	}
	rot := append(append([]LaneFillCandidate(nil), cands[4:]...), cands[:4]...)
	for name, in := range map[string][]LaneFillCandidate{"reversed": rev, "rotated": rot} {
		if got := plan(cfg, lanes, in); !reflect.DeepEqual(base, got) {
			t.Fatalf("%s input changed the output:\n%v\nvs\n%v", name, summarize(base), summarize(got))
		}
	}
	// The same input twice.
	if got := plan(cfg, lanes, cands); !reflect.DeepEqual(base, got) {
		t.Fatal("repeat run differs")
	}

	// Group order: lane decisions, blocked, merge reports, decompose ahead.
	rank := map[LaneFillAction]int{
		LaneFillActionStartPRD: 0, LaneFillActionDecompose: 0, LaneFillActionIdle: 0,
		LaneFillActionBlocked: 1, LaneFillActionReportMerge: 2, LaneFillActionDecomposeAhead: 3,
	}
	last := 0
	for _, d := range base {
		if rank[d.Action] < last {
			t.Fatalf("group order broken: %v", summarize(base))
		}
		last = rank[d.Action]
	}
	merges := actions(base, LaneFillActionReportMerge)
	if len(merges) != 2 || merges[0].PRDID != "m2" || merges[1].PRDID != "m1" {
		t.Fatalf("merge order: %v", summarize(base))
	}
	// Lane decisions run gpu lanes first, in lane order.
	var laneIDs []string
	for _, d := range base {
		if d.LaneID != "" {
			laneIDs = append(laneIDs, d.LaneID)
		}
	}
	if !reflect.DeepEqual(laneIDs, []string{"gpu-1", "gpu-2", "claude-1", "claude-2"}) {
		t.Fatalf("lane order = %v", laneIDs)
	}
}

func TestLaneFillPlanDecisionFields(t *testing.T) {
	cfg := laneFillTestConfig()
	ds := plan(cfg, testLanes(1, 0), []LaneFillCandidate{readyCand("r1", "", 4, 0)})
	d := byLane(t, ds, "gpu-1")
	if d.At != laneFillTestNow || d.LastSeenAt != laneFillTestNow || d.RepeatCount != 1 {
		t.Fatalf("time fields: %+v", d)
	}
	if d.Mode != LaneFillModeShadow || d.LaneType != LaneTypeGPU || d.Title != "title r1" {
		t.Fatalf("fields: %+v", d)
	}
	if d.Executed || d.Error != "" || d.Blocker != "" || d.Reason == "" {
		t.Fatalf("planner must not execute or fail: %+v", d)
	}
}
