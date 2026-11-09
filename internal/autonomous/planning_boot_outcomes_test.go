package autonomous

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

var errTestIngest = errors.New("ingest refused by the test fake")

// bootOutcomeFake is the set of seams a boot-resume planning test swaps in:
// the planner output reader, the ingest, the decompose starter and the
// decision recorder. The live-session lookup and the killer are the Manager's
// injected functions, set on the fixture.
type bootOutcomeFake struct {
	output    []byte
	readCalls int
	ingested  []string
	ingestErr error
	starts    []string
	decisions []BootResumeEntry
	killed    []string
	live      []string
	prdID     string
	m         *Manager
	api       *API
}

func newBootOutcomeFake(t *testing.T, plannerID, outputFile string, live []string, restartedOnce bool) *bootOutcomeFake {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDPlanning
	p.PlannerSessionID = plannerID
	p.PlannerOutputFile = outputFile
	p.PlanningRestartedOnce = restartedOnce
	_ = m.Store().SavePRD(p)

	f := &bootOutcomeFake{m: m, api: api, prdID: prd.ID, live: live}
	m.SetDecomposeSessionsFn(func(id string) []string {
		if id != prd.ID {
			return nil
		}
		return f.live
	})
	m.SetSessionKillerFn(func(id, _, _ string) error { f.killed = append(f.killed, id); return nil })
	m.SetPlanningReattachFn(nil)
	return f
}

func (f *bootOutcomeFake) hooks() planningResumeHooks {
	return planningResumeHooks{
		readOutput: func(_ *PRD, _ string) []byte {
			f.readCalls++
			return f.output
		},
		ingest: func(_ string, raw string) error {
			f.ingested = append(f.ingested, raw)
			return f.ingestErr
		},
		startDecompose: func(id string) { f.starts = append(f.starts, id) },
		recordDecision: func(e BootResumeEntry) { f.decisions = append(f.decisions, e) },
	}
}

func (f *bootOutcomeFake) run(t *testing.T) *PRD {
	t.Helper()
	p, ok := f.m.Store().GetPRD(f.prdID)
	if !ok {
		t.Fatalf("prd %s missing", f.prdID)
	}
	f.api.bootResumePlanningWith(p, f.hooks())
	got, _ := f.m.Store().GetPRD(f.prdID)
	return got
}

func (f *bootOutcomeFake) onlyDecision(t *testing.T) BootResumeEntry {
	t.Helper()
	if len(f.decisions) != 1 {
		t.Fatalf("want exactly one decision, got %d: %+v", len(f.decisions), f.decisions)
	}
	return f.decisions[0]
}

func TestBootResume_PlanningIngestRecordsRecoveredDecision(t *testing.T) {
	f := newBootOutcomeFake(t, "dec-done", "/w/proj/.decompose-output-a.json", nil, false)
	f.output = []byte(validPlanJSON)

	got := f.run(t)

	if len(f.ingested) != 1 || f.ingested[0] != validPlanJSON {
		t.Fatalf("ingest calls = %q, want the output file content once", f.ingested)
	}
	if len(f.starts) != 0 {
		t.Fatalf("ingest must not start a new planner, got %v", f.starts)
	}
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Fatalf("planner record not cleared after ingest: %q %q", got.PlannerSessionID, got.PlannerOutputFile)
	}
	d := f.onlyDecision(t)
	if d.Outcome != "planning_ingested" || !strings.Contains(d.Reason, "recovered from the planner output file") {
		t.Fatalf("decision = %+v, want planning_ingested naming the output file recovery", d)
	}
	if d.PRDID != f.prdID || d.At.IsZero() || d.At.After(time.Now().Add(time.Minute)) {
		t.Fatalf("decision identity/time wrong: %+v", d)
	}
}

