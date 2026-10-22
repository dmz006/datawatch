package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestRunTests_ShellCompounds verifies the command is run through a real
// shell so compound commands work end to end: both halves of an
// `&&`-chained command execute, creating their marker files.
func TestRunTests_ShellCompounds(t *testing.T) {
	dir := t.TempDir()
	m1 := filepath.Join(dir, "part-a")
	m2 := filepath.Join(dir, "part-b")

	cmd := "printf a > " + m1 + " && printf b > " + m2
	r := RunTests(cmd, dir, 0)
	if r.ExitCode != 0 {
		t.Fatalf("exit=%d output=%q", r.ExitCode, r.Output)
	}
	if _, err := os.Stat(m1); err != nil {
		t.Errorf("first half did not run: %v", err)
	}
	if _, err := os.Stat(m2); err != nil {
		t.Errorf("second half did not run: %v", err)
	}
}

// TestRunTests_MissingProjectDir verifies a non-existent project dir yields
// an invalid-looking result (exit -1) with a plain reason, not a panic.
func TestRunTests_MissingProjectDir(t *testing.T) {
	r := RunTests("echo hi", t.TempDir()+"/no-such-dir", 0)
	if r.ExitCode != -1 {
		t.Errorf("exit=%d want -1", r.ExitCode)
	}
	if valid, _ := ClassifyBaseline(r); valid {
		t.Error("run from a missing directory must not be a valid baseline")
	}
}

// TestRunTests_Timeout verifies the deadline kills a long command and marks
// the result as timed out.
func TestRunTests_Timeout(t *testing.T) {
	dir := t.TempDir()
	r := RunTests("sleep 5", dir, 1)
	if !r.TimedOut {
		t.Error("expected TimedOut=true")
	}
	if r.ExitCode != -1 {
		t.Errorf("exit=%d want -1 after timeout", r.ExitCode)
	}
	if valid, reason := ClassifyBaseline(r); valid {
		t.Errorf("timed-out run must not be a valid baseline (%q)", reason)
	}
}

// TestRunTests_GoOutputParsing parses real go test output (delivered to the
// shell via a file), including the case where package-level
// "FAIL\tpkg" lines must NOT be treated as failing test names.
func TestRunTests_GoOutputParsing(t *testing.T) {
	dir := t.TempDir()
	var out strings.Builder
	out.WriteString("=== RUN   TestGood\n")
	out.WriteString("--- PASS: TestGood (0.00s)\n")
	out.WriteString("=== RUN   TestBroken\n")
	out.WriteString("    x_test.go:10: boom\n")
	out.WriteString("--- FAIL: TestBroken (0.01s)\n")
	out.WriteString("FAIL\n")
	out.WriteString("FAIL\texample.com/ex\t0.02s\n")
	out.WriteString("FAIL\n")
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	r := RunTests("cat "+path, dir, 0)
	if len(r.FailedTests) != 1 || r.FailedTests[0] != "example.com/ex.TestBroken" {
		t.Errorf("FailedTests=%v want [example.com/ex.TestBroken]", r.FailedTests)
	}
	if r.PassCount != 1 {
		t.Errorf("PassCount=%d want 1", r.PassCount)
	}
	if r.FailCount != 1 {
		t.Errorf("FailCount=%d want 1", r.FailCount)
	}
	if r.TestsFound != 2 {
		t.Errorf("TestsFound=%d want 2", r.TestsFound)
	}
}

