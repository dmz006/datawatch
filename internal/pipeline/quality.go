package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
)

// defaultTimeoutSec is applied when the quality gate config does not set a
// timeout. A zero or negative value passed directly to RunTests means no
// time limit.
const defaultTimeoutSec = 300

// QualityGateConfig controls test regression detection.
type QualityGateConfig struct {
	Enabled           bool   `yaml:"enabled" json:"enabled"`
	TestCommand       string `yaml:"test_command" json:"test_command"` // ONE shell string run via sh -c; compound commands such as "a && b" are fine
	Timeout           int    `yaml:"timeout" json:"timeout"`           // seconds, 0/unset = default 300
	BlockOnRegression bool   `yaml:"block_on_regression" json:"block_on_regression"`
}

// EffectiveTimeout returns the timeout in seconds the gate should use: the
// configured value when positive, else the default.
func (c QualityGateConfig) EffectiveTimeout() int {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return defaultTimeoutSec
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
	// FailedTests names the failing tests (go: "pkg.Test", node: the plain
	// test name); a go package that failed without naming a test is its
	// import path.
	FailedTests []string `json:"failed_tests,omitempty"`
	// PackageFailures is the subset of FailedTests that are package-level
	// failures (build or setup failure, panic) rather than individual tests.
	PackageFailures []string `json:"package_failures,omitempty"`
	TestsFound      int      `json:"tests_found"`         // individual pass/fail results seen
	TimedOut        bool     `json:"timed_out,omitempty"` // true when killed by the deadline
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

// RunTests executes command through `sh -c` in projectDir and parses the
// output into pass/fail counts and failing test names. timeout is in
// seconds; <= 0 means no limit. A run that could not be started or was
// killed by the deadline has ExitCode -1.
func RunTests(command, projectDir string, timeout int) TestResult {
	start := time.Now()
	result := TestResult{Command: command, RunAt: start}

	unusable := func(reason string) TestResult {
		result.ExitCode = -1
		result.Output = reason
		result.Duration = time.Since(start).Seconds()
		return result
	}

	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return unusable("empty test command")
	}
	if fi, err := os.Stat(projectDir); err != nil || !fi.IsDir() {
		return unusable("project directory does not exist: " + projectDir)
	}
	shPath, err := exec.LookPath("sh")
	if err != nil {
		return unusable("no shell executable 'sh' found on PATH")
	}

	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, shPath, "-c", trimmed) //nolint:gosec
	cmd.Dir = projectDir
	cmd.WaitDelay = 2 * time.Second // do not wait on pipes held open by orphaned children
	out, err := cmd.CombinedOutput()

	result.Output = string(out)
	result.Duration = time.Since(start).Seconds()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && ctx.Err() == nil {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			if ctx.Err() == context.DeadlineExceeded {
				result.TimedOut = true
				result.Output += "\n[timeout] the command was killed after it did not finish in time\n"
			}
		}
	}

	parseTestOutput(&result)
	return result
}

var (
	nodeOKLine   = regexp.MustCompile(`^ok\s+\d+(\s|$)`) // node TAP: "ok 1 - name"
	nodeFailLine = regexp.MustCompile(`^not ok\s+\d+`)   // node TAP: "not ok 1 - name"
)

// parseTestOutput fills PassCount, FailCount, FailedTests, PackageFailures
// and TestsFound from the runner output. Recognized formats:
//
//	go test:     failing names from parseGoTestFailures, passes from
//	             "--- PASS: Name" and "ok  \tpkg"
//	node --test: "ok 1 - name", "not ok 1 - name", "✖ name (...ms)"
func parseTestOutput(r *TestResult) {
	seen := make(map[string]bool)
	addFailed := func(name string) bool {
		if name == "" || seen[name] {
			return false
		}
		seen[name] = true
		r.FailedTests = append(r.FailedTests, name)
		return true
	}

	for _, f := range parseGoTestFailures(r.Output) {
		if !addFailed(f.String()) {
			continue
		}
		if f.test == "" {
			r.PackageFailures = append(r.PackageFailures, f.String())
		} else {
			r.TestsFound++
		}
	}

	for _, raw := range strings.Split(r.Output, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "--- PASS:"),
			nodeOKLine.MatchString(line),
			strings.HasPrefix(line, "ok\t"),
			strings.HasPrefix(line, "ok  "):
			r.PassCount++
			r.TestsFound++

		case nodeFailLine.MatchString(line):
			if addFailed(nodeTestName(line)) {
				r.TestsFound++
			}

		case strings.HasPrefix(line, "✖"):
			name := strings.TrimSpace(strings.TrimPrefix(line, "✖"))
			if i := strings.LastIndex(name, " ("); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			if addFailed(name) {
				r.TestsFound++
			}
		}
	}

	r.FailCount = len(r.FailedTests)
}

// nodeTestName extracts a readable test name from a node --test failure
// line such as "not ok 3 - my test [FAIL]".
func nodeTestName(line string) string {
	name := strings.TrimSpace(line)
	if i := strings.Index(name, " - "); i >= 0 {
		name = strings.TrimSpace(name[i+len(" - "):])
	} else {
		name = strings.TrimSpace(strings.TrimPrefix(name, "not ok"))
		name = strings.TrimLeft(name, "0123456789 \t")
	}
	if i := strings.Index(name, " ["); i >= 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// testLevelFailures counts failing names that are individual tests rather
// than package-level failures.
func (r TestResult) testLevelFailures() int {
	pkg := make(map[string]bool, len(r.PackageFailures))
	for _, n := range r.PackageFailures {
		pkg[n] = true
	}
	n := 0
	for _, name := range r.FailedTests {
		if !pkg[name] {
			n++
		}
	}
	return n
}

// ClassifyBaseline reports whether r is a usable run for regression
// comparison. A run that completed, reported tests and names at least one
// failing test is valid (some failures are normal baseline state); a run
// that could not start, timed out, or failed without naming any individual
// test is not.
func ClassifyBaseline(r TestResult) (valid bool, reason string) {
	if r.ExitCode == -1 {
		if r.TimedOut {
			return false, "the test command timed out and was killed before finishing"
		}
		return false, "the test command could not be started"
	}
	if r.ExitCode != 0 && r.testLevelFailures() == 0 {
		return false, "the test run ended with an error but did not report any failing test (command, build or environment problem)"
	}
	if r.TestsFound == 0 {
		return false, "no tests were found in the test output"
	}
	return true, ""
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
	quoted := make([]string, len(parts))
	for i, p := range parts {
		quoted[i] = shellQuote(p)
	}
	return RunTests(strings.Join(quoted, " "), projectDir, timeout)
}

// shellQuote wraps s in single quotes for sh, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
