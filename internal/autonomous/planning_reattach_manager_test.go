// Manager-level coverage for the boot-resume pass (API.bootResumePlanning)
// over the planning paths: re-attach a live planner to a fake reader, ingest
// a finished planner's output file, restart planning exactly once across two
// boots, and the per-value behaviour of planning_restart_policy (including an
// unknown value reading as reattach). The Manager is built with apiFixture —
// the same fake store, session liveness, killer and reader wiring as the
// other boot-resume tests — and the pass runs through
// api.bootResumePlanningWith with the manager's real IngestPlannerOutput and
// readPlannerOutput seams.
package autonomous

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// planningMgrResumeFake is the fixture for the boot-resume planning tests in
// this file: a planning PRD with a recorded planner session, plus fakes for
// the live-session lookup, the session killer, the reader hook and the
// boot-resume hooks the pass reaches through.
type planningMgrResumeFake struct {
	m     *Manager
	api   *API
	prdID string
	// live lists the PRD's live decompose (planner) sessions.
	live []string
	// killed records session ids handed to the killer fake.
	killed []string
	// hookCalls counts and hookArgs records the fake reader calls.
	hookCalls int
	hookArgs  [][3]string
	// ingested records the raw output the manager's IngestPlannerOutput saw.
	ingested []string
	// starts records startDecompose calls (fake: no real planner is started).
	starts []string
	// decisions records the pass's decision entries.
	decisions []BootResumeEntry
	// readerStatus is the PRD's status when the fake reader hook ran.
	readerStatus PRDStatus
}

func newPlanningMgrResumeFake(t *testing.T, plannerID, outputFile string, live []string) *planningMgrResumeFake {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	prd, err := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDPlanning
	p.PlannerSessionID = plannerID
	p.PlannerOutputFile = outputFile
	if err := m.Store().SavePRD(p); err != nil {
		t.Fatal(err)
	}

	f := &planningMgrResumeFake{m: m, api: api, prdID: prd.ID, live: live}
	m.SetDecomposeSessionsFn(func(id string) []string {
		if id != prd.ID {
			return nil
		}
		return f.live
	})
	m.SetSessionKillerFn(func(id, _, _ string) error {
		f.killed = append(f.killed, id)
		return nil
	})
	m.SetPlanningReattachFn(nil)
	return f
}

// hooks supplies the pass's seams with the manager's real output reader and
// ingest, and test-side recorders for the starter and the decision log.
func (f *planningMgrResumeFake) hooks() planningResumeHooks {
	return planningResumeHooks{
		readOutput: readPlannerOutput,
		ingest: func(prdID, raw string) error {
			f.ingested = append(f.ingested, raw)
			return f.m.IngestPlannerOutput(prdID, raw)
		},
		startDecompose: func(prdID string) { f.starts = append(f.starts, prdID) },
		recordDecision: func(e BootResumeEntry) { f.decisions = append(f.decisions, e) },
	}
}

// runPRD replays the pass for f.prdID, reading the PRD fresh first, and
// returns the stored PRD afterwards.
func (f *planningMgrResumeFake) run(t *testing.T) *PRD {
	t.Helper()
	p, ok := f.m.Store().GetPRD(f.prdID)
	if !ok {
		t.Fatalf("prd %s missing", f.prdID)
	}
	f.api.bootResumePlanningWith(p, f.hooks())
	got, ok := f.m.Store().GetPRD(f.prdID)
	if !ok {
		t.Fatalf("prd %s missing after the pass", f.prdID)
	}
	return got
}

// wireReader installs the fake reader: it records its call and the PRD's
// status at hook time, then finishes the decompose through the manager's
// IngestPlannerOutput from the recorded output file.
func (f *planningMgrResumeFake) wireReader(t *testing.T) {
	t.Helper()
	f.m.SetPlanningReattachFn(func(prdID, sessionID, outputFile string) {
		f.hookCalls++
		f.hookArgs = append(f.hookArgs, [3]string{prdID, sessionID, outputFile})
		p, ok := f.m.Store().GetPRD(prdID)
		if !ok {
			t.Errorf("fake reader: prd %q missing", prdID)
			return
		}
		f.readerStatus = statusOf(f.m, p)
		if err := f.m.IngestPlannerOutput(prdID, string(readPlannerOutput(p, outputFile))); err != nil {
			t.Errorf("fake reader: IngestPlannerOutput: %v", err)
		}
	})
}

func (f *planningMgrResumeFake) decisionsWith(outcome string) []BootResumeEntry {
	var out []BootResumeEntry
	for _, d := range f.decisions {
		if d.Outcome == outcome {
			out = append(out, d)
		}
	}
	return out
}