// TestRunTests_RealGoModule runs a tiny real go module with one passing and
// one failing test and checks the parsed names and baseline validity.
func TestRunTests_RealGoModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/ex\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := `package ex

import "testing"

func TestGood(t *testing.T) {}

func TestBroken(t *testing.T) { t.Fatal("boom") }
`
	if err := os.WriteFile(filepath.Join(dir, "ex_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	r := RunTests("go test ./...", dir, 180)
	if r.ExitCode == 0 {
		t.Fatal("expected non-zero exit with a failing test")
	}
	if !containsName(r.FailedTests, "example.com/ex.TestBroken") {
		t.Errorf("FailedTests=%v want it to contain example.com/ex.TestBroken (output=%q)", r.FailedTests, r.Output)
	}
	if containsName(r.FailedTests, "example.com/ex.TestGood") {
		t.Errorf("FailedTests must not contain passing test TestGood: %v", r.FailedTests)
	}
	if r.TestsFound == 0 {
		t.Error("expected to find tests")
	}

	// A run with real failing tests is a VALID baseline (some-failing is
	// normal baseline state) — only tool/setup failures are invalid.
	valid, reason := ClassifyBaseline(r)
	if !valid {
		t.Errorf("expected valid baseline, got: %s", reason)
	}

	t.Run("node", func(t *testing.T) {
		if _, err := exec.LookPath("node"); err != nil {
			t.Skip("node not available")
		}
		js := `const test = require('node:test');
test('works', () => {});
test('breaks', () => { throw new Error('boom'); });
`
		if err := os.WriteFile(filepath.Join(dir, "x.test.js"), []byte(js), 0o644); err != nil {
			t.Fatal(err)
		}
		nr := RunTests("node --test x.test.js", dir, 60)
		if nr.ExitCode == 0 {
			t.Fatalf("expected non-zero exit (output=%q)", nr.Output)
		}
		if !containsName(nr.FailedTests, "breaks") || containsName(nr.FailedTests, "works") {
			t.Errorf("FailedTests=%v want [breaks] (output=%q)", nr.FailedTests, nr.Output)
		}
		if valid, reason := ClassifyBaseline(nr); !valid {
			t.Errorf("expected valid baseline, got: %s", reason)
		}
	})
}

// TestClassifyBaseline_ExitNonZeroNoTests verifies a command that exits
// non-zero without any test output (e.g. build failure or
// "no required module") is not a usable baseline.
func TestClassifyBaseline_ExitNonZeroNoTests(t *testing.T) {
	dir := t.TempDir()
	r := RunTests("exit 2", dir, 0)
	if r.ExitCode != 2 {
		t.Fatalf("exit=%d want 2", r.ExitCode)
	}
	valid, reason := ClassifyBaseline(r)
	if valid {
		t.Fatal("expected invalid baseline for exit-2 run with no tests")
	}
	if reason == "" {
		t.Error("expected a plain-language reason")
	}
}

// TestClassifyBaseline_ZeroTestsFound verifies a command that exits 0 but
// reports no tests at all is not a usable baseline.
func TestClassifyBaseline_ZeroTestsFound(t *testing.T) {
	dir := t.TempDir()
	r := RunTests("echo hello", dir, 0)
	valid, reason := ClassifyBaseline(r)
	if valid {
		t.Fatal("expected invalid baseline with zero tests found")
	}
	if reason == "" {
		t.Error("expected a plain-language reason")
	}
}

// TestClassifyBaseline_PackageSetupFailureOnlyIsInvalid verifies that a run
// whose only failure is a package-level setup/build failure is not a usable
// baseline, while the same failure next to a real failing test is.
func TestClassifyBaseline_PackageSetupFailureOnlyIsInvalid(t *testing.T) {
	r := TestResult{ExitCode: 1, Output: "FAIL\texample.com/x [setup failed]\nFAIL\n"}
	parseTestOutput(&r)
	if valid, reason := ClassifyBaseline(r); valid || reason == "" {
		t.Fatalf("setup-failure-only run: valid=%v reason=%q, want invalid with a reason", valid, reason)
	}

	mixed := TestResult{ExitCode: 1, Output: "--- PASS: TestOK (0.00s)\n--- FAIL: TestX (0.00s)\nFAIL\texample.com/x [setup failed]\nFAIL\n"}
	parseTestOutput(&mixed)
	if !containsName(mixed.FailedTests, "example.com/x.TestX") {
		t.Fatalf("FailedTests=%v want example.com/x.TestX", mixed.FailedTests)
	}
	if valid, reason := ClassifyBaseline(mixed); !valid {
		t.Errorf("mixed run should be valid, got: %s", reason)
	}
}

// TestRunTests_NodeOutputParsing verifies node --test TAP and spec failure
// lines are recorded as plain names and de-duplicated.
func TestRunTests_NodeOutputParsing(t *testing.T) {
	dir := t.TempDir()
	out := "ok 1 - fine\nnot ok 2 - broken [FAIL]\n✖ broken (1.2ms)\n"
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	r := RunTests("cat "+path+"; exit 1", dir, 0)
	if len(r.FailedTests) != 1 || r.FailedTests[0] != "broken" {
		t.Errorf("FailedTests=%v want [broken]", r.FailedTests)
	}
	if r.PassCount != 1 || r.TestsFound != 2 {
		t.Errorf("PassCount=%d TestsFound=%d want 1 and 2", r.PassCount, r.TestsFound)
	}
	if valid, reason := ClassifyBaseline(r); !valid {
		t.Errorf("expected valid, got: %s", reason)
	}
}

// TestShellQuote verifies arguments with shell metacharacters survive sh.
func TestShellQuote(t *testing.T) {
	arg := `^(A|B)$ it's`
	r := RunTests("printf %s "+shellQuote(arg), t.TempDir(), 0)
	if r.Output != arg {
		t.Errorf("output=%q want %q", r.Output, arg)
	}
}

// TestEffectiveTimeout verifies the default applies when unset.
func TestEffectiveTimeout(t *testing.T) {
	if got := (QualityGateConfig{}).EffectiveTimeout(); got != defaultTimeoutSec {
		t.Errorf("got %d want %d", got, defaultTimeoutSec)
	}
	if got := (QualityGateConfig{Timeout: 7}).EffectiveTimeout(); got != 7 {
		t.Errorf("got %d want 7", got)
	}
}
