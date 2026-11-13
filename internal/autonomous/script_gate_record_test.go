//go:build unix

package autonomous

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gateRecordFixture sets a temp run root and a project with one gate script.
func gateRecordFixture(t *testing.T, script string) (projectDir string, check GateCheck, countFile string) {
	t.Helper()
	prev := verificationRunRoot()
	SetVerificationRunRoot(filepath.Join(t.TempDir(), "runs"))
	prevPoll := gateWaitPollInterval
	gateWaitPollInterval = 20 * time.Millisecond
	t.Cleanup(func() { SetVerificationRunRoot(prev); gateWaitPollInterval = prevPoll })
	projectDir = t.TempDir()
	countFile = filepath.Join(projectDir, "runs.count")
	body := "echo run >> " + countFile + "\n" + script
	dir := filepath.Join(projectDir, gateVerifyFolder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gate.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return projectDir, GateCheck{Kind: GateCheckScript, Script: gateVerifyFolder + "/gate.sh"}, countFile
}

func gateRunCount(t *testing.T, countFile string) int {
	t.Helper()
	b, _ := os.ReadFile(countFile)
	return strings.Count(string(b), "run")
}

func TestGateRecordWrittenAndDelivered(t *testing.T) {
	dir, check, count := gateRecordFixture(t, "echo hello\nexit 0\n")
	res := RunScriptGate(context.Background(), check, dir, 20*time.Second)
	if !res.Passed || !strings.Contains(res.Output, "hello") {
		t.Fatalf("want pass with output, got %+v", res)
	}
	rec, err := readGateRecord(gateRunDirFor(verificationRunRoot(), check, dir))
	if err != nil {
		t.Fatalf("record not written: %v", err)
	}
	if rec.PID <= 0 || rec.ExitCode == nil || *rec.ExitCode != 0 || !rec.Delivered || rec.StartedAt.IsZero() || rec.OutputFile == "" || rec.ExitFile == "" {
		t.Fatalf("bad record: %+v", rec)
	}
	// A delivered record is never consumed again: the next call runs afresh.
	RunScriptGate(context.Background(), check, dir, 20*time.Second)
	if n := gateRunCount(t, count); n != 2 {
		t.Fatalf("want 2 runs, got %d", n)
	}
}

func TestGateDurableFailureExitCode(t *testing.T) {
	dir, check, _ := gateRecordFixture(t, "echo boom\nexit 3\n")
	res := RunScriptGate(context.Background(), check, dir, 20*time.Second)
	if res.Passed || res.ExitCode != 3 || !strings.Contains(res.Reason, "code 3") {
		t.Fatalf("got %+v", res)
	}
}

func TestGateDurableTimeoutEndsGroup(t *testing.T) {
	dir, check, _ := gateRecordFixture(t, "sleep 30\n")
	start := time.Now()
	res := RunScriptGate(context.Background(), check, dir, 400*time.Millisecond)
	if !res.TimedOut || res.Passed || !strings.Contains(res.Reason, "timed out") {
		t.Fatalf("got %+v", res)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("timeout took %s", time.Since(start))
	}
	rec, err := readGateRecord(gateRunDirFor(verificationRunRoot(), check, dir))
	if err != nil || !rec.TimedOut || !rec.Delivered {
		t.Fatalf("record %+v err %v", rec, err)
	}
}

func TestGateReattachSurvivesCancelAndDeliversOnce(t *testing.T) {
	dir, check, count := gateRecordFixture(t, "sleep 1\necho finished\nexit 0\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	first := RunScriptGate(ctx, check, dir, 30*time.Second)
	if !first.Interrupted || first.Passed {
		t.Fatalf("want interrupted, got %+v", first)
	}
	rec, err := readGateRecord(gateRunDirFor(verificationRunRoot(), check, dir))
	if err != nil || rec.Delivered || !gateRecordAlive(rec) {
		t.Fatalf("gate must still be running and undelivered: %+v err %v", rec, err)
	}
	// "Restarted daemon": a fresh call re-attaches instead of starting again.
	second := RunScriptGate(context.Background(), check, dir, 30*time.Second)
	if !second.Passed || !strings.Contains(second.Output, "finished") {
		t.Fatalf("re-attach result: %+v", second)
	}
	if n := gateRunCount(t, count); n != 1 {
		t.Fatalf("gate ran %d times, want 1", n)
	}
}

func TestGateReattachDeliversFinishedResultOnce(t *testing.T) {
	dir, check, count := gateRecordFixture(t, "sleep 0.5\necho done\nexit 0\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	_ = RunScriptGate(ctx, check, dir, 30*time.Second)
	// Let the gate finish while nobody is waiting.
	runDir := gateRunDirFor(verificationRunRoot(), check, dir)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(runDir, verifyExitFile)); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	res := RunScriptGate(context.Background(), check, dir, 30*time.Second)
	if !res.Passed || !strings.Contains(res.Output, "done") {
		t.Fatalf("got %+v", res)
	}
	if n := gateRunCount(t, count); n != 1 {
		t.Fatalf("want the finished run delivered, not rerun: %d runs", n)
	}
}

func TestGateRecordGoneProcessWithoutExitIsSignalFailure(t *testing.T) {
	dir, check, _ := gateRecordFixture(t, "exit 0\n")
	root := verificationRunRoot()
	runDir := gateRunDirFor(root, check, dir)
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	rec := gateRunRecord{CheckID: gateCheckID(check), ProjectDir: dir, PID: 999999, StartedAt: time.Now(), OutputFile: filepath.Join(runDir, verifyOutputFile), ExitFile: filepath.Join(runDir, verifyExitFile)}
	if err := writeGateRecord(runDir, &rec); err != nil {
		t.Fatal(err)
	}
	prev := verifyPIDLiveFn
	verifyPIDLiveFn = func(int, time.Time) bool { return false }
	defer func() { verifyPIDLiveFn = prev }()
	res := RunScriptGate(context.Background(), check, dir, 20*time.Second)
	if res.Passed || res.Reason != "gate was killed by a signal" {
		t.Fatalf("a vanished gate must not pass: %+v", res)
	}
}

func TestGateRecordDifferentProjectDoesNotMatch(t *testing.T) {
	dir, check, _ := gateRecordFixture(t, "exit 0\n")
	rec := gateRunRecord{CheckID: gateCheckID(check), ProjectDir: dir}
	if !gateRecordMatches(rec, check, dir) {
		t.Fatal("same check and project must match")
	}
	if gateRecordMatches(rec, check, dir+"-other") {
		t.Fatal("another project must not match")
	}
	if gateRecordMatches(rec, GateCheck{Kind: GateCheckCommand, Command: "true"}, dir) {
		t.Fatal("another check must not match")
	}
	rec.Delivered = true
	if gateRecordMatches(rec, check, dir) {
		t.Fatal("a delivered record must not match")
	}
}

func TestGateDurableNoRootKeepsDirectChild(t *testing.T) {
	dir, check, _ := gateRecordFixture(t, "exit 0\n")
	SetVerificationRunRoot("")
	res := RunScriptGate(context.Background(), check, dir, 20*time.Second)
	if !res.Passed {
		t.Fatalf("got %+v", res)
	}
}

func TestGateReattachBootHoldRetiresGoneRecord(t *testing.T) {
	dir, check, _ := gateRecordFixture(t, "exit 0\n")
	runDir := gateRunDirFor(verificationRunRoot(), check, dir)
	_ = os.MkdirAll(runDir, 0o750)
	rec := gateRunRecord{CheckID: gateCheckID(check), ProjectDir: dir, PID: 999999, StartedAt: time.Now()}
	_ = writeGateRecord(runDir, &rec)
	prev := verifyPIDLiveFn
	verifyPIDLiveFn = func(int, time.Time) bool { return false }
	defer func() { verifyPIDLiveFn = prev }()
	m := &Manager{}
	prd := &PRD{ProjectDir: dir, Story: []Story{{Tasks: []Task{{ID: "g", Kind: TaskKindFinalGate, VerificationScript: check.Script}}}}}
	if m.holdFinalGateForRecord(prd, &prd.Story[0].Tasks[0]) {
		t.Fatal("a gone gate must not be held")
	}
	got, _ := readGateRecord(runDir)
	if !got.Delivered {
		t.Fatal("gone record must be retired so a fresh run starts")
	}
}
