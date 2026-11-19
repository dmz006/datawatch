//go:build unix

package mergeshadow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const intermittentJSONFixturePath = "../../testdata/mergeshadow/intermittent-failure/fake-go-test-json.sh"

// intermittentJSONGate runs the JSON fake through the rerun gate. The first run
// fails and the rerun passes, decided by a flag file, with no sleep or load.
func intermittentJSONGate(t *testing.T) GateResult {
	t.Helper()
	abs, err := filepath.Abs(intermittentJSONFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MERGESHADOW_FLAG_FILE", filepath.Join(t.TempDir(), "first-run-flag"))
	dir := makefileWithTestTarget(t, "sh '"+abs+"'")
	return RunGateWithRerun(context.Background(), dir, rerunGateName, 60*time.Second)
}

// A gate that fails on the first run and passes on the rerun is intermittent,
// and an intermittent gate is not a blocking finding.
func TestIntermittentClassificationFirstFailsRerunPasses(t *testing.T) {
	res := intermittentJSONGate(t)
	if res.Status != GateIntermittent {
		t.Fatalf("status = %q, want %q (output %q)", res.Status, GateIntermittent, res.Output)
	}
	gates := toGates([]GateResult{res})
	for _, f := range findingsFor(nil, nil, gates, "t1") {
		if f.Blocking && strings.HasPrefix(f.Code, "gate_") {
			t.Errorf("intermittent gate produced blocking finding %+v", f)
		}
	}
	names, msg := failingTestNames(res.Output)
	if len(names) != 1 || names[0] != "TestSharedCounter" {
		t.Errorf("names = %v, want [TestSharedCounter]", names)
	}
	if !strings.Contains(msg, "expected 1 increment, got 2") {
		t.Errorf("message = %q", msg)
	}
}

func TestIntermittentDefectRecordContentAndStableKey(t *testing.T) {
	res := intermittentJSONGate(t)
	at := time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)
	got := IntermittentDefectsFromGate(res, "a1", "abc123", at)
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1", len(got))
	}
	d := got[0]
	if d.Test != "TestSharedCounter" || d.Commit != "abc123" || d.Date != "2026-11-13" || d.AutomatonID != "a1" || d.Status != StatusIntermittentDefect {
		t.Errorf("record = %+v", d)
	}
	if !strings.Contains(d.Message, "expected 1 increment, got 2") {
		t.Errorf("message = %q", d.Message)
	}
	if d.FirstLines != res.Finding || d.FirstLines == "" {
		t.Errorf("first lines = %q, want the gate finding %q", d.FirstLines, res.Finding)
	}
	if want := IntermittentDefectKey("TestSharedCounter", "abc123"); d.Key != want {
		t.Errorf("key = %q, want %q", d.Key, want)
	}
	// A later date and another Automaton do not change the key.
	later := IntermittentDefectsFromGate(res, "a2", "abc123", at.Add(48*time.Hour))
	if later[0].Key != d.Key {
		t.Error("same test and commit gave a different key")
	}
	other := IntermittentDefectsFromGate(res, "a1", "def456", at)
	if other[0].Key == d.Key {
		t.Error("a different commit gave the same key")
	}
}

func TestIntermittentNoRecordForPassingOrPersistentFailure(t *testing.T) {
	at := time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)
	for _, st := range []GateStatus{GatePass, GateFail, GateTimeout, GateSkipped} {
		if got := IntermittentDefectsFromGate(GateResult{Name: "test", Status: st, Output: "--- FAIL: TestX (0s)"}, "a1", "c", at); len(got) != 0 {
			t.Errorf("status %q made %d records", st, len(got))
		}
	}
	// A persistent failure through the real rerun gate: both runs fail.
	dir := makefileWithTestTarget(t, "echo '--- FAIL: TestHard (0.00s)'; exit 2")
	res := RunGateWithRerun(context.Background(), dir, rerunGateName, 30*time.Second)
	if res.Status != GateFail {
		t.Fatalf("status = %q, want fail", res.Status)
	}
	if got := IntermittentDefectsFromGate(res, "a1", "c", at); len(got) != 0 {
		t.Errorf("persistent failure made %d records", len(got))
	}
}

