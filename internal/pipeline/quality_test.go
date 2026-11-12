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
// testRoots is the allowed project root the tests configure: every test
// directory comes from t.TempDir, which lies under the system temp directory.
var testRoots = []string{os.TempDir()}

func TestRunTests_ShellCompounds(t *testing.T) {
	dir := t.TempDir()
	m1 := filepath.Join(dir, "part-a")
	m2 := filepath.Join(dir, "part-b")

	cmd := "printf a > " + m1 + " && printf b > " + m2
	r := RunTests(cmd, dir, 0, testRoots)
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
	r := RunTests("echo hi", t.TempDir()+"/no-such-dir", 0, testRoots)
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
	r := RunTests("sleep 5", dir, 1, testRoots)
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
	r := RunTests("cat "+path, dir, 0, testRoots)
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

	r := RunTests("go test ./...", dir, 180, testRoots)
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
		nr := RunTests("node --test x.test.js", dir, 60, testRoots)
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
	r := RunTests("exit 2", dir, 0, testRoots)
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
	r := RunTests("echo hello", dir, 0, testRoots)
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
	r := RunTests("cat "+path+"; exit 1", dir, 0, testRoots)
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
	r := RunTests("printf %s "+shellQuote(arg), t.TempDir(), 0, testRoots)
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

// baselineFrom builds a completed run whose failing tests are exactly names.
func baselineFrom(names ...string) TestResult {
	return TestResult{ExitCode: 1, FailedTests: names, FailCount: len(names),
		PassCount: 10, TestsFound: 10 + len(names)}
}

// TestCompare_NewFailingNameIsRegression verifies a new failing test name
// in the current run flags a regression against a valid baseline.
func TestCompare_NewFailingNameIsRegression(t *testing.T) {
	r := CompareResults(baselineFrom("Alpha", "Beta"), baselineFrom("Alpha", "Beta", "Gamma"))
	if !r.Regression {
		t.Fatal("expected regression")
	}
	if r.Invalid {
		t.Fatal("gate must not be invalid")
	}
	if r.NewFailures != 1 {
		t.Errorf("NewFailures=%d want 1", r.NewFailures)
	}
	if !containsName(r.NewFailureNames, "Gamma") {
		t.Errorf("NewFailureNames=%v want Gamma listed", r.NewFailureNames)
	}
}

// TestCompare_SameCountDifferentNamesIsRegression verifies equal failure
// counts with a different name are still a regression naming only the new test.
func TestCompare_SameCountDifferentNamesIsRegression(t *testing.T) {
	r := CompareResults(baselineFrom("Alpha"), baselineFrom("Zeta"))
	if !r.Regression {
		t.Fatal("same failure count with a different name must be a regression")
	}
	if r.NewFailures != 1 {
		t.Errorf("NewFailures=%d want 1", r.NewFailures)
	}
	if !containsName(r.NewFailureNames, "Zeta") {
		t.Errorf("NewFailureNames=%v want Zeta listed", r.NewFailureNames)
	}
	if containsName(r.NewFailureNames, "Alpha") {
		t.Errorf("NewFailureNames=%v must not list a baseline test", r.NewFailureNames)
	}
}

// TestCompare_FewerFailuresIsImproved verifies failures disappearing (and
// none appearing) is an improvement, not a regression.
func TestCompare_FewerFailuresIsImproved(t *testing.T) {
	r := CompareResults(baselineFrom("Alpha", "Beta", "Gamma"), baselineFrom("Alpha"))
	if r.Regression {
		t.Fatal("must not be a regression")
	}
	if !r.Improved {
		t.Fatal("expected improved")
	}
	if r.NewFailures != 0 {
		t.Errorf("NewFailures=%d want 0", r.NewFailures)
	}
}

// TestCompare_IdenticalIsStable verifies identical failing-name sets are
// neither a regression nor an improvement.
func TestCompare_IdenticalIsStable(t *testing.T) {
	r := CompareResults(baselineFrom("Alpha", "Beta"), baselineFrom("Alpha", "Beta"))
	if r.Regression || r.Improved || r.Invalid {
		t.Fatalf("expected stable, got %+v", r)
	}
	if r.NewFailures != 0 {
		t.Errorf("NewFailures=%d want 0", r.NewFailures)
	}
}

// TestCompare_InvalidBaseline verifies an unusable baseline yields
// Invalid=true, Regression=false and a "gate invalid: ..." summary.
func TestCompare_InvalidBaseline(t *testing.T) {
	r := CompareResults(TestResult{ExitCode: 2}, baselineFrom("Alpha"))
	if !r.Invalid {
		t.Fatal("expected invalid gate")
	}
	if r.Regression {
		t.Fatal("must not be a regression")
	}
	if r.InvalidReason == "" {
		t.Error("expected an invalid reason")
	}
	if !strings.HasPrefix(r.Summary, "gate invalid: ") {
		t.Errorf("summary %q must start with %q", r.Summary, "gate invalid: ")
	}
}

// TestCompare_InvalidCurrentRunIsNotRegression verifies a current run that
// itself could not be used is invalid, not a regression.
func TestCompare_InvalidCurrentRunIsNotRegression(t *testing.T) {
	r := CompareResults(baselineFrom("Alpha"), TestResult{ExitCode: -1, TimedOut: true})
	if !r.Invalid {
		t.Fatal("expected invalid gate")
	}
	if r.Regression {
		t.Fatal("a tool failure in the current run must not be a regression")
	}
	if r.InvalidReason == "" {
		t.Error("expected an invalid reason")
	}
	if !strings.HasPrefix(r.Summary, "gate invalid: current run could not be compared - ") {
		t.Errorf("summary %q has the wrong prefix", r.Summary)
	}
}

// TestIsPlainGoTest verifies only a single go test invocation is narrowed.
func TestIsPlainGoTest(t *testing.T) {
	yes := []string{"go test ./...", "go test -count=1 ./internal/pipeline/"}
	no := []string{"go test ./... && node --test a.test.js", "go test ./... | tee x", "go test ./... > out",
		"go test ./...; true", "go test $(echo ./...)", "node --test a.test.js", "FOO=1 go test ./...", "go build ./..."}
	for _, c := range yes {
		if !isPlainGoTest(c) {
			t.Errorf("%q should be a plain go test", c)
		}
	}
	for _, c := range no {
		if isPlainGoTest(c) {
			t.Errorf("%q should not be a plain go test", c)
		}
	}
}

func TestRunTestsRefusesUnsafeProjectDir(t *testing.T) {
	for _, dir := range []string{"/", "/tmp/x\x00y", ""} {
		res := RunTests("true", dir, 5, testRoots)
		if res.ExitCode != -1 {
			t.Errorf("RunTests in %q ran: %+v", dir, res)
		}
	}
	if res := RunTests("true", t.TempDir()+"/./", 5, testRoots); res.ExitCode != 0 {
		t.Errorf("valid dir refused: %+v", res)
	}
}

// refused reports whether RunTests declined to start (ExitCode -1 with the
// guard's reason) instead of running the command.
func refused(res TestResult) bool {
	return res.ExitCode == -1 && strings.Contains(res.Output, "project directory refused")
}

func TestRunTestsEnforcesProjectRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	extra := t.TempDir()
	inside := filepath.Join(root, "proj")
	extraDir := filepath.Join(extra, "proj")
	for _, d := range []string{inside, extraDir, outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	roots := []string{root}

	if res := RunTests("true", inside, 5, roots); res.ExitCode != 0 {
		t.Errorf("path inside the root refused: %+v", res)
	}
	if res := RunTests("true", extraDir, 5, roots); !refused(res) {
		t.Errorf("path in an unconfigured root ran: %+v", res)
	}
	if res := RunTests("true", extraDir, 5, []string{root, extra}); res.ExitCode != 0 {
		t.Errorf("path inside a configured extra root refused: %+v", res)
	}
	if res := RunTests("true", outside, 5, roots); !refused(res) {
		t.Errorf("path outside every root ran: %+v", res)
	}
	if res := RunTests("true", inside, 5, nil); !refused(res) {
		t.Errorf("no configured root must refuse everything: %+v", res)
	}

	// Traversal strings.
	for _, dir := range []string{
		filepath.Join(inside, "..", "..", filepath.Base(outside)),
		inside + "/../../" + filepath.Base(outside),
		root + "/..",
	} {
		if res := RunTests("true", dir, 5, roots); !refused(res) {
			t.Errorf("traversal %q ran: %+v", dir, res)
		}
	}

	// Symlink escape, and a dangling link whose target is outside.
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if res := RunTests("true", escape, 5, roots); !refused(res) {
		t.Errorf("symlink escape ran: %+v", res)
	}
	dangling := filepath.Join(root, "dangling")
	if err := os.Symlink(filepath.Join(outside, "not-there"), dangling); err != nil {
		t.Fatal(err)
	}
	if res := RunTests("true", dangling, 5, roots); !refused(res) {
		t.Errorf("dangling link escape ran: %+v", res)
	}
}
