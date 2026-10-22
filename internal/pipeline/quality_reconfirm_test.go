package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeTestCmd writes a shell script that mimics go test output. The
// script counts its invocations in a state file next to it and fails on every
// run whose number is in failRuns.
func writeFakeTestCmd(t *testing.T, failRuns ...int) (script, state string) {
	t.Helper()
	dir := t.TempDir()
	script = filepath.Join(dir, "faketest.sh")
	state = filepath.Join(dir, "runs")
	var cases []string
	for _, n := range failRuns {
		cases = append(cases, fmt.Sprintf("%d", n))
	}
	body := fmt.Sprintf(`#!/bin/sh
dir=$(dirname "$0")
n=$(cat "$dir/runs" 2>/dev/null || echo 0)
n=$((n+1))
echo $n > "$dir/runs"
case $n in
%s)
  printf -- '--- FAIL: FakeFlakyTest (0.00s)\nFAIL\nFAIL\texample.com/fake\t0.01s\n'
  exit 1;;
*)
  printf 'ok  \texample.com/fake\t0.01s\n'
  exit 0;;
esac
`, strings.Join(cases, "|"))
	if len(cases) == 0 {
		body = strings.Replace(body, "\n)\n", "\nnever)\n", 1)
	}
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, state
}

func runCount(t *testing.T, state string) string {
	t.Helper()
	b, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func cleanBaseline() TestResult { return TestResult{Command: "baseline", PassCount: 1} }

// Fixture A: fails once, clean on re-run. Before the fix the gate reported
// Regression=true for this flake.
func TestEvaluateGate_FlakeNotReportedAsRegression(t *testing.T) {
	script, state := writeFakeTestCmd(t, 1)
	dir := filepath.Dir(script)

	current := RunTests(script, dir, 30)
	if !CompareResults(cleanBaseline(), current).Regression {
		t.Fatal("precondition: first run must look like a regression")
	}
	got := EvaluateGate(cleanBaseline(), current, dir, 30)
	if got.Regression {
		t.Fatalf("flake reported as regression: %s", got.Summary)
	}
	if got.NewFailures != 0 || len(got.FailingTests) != 0 {
		t.Errorf("NewFailures=%d FailingTests=%v, want none", got.NewFailures, got.FailingTests)
	}
	if !strings.HasPrefix(got.Summary, "STABLE") || !strings.Contains(got.Summary, "FakeFlakyTest") {
		t.Errorf("summary %q should be STABLE and name the flaky test", got.Summary)
	}
	if n := runCount(t, state); n != "2" {
		t.Errorf("script ran %s times, want 2 (initial + one re-run)", n)
	}
}

// Fixture B: always fails, so the regression is confirmed and named.
func TestEvaluateGate_ConfirmedRegressionNamesTests(t *testing.T) {
	script, state := writeFakeTestCmd(t, 1, 2, 3)
	dir := filepath.Dir(script)

	current := RunTests(script, dir, 30)
	got := EvaluateGate(cleanBaseline(), current, dir, 30)
	if !got.Regression {
		t.Fatalf("persistent failure not reported: %s", got.Summary)
	}
	if !strings.Contains(got.Summary, "FakeFlakyTest") || !strings.Contains(got.Summary, "confirmed on re-run") {
		t.Errorf("summary %q should name FakeFlakyTest and say confirmed", got.Summary)
	}
	if got.NewFailures != 1 || len(got.FailingTests) != 1 || !strings.Contains(got.FailingTests[0], "FakeFlakyTest") {
		t.Errorf("NewFailures=%d FailingTests=%v", got.NewFailures, got.FailingTests)
	}
	if n := runCount(t, state); n != "2" {
		t.Errorf("script ran %s times, want 2", n)
	}
}

// Fixture C: clean both ways, so there is no re-run at all.
func TestEvaluateGate_CleanRunDoesNotRerun(t *testing.T) {
	script, state := writeFakeTestCmd(t)
	dir := filepath.Dir(script)

	current := RunTests(script, dir, 30)
	got := EvaluateGate(cleanBaseline(), current, dir, 30)
	if got.Regression || got.NewFailures != 0 {
		t.Fatalf("clean run flagged: %+v", got)
	}
	if !strings.HasPrefix(got.Summary, "STABLE") {
		t.Errorf("summary %q should be STABLE", got.Summary)
	}
	if n := runCount(t, state); n != "1" {
		t.Errorf("script ran %s times, want 1 (no re-run)", n)
	}
}

func TestFailingTests(t *testing.T) {
	out := `=== RUN   TestA
--- FAIL: TestA (0.00s)
    --- FAIL: TestA/sub_case (0.00s)
--- PASS: TestB (0.00s)
FAIL
FAIL	example.com/a	0.01s
ok  	example.com/b	0.01s
FAIL	example.com/c [build failed]
`
	got := FailingTests(out)
	want := []string{"example.com/a.TestA", "example.com/a.TestA/sub_case", "example.com/c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("FailingTests = %v, want %v", got, want)
	}
}