func TestIntermittentPlainOutputAndUnnamedFallback(t *testing.T) {
	at := time.Date(2026, 11, 13, 9, 30, 0, 0, time.UTC)
	plain := GateResult{Name: "test", Status: GateIntermittent, Output: "--- FAIL: TestPlain (0.01s)\n    plain_test.go:9: boom\n", Finding: "--- FAIL: TestPlain (0.01s)"}
	got := IntermittentDefectsFromGate(plain, "a1", "c1", at)
	if len(got) != 1 || got[0].Test != "TestPlain" {
		t.Fatalf("plain output records = %+v", got)
	}
	unnamed := GateResult{Name: "test", Status: GateIntermittent, Output: "make: *** [test] Error 1\n", Finding: "make: *** [test] Error 1"}
	got = IntermittentDefectsFromGate(unnamed, "a1", "c1", at)
	if len(got) != 1 || got[0].Test != unnamedFailingTest || got[0].Message == "" {
		t.Fatalf("unnamed records = %+v", got)
	}
}

// intermittentGates is a deterministic gate runner: build and lint pass, the
// test gate is intermittent with go test JSON output.
func intermittentGates(status GateStatus) GateFunc {
	return func(_ context.Context, _ string, _ time.Duration) []GateResult {
		out := []GateResult{{Name: "build", Status: GatePass}, {Name: "lint", Status: GatePass}}
		test := GateResult{Name: "test", Status: status}
		if status == GateIntermittent {
			test.Output = `{"Action":"output","Package":"p","Test":"TestFlaky","Output":"    flaky_test.go:7: got 2, want 1\n"}` + "\n" +
				`{"Action":"fail","Package":"p","Test":"TestFlaky"}` + "\n"
			test.Finding = "flaky_test.go:7: got 2, want 1"
			test.Detail = intermittentDetail
		}
		return append(out, test)
	}
}

func TestIntermittentRunnerRecordsOnceAndDoesNotBlock(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "work")
	r := Runner{Gates: intermittentGates(GateIntermittent), Now: fixedClock()}

	plan, err := r.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plan.Findings {
		if f.Blocking && strings.HasPrefix(f.Code, "gate_") {
			t.Errorf("blocking gate finding %+v for an intermittent gate", f)
		}
	}
	if len(plan.IntermittentDefects) != 1 {
		t.Fatalf("created = %d, want 1", len(plan.IntermittentDefects))
	}
	d := plan.IntermittentDefects[0]
	wantCommit := strings.TrimSpace(git(t, repo, "rev-parse", "work"))
	if d.Test != "TestFlaky" || d.Commit != wantCommit || d.Date != "2026-11-09" {
		t.Errorf("record = %+v, want commit %s", d, wantCommit)
	}

	// The same test at the same commit does not file twice.
	plan2, err := r.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan2.IntermittentDefects) != 0 {
		t.Errorf("second run created %d records", len(plan2.IntermittentDefects))
	}
	stored, err := ListIntermittentDefects(in.DataDir)
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored = %v (err %v), want 1", stored, err)
	}
}

func TestIntermittentRunnerPassingGatesWriteNothing(t *testing.T) {
	repo := runnerRepo(t)
	in := runnerInput(t, repo, "work")
	plan, err := Runner{Gates: intermittentGates(GatePass), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.IntermittentDefects) != 0 {
		t.Errorf("created = %d, want 0", len(plan.IntermittentDefects))
	}
	if _, err := os.Stat(filepath.Join(in.DataDir, intermittentDefectDir)); !os.IsNotExist(err) {
		t.Errorf("store directory written for a passing gate: %v", err)
	}
	failPlan, err := Runner{Gates: intermittentGates(GateFail), Now: fixedClock()}.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(failPlan.IntermittentDefects) != 0 {
		t.Errorf("persistent failure created %d records", len(failPlan.IntermittentDefects))
	}
}
