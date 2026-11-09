package autonomous

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

// TestRestartGuardLists — the restart guard's report at manager level: a
// manager holding exactly one planning Automaton, one verifying task (whose
// adopted verifier is live) and one running guardrail judge reports exactly
// those three and nothing else; the report is read-only (store, sessions and
// judge host untouched); and the force question follows the policy: "draft"
// asks for force, while "reattach" with everything covered does not.

func TestRestartGuardLists(t *testing.T) {
	// The verifying task: worker session dead, adopted verifier live —
	// a re-attach path keeps it (covered under reattach).
	m, prd := verifyingBootManager(t, []Story{
		{Title: "story", Tasks: []Task{
			{Title: "task", Status: TaskVerifying,
				SessionID:         "worker-1",
				VerifierSessionID: "verifier-1",
				AttemptCommitSHAs: []string{"abc123"},
			},
		}},
	})
	tid := prd.Story[0].Tasks[0].ID

	// The planning Automaton: its decompose is in flight.
	plan, err := m.CreatePRD("still planning", "/projplanning", "", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	plan, _ = m.Store().GetPRD(plan.ID)
	plan.Status = PRDPlanning
	_ = m.Store().SavePRD(plan)

	// The running guardrail judge: one live judge on the verifying task,
	// which still needs its judgment — the boot reconcile adopts it (covered).
	host := &fakeJudgeHost{
		live:  []JudgeSession{{ID: "judge-1", UnitID: tid, Started: time.Now()}},
		ended: map[string]string{},
		bound: map[string]string{},
	}
	m.SetJudgeHost(host)
	m.SetSessionListerFn(func() []SessionInfo {
		return []SessionInfo{
			{ID: "worker-1", Alive: false},
			{ID: "verifier-1", Alive: true},
			{ID: "judge-1", Alive: true},
		}
	})

	// Read-only check: the full store snapshot, compared before and after.
	snapshot := func() map[string][]byte {
		out := map[string][]byte{}
		for _, p := range m.Store().ListPRDs() {
			b, err := json.Marshal(p)
			if err != nil {
				t.Fatalf("marshal prd %s: %v", p.ID, err)
			}
			out[p.ID] = b
		}
		return out
	}
	liveJudgeIDs := func() []string {
		ids := make([]string, 0, len(host.live))
		for _, s := range host.live {
			ids = append(ids, s.ID)
		}
		sort.Strings(ids)
		return ids
	}

	// Policy "draft": a restart interrupts the planning Automaton, so force
	// is asked even though the verifying task and the judge are covered.
	cfg := m.Config()
	cfg.PlanningRestartPolicy = config.PlanningRestartPolicyDraft
	m.SetConfig(cfg)

	before := snapshot()
	rep := m.RestartInterruptions()

	// Exactly the planning Automaton, the verifying task and the judge —
	// and nothing else.
	if rep.PlanningCnt != 1 || len(rep.PlanningAutomata) != 1 || rep.PlanningAutomata[0].ID != plan.ID {
		t.Fatalf("want exactly the one planning Automaton %s, got %d: %+v", plan.ID, rep.PlanningCnt, rep.PlanningAutomata)
	}
	if rep.VerifyingTaskCnt != 1 || len(rep.VerifyingTasks) != 1 {
		t.Fatalf("want exactly one verifying task, got %d: %+v", rep.VerifyingTaskCnt, rep.VerifyingTasks)
	}
	v := rep.VerifyingTasks[0]
	if v.PRDID != prd.ID || v.TaskID != tid || v.VerifierSessionID != "verifier-1" || !v.Covered {
		t.Fatalf("verifying task entry must be the one held, covered by its live verifier: %+v", v)
	}
	if rep.RunningJudgeCnt != 1 || len(rep.RunningJudges) != 1 {
		t.Fatalf("want exactly one running judge, got %d: %+v", rep.RunningJudgeCnt, rep.RunningJudges)
	}
	j := rep.RunningJudges[0]
	if j.SessionID != "judge-1" || j.UnitID != tid || !j.Covered {
		t.Fatalf("judge entry must be the one held, covered by adoption: %+v", j)
	}
	// The only running task is the verifying one.
	if rep.RunningTaskCnt != 1 || len(rep.RunningTasks) != 1 ||
		rep.RunningTasks[0].PRDID != prd.ID || rep.RunningTasks[0].TaskID != tid {
		t.Fatalf("want exactly the one held task listed, got %+v", rep.RunningTasks)
	}

	// Read-only: the store, the judge host and the session list are untouched.
	if !reflect.DeepEqual(snapshot(), before) {
		t.Fatal("the report must not mutate the PRD store")
	}
	if len(host.ended) != 0 {
		t.Fatalf("the report must not end sessions, ended=%v", host.ended)
	}
	if len(host.bound) != 0 {
		t.Fatalf("the report must not re-bind leases, bound=%v", host.bound)
	}
	if got := liveJudgeIDs(); len(got) != 1 || got[0] != "judge-1" {
		t.Fatalf("the judge host must be untouched, live=%v", got)
	}

	// Force question: draft always asks for force.
	if !rep.NeedsForce {
		t.Fatalf("policy %q must ask for force with work to interrupt, got NeedsForce=false", config.PlanningRestartPolicyDraft)
	}

	// Policy "reattach" with every verifying task and judge covered: no force
	// is needed — boot re-attaches all of them.
	cfg = m.Config()
	cfg.PlanningRestartPolicy = config.PlanningRestartPolicyReattach
	m.SetConfig(cfg)

	rep = m.RestartInterruptions()
	if rep.NeedsForce {
		t.Fatalf("policy %q with everything covered must not ask for force: %+v", config.PlanningRestartPolicyReattach, rep)
	}
	// The lists are the same three things under either policy.
	if rep.PlanningCnt != 1 || rep.VerifyingTaskCnt != 1 || rep.RunningJudgeCnt != 1 || rep.RunningTaskCnt != 1 {
		t.Fatalf("the lists must not change with the policy: %+v", rep)
	}
	if !reflect.DeepEqual(snapshot(), before) {
		t.Fatal("the report must stay read-only under reattach")
	}
}
