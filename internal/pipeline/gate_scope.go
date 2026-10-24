package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/config"
)

// Gate scope values select how much of the test suite a gate task runs.
const (
	// GateScopeFull runs the whole configured test command.
	GateScopeFull = "full"
	// GateScopeTouchedPlusSmoke runs the touched packages with their
	// one-level importers, plus the configured smoke packages.
	GateScopeTouchedPlusSmoke = "touched-plus-smoke"
	// GateScopeTouched runs only the touched packages with their one-level
	// importers.
	GateScopeTouched = "touched"

	// DefaultGateScope applies when neither an automaton value nor a global
	// value is configured. The default stays full until the operator
	// confirms the new scope; flipping it to a narrower value is a one-line
	// change right here.
	DefaultGateScope = GateScopeFull
)

// DefaultGateSmokePackages is the built-in smoke set (the proposed smoke
// set from docs/plans/2026-10-23-quality-gate-scope.md), aliased from
// config.DefaultGateSmokePackages, which owns the single source of truth
// so the config load paths can fill it in without importing pipeline
// (that direction is an import cycle: pipeline -> session -> config).
var DefaultGateSmokePackages = config.DefaultGateSmokePackages

const (
	scopeSourceAutomaton = "automaton"
	scopeSourceGlobal    = "global"
	scopeSourceDefault   = "default"

	// maxChangedFiles caps how many changed file paths a plan records; the
	// total count is always kept.
	maxChangedFiles = 200
)

// ParseGateScope validates s. The empty string is valid and means
// inherit/unset; anything unrecognized is an error.
func ParseGateScope(s string) (string, error) {
	switch s {
	case "", GateScopeFull, GateScopeTouchedPlusSmoke, GateScopeTouched:
		return s, nil
	}
	return "", fmt.Errorf("unknown gate scope %q (valid values: %q, %q, %q)",
		s, GateScopeFull, GateScopeTouchedPlusSmoke, GateScopeTouched)
}

// ResolveGateScope picks the scope by override order: the per-automaton
// (prd) value wins, then the global value, then the built-in default.
// source is "automaton", "global" or "default" and belongs in the result
// record. Non-empty values are passed through as-is, so callers should run
// the result of their choice through ParseGateScope to reject typos.
func ResolveGateScope(prdScope, globalScope string) (scope, source string) {
	switch {
	case prdScope != "":
		return prdScope, scopeSourceAutomaton
	case globalScope != "":
		return globalScope, scopeSourceGlobal
	default:
		return DefaultGateScope, scopeSourceDefault
	}
}

// GateScopeRunner supplies the facts needed to scope a gate run. A fake
// implementing this interface is enough for every unit test.
type GateScopeRunner interface {
	// ListPackages returns the import paths that the package patterns
	// resolve to; a pattern matching nothing yields an empty result, a
	// real failure (missing toolchain and the like) yields an error.
	ListPackages(dir string, patterns ...string) ([]string, error)
	// PackageImports returns the one-level import graph: import path to
	// the union of its non-test, test and external-test imports.
	PackageImports(dir string) (map[string][]string, error)
	// ChangedFiles returns the paths changed since baseSHA, including
	// uncommitted and untracked files.
	ChangedFiles(dir, baseSHA string) ([]string, error)
}

// gateScopeListRunner is the default GateScopeRunner, backed by `go list`
// and `git`. Binaries are resolved by command name from PATH and run with
// TMUX and TMUX_TMPDIR removed from the environment.
type gateScopeListRunner struct{}

// NewGateScopeListRunner returns the default GateScopeRunner.
func NewGateScopeListRunner() GateScopeRunner { return gateScopeListRunner{} }

