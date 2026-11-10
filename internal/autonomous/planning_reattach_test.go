package autonomous

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// reattachFixture makes a planning PRD that has a recorded planner session and
// output file, plus the fakes boot-resume talks to: a live-session lookup, a
// kill recorder and a reader-hook recorder.
type reattachFixture struct {
	m        *Manager
	api      *API
	prdID    string
	killed   []string
	hookArgs [][3]string
}

func newReattachFixture(t *testing.T, plannerID, outputFile string, live []string) *reattachFixture {
	t.Helper()
	m, api, _, _ := apiFixture(t)
	prd, _ := m.CreatePRD("s", "/w/proj", "opencode", "", EffortNormal)
	p, _ := m.Store().GetPRD(prd.ID)
	p.Status = PRDPlanning
	p.PlannerSessionID = plannerID
	p.PlannerOutputFile = outputFile
	_ = m.Store().SavePRD(p)

	f := &reattachFixture{m: m, api: api, prdID: prd.ID}
	m.SetDecomposeSessionsFn(func(id string) []string {
		if id != prd.ID {
			return nil
		}
		return live
	})
	m.SetSessionKillerFn(func(id, _, _ string) error { f.killed = append(f.killed, id); return nil })
	m.SetPlanningReattachFn(func(prdID, sessionID, outputFile string) {
		f.hookArgs = append(f.hookArgs, [3]string{prdID, sessionID, outputFile})
	})
	return f
}

func (f *reattachFixture) status(t *testing.T) PRDStatus {
	t.Helper()
	p, ok := f.m.Store().GetPRD(f.prdID)
	if !ok {
		t.Fatalf("prd %s missing", f.prdID)
	}
	return p.Status
}

func TestBootResume_PlanningReattachLiveSessionStaysPlanning(t *testing.T) {
	const outputFile = "/w/proj/.decompose-output-abc.json"
	f := newReattachFixture(t, "dec-live", outputFile, []string{"dec-live"})

	f.api.resumeRunningPRDs()

	if got := f.status(t); got != PRDPlanning {
		t.Fatalf("status = %s, want %s (a live planner must not be reset)", got, PRDPlanning)
	}
	if len(f.hookArgs) != 1 {
		t.Fatalf("reader hook called %d times, want 1", len(f.hookArgs))
	}
	if want := [3]string{f.prdID, "dec-live", outputFile}; f.hookArgs[0] != want {
		t.Fatalf("reader hook args = %v, want %v", f.hookArgs[0], want)
	}
	if len(f.killed) != 0 {
		t.Fatalf("nothing should be killed, got %v", f.killed)
	}
}

func TestPlannerSessionLiveMatchesHostPrefixedIDs(t *testing.T) {
	// The daemon records the short session id on the PRD (session store's
	// GetByShortID form) but reports the host-prefixed full id in its live
	// session list, so the recorded id must match either form.
	if !plannerSessionLive([]string{"restart-matrix-9f2c"}, "9f2c") {
		t.Fatal("a host-prefixed live entry must match the short recorded id")
	}
	if !plannerSessionLive([]string{"9f2c"}, "9f2c") {
		t.Fatal("an exact-match live entry must still match")
	}
	if plannerSessionLive([]string{"restart-matrix-9f2"}, "9f2c") {
		t.Fatal("a different live id must not match as a bare suffix")
	}
	if plannerSessionLive(nil, "9f2c") || plannerSessionLive(nil, "") || plannerSessionLive([]string{"a"}, "") {
		t.Fatal("an empty live list or empty recorded id must not match")
	}
}

func TestBootResume_PlanningReattachLiveHostPrefixedSessionStaysPlanning(t *testing.T) {
	const outputFile = "/w/proj/.decompose-output-abc.json"
	// Recorded planner id "9f2c"; the daemon's live list reports the full
	// host-prefixed id, as sessionEndFromAutonomous/decomposeSessionsFn do.
	f := newReattachFixture(t, "9f2c", outputFile, []string{"restart-matrix-9f2c"})

	f.api.resumeRunningPRDs()

	if got := f.status(t); got != PRDPlanning {
		t.Fatalf("status = %s, want %s (a live planner must not be reset)", got, PRDPlanning)
	}
	if len(f.hookArgs) != 1 || f.hookArgs[0][1] != "9f2c" {
		t.Fatalf("reader hook args = %v, want one call for 9f2c", f.hookArgs)
	}
	if len(f.killed) != 0 {
		t.Fatalf("nothing should be killed, got %v", f.killed)
	}
}

func TestBootResume_PlanningReattachKillsOnlyUnownedPlannerSessions(t *testing.T) {
	f := newReattachFixture(t, "dec-owned", "/w/proj/.decompose-output-x.json", []string{"dec-owned", "dec-orphan"})

	f.api.resumeRunningPRDs()

	if len(f.hookArgs) != 1 || f.hookArgs[0][1] != "dec-owned" {
		t.Fatalf("reader hook args = %v, want one call for dec-owned", f.hookArgs)
	}
	if len(f.killed) != 1 || f.killed[0] != "dec-orphan" {
		t.Fatalf("want only the unowned planner killed, got %v", f.killed)
	}
	if got := f.status(t); got != PRDPlanning {
		t.Fatalf("status = %s, want %s", got, PRDPlanning)
	}
}

