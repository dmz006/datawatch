package autonomous

import (
	"strings"
	"testing"
)

// restartReportLookup returns a judge-unit lookup over fixed unit states.
func restartReportLookup(units map[string]JudgeUnitInfo) func(string) (JudgeUnitInfo, bool) {
	return func(id string) (JudgeUnitInfo, bool) {
		u, ok := units[id]
		return u, ok
	}
}

func restartReportTask(id string, st TaskStatus, sess, verifier string) Task {
	return Task{ID: id, Status: st, SessionID: sess, VerifierSessionID: verifier}
}

func TestRestartInterruptionListsPlanningAutomataVerifyingTasksAndRunningJudges(t *testing.T) {
	prds := []*PRD{
		{ID: "pC", Title: "Gamma", Status: PRDPlanning},
		{ID: "pA", Title: "Alpha", Status: PRDPlanning},
		{ID: "pB", Title: "Beta", Status: PRDRunning, Story: []Story{{Tasks: []Task{
			restartReportTask("t2", TaskVerifying, "w2", ""),   // worker dead, no verifier: not covered
			restartReportTask("t1", TaskVerifying, "w1", "v1"), // worker alive: covered
			restartReportTask("t3", TaskInProgress, "w3", ""),  // running, not verifying
			restartReportTask("t4", TaskCompleted, "", ""),     // neither
		}}}},
	}
	sessions := []SessionInfo{{ID: "w1", Alive: true}, {ID: "v1", Alive: true}, {ID: "w3", Alive: true}}
	judges := []JudgeSession{{ID: "j2", UnitID: "t2"}, {ID: "j1", UnitID: "t1"}, {ID: "j9", UnitID: "gone"}}
	lookup := restartReportLookup(map[string]JudgeUnitInfo{
		"t1": {PRDID: "pB", NeedsJudge: true},
		"t2": {PRDID: "pB", NeedsJudge: false}, // finished: its judge is ended by the plan
	})

	got := BuildRestartInterruption(prds, sessions, judges, lookup, "reattach")

	var planning []string
	for _, p := range got.PlanningAutomata {
		planning = append(planning, p.ID+":"+p.Title)
	}
	if strings.Join(planning, " ") != "pA:Alpha pC:Gamma" || got.PlanningCnt != 2 {
		t.Fatalf("planning automata = %v (count %d), want pA:Alpha pC:Gamma and 2", planning, got.PlanningCnt)
	}

	var verifying []string
	for _, v := range got.VerifyingTasks {
		verifying = append(verifying, v.PRDID+"/"+v.TaskID+":"+map[bool]string{true: "covered", false: "uncovered"}[v.Covered])
	}
	if strings.Join(verifying, " ") != "pB/t1:covered pB/t2:uncovered" || got.VerifyingTaskCnt != 2 {
		t.Fatalf("verifying tasks = %v (count %d)", verifying, got.VerifyingTaskCnt)
	}

	var judgesOut []string
	for _, j := range got.RunningJudges {
		judgesOut = append(judgesOut, j.SessionID+"/"+j.UnitID+":"+map[bool]string{true: "covered", false: "uncovered"}[j.Covered])
	}
	if strings.Join(judgesOut, " ") != "j1/t1:covered j2/t2:uncovered j9/gone:uncovered" || got.RunningJudgeCnt != 3 {
		t.Fatalf("running judges = %v (count %d)", judgesOut, got.RunningJudgeCnt)
	}

	// Existing fields stay as they were.
	if got.RunningTaskCnt != 3 || got.LiveSessionCnt != 3 {
		t.Fatalf("running tasks %d, live sessions %d; want 3 and 3", got.RunningTaskCnt, got.LiveSessionCnt)
	}
	if !got.NeedsForce {
		t.Fatalf("NeedsForce must be true: t2 and j2 are uncovered")
	}
}

func TestRestartInterruptionNeedsForceFalseWhenAllCoveredAndReattach(t *testing.T) {
	prds := []*PRD{{ID: "p1", Story: []Story{{Tasks: []Task{
		restartReportTask("t1", TaskVerifying, "w1", ""),
		restartReportTask("t2", TaskVerifying, "w2", "v2"),
	}}}}}
	sessions := []SessionInfo{{ID: "w1", Alive: true}, {ID: "w2", Alive: false}, {ID: "v2", Alive: true}}
	judges := []JudgeSession{{ID: "j1", UnitID: "t1"}}
	lookup := restartReportLookup(map[string]JudgeUnitInfo{"t1": {PRDID: "p1", NeedsJudge: true}})

	got := BuildRestartInterruption(prds, sessions, judges, lookup, "reattach")
	if got.NeedsForce {
		t.Fatalf("NeedsForce = true with reattach and every verifying task and judge covered: %+v", got)
	}
	// An unset policy normalizes to reattach.
	if def := BuildRestartInterruption(prds, sessions, judges, lookup, ""); def.NeedsForce {
		t.Fatalf("NeedsForce = true with the default (reattach) policy and all covered")
	}
	// Nothing to cover at all is also not a reason to force.
	if empty := BuildRestartInterruption(nil, nil, nil, nil, "reattach"); empty.NeedsForce {
		t.Fatalf("NeedsForce = true for an empty report under reattach")
	}
}

func TestRestartInterruptionNeedsForceTrueWithDraftOrRestart(t *testing.T) {
	prds := []*PRD{{ID: "p1", Story: []Story{{Tasks: []Task{restartReportTask("t1", TaskVerifying, "w1", "")}}}}}
	sessions := []SessionInfo{{ID: "w1", Alive: true}}
	judges := []JudgeSession{{ID: "j1", UnitID: "t1"}}
	lookup := restartReportLookup(map[string]JudgeUnitInfo{"t1": {PRDID: "p1", NeedsJudge: true}})

	for _, policy := range []string{"draft", "restart"} {
		got := BuildRestartInterruption(prds, sessions, judges, lookup, policy)
		if !got.NeedsForce {
			t.Fatalf("policy %s: NeedsForce = false, want true even with everything covered: %+v", policy, got)
		}
	}
	// Under reattach an uncovered verifying task still needs force.
	uncovered := BuildRestartInterruption(prds, []SessionInfo{{ID: "w1", Alive: false}}, nil, nil, "reattach")
	if !uncovered.NeedsForce {
		t.Fatalf("reattach with an uncovered verifying task: NeedsForce = false, want true")
	}
}

func TestRestartInterruptionManagerReadsLiveStateWithoutMutation(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	host := &fakeJudgeHost{live: []JudgeSession{{ID: "j1", UnitID: "missing-unit"}}}
	m.SetJudgeHost(host)
	m.SetSessionListerFn(func() []SessionInfo { return []SessionInfo{{ID: "s1", Alive: true}} })

	got := m.RestartInterruptions()
	if got.RunningJudgeCnt != 1 || got.RunningJudges[0].SessionID != "j1" || got.RunningJudges[0].Covered {
		t.Fatalf("manager report judges = %+v, want one uncovered judge j1", got.RunningJudges)
	}
	if got.LiveSessionCnt != 1 || !got.NeedsForce {
		t.Fatalf("manager report = %+v", got)
	}
	// Read-only: no judge was ended or re-bound, and the live list is unchanged.
	if len(host.ended) != 0 || len(host.bound) != 0 || len(host.live) != 1 {
		t.Fatalf("report mutated the judge host: ended=%v bound=%v live=%v", host.ended, host.bound, host.live)
	}
}