// TestPlanningReattachLiveSessionStaysPlanningAndCompletes verifies that with
// the planner session still alive the Automaton stays in planning, the reader
// hook is called exactly once, nothing is killed or saved as draft, and the
// decompose completes through the fake reader.
func TestPlanningReattachLiveSessionStaysPlanningAndCompletes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(out, []byte(validPlanJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newPlanningMgrResumeFake(t, "dec-live", out, []string{"dec-live"})
	f.wireReader(t)

	got := f.run(t)

	if f.hookCalls != 1 {
		t.Fatalf("reader hook called %d times, want 1", f.hookCalls)
	}
	if want := [3]string{f.prdID, "dec-live", out}; f.hookArgs[0] != want {
		t.Fatalf("reader hook args = %v, want %v", f.hookArgs[0], want)
	}
	if f.readerStatus != PRDPlanning {
		t.Fatalf("status when the reader ran = %s, want %s: the Automaton must still be planning then", f.readerStatus, PRDPlanning)
	}
	if len(f.killed) != 0 {
		t.Fatalf("nothing may be killed on the re-attach path, got %v", f.killed)
	}
	if got.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want %s: the decompose must complete through the fake reader", got.Status, PRDNeedsReview)
	}
	if len(got.Story) == 0 {
		t.Fatal("the reader-collected plan must be stored as the decompose result")
	}
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Fatalf("planner record not cleared after the reader finished: %q %q", got.PlannerSessionID, got.PlannerOutputFile)
	}
	if ds := f.decisionsWith("planning_reset_to_draft"); len(ds) != 0 {
		t.Fatalf("no draft reset may be saved on the re-attach path, got %+v", ds)
	}
}

// TestPlanningIngestDeadPlannerCompleteOutput verifies that a planner session
// that has ended with a complete, parsable output file gets its plan ingested
// through the pass, with no new planner and no reader.
func TestPlanningIngestDeadPlannerCompleteOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(out, []byte(validPlanJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newPlanningMgrResumeFake(t, "dec-gone", out, nil)

	got := f.run(t)

	if f.hookCalls != 0 {
		t.Fatalf("a dead planner must not get a reader, got %v", f.hookArgs)
	}
	if len(f.ingested) != 1 || f.ingested[0] != validPlanJSON {
		t.Fatalf("ingested = %q, want the output file content once", f.ingested)
	}
	if len(f.starts) != 0 {
		t.Fatalf("ingest must not start a new planner, got %v", f.starts)
	}
	if got.Status != PRDNeedsReview {
		t.Fatalf("status = %s, want %s after the plan is ingested", got.Status, PRDNeedsReview)
	}
	if len(got.Story) == 0 {
		t.Fatal("stories not attached from the ingested output")
	}
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Fatalf("planner record not cleared after ingest: %q %q", got.PlannerSessionID, got.PlannerOutputFile)
	}
	if len(f.decisionsWith("planning_ingested")) != 1 {
		t.Fatalf("decisions = %+v, want exactly one planning_ingested", f.decisions)
	}
}

// TestPlanningRestartOnceGoneSessionNoOutput verifies that when the planner
// session is gone and left no output, planning restarts exactly once with a
// recorded decision, and a second boot-resume pass does not restart it again.
func TestPlanningRestartOnceGoneSessionNoOutput(t *testing.T) {
	f := newPlanningMgrResumeFake(t, "dec-gone", filepath.Join(t.TempDir(), "missing.json"), nil)

	got := f.run(t)

	if len(f.starts) != 1 || f.starts[0] != f.prdID {
		t.Fatalf("starts = %v, want planning started exactly once", f.starts)
	}
	if !got.PlanningRestartedOnce {
		t.Fatal("PlanningRestartedOnce must be set by the restart")
	}
	if got.PlannerSessionID != "" || got.PlannerOutputFile != "" {
		t.Fatalf("planner record not cleared on restart: %q %q", got.PlannerSessionID, got.PlannerOutputFile)
	}
	if ds := f.decisionsWith("planning_restarted"); len(ds) != 1 {
		t.Fatalf("decisions = %+v, want exactly one planning_restarted", f.decisions)
	}

	// Second boot: the restarted planner died too, so the Automaton is
	// planning again with a fresh recorded session and again no output.
	f.m.Store().MutatePRD(f.prdID, func(p *PRD) bool {
		p.Status = PRDPlanning
		p.PlannerSessionID = "dec-second"
		p.PlannerOutputFile = filepath.Join(t.TempDir(), "missing-again.json")
		return true
	})

	got = f.run(t)

	if len(f.starts) != 1 {
		t.Fatalf("second pass restarted planning again: starts = %v", f.starts)
	}
	if got.Status != PRDDraft {
		t.Fatalf("second pass status = %s, want %s", got.Status, PRDDraft)
	}
	if len(f.decisionsWith("planning_restarted")) != 1 {
		t.Fatalf("a second planning_restarted decision was recorded: %+v", f.decisions)
	}
	if ds := f.decisionsWith("planning_reset_to_draft"); len(ds) != 1 || !strings.Contains(ds[0].Reason, "already used") {
		t.Fatalf("decisions = %+v, want planning_reset_to_draft naming the already-used restart", f.decisions)
	}
}

