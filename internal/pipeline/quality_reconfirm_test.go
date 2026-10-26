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

func cleanBaseline() TestResult {
	return TestResult{Command: "baseline", PassCount: 1, TestsFound: 1}
}

// Fixture A: fails once, clean on re-run. Before the fix the gate reported
// Regression=true for this flake.
func TestEvaluateGate_FlakeNotReportedAsRegression(t *testing.T) {
	script, state := writeFakeTestCmd(t, 1)
	dir := filepath.Dir(script)

	current := RunTests(script, dir, 30, testRoots)
	if !CompareResults(cleanBaseline(), current).Regression {
		t.Fatal("precondition: first run must look like a regression")
	}
	got := EvaluateGate(cleanBaseline(), current, dir, 30, testRoots)
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

	current := RunTests(script, dir, 30, testRoots)
	got := EvaluateGate(cleanBaseline(), current, dir, 30, testRoots)
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

	current := RunTests(script, dir, 30, testRoots)
	got := EvaluateGate(cleanBaseline(), current, dir, 30, testRoots)
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

// writeCountingScript writes an executable shell script that counts its
// invocations in a "runs" file next to it and then runs body, in which $n is
// the 1-based invocation number.
func writeCountingScript(t *testing.T, body string) (script, state string) {
	t.Helper()
	dir := t.TempDir()
	script = filepath.Join(dir, "counted.sh")
	state = filepath.Join(dir, "runs")
	src := "#!/bin/sh\ndir=$(dirname \"$0\")\nn=$(cat \"$dir/runs\" 2>/dev/null || echo 0)\nn=$((n+1))\necho $n > \"$dir/runs\"\n" + body
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	return script, state
}

// A compound command is never narrowed: the confirming re-run executes the
// whole command, and a failure that does not reproduce is not a regression.
func TestEvaluateGate_CompoundCommandRerunsWholeCommand(t *testing.T) {
	script, state := writeFakeTestCmd(t, 1)
	dir := filepath.Dir(script)
	command := script + " && true"

	current := RunTests(command, dir, 30, testRoots)
	if len(current.FailedTests) != 1 {
		t.Fatalf("precondition: want one failing test, got %v", current.FailedTests)
	}
	got := EvaluateGate(cleanBaseline(), current, dir, 30, testRoots)
	if got.Regression || got.Invalid {
		t.Fatalf("flake in a compound command flagged: %+v", got)
	}
	if !strings.HasPrefix(got.Summary, "STABLE") {
		t.Errorf("summary %q should be STABLE", got.Summary)
	}
	if n := runCount(t, state); n != "2" {
		t.Errorf("script ran %s times, want 2 (whole command re-run once)", n)
	}
}

// An unusable baseline or current run means no comparison and no re-run.
func TestEvaluateGate_InvalidRunIsNotRerun(t *testing.T) {
	t.Run("invalid baseline", func(t *testing.T) {
		script, state := writeFakeTestCmd(t, 1, 2)
		dir := filepath.Dir(script)
		current := RunTests(script, dir, 30, testRoots)

		got := EvaluateGate(TestResult{Command: "baseline", ExitCode: 2}, current, dir, 30, testRoots)
		if !got.Invalid || got.Regression || got.InvalidReason == "" {
			t.Fatalf("want invalid, not regression: %+v", got)
		}
		if n := runCount(t, state); n != "1" {
			t.Errorf("script ran %s times, want 1 (no re-run)", n)
		}
	})
	t.Run("invalid current", func(t *testing.T) {
		script, state := writeFakeTestCmd(t, 1, 2)
		dir := filepath.Dir(script)
		current := TestResult{Command: script, ExitCode: -1, TimedOut: true}

		got := EvaluateGate(cleanBaseline(), current, dir, 30, testRoots)
		if !got.Invalid || got.Regression || got.InvalidReason == "" {
			t.Fatalf("want invalid, not regression: %+v", got)
		}
		if _, err := os.Stat(state); err == nil {
			t.Error("script ran, want no re-run for an invalid current run")
		}
	})
}

// A node TAP failure is a regression by name even when the failure count is
// unchanged, and is confirmed by the re-run.
func TestEvaluateGate_NodeFailureNameRegression(t *testing.T) {
	script, state := writeCountingScript(t,
		"printf 'ok 1 - gadget works\\nnot ok 2 - widget renders\\n'\nexit 1\n")
	dir := filepath.Dir(script)
	baseline := TestResult{Command: script, ExitCode: 1, PassCount: 4, FailCount: 1, TestsFound: 5,
		FailedTests: []string{"gadget works"}}

	current := RunTests(script, dir, 30, testRoots)
	if current.FailCount != baseline.FailCount {
		t.Fatalf("precondition: failure count must be unchanged, got %d", current.FailCount)
	}
	got := EvaluateGate(baseline, current, dir, 30, testRoots)
	if !got.Regression {
		t.Fatalf("new failing name not reported: %s", got.Summary)
	}
	if len(got.FailingTests) != 1 || got.FailingTests[0] != "widget renders" {
		t.Errorf("FailingTests=%v want [widget renders]", got.FailingTests)
	}
	if !strings.Contains(got.Summary, "confirmed on re-run") || !strings.Contains(got.Summary, "widget renders") {
		t.Errorf("summary %q should be confirmed and name the test", got.Summary)
	}
	if n := runCount(t, state); n != "2" {
		t.Errorf("script ran %s times, want 2", n)
	}
}

// A re-run that cannot be used (here: it fails without naming a test) is an
// invalid gate, not a regression and not a clean pass.
func TestEvaluateGate_UnusableRerunIsInvalid(t *testing.T) {
	script, state := writeCountingScript(t, `if [ "$n" = 1 ]; then
  printf -- '--- FAIL: FakeFlakyTest (0.00s)\nFAIL\nFAIL\texample.com/fake\t0.01s\n'
  exit 1
fi
echo 'environment broke'
exit 3
`)
	dir := filepath.Dir(script)

	current := RunTests(script, dir, 30, testRoots)
	got := EvaluateGate(cleanBaseline(), current, dir, 30, testRoots)
	if !got.Invalid || got.Regression {
		t.Fatalf("want invalid, not regression: %+v", got)
	}
	if !strings.HasPrefix(got.InvalidReason, "confirming re-run unusable:") {
		t.Errorf("InvalidReason %q should start with %q", got.InvalidReason, "confirming re-run unusable:")
	}
	if n := runCount(t, state); n != "2" {
		t.Errorf("script ran %s times, want 2", n)
	}
}