func TestBootResume_PlanningIngestRealPathStoresPlanAsDecompose(t *testing.T) {
	out := t.TempDir() + "/plan.json"
	if err := os.WriteFile(out, []byte(validPlanJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDPlanning
	p.PlannerSessionID = "dec-gone"
	p.PlannerOutputFile = out
	_ = m.Store().SavePRD(p)
	var decisions []BootResumeEntry
	starts := 0
	h := api.productionPlanningResumeHooks()
	h.startDecompose = func(string) { starts++ }
	h.recordDecision = func(e BootResumeEntry) { decisions = append(decisions, e) }

	api.bootResumePlanningWith(p, h)

	got, _ := m.Store().GetPRD(prd.ID)
	if got.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want %s: the output is stored like a normal decompose", got.Status, PRDNeedsReview)
	}
	if len(got.Story) == 0 {
		t.Fatal("stories not attached from the recovered output")
	}
	if starts != 0 || len(decisions) != 1 || decisions[0].Outcome != "planning_ingested" {
		t.Fatalf("starts=%d decisions=%+v", starts, decisions)
	}
}

func TestBootResume_PlanningIngestFailureFallsBackToDraft(t *testing.T) {
	f := newBootOutcomeFake(t, "dec-done", "/w/proj/.decompose-output-b.json", nil, false)
	f.output = []byte(validPlanJSON)
	f.ingestErr = errTestIngest

	got := f.run(t)

	if got.Status != PRDDraft {
		t.Fatalf("status = %s, want %s when ingest fails", got.Status, PRDDraft)
	}
	if d := f.onlyDecision(t); d.Outcome != "planning_reset_to_draft" {
		t.Fatalf("outcome = %s, want planning_reset_to_draft", d.Outcome)
	}
}

func TestBootResume_PlanningIngestKillsUnownedLiveSessionAndSaysSo(t *testing.T) {
	f := newBootOutcomeFake(t, "dec-done", "/w/proj/.decompose-output-c.json", []string{"dec-stray"}, false)
	f.output = []byte(validPlanJSON)

	f.run(t)

	if len(f.killed) != 1 || f.killed[0] != "dec-stray" {
		t.Fatalf("killed = %v, want the unowned live planner dec-stray", f.killed)
	}
	if d := f.onlyDecision(t); !strings.Contains(d.Reason, "1 live planner session(s) with no reader were killed") {
		t.Fatalf("decision does not say the orphan was killed: %q", d.Reason)
	}
}

func TestBootResume_PlanningRestartOncePolicyRestartStartsPlanningAgain(t *testing.T) {
	f := newBootOutcomeFake(t, "dec-gone", "/w/proj/.decompose-output-d.json", nil, false)
	c := f.m.Config()
	c.PlanningRestartPolicy = "restart"
	f.m.SetConfig(c)

	got := f.run(t)

	if len(f.starts) != 1 || f.starts[0] != f.prdID {
		t.Fatalf("starts = %v, want planning started once", f.starts)
	}
	if !got.PlanningRestartedOnce {
		t.Fatal("PlanningRestartedOnce not set after the restart")
	}
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Fatalf("planner record not cleared on restart: %q %q", got.PlannerSessionID, got.PlannerOutputFile)
	}
	d := f.onlyDecision(t)
	if d.Outcome != "planning_restarted" || !strings.Contains(d.Reason, "policy=restart") {
		t.Fatalf("decision = %+v, want planning_restarted naming policy=restart", d)
	}
}

func TestBootResume_PlanningRestartOnceNoSessionNoOutputRestarts(t *testing.T) {
	f := newBootOutcomeFake(t, "dec-gone", "/w/proj/.decompose-output-e.json", nil, false)

	f.run(t)

	if len(f.starts) != 1 {
		t.Fatalf("starts = %v, want one restart when the session is gone with no output", f.starts)
	}
	if d := f.onlyDecision(t); d.Outcome != "planning_restarted" || !strings.Contains(d.Reason, "no usable output") {
		t.Fatalf("decision = %+v", d)
	}
}

func TestBootResume_PlanningRestartOnceKillsLiveSessionWithoutReader(t *testing.T) {
	f := newBootOutcomeFake(t, "dec-live", "/w/proj/.decompose-output-f.json", []string{"dec-live"}, false)
	c := f.m.Config()
	c.PlanningRestartPolicy = "restart"
	f.m.SetConfig(c)

	f.run(t)

	if len(f.killed) != 1 || f.killed[0] != "dec-live" {
		t.Fatalf("killed = %v, want the reader-less live planner killed before restarting", f.killed)
	}
	if len(f.starts) != 1 {
		t.Fatalf("starts = %v, want one restart", f.starts)
	}
	if d := f.onlyDecision(t); !strings.Contains(d.Reason, "1 live planner session(s) with no reader were killed") {
		t.Fatalf("decision does not say the session was killed: %q", d.Reason)
	}
}

func TestBootResume_PlanningRestartOnceSecondRestartFallsBackToDraft(t *testing.T) {
	f := newBootOutcomeFake(t, "dec-gone", "/w/proj/.decompose-output-g.json", nil, true)
	c := f.m.Config()
	c.PlanningRestartPolicy = "restart"
	f.m.SetConfig(c)

	got := f.run(t)

	if len(f.starts) != 0 {
		t.Fatalf("a second restart must not start planning, got %v", f.starts)
	}
	if got.Status != PRDDraft {
		t.Fatalf("status = %s, want %s when the restart was already used", got.Status, PRDDraft)
	}
	if d := f.onlyDecision(t); d.Outcome != "planning_reset_to_draft" || !strings.Contains(d.Reason, "already restarted once") {
		t.Fatalf("decision = %+v", d)
	}
}

func TestBootResume_PlanningRestartOnceCannotLoopAcrossBoots(t *testing.T) {
	f := newBootOutcomeFake(t, "dec-gone", "/w/proj/.decompose-output-h.json", nil, false)
	c := f.m.Config()
	c.PlanningRestartPolicy = "restart"
	f.m.SetConfig(c)

	f.run(t)
	if len(f.starts) != 1 {
		t.Fatalf("first boot: starts = %v, want one restart", f.starts)
	}

	// Simulate the restarted planner dying too: the Automaton is planning
	// again with a new recorded session and no output.
	f.m.Store().MutatePRD(f.prdID, func(p *PRD) bool {
		p.Status = PRDPlanning
		p.PlannerSessionID = "dec-second"
		p.PlannerOutputFile = "/w/proj/.decompose-output-h2.json"
		return true
	})
	got := f.run(t)

	if len(f.starts) != 1 {
		t.Fatalf("second boot restarted planning again: starts = %v", f.starts)
	}
	if got.Status != PRDDraft {
		t.Fatalf("second boot status = %s, want %s", got.Status, PRDDraft)
	}
}
