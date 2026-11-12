package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/gitguard"
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
	// Scope is this automaton's gate scope (a GateScope* value); empty
	// inherits the global value, then DefaultGateScope.
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
	// SmokePackages are the package patterns run as the smoke set under
	// the touched-plus-smoke scope; empty is unset.
	SmokePackages []string `yaml:"smoke_packages,omitempty" json:"smoke_packages,omitempty"`
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
	// NewFailureNames lists the tests failing now that were not failing in
	// the baseline.
	NewFailureNames []string `json:"new_failure_names,omitempty"`
	// Invalid is true when a run was unusable for comparison (could not
	// start, timed out, or failed without naming a test); an invalid gate is
	// never a regression.
	Invalid       bool   `json:"invalid"`
	InvalidReason string `json:"invalid_reason,omitempty"`

	// ScopePlan carries the scoping decision for this gate run when the
	// gate ran under a non-full scope.
	ScopePlan *GateScopePlan `json:"scope_plan,omitempty"`
}

// RunTests executes command through `sh -c` in projectDir and parses the
// output into pass/fail counts and failing test names. timeout is in
// seconds; <= 0 means no limit. A run that could not be started or was
// killed by the deadline has ExitCode -1.
//
// projectDir must lie under one of roots (gitguard.ResolveProjectDir): the
// directory comes from a PRD record, so it is checked before anything runs in
// it, and a directory outside every root is reported as an unusable run.
func RunTests(command, projectDir string, timeout int, roots []string) TestResult {
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
	cleanDir, derr := containedProjectDir(projectDir, roots)
	if derr != nil {
		return unusable("project directory refused: " + derr.Error())
	}
	projectDir = cleanDir
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

// containedProjectDir applies the allowed-roots check (gitguard.
// ResolveContainedProjectDir) and returns the directory checked under the root
// that holds it. The prefix test on the returned value is repeated here, behind
// an early return, so the one path handed to os.Stat and the command's
// working directory is the one that was checked. A directory that is neither a
// root nor under one is refused and nothing is returned.
func containedProjectDir(dir string, roots []string) (string, error) {
	resolved, err := gitguard.ResolveContainedProjectDir(dir, roots)
	if err != nil {
		return "", err
	}
	sep := string(filepath.Separator)
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		contained, cerr := gitguard.ContainedPath(root, resolved)
		if cerr != nil {
			continue
		}
		rootAbs, aerr := filepath.Abs(root)
		if aerr != nil {
			continue
		}
		rootPrefix := strings.TrimSuffix(rootAbs, sep) + sep
		checked := strings.TrimSuffix(contained, sep) + sep
		if !strings.HasPrefix(checked, rootPrefix) {
			continue
		}
		return filepath.Clean(checked), nil
	}
	return "", fmt.Errorf("%w: %q", gitguard.ErrOutsideAllowedRoots, dir)
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

// invalidGate returns result marked invalid (never a regression) with the
// given reason; summary is the text after "gate invalid: ".
func invalidGate(result QualityGateResult, reason, summary string) QualityGateResult {
	result.Invalid = true
	result.InvalidReason = reason
	result.Regression = false
	result.Summary = "gate invalid: " + summary
	return result
}

// CompareResults compares baseline and current test results by failing test
// name. A name failing now that is not failing in the baseline is a
// regression even when the failure counts are equal. A baseline or current
// run that is not usable makes the gate invalid, never a regression.
func CompareResults(baseline, current TestResult) QualityGateResult {
	result := QualityGateResult{Baseline: baseline, Current: current}

	if valid, reason := ClassifyBaseline(baseline); !valid {
		return invalidGate(result, reason, reason)
	}
	if valid, reason := ClassifyBaseline(current); !valid {
		return invalidGate(result, reason, "current run could not be compared - "+reason)
	}

	baseSet := make(map[string]bool, len(baseline.FailedTests))
	for _, n := range baseline.FailedTests {
		baseSet[n] = true
	}
	curSet := make(map[string]bool, len(current.FailedTests))
	for _, n := range current.FailedTests {
		curSet[n] = true
	}

	var newNames, fixedNames []string
	for _, n := range current.FailedTests {
		if !baseSet[n] {
			newNames = append(newNames, n)
		}
	}
	for _, n := range baseline.FailedTests {
		if !curSet[n] {
			fixedNames = append(fixedNames, n)
		}
	}

	result.NewFailureNames = newNames
	result.NewFailures = len(newNames)

	switch {
	case len(newNames) > 0:
		result.Regression = true
		result.Summary = fmt.Sprintf("REGRESSION: %d new failing test(s) not in baseline: %s (failures %d → %d)",
			len(newNames), strings.Join(newNames, ", "), len(baseline.FailedTests), len(current.FailedTests))
	case len(fixedNames) > 0:
		result.Improved = true
		result.Summary = fmt.Sprintf("IMPROVED: %d previously failing test(s) now pass: %s (failures %d → %d)",
			len(fixedNames), strings.Join(fixedNames, ", "), len(baseline.FailedTests), len(current.FailedTests))
	default:
		result.Summary = fmt.Sprintf("STABLE: %d pass, %d fail (unchanged)", current.PassCount, current.FailCount)
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

// isPlainGoTest reports whether command is exactly one `go test` invocation:
// no shell operators (&&, ||, ;, |, redirects, &, command substitution,
// backticks, newlines) and nothing before the go binary.
func isPlainGoTest(command string) bool {
	if strings.ContainsAny(command, "&|;<>`\n") || strings.Contains(command, "$(") {
		return false
	}
	f := strings.Fields(command)
	return len(f) >= 2 && f[0] == "go" && f[1] == "test"
}

// commandPackages returns the package arguments already present in a plain
// go test command (non-flag args after "test" that look like package
// patterns).
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

// rerunCandidates re-runs the candidate failures once. When current.Command
// is a plain go test invocation and every candidate is a named go test, only
// those tests (in their packages) are re-run; any other command, and any
// package-level candidate, re-runs the whole command through RunTests.
func rerunCandidates(current TestResult, candidates []string, projectDir string, timeout int, roots []string) TestResult {
	if !isPlainGoTest(current.Command) {
		return RunTests(current.Command, projectDir, timeout, roots)
	}
	want := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		want[c] = true
	}
	tops := map[string]bool{}
	pkgSet := map[string]bool{}
	matched := 0
	for _, f := range parseGoTestFailures(current.Output) {
		if !want[f.String()] {
			continue
		}
		matched++
		if f.test == "" { // package-level failure: nothing to narrow to
			return RunTests(current.Command, projectDir, timeout, roots)
		}
		tops[regexp.QuoteMeta(strings.SplitN(f.test, "/", 2)[0])] = true
		if f.pkg != "" {
			pkgSet[f.pkg] = true
		}
	}
	if matched < len(want) || len(tops) == 0 {
		return RunTests(current.Command, projectDir, timeout, roots)
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
	return RunTests(strings.Join(quoted, " "), projectDir, timeout, roots)
}

// shellQuote wraps s in single quotes for sh, escaping embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// EvaluateGate is the entry point for the quality gate. It compares current
// against baseline with CompareResults; only a name-based regression is
// re-run, once, and only the new failing names are candidates. A candidate
// that fails again is confirmed; a candidate that does not is reported
// STABLE. An unusable re-run makes the gate invalid, never a regression.
func EvaluateGate(baseline, current TestResult, projectDir string, timeout int, roots []string) QualityGateResult {
	result := CompareResults(baseline, current)
	if result.Invalid || !result.Regression {
		return result
	}

	candidates := result.NewFailureNames
	rerun := rerunCandidates(current, candidates, projectDir, timeout, roots)
	if valid, reason := ClassifyBaseline(rerun); !valid {
		return invalidGate(result, "confirming re-run unusable: "+reason,
			"confirming re-run unusable: "+reason)
	}

	failedAgain := make(map[string]bool, len(rerun.FailedTests))
	for _, n := range rerun.FailedTests {
		failedAgain[n] = true
	}
	var confirmed []string
	for _, n := range candidates {
		if failedAgain[n] {
			confirmed = append(confirmed, n)
		}
	}

	if len(confirmed) == 0 {
		result.Regression = false
		result.NewFailures = 0
		result.NewFailureNames = nil
		result.Summary = fmt.Sprintf("STABLE: %d failing test(s) did not reproduce on re-run: %s",
			len(candidates), strings.Join(candidates, ", "))
		return result
	}
	result.Regression = true
	result.NewFailures = len(confirmed)
	result.FailingTests = confirmed
	result.NewFailureNames = confirmed
	result.Summary = "REGRESSION (confirmed on re-run): " + strings.Join(confirmed, ", ")
	return result
}
