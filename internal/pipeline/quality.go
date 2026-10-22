package pipeline

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// QualityGateConfig controls test regression detection.
type QualityGateConfig struct {
	Enabled           bool   `yaml:"enabled" json:"enabled"`
	TestCommand       string `yaml:"test_command" json:"test_command"` // e.g. "go test ./..."
	Timeout           int    `yaml:"timeout" json:"timeout"`           // seconds, default 300
	BlockOnRegression bool   `yaml:"block_on_regression" json:"block_on_regression"`
}

// TestResult holds the result of running a test command.
type TestResult struct {
	Command   string    `json:"command"`
	ExitCode  int       `json:"exit_code"`
	Output    string    `json:"output"`
	PassCount int       `json:"pass_count"`
	FailCount int       `json:"fail_count"`
	Duration  float64   `json:"duration_sec"`
	RunAt     time.Time `json:"run_at"`
}

// QualityGateResult is the comparison between baseline and current test runs.
type QualityGateResult struct {
	Baseline    TestResult `json:"baseline"`
	Current     TestResult `json:"current"`
	NewFailures int        `json:"new_failures"` // failures not in baseline
	Regression  bool       `json:"regression"`   // true if new failures found
	Improved    bool       `json:"improved"`     // true if fewer failures
	Summary     string     `json:"summary"`
	// FailingTests names the tests that failed again on the confirming
	// re-run; empty unless a regression was confirmed.
	FailingTests []string `json:"failing_tests,omitempty"`
}

// RunTests executes the test command and parses results.
func RunTests(command, projectDir string, timeout int) TestResult {
	_ = timeout // reserved for future context-based cancellation

	return runTestCommand(command, strings.Fields(command), projectDir)
}

// runTestCommand executes parts in projectDir and parses the result; command
// is the label recorded on the TestResult.
func runTestCommand(command string, parts []string, projectDir string) TestResult {
	start := time.Now()
	if len(parts) == 0 {
		return TestResult{Command: command, ExitCode: -1, Output: "empty command"}
	}

	cmd := exec.Command(parts[0], parts[1:]...) //nolint:gosec
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()

	result := TestResult{
		Command:  command,
		Output:   string(out),
		Duration: time.Since(start).Seconds(),
		RunAt:    time.Now(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}

	// Parse pass/fail counts from output (Go test format)
	for _, line := range strings.Split(result.Output, "\n") {
		if strings.Contains(line, "PASS") {
			result.PassCount++
		}
		if strings.Contains(line, "FAIL") {
			result.FailCount++
		}
	}

	return result
}

// CompareResults compares baseline and current test results.
func CompareResults(baseline, current TestResult) QualityGateResult {
	result := QualityGateResult{
		Baseline: baseline,
		Current:  current,
	}

	if current.FailCount > baseline.FailCount {
		result.NewFailures = current.FailCount - baseline.FailCount
		result.Regression = true
		result.Summary = fmt.Sprintf("REGRESSION: %d new test failures (%d → %d)",
			result.NewFailures, baseline.FailCount, current.FailCount)
	} else if current.FailCount < baseline.FailCount {
		result.Improved = true
		result.Summary = fmt.Sprintf("IMPROVED: %d fewer failures (%d → %d)",
			baseline.FailCount-current.FailCount, baseline.FailCount, current.FailCount)
	} else {
		result.Summary = fmt.Sprintf("STABLE: %d pass, %d fail (unchanged)",
			current.PassCount, current.FailCount)
	}

	return result
}

// goTestFailure is one failed test (or failed package with no named test)
// parsed from go test output.
type goTestFailure struct {
	pkg  string // import path from the following "FAIL\t<pkg>" line, if any
	test string // test name as printed, empty for a package-level failure
}

func (f goTestFailure) String() string {
	switch {
	case f.test == "":
		return f.pkg
	case f.pkg == "":
		return f.test
	}
	return f.pkg + "." + f.test
}

var failTestLine = regexp.MustCompile(`^\s*--- FAIL: (\S+)`)

// parseGoTestFailures extracts failures from go test output. go test prints a
// package's "--- FAIL: Name" lines before its "FAIL\t<importpath>" line, so
// tests seen since the previous package line belong to the next one.
func parseGoTestFailures(output string) []goTestFailure {
	var out []goTestFailure
	var pending []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if m := failTestLine.FindStringSubmatch(line); m != nil {
			pending = append(pending, m[1])
			continue
		}
		if !strings.HasPrefix(line, "FAIL\t") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "FAIL\t"))
		if len(fields) == 0 {
			continue
		}
		pkg := fields[0]
		if len(pending) == 0 {
			out = append(out, goTestFailure{pkg: pkg})
			continue
		}
		for _, t := range pending {
			out = append(out, goTestFailure{pkg: pkg, test: t})
		}
		pending = nil
	}
	for _, t := range pending { // output without a package line
		out = append(out, goTestFailure{test: t})
	}
	return out
}