// scopeToolEnv strips TMUX and TMUX_TMPDIR from the current environment.
func scopeToolEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if ok && (key == "TMUX" || key == "TMUX_TMPDIR") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// runScopeTool resolves binary by name from PATH, runs it in dir with the
// sanitised environment and returns the combined output.
func runScopeTool(dir, binary string, args ...string) (string, error) {
	binPath, err := exec.LookPath(binary)
	if err != nil {
		return "", fmt.Errorf("%s executable not found on PATH", binary)
	}
	cmd := exec.Command(binPath, args...) //nolint:gosec
	cmd.Dir = dir
	cmd.Env = scopeToolEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// goListNoMatch reports whether output is one of the usual "matched no
// packages" style go list failures, which mean "empty result" rather than
// a hard error.
func goListNoMatch(output string) bool {
	out := strings.ToLower(output)
	for _, marker := range []string{
		"matched no packages",
		"no such package",
		"no go files",
		"cannot find main module",
		"outside main module",
	} {
		if strings.Contains(out, marker) {
			return true
		}
	}
	return false
}

func (gateScopeListRunner) ListPackages(dir string, patterns ...string) ([]string, error) {
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	seen := make(map[string]bool)
	var out []string
	for _, pattern := range patterns {
		combined, err := runScopeTool(dir, "go", "list", "-f", "{{.ImportPath}}", pattern)
		if err != nil && !goListNoMatch(combined) {
			return nil, fmt.Errorf("go list %s failed: %s", pattern, strings.TrimSpace(combined))
		}
		for _, line := range strings.Split(combined, "\n") {
			ip := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
			if ip == "" || seen[ip] {
				continue
			}
			seen[ip] = true
			out = append(out, ip)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (gateScopeListRunner) PackageImports(dir string) (map[string][]string, error) {
	const format = `{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}`
	combined, err := runScopeTool(dir, "go", "list", "-f", format, "./...")
	if err != nil && !goListNoMatch(combined) {
		return nil, fmt.Errorf("go list import graph failed: %s", strings.TrimSpace(combined))
	}
	imp := make(map[string][]string)
	for _, line := range strings.Split(combined, "\n") {
		fields := strings.Fields(strings.TrimSuffix(line, "\r"))
		if len(fields) == 0 {
			continue
		}
		importPath := fields[0]
		uniq := make(map[string]bool, len(fields)-1)
		deps := make([]string, 0, len(fields)-1)
		for _, d := range fields[1:] {
			if uniq[d] {
				continue
			}
			uniq[d] = true
			deps = append(deps, d)
		}
		sort.Strings(deps)
		imp[importPath] = deps
	}
	return imp, nil
}

func (gateScopeListRunner) ChangedFiles(dir, baseSHA string) ([]string, error) {
	seen := make(map[string]bool)
	var out []string
	add := func(combined string) {
		for _, line := range strings.Split(combined, "\n") {
			f := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
			if f == "" || seen[f] {
				continue
			}
			seen[f] = true
			out = append(out, f)
		}
	}
	if baseSHA != "" {
		combined, err := runScopeTool(dir, "git", "diff", "--name-only", baseSHA)
		if err != nil {
			return nil, fmt.Errorf("git diff --name-only since the task base commit failed: %s",
				strings.TrimSpace(combined))
		}
		add(combined)
	}
	combined, err := runScopeTool(dir, "git", "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("listing untracked files failed: %s", strings.TrimSpace(combined))
	}
	add(combined)
	sort.Strings(out)
	return out, nil
}

// GateScopeRequest carries everything PlanGateScope needs.
type GateScopeRequest struct {
	Dir           string   // project directory to run in
	BaseSHA       string   // task base commit for detecting changes
	Scope         string   // a GateScope* value; empty inherits DefaultGateScope
	ScopeSource   string   // "automaton", "global" or "default"
	SmokePatterns []string // package patterns for the smoke set
	IsFinal       bool     // final gate task: always runs the full suite
	TestCommand   string   // the configured test command
}

// GateScopeReason explains why one package is part of the scoped run.
type GateScopeReason struct {
	Package string `json:"package,omitempty"`
	Reason  string `json:"reason"`
}

// GateScopePlan is the scoped-run decision for one gate task. A full plan
// (Full=true) means the executor runs the configured command unchanged and
// Packages is empty; a scoped plan names the packages that will run.
type GateScopePlan struct {
	Scope       string `json:"scope,omitempty"`
	ScopeSource string `json:"scope_source,omitempty"`
	Full        bool   `json:"full,omitempty"`
	// FullReason explains a plan that runs the full suite (final task,
	// scope is full, a module file changed, or a runner failure fallback).
	FullReason        string            `json:"full_reason,omitempty"`
	Packages          []string          `json:"packages,omitempty"`
	Reasons           []GateScopeReason `json:"reasons,omitempty"`
	ChangedFiles      []string          `json:"changed_files,omitempty"`
	ChangedFilesTotal int               `json:"changed_files_total,omitempty"`
	Warnings          []string          `json:"warnings,omitempty"`
	FallbackReason    string            `json:"fallback_reason,omitempty"`
	NothingToRun      bool              `json:"nothing_to_run,omitempty"`
	// VetFailures is filled in by the executor from the vet run.
	VetFailures []string `json:"vet_failures,omitempty"`
}

// PlanGateScope decides what a gate task runs. Full scope and final gate
// tasks run everything; otherwise the touched packages (changed packages
// plus their one-level importers) are computed and, for
// touched-plus-smoke, the smoke packages are added. Any runner failure
// falls back to the full suite: a missed breakage is worse than a slow run.
func PlanGateScope(runner GateScopeRunner, req GateScopeRequest) GateScopePlan {
	scope := req.Scope
	if scope == "" {
		scope = DefaultGateScope
	}
	plan := GateScopePlan{Scope: scope, ScopeSource: req.ScopeSource}

	full := func(reason string) GateScopePlan {
		plan.Full = true
		plan.FullReason = reason
		return plan
	}
	fail := func(reason string) GateScopePlan {
		plan.Full = true
		plan.FullReason = reason
		plan.FallbackReason = reason
		return plan
	}

	switch {
	case req.IsFinal:
		return full("final gate task always runs the full suite")
	case scope == GateScopeFull:
		return full("scope is full")
	}

	changed, err := runner.ChangedFiles(req.Dir, req.BaseSHA)
	if err != nil {
		return fail("could not determine the changed files: " + err.Error())
	}
	plan.ChangedFilesTotal = len(changed)
	if len(changed) > maxChangedFiles {
		plan.ChangedFiles = changed[:maxChangedFiles]
	} else {
		plan.ChangedFiles = changed
	}

	// A module file change can affect anything, so it widens to full with
	// the reason recorded (this is a widening, not a failure fallback).
	for _, f := range changed {
		base := f
		if i := strings.LastIndex(f, "/"); i >= 0 {
			base = f[i+1:]
		}
		if base == "go.mod" || base == "go.sum" {
			return full("a module file (" + base + ") changed, so the full suite runs")
		}
	}

	pkgSet := make(map[string]bool)
	reasons := make(map[string]map[string]bool)
	add := func(pkg, reason string) {
		pkgSet[pkg] = true
		if reasons[pkg] == nil {
			reasons[pkg] = make(map[string]bool)
		}
		reasons[pkg][reason] = true
	}

	// Map each changed .go file (including _test.go) to the package of its
	// directory. A changed file whose directory no longer has a package
	// (for example a deleted file) yields no packages and is skipped;
	// files the mapper cannot attribute (docs and other non-Go files) do
	// not widen the scope.
	dirSet := make(map[string]bool)
	for _, f := range changed {
		if !strings.HasSuffix(f, ".go") {
			continue
		}
		d := ""
		if i := strings.LastIndex(f, "/"); i >= 0 {
			d = f[:i]
		}
		dirSet[d] = true
	}
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	changedSet := make(map[string]bool)
	for _, d := range dirs {
		pattern := d
		if d != "" {
			pattern = "./" + d
		}
		pkgs, err := runner.ListPackages(req.Dir, pattern)
		if err != nil {
			return fail("go list failed for directory " + d + ": " + err.Error())
		}
		if len(pkgs) == 0 {
			continue
		}
		for _, p := range pkgs {
			pkgSet[p] = true
			add(p, "changed")
			changedSet[p] = true
		}
	}

	// One-level importers only: every package that directly imports a
	// changed package runs, but their importers do not.
	if len(changedSet) > 0 {
		importers, err := runner.PackageImports(req.Dir)
		if err != nil {
			return fail("could not load the module import graph: " + err.Error())
		}
		for pkg, imports := range importers {
			if changedSet[pkg] {
				continue // already in scope as a changed package
			}
			for _, dep := range imports {
				if changedSet[dep] {
					add(pkg, "imports "+dep)
					break
				}
			}
		}
	}

	if scope == GateScopeTouchedPlusSmoke {
		for _, pattern := range req.SmokePatterns {
			pkgs, err := runner.ListPackages(req.Dir, pattern)
			if err != nil {
				return fail("go list failed for smoke pattern " + pattern + ": " + err.Error())
			}
			if len(pkgs) == 0 {
				plan.Warnings = append(plan.Warnings,
					"smoke pattern "+pattern+" matched no packages")
				continue
			}
			for _, p := range pkgs {
				add(p, "smoke set")
			}
		}
	}

	pkgs := make([]string, 0, len(pkgSet))
	for p := range pkgSet {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	plan.Packages = pkgs

	for pkg, set := range reasons {
		for reason := range set {
			plan.Reasons = append(plan.Reasons, GateScopeReason{Package: pkg, Reason: reason})
		}
	}
	sort.Slice(plan.Reasons, func(i, j int) bool {
		if plan.Reasons[i].Package != plan.Reasons[j].Package {
			return plan.Reasons[i].Package < plan.Reasons[j].Package
		}
		return plan.Reasons[i].Reason < plan.Reasons[j].Reason
	})

	if len(pkgs) == 0 {
		plan.NothingToRun = true
		plan.Warnings = append(plan.Warnings,
			"no changed file maps to a Go package; there is nothing to run for this scope")
	}
	return plan
}

// scopedCommandUnsafe reports whether the shell string has operators beyond
// "&&", which makes rewriting its go test segments unsafe.
func scopedCommandUnsafe(command string) bool {
	rest := strings.ReplaceAll(command, "&&", " ")
	if strings.ContainsAny(rest, "&;|<>`") || strings.Contains(rest, "$(") ||
		strings.ContainsAny(rest, "\n\r") {
		return true
	}
	return false
}

// looksLikePackage reports whether a field is a package argument (a
// relative path such as ./x or an import path) rather than a flag value.
func looksLikePackage(arg string) bool {
	return strings.HasPrefix(arg, ".") || strings.Contains(arg, "/")
}

// ScopedTestCommand rewrites the package arguments of each `go test`
// segment of the configured command with the scoped package list (keeping
// the flags) and leaves non-Go segments untouched. The command is accepted
// only when it can be split on "&&" while carrying no other shell
// operators; anything else returns ok=false with a reason so the caller
// falls back to the full command and records the reason.
func ScopedTestCommand(configured string, pkgs []string) (string, bool, string) {
	if len(pkgs) == 0 {
		return configured, false, "no packages to narrow the test command to"
	}
	if scopedCommandUnsafe(configured) {
		return configured, false,
			"test command is not a plain go test invocation (shell operators make it unsafe to rewrite)"
	}
	quoted := make([]string, len(pkgs))
	for i, p := range pkgs {
		quoted[i] = shellQuote(p)
	}

	rebuilt := make([]string, 0, strings.Count(configured, "&&")+1)
	sawGoTest := false
	for _, seg := range strings.Split(configured, "&&") {
		fields := strings.Fields(seg)
		if len(fields) < 2 || fields[0] != "go" || fields[1] != "test" {
			rebuilt = append(rebuilt, strings.TrimSpace(seg))
			continue
		}
		sawGoTest = true
		kept := []string{"go", "test"}
		for _, f := range fields[2:] {
			if !strings.HasPrefix(f, "-") && looksLikePackage(f) {
				continue // package argument: replaced by the scoped list below
			}
			kept = append(kept, shellQuote(f))
		}
		kept = append(kept, quoted...)
		rebuilt = append(rebuilt, strings.Join(kept, " "))
	}
	if !sawGoTest {
		return configured, false, "test command has no go test segment to narrow"
	}
	return strings.Join(rebuilt, " && "), true, ""
}

// ScopedVetCommand builds the vet command for the scoped packages. The vet
// run happens as its own run (like a RunTests invocation) so vet failures
// stay separable from test failures. With no packages it vets the whole
// module.
func ScopedVetCommand(pkgs []string) string {
	if len(pkgs) == 0 {
		return "go vet ./..."
	}
	quoted := make([]string, len(pkgs))
	for i, p := range pkgs {
		quoted[i] = shellQuote(p)
	}
	return "go vet " + strings.Join(quoted, " ")
}

// RestrictBaseline limits a full-suite baseline to the packages that ran in
// the scoped run, so CompareResults does not report previously failing
// tests of packages that did not run as improvements and does not
// mis-count. A failure name belongs to a package when it equals the
// package import path or is qualified with it (package.test form).
func RestrictBaseline(baseline TestResult, pkgs []string) TestResult {
	inScope := func(name string) bool {
		for _, p := range pkgs {
			if name == p || strings.HasPrefix(name, p+".") {
				return true
			}
		}
		return false
	}
	out := baseline
	keptFailed := make([]string, 0, len(baseline.FailedTests))
	for _, n := range baseline.FailedTests {
		if inScope(n) {
			keptFailed = append(keptFailed, n)
		}
	}
	keptPkg := make([]string, 0, len(baseline.PackageFailures))
	for _, n := range baseline.PackageFailures {
		if inScope(n) {
			keptPkg = append(keptPkg, n)
		}
	}
	passes := baseline.TestsFound - baseline.FailCount
	if passes < 0 {
		passes = 0
	}
	out.FailedTests = keptFailed
	out.PackageFailures = keptPkg
	out.PassCount = passes
	out.FailCount = len(keptFailed)
	out.TestsFound = passes + len(keptFailed)
	if len(keptFailed) == 0 && baseline.ExitCode != 0 {
		// A baseline restricted to packages that all passed is a clean
		// run; keeping the suite-level non-zero exit would make the gate
		// invalid ("error without any named failing test").
		out.ExitCode = 0
	}
	return out
}

// vetPackageName strips the bracketed suffix go attaches to package
// headers, such as "[build failed]" or "[no test files]".
func vetPackageName(header string) string {
	name := strings.TrimSpace(header)
	if i := strings.Index(name, " ["); i > 0 {
		name = strings.TrimSpace(name[:i])
	}
	// "pkg.test" is the test binary of the same package.
	name = strings.TrimSuffix(name, ".test")
	return name
}

// VetFailuresFromOutput parses go vet output into the sorted failing
// package names. A "# <pkg>" header opens a package section; the first
// non-header line in the section (a "vet:" line or a file:line:col
// diagnostic) marks the package as failing. A bare header is not a
// failure.
func VetFailuresFromOutput(output string) []string {
	seen := make(map[string]bool)
	current := ""
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if strings.HasPrefix(line, "# ") {
			current = vetPackageName(line[2:])
			continue
		}
		if line == "" || current == "" {
			continue
		}
		seen[current] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// VetRegressions returns the packages that newly fail vet against the
// baseline, named "vet:<pkg>".
func VetRegressions(baseline, current []string) []string {
	base := make(map[string]bool, len(baseline))
	for _, p := range baseline {
		base[p] = true
	}
	seen := make(map[string]bool)
	var out []string
	for _, p := range current {
		if p == "" || base[p] || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, "vet:"+p)
	}
	sort.Strings(out)
	return out
}