// TestPlanningRestartPolicyValues exercises each planning_restart_policy value
// through the pass: reattach attaches the reader, restart restarts planning,
// draft resets to draft (killing the unowned planner), and an unknown value
// reads as reattach.
func TestPlanningRestartPolicyValues(t *testing.T) {
	out := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(out, []byte(validPlanJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	newLive := func(t *testing.T) *planningMgrResumeFake {
		t.Helper()
		return newPlanningMgrResumeFake(t, "dec-live", out, []string{"dec-live"})
	}

	t.Run("reattach attaches the reader", func(t *testing.T) {
		f := newLive(t)
		c := f.m.Config()
		c.PlanningRestartPolicy = "reattach"
		f.m.SetConfig(c)
		f.wireReader(t)

		got := f.run(t)

		if f.hookCalls != 1 || f.hookArgs[0][1] != "dec-live" {
			t.Fatalf("reader hook args = %v (calls=%d), want one call for dec-live", f.hookArgs, f.hookCalls)
		}
		if len(f.killed) != 0 || len(f.starts) != 0 {
			t.Fatalf("reattach must not kill or restart: killed=%v starts=%v", f.killed, f.starts)
		}
		if got.Status != PRDNeedsReview {
			t.Fatalf("status = %s, want %s: the reader finishes the decompose", got.Status, PRDNeedsReview)
		}
		if ds := f.decisionsWith("planning_reset_to_draft"); len(ds) != 0 {
			t.Fatalf("reattach must not reset to draft: %+v", ds)
		}
	})

	t.Run("restart restarts planning", func(t *testing.T) {
		f := newLive(t)
		c := f.m.Config()
		c.PlanningRestartPolicy = "restart"
		f.m.SetConfig(c)

		got := f.run(t)

		if f.hookCalls != 0 {
			t.Fatalf("restart must not attach a reader, got %v", f.hookArgs)
		}
		if len(f.killed) != 1 || f.killed[0] != "dec-live" {
			t.Fatalf("killed = %v, want the unowned planner", f.killed)
		}
		if len(f.starts) != 1 || f.starts[0] != f.prdID {
			t.Fatalf("starts = %v, want planning started once", f.starts)
		}
		if !got.PlanningRestartedOnce {
			t.Fatal("PlanningRestartedOnce must be set")
		}
		if ds := f.decisionsWith("planning_restarted"); len(ds) != 1 || !strings.Contains(ds[0].Reason, "policy=restart") {
			t.Fatalf("decisions = %+v, want planning_restarted naming policy=restart", f.decisions)
		}
	})

	t.Run("draft resets and kills the unowned planner", func(t *testing.T) {
		f := newLive(t)
		c := f.m.Config()
		c.PlanningRestartPolicy = "draft"
		f.m.SetConfig(c)

		got := f.run(t)

		if f.hookCalls != 0 || len(f.starts) != 0 {
			t.Fatalf("draft must not attach a reader or restart: hookCalls=%d starts=%v", f.hookCalls, f.starts)
		}
		if len(f.killed) != 1 || f.killed[0] != "dec-live" {
			t.Fatalf("killed = %v, want the unowned planner killed as an orphan", f.killed)
		}
		if got.Status != PRDDraft {
			t.Fatalf("status = %s, want %s", got.Status, PRDDraft)
		}
		if ds := f.decisionsWith("planning_reset_to_draft"); len(ds) != 1 {
			t.Fatalf("decisions = %+v, want exactly one planning_reset_to_draft", f.decisions)
		}
	})

	t.Run("unknown value reads as reattach", func(t *testing.T) {
		f := newLive(t)
		c := f.m.Config()
		c.PlanningRestartPolicy = "totally-bogus"
		f.m.SetConfig(c)
		f.wireReader(t)

		got := f.run(t)

		if f.hookCalls != 1 || f.hookArgs[0][1] != "dec-live" {
			t.Fatalf("an unknown policy must read as reattach and attach the reader: args=%v (calls=%d)", f.hookArgs, f.hookCalls)
		}
		if len(f.killed) != 0 || len(f.starts) != 0 {
			t.Fatalf("unknown policy must not kill or restart: killed=%v starts=%v", f.killed, f.starts)
		}
		if got.Status != PRDNeedsReview {
			t.Fatalf("status = %s, want %s: the reader finishes the decompose", got.Status, PRDNeedsReview)
		}
		if ds := f.decisionsWith("planning_reset_to_draft"); len(ds) != 0 {
			t.Fatalf("unknown policy must not reset to draft: %+v", ds)
		}
	})
}