func TestBootResume_PlanningReattachDraftPolicyResetsToDraft(t *testing.T) {
	f := newReattachFixture(t, "dec-live", "/w/proj/.decompose-output-x.json", []string{"dec-live"})
	c := f.m.Config()
	c.PlanningRestartPolicy = "draft"
	f.m.SetConfig(c)

	f.api.resumeRunningPRDs()

	if got := f.status(t); got != PRDDraft {
		t.Fatalf("status = %s, want %s under the draft policy", got, PRDDraft)
	}
	if len(f.hookArgs) != 0 {
		t.Fatalf("draft policy must not attach a reader, got %v", f.hookArgs)
	}
	if len(f.killed) != 1 || f.killed[0] != "dec-live" {
		t.Fatalf("draft policy should kill the planner, got %v", f.killed)
	}
}

func TestBootResume_PlanningReattachWithoutReaderResetsToDraft(t *testing.T) {
	f := newReattachFixture(t, "dec-live", "/w/proj/.decompose-output-x.json", []string{"dec-live"})
	f.m.SetPlanningReattachFn(nil)

	f.api.resumeRunningPRDs()

	if got := f.status(t); got != PRDDraft {
		t.Fatalf("status = %s, want %s when no reader is wired", got, PRDDraft)
	}
	if len(f.killed) != 1 {
		t.Fatalf("want the planner killed when no reader is wired, got %v", f.killed)
	}
}

func TestBootResume_PlanningReattachDeadSessionIngestsCompleteOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(out, []byte(validPlanJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newReattachFixture(t, "dec-gone", out, nil)

	f.api.resumeRunningPRDs()

	if len(f.hookArgs) != 0 {
		t.Fatalf("a dead planner must not get a reader, got %v", f.hookArgs)
	}
	if got := f.status(t); got != PRDNeedsReview {
		t.Fatalf("status = %s, want %s after ingesting the complete output", got, PRDNeedsReview)
	}
	p, _ := f.m.Store().GetPRD(f.prdID)
	if p.PlannerSessionID != "" || p.PlannerOutputFile != "" {
		t.Fatalf("planner record must be cleared after ingest, got %q %q", p.PlannerSessionID, p.PlannerOutputFile)
	}
}

func TestPlanningReattach_IngestPlannerOutputCompletesDecompose(t *testing.T) {
	f := newReattachFixture(t, "", "", nil)

	if err := f.m.IngestPlannerOutput(f.prdID, validPlanJSON); err != nil {
		t.Fatalf("IngestPlannerOutput: %v", err)
	}
	if got := f.status(t); got != PRDNeedsReview {
		t.Fatalf("status = %s, want %s", got, PRDNeedsReview)
	}
}

func TestPlanningReattach_IngestPlannerOutputParseFailureGoesToDraft(t *testing.T) {
	f := newReattachFixture(t, "", "", nil)

	if err := f.m.IngestPlannerOutput(f.prdID, "{not json"); err == nil {
		t.Fatal("want a parse error for unparsable output")
	}
	if got := f.status(t); got != PRDDraft {
		t.Fatalf("status = %s, want %s after a parse failure", got, PRDDraft)
	}
}

func TestPlanningReattach_IngestSkipsAutomatonNoLongerPlanning(t *testing.T) {
	f := newReattachFixture(t, "", "", nil)
	p, _ := f.m.Store().GetPRD(f.prdID)
	p.Status = PRDCancelled
	_ = f.m.Store().SavePRD(p)

	if err := f.m.IngestPlannerOutput(f.prdID, validPlanJSON); err != nil {
		t.Fatalf("ingest on a cancelled Automaton should be a no-op, got %v", err)
	}
	if got := f.status(t); got != PRDCancelled {
		t.Fatalf("status = %s, want %s", got, PRDCancelled)
	}
}

func TestPlanningReattach_FailReaderOutcomeGoesToDraft(t *testing.T) {
	f := newReattachFixture(t, "dec-x", "/w/proj/.decompose-output-x.json", nil)

	f.m.FailPlanningReattach(f.prdID, errors.New("no output"))

	if got := f.status(t); got != PRDDraft {
		t.Fatalf("status = %s, want %s after the reader gave up", got, PRDDraft)
	}
	p, _ := f.m.Store().GetPRD(f.prdID)
	if p.PlannerSessionID != "" {
		t.Fatalf("planner record must be cleared, got %q", p.PlannerSessionID)
	}
}

func TestPlanningReattach_SessionInMatchesOnlyListedIDs(t *testing.T) {
	if !sessionIn([]string{"a", "b"}, "b") {
		t.Fatal("b should be found")
	}
	if sessionIn([]string{"a"}, "") || sessionIn(nil, "a") {
		t.Fatal("absent or empty ids must not match")
	}
}

// validPlanJSON is a minimal plan that passes ParsePlanning and the plan checks.
const validPlanJSON = `{"title":"T","stories":[{"title":"S","tasks":[{"title":"Task","spec":"do it"}]}]}`