// FailingTests returns the failing tests named in go test output: each
// "--- FAIL: TestName (duration)" line (subtests as printed), qualified with
// the package from the "FAIL\t<importpath>" line that follows it. A package
// that failed without naming a test (build failure, panic) is returned as its
// import path.
func FailingTests(output string) []string {
	var names []string
	for _, f := range parseGoTestFailures(output) {
		names = append(names, f.String())
	}
	return names
}

// isGoTest reports whether command is a "go test" invocation.
func isGoTest(command string) bool {
	f := strings.Fields(command)
	return len(f) >= 2 && f[0] == "go" && f[1] == "test"
}

// commandPackages returns the package arguments already present in a go test
// command (non-flag args after "test" that look like package patterns).
func commandPackages(command string) []string {
	var pkgs []string
	for _, a := range strings.Fields(command)[2:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		if strings.HasPrefix(a, ".") || strings.Contains(a, "/") {
			pkgs = append(pkgs, a)
		}
	}
	return pkgs
}

// rerunFailing re-runs only the given failures when current.Command is a go
// test invocation naming them, otherwise the whole command once.
func rerunFailing(current TestResult, fails []goTestFailure, projectDir string, timeout int) TestResult {
	if !isGoTest(current.Command) {
		return RunTests(current.Command, projectDir, timeout)
	}
	tops := map[string]bool{}
	pkgSet := map[string]bool{}
	for _, f := range fails {
		if f.test != "" {
			tops[regexp.QuoteMeta(strings.SplitN(f.test, "/", 2)[0])] = true
		}
		if f.pkg != "" {
			pkgSet[f.pkg] = true
		}
	}
	if len(tops) == 0 { // package-level failure only: nothing to narrow to
		return RunTests(current.Command, projectDir, timeout)
	}
	var pkgs []string
	for p := range pkgSet {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	if len(pkgs) == 0 {
		pkgs = commandPackages(current.Command)
	}
	if len(pkgs) == 0 {
		pkgs = []string{"./..."}
	}
	var alts []string
	for t := range tops {
		alts = append(alts, t)
	}
	sort.Strings(alts)
	parts := append([]string{"go", "test", "-count=1", "-run", "^(" + strings.Join(alts, "|") + ")$"}, pkgs...)
	return runTestCommand(strings.Join(parts, " "), parts, projectDir)
}

// EvaluateGate is the re-confirming entry point for the quality gate. It
// compares current against baseline like CompareResults, but an apparent
// regression is only reported after the failing tests are re-run once
// (Reconfirm) and still fail. A failure that does not reproduce is reported as STABLE, and
// a confirmed regression names the failing tests.
func EvaluateGate(baseline, current TestResult, projectDir string, timeout int) QualityGateResult {
	result := CompareResults(baseline, current)
	if !result.Regression {
		return result
	}

	first := parseGoTestFailures(current.Output)
	rerun := rerunFailing(current, first, projectDir, timeout)

	var still []string
	if len(first) == 0 {
		// Nothing nameable in the first run (non-go output): confirm by
		// repeating the comparison on the re-run.
		if !CompareResults(baseline, rerun).Regression {
			result.Regression = false
			result.NewFailures = 0
			result.Summary = fmt.Sprintf("STABLE: %d apparent new failure(s) did not reproduce on re-run", current.FailCount-baseline.FailCount)
			return result
		}
		result.Summary = "REGRESSION (confirmed on re-run): " + strings.TrimPrefix(result.Summary, "REGRESSION: ")
		return result
	}

	firstNames := map[string]bool{}
	for _, f := range first {
		firstNames[f.test+"|"+f.pkg] = true
	}
	for _, f := range parseGoTestFailures(rerun.Output) {
		if firstNames[f.test+"|"+f.pkg] {
			still = append(still, f.String())
		}
	}

	if len(still) == 0 {
		result.Regression = false
		result.NewFailures = 0
		result.Summary = fmt.Sprintf("STABLE: %d failing test(s) did not reproduce on re-run: %s",
			len(first), strings.Join(FailingTests(current.Output), ", "))
		return result
	}
	result.Regression = true
	result.NewFailures = len(still)
	result.FailingTests = still
	result.Summary = "REGRESSION (confirmed on re-run): " + strings.Join(still, ", ")
	return result
}
